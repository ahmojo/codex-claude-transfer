package cli

import (
	"reflect"
	"testing"
)

func TestBuildExportArgs(t *testing.T) {
	cases := []struct {
		name string
		in   exportChoices
		want []string
	}{
		{
			name: "current project default",
			in:   exportChoices{mode: "current"},
			want: []string{"export", "--project", "."},
		},
		{
			name: "all with git and since",
			in:   exportChoices{mode: "all", withGit: true, since: "7d"},
			want: []string{"export", "--all", "--with-git", "--since", "7d"},
		},
		{
			name: "session with passphrase and output",
			in:   exportChoices{mode: "session", sessionID: "abcd", encryptMode: "passphrase", output: "out.codexbundle"},
			want: []string{"export", "--session", "abcd", "--passphrase", "--output", "out.codexbundle"},
		},
		{
			name: "project path with recipient and codex-home",
			in:   exportChoices{mode: "project", projectPath: "/p", encryptMode: "recipient", recipient: "age1xyz", codexHome: "/h"},
			want: []string{"export", "--project", "/p", "--encrypt-to", "age1xyz", "--codex-home", "/h"},
		},
		{
			name: "recipient mode but empty recipient omits flag",
			in:   exportChoices{mode: "all", encryptMode: "recipient"},
			want: []string{"export", "--all"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildExportArgs(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("buildExportArgs() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestBuildImportArgs(t *testing.T) {
	base := importChoices{bundle: "b.codexbundle"}

	if got, want := buildImportArgs(base, true), []string{"import", "b.codexbundle", "--dry-run"}; !reflect.DeepEqual(got, want) {
		t.Errorf("dry-run: got %v, want %v", got, want)
	}
	if got, want := buildImportArgs(base, false), []string{"import", "b.codexbundle"}; !reflect.DeepEqual(got, want) {
		t.Errorf("real: got %v, want %v", got, want)
	}

	full := importChoices{
		bundle:        "b.age",
		replaceBackup: true,
		project:       "/proj",
		mapCWD:        []string{"/old=/new", ""},
		clone:         "/dev/x",
		decryptMode:   "identity",
		identity:      "/key.txt",
		codexHome:     "/h",
	}
	got := buildImportArgs(full, false)
	want := []string{
		"import", "b.age",
		"--replace-with-backup",
		"--project", "/proj",
		"--map-cwd", "/old=/new", // empty mapping is skipped
		"--clone", "/dev/x",
		"--identity", "/key.txt",
		"--codex-home", "/h",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("full import args = %v, want %v", got, want)
	}

	pass := buildImportArgs(importChoices{bundle: "b.age", decryptMode: "passphrase"}, false)
	if want := []string{"import", "b.age", "--passphrase"}; !reflect.DeepEqual(pass, want) {
		t.Errorf("passphrase import args = %v, want %v", pass, want)
	}

	copyArgs := buildImportArgs(importChoices{bundle: "b.codexbundle", importAsCopy: true}, false)
	if want := []string{"import", "b.codexbundle", "--import-as-copy"}; !reflect.DeepEqual(copyArgs, want) {
		t.Errorf("import-as-copy args = %v, want %v", copyArgs, want)
	}
}

func TestBuildInspectArgs(t *testing.T) {
	if got, want := buildInspectArgs("b.codexbundle", "", ""), []string{"inspect", "b.codexbundle"}; !reflect.DeepEqual(got, want) {
		t.Errorf("plain inspect = %v, want %v", got, want)
	}
	if got, want := buildInspectArgs("b.age", "passphrase", ""), []string{"inspect", "b.age", "--passphrase"}; !reflect.DeepEqual(got, want) {
		t.Errorf("passphrase inspect = %v, want %v", got, want)
	}
	if got, want := buildInspectArgs("b.age", "identity", "/k"), []string{"inspect", "b.age", "--identity", "/k"}; !reflect.DeepEqual(got, want) {
		t.Errorf("identity inspect = %v, want %v", got, want)
	}
}

func TestStripQuotes(t *testing.T) {
	cases := map[string]string{
		`"C:\Users\faruk\OneDrive\Desktop\server"`: `C:\Users\faruk\OneDrive\Desktop\server`,
		`'C:\Users\faruk\server'`:                  `C:\Users\faruk\server`,
		`  "C:\with spaces\x"  `:                   `C:\with spaces\x`,
		`C:\no\quotes`:                             `C:\no\quotes`,
		`"unbalanced`:                              `"unbalanced`,
		`""`:                                       ``,
		``:                                         ``,
	}
	for in, want := range cases {
		if got := stripQuotes(in); got != want {
			t.Errorf("stripQuotes(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildExportArgsStripsQuotedProject(t *testing.T) {
	got := buildExportArgs(exportChoices{mode: "project", projectPath: `"C:\Users\faruk\OneDrive\Desktop\server"`})
	want := []string{"export", "--project", `C:\Users\faruk\OneDrive\Desktop\server`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("quoted project not stripped: got %v, want %v", got, want)
	}
}

func TestBuildImportArgsStripsQuotedBundle(t *testing.T) {
	got := buildImportArgs(importChoices{bundle: `"C:\path\b.codexbundle"`}, true)
	want := []string{"import", `C:\path\b.codexbundle`, "--dry-run"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("quoted bundle not stripped: got %v, want %v", got, want)
	}
}

func TestMustBeAbsolutePath(t *testing.T) {
	if err := mustBeAbsolutePath(""); err == nil {
		t.Error("empty path should be rejected")
	}
	if err := mustBeAbsolutePath("relative/dir"); err == nil {
		t.Error("relative path should be rejected")
	}
	// A quoted absolute path should be accepted (quotes stripped first). Use a
	// host-absolute path so this holds on Windows (needs a drive) and Unix alike.
	abs := t.TempDir()
	if err := mustBeAbsolutePath(`"` + abs + `"`); err != nil {
		t.Errorf("quoted absolute path %q should be accepted, got %v", abs, err)
	}
}

func TestFormatArgv(t *testing.T) {
	got := formatArgv([]string{"export", "--project", "/path with space", "--all"})
	want := `export --project "/path with space" --all`
	if got != want {
		t.Errorf("formatArgv = %q, want %q", got, want)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 50); got != "short" {
		t.Errorf("no truncation expected, got %q", got)
	}
	long := "this is a fairly long preview line that should be cut"
	got := truncate(long, 10)
	if r := []rune(got); len(r) != 10 {
		t.Errorf("truncate length = %d, want 10 (%q)", len(r), got)
	}
}
