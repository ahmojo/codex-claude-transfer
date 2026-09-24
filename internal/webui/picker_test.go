package webui

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestPickPathRequestValidation(t *testing.T) {
	_, ts := testServer(t)
	cases := []struct {
		name, method, token, body string
		want                      int
	}{
		{"token required", "POST", "", "{\"kind\":\"folder\"}", http.StatusUnauthorized},
		{"availability", "GET", testToken, "", http.StatusOK},
		{"invalid JSON", "POST", testToken, "{", http.StatusBadRequest},
		{"unknown kind", "POST", testToken, "{\"kind\":\"invalid\"}", http.StatusBadRequest},
		{"unknown field", "POST", testToken, "{\"kind\":\"folder\",\"unexpected\":1}", http.StatusBadRequest},
		{"too long", "POST", testToken, "{\"kind\":\"folder\",\"initial\":\"" + strings.Repeat("a", 4097) + "\"}", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, data := do(t, ts, tc.method, "/api/pick-path", tc.token, tc.body)
			if res.StatusCode != tc.want {
				t.Errorf("status=%d, want %d: %s", res.StatusCode, tc.want, data)
			}
		})
	}
}

func TestPathFieldsHavePickers(t *testing.T) {
	_, ts := testServer(t)
	res, data := do(t, ts, "GET", "/", "", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("index status=%d", res.StatusCode)
	}
	for _, id := range []string{
		"export-output", "export-recipients-file", "inspect-path", "inspect-identity",
		"import-path", "import-identity", "import-project", "import-clone",
	} {
		if !strings.Contains(string(data), "data-pick-target=\""+id+"\"") {
			t.Errorf("missing picker for %s", id)
		}
		if !strings.Contains(string(data), "id=\""+id+"\" type=\"text\"") {
			t.Errorf("manual input missing for %s", id)
		}
	}
}

// Dialogs start in a sibling CHATS folder only when cct runs from a portable
// TOOLS folder; any other install keeps the default starting folder.
func TestPortableChatsDir(t *testing.T) {
	root := t.TempDir()
	mkdir := func(parts ...string) string {
		dir := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	mkdir("usb", "TOOLS")
	usbChats := mkdir("usb", "CHATS")
	mkdir("lower", "tools")
	lowerChats := mkdir("lower", "CHATS")
	mkdir("installed", "bin")
	mkdir("installed", "CHATS")
	mkdir("bare", "TOOLS")
	mkdir("file", "TOOLS")
	if err := os.WriteFile(filepath.Join(root, "file", "CHATS"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ name, executable, want string }{
		{"portable layout", filepath.Join(root, "usb", "TOOLS", "cct.exe"), usbChats},
		{"TOOLS matched case-insensitively", filepath.Join(root, "lower", "tools", "cct.exe"), lowerChats},
		{"not inside TOOLS", filepath.Join(root, "installed", "bin", "cct.exe"), ""},
		{"no CHATS folder", filepath.Join(root, "bare", "TOOLS", "cct.exe"), ""},
		{"CHATS is a file", filepath.Join(root, "file", "TOOLS", "cct.exe"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := portableChatsDir(tc.executable); got != tc.want {
				t.Errorf("portableChatsDir(%q) = %q, want %q", tc.executable, got, tc.want)
			}
		})
	}
}

func TestEncodePowerShellCommand(t *testing.T) {
	// Non-ASCII and a surrogate pair must survive, and the BOM must not.
	script := "\ufeff$p = 'Пути 😀'\r\n[Console]::WriteLine($p)"
	raw, err := base64.StdEncoding.DecodeString(encodePowerShellCommand(script))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw)%2 != 0 {
		t.Fatalf("encoded command has %d bytes; UTF-16 needs an even count", len(raw))
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	if got, want := string(utf16.Decode(units)), strings.TrimPrefix(script, "\ufeff"); got != want {
		t.Errorf("decoded command = %q, want %q", got, want)
	}
}

// requirePowerShell skips tests that run Windows PowerShell for real.
func requirePowerShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("the native picker runs Windows PowerShell")
	}
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skip("powershell.exe is not available")
	}
}

func TestPowerShellPickerReturnsUnicodePath(t *testing.T) {
	requirePowerShell(t)
	// The script carries non-ASCII text as well, so a broken -EncodedCommand
	// encoding changes the result just like a broken output decoding would.
	script := "[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)\n" +
		"[Console]::WriteLine($env:CCT_PICK_INITIAL + ' ✓')"
	initial := `C:\Users\Тест\日本語\😀 chats.codexbundle`
	path, cancelled, err := runPowerShellPicker(context.Background(), script, "CCT_PICK_INITIAL="+initial)
	if err != nil || cancelled {
		t.Fatalf("path=%q cancelled=%v err=%v", path, cancelled, err)
	}
	if want := initial + " ✓"; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestPowerShellPickerOutcomes(t *testing.T) {
	requirePowerShell(t)
	cases := []struct {
		name, script  string
		wantCancelled bool
		wantErr       string
	}{
		{"cancel exits 2", "exit 2", true, ""},
		{"stderr is reported", "[Console]::Error.WriteLine('picker exploded'); exit 1", false, "picker exploded"},
		{"uncaught exception fails", "$ErrorActionPreference = 'Stop'; throw 'dialog failed'", false, "dialog failed"},
		{"empty output fails", "exit 0", false, "empty path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, cancelled, err := runPowerShellPicker(context.Background(), tc.script)
			if path != "" || cancelled != tc.wantCancelled {
				t.Errorf("path=%q cancelled=%v, want empty path and cancelled=%v", path, cancelled, tc.wantCancelled)
			}
			if tc.wantErr == "" && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Errorf("error = %v, want one mentioning %q", err, tc.wantErr)
			}
		})
	}
}

// The embedded dialog script cannot be clicked through in a test, but it can
// be parsed by the same PowerShell that runs it.
func TestPickerScriptParses(t *testing.T) {
	requirePowerShell(t)
	check := "$errors = $null\n" +
		"[void][System.Management.Automation.Language.Parser]::ParseInput($env:CCT_PICKER_SCRIPT, [ref]$null, [ref]$errors)\n" +
		"if ($errors.Count) { $errors | ForEach-Object { [Console]::Error.WriteLine($_.Message) }; exit 1 }\n" +
		"[Console]::WriteLine('ok')"
	out, _, err := runPowerShellPicker(context.Background(), check, "CCT_PICKER_SCRIPT="+pathPickerScript)
	if err != nil || out != "ok" {
		t.Fatalf("picker script does not parse: out=%q err=%v", out, err)
	}
}
