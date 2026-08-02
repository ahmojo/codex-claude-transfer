package config

import "testing"

func TestSetGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	var c Config
	if err := c.Set("tool", "claude"); err != nil {
		t.Fatalf("set tool: %v", err)
	}
	if err := c.Set("port", "8080"); err != nil {
		t.Fatalf("set port: %v", err)
	}
	if err := c.Set("codex-home", "/x/.codex"); err != nil {
		t.Fatalf("set codex-home: %v", err)
	}
	if err := c.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Tool != "claude" || got.Port != 8080 || got.CodexHome != "/x/.codex" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if v, _ := got.Get("tool"); v != "claude" {
		t.Fatalf("Get tool = %q", v)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	got, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("missing config should not error: %v", err)
	}
	if (got != Config{}) {
		t.Fatalf("missing config should be zero, got %+v", got)
	}
}

func TestSetValidation(t *testing.T) {
	var c Config
	if err := c.Set("tool", "vim"); err == nil {
		t.Fatal("expected error for unknown tool")
	}
	if err := c.Set("port", "70000"); err == nil {
		t.Fatal("expected error for out-of-range port")
	}
	if err := c.Set("nope", "x"); err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestSetEmptyClears(t *testing.T) {
	c := Config{Tool: "codex", Port: 9}
	if err := c.Set("tool", ""); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("port", ""); err != nil {
		t.Fatal(err)
	}
	if c.Tool != "" || c.Port != 0 {
		t.Fatalf("empty value should clear: %+v", c)
	}
}

func TestRepoSyncKeys(t *testing.T) {
	dir := t.TempDir()
	var c Config
	if err := c.Set("repo-sync", RepoSyncEncrypted); err != nil {
		t.Fatalf("set repo-sync: %v", err)
	}
	if err := c.Set("repo-sync-recipient", "age1qqqqqqqqqqqqqqqqqqqqqq"); err != nil {
		t.Fatalf("set recipient: %v", err)
	}
	if err := c.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if v, _ := got.Get("repo-sync"); v != RepoSyncEncrypted {
		t.Fatalf("repo-sync = %q", v)
	}
	if v, _ := got.Get("repo-sync-recipient"); v != "age1qqqqqqqqqqqqqqqqqqqqqq" {
		t.Fatalf("recipient = %q", v)
	}

	// An ssh recipient is fine; a mode typo, a private key, and a multi-line
	// value are not.
	if err := c.Set("repo-sync-recipient", "ssh-ed25519 AAAAC3Nza user@host"); err != nil {
		t.Fatalf("ssh recipient should be accepted: %v", err)
	}
	if err := c.Set("repo-sync", "encryped"); err == nil {
		t.Fatal("expected error for an unknown repo-sync mode")
	}
	if err := c.Set("repo-sync-recipient", "AGE-SECRET-KEY-1QQQQ"); err == nil {
		t.Fatal("a private key must be refused as a recipient")
	}
	if err := c.Set("repo-sync-recipient", "age1abc\nage1def"); err == nil {
		t.Fatal("a multi-line recipient must be refused")
	}
	if err := c.Set("repo-sync-recipient", "not-a-key"); err == nil {
		t.Fatal("expected error for a recipient that is not an age key")
	}
	// Clearing works for both.
	if err := c.Set("repo-sync", ""); err != nil || c.RepoSync != "" {
		t.Fatalf("clear repo-sync: %v (%q)", err, c.RepoSync)
	}
	if err := c.Set("repo-sync-recipient", ""); err != nil || c.RepoSyncRecipient != "" {
		t.Fatalf("clear recipient: %v (%q)", err, c.RepoSyncRecipient)
	}
}

func TestEntriesOmitsUnset(t *testing.T) {
	c := Config{Tool: "codex"}
	got := c.Entries()
	if len(got) != 1 || got[0][0] != "tool" || got[0][1] != "codex" {
		t.Fatalf("entries = %v", got)
	}
}
