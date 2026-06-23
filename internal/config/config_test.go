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

func TestEntriesOmitsUnset(t *testing.T) {
	c := Config{Tool: "codex"}
	got := c.Entries()
	if len(got) != 1 || got[0][0] != "tool" || got[0][1] != "codex" {
		t.Fatalf("entries = %v", got)
	}
}
