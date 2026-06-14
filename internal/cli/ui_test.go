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
