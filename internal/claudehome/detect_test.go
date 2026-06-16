package claudehome

import (
	"path/filepath"
	"testing"
)

func TestEncodeCWD(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Verified against a live install (see the research doc).
		{`C:\Users\faruk\Documents\Codex_sync`, "C--Users-faruk-Documents-Codex-sync"},
		{`C:\Users\faruk\Desktop\Java documentation`, "C--Users-faruk-Desktop-Java-documentation"},
		{`C:\Users\faruk\AppData\Local\Temp\cs.Enc Test (v2)@home_x-y`,
			"C--Users-faruk-AppData-Local-Temp-cs-Enc-Test--v2--home-x-y"},
		// POSIX root.
		{"/home/user/proj", "-home-user-proj"},
		// Hyphen, digits and case are preserved; underscore is NOT (-> dash).
		{"a_B-9", "a-B-9"},
		// Every non-[A-Za-z0-9-] becomes a single dash.
		{"a/b\\c:d e.f", "a-b-c-d-e-f"},
	}
	for _, c := range cases {
		if got := EncodeCWD(c.in); got != c.want {
			t.Errorf("EncodeCWD(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEncodeCWDNonReversibleButStable(t *testing.T) {
	// Two different real paths can collapse to the same folder name; that is the
	// documented lossy behavior, not a bug.
	a := EncodeCWD(`C:\x\y`)
	b := EncodeCWD(`C:/x/y`)
	if a != b {
		t.Errorf("expected lossy collision, got %q vs %q", a, b)
	}
}

func TestDetectOverride(t *testing.T) {
	dir := t.TempDir()
	home, err := Detect(dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if home.Source != "flag" {
		t.Errorf("source = %q, want flag", home.Source)
	}
	wantProjects := filepath.Join(home.Root, ProjectsSubdir)
	if home.ProjectsDir != wantProjects {
		t.Errorf("ProjectsDir = %q, want %q", home.ProjectsDir, wantProjects)
	}
}

func TestDetectEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_HOME", dir)
	home, err := Detect("")
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if home.Source != "env" {
		t.Errorf("source = %q, want env", home.Source)
	}
}
