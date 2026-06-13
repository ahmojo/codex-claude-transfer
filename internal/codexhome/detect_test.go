package codexhome

import (
	"path/filepath"
	"testing"
)

func TestDetectUsesOverrideFlag(t *testing.T) {
	dir := t.TempDir()
	home, err := Detect(dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if home.Source != "flag" {
		t.Errorf("source = %q, want flag", home.Source)
	}
	if home.Root != dir {
		t.Errorf("root = %q, want %q", home.Root, dir)
	}
	if home.SessionsDir != filepath.Join(dir, SessionsSubdir) {
		t.Errorf("sessions dir = %q", home.SessionsDir)
	}
	if home.ArchivedSessionsDir != filepath.Join(dir, ArchivedSessionsSubdir) {
		t.Errorf("archived dir = %q", home.ArchivedSessionsDir)
	}
}

func TestDetectUsesEnvWhenNoFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	home, err := Detect("")
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if home.Source != "env" {
		t.Errorf("source = %q, want env", home.Source)
	}
	if home.Root != dir {
		t.Errorf("root = %q, want %q", home.Root, dir)
	}
}

func TestDetectFlagBeatsEnv(t *testing.T) {
	envDir := t.TempDir()
	flagDir := t.TempDir()
	t.Setenv("CODEX_HOME", envDir)
	home, err := Detect(flagDir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if home.Root != flagDir {
		t.Errorf("flag should win; root = %q", home.Root)
	}
}

func TestExistsHelpers(t *testing.T) {
	dir := t.TempDir()
	home, _ := Detect(dir)
	if !home.RootExists() {
		t.Errorf("root should exist")
	}
	if home.SessionsDirExists() {
		t.Errorf("sessions dir should not exist yet")
	}
}
