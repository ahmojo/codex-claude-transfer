package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestDoctorJSONOutput(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	writeSessionCWD(t, home, "abcd1111-2222-3333-4444-555566667777", "/proj/a")

	var out, errOut bytes.Buffer
	if code := Run([]string{"doctor", "--json", "--codex-home", home}, &out, &errOut); code != 0 {
		t.Fatalf("doctor --json exit = %d, stderr=%s", code, errOut.String())
	}
	var got doctorJSON
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("doctor output is not valid JSON: %v\n%s", err, out.String())
	}
	if got.CodexHome == "" || len(got.Checks) == 0 {
		t.Fatalf("unexpected doctor json: %+v", got)
	}
	// Every check must carry a recognized status string.
	for _, c := range got.Checks {
		switch c.Status {
		case "ok", "warn", "info":
		default:
			t.Errorf("unexpected status %q", c.Status)
		}
	}
}

func TestListJSONOutput(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	writeSessionCWD(t, home, "abcd1111-2222-3333-4444-555566667777", "/proj/a")

	var out, errOut bytes.Buffer
	if code := Run([]string{"list", "--json", "--codex-home", home}, &out, &errOut); code != 0 {
		t.Fatalf("list --json exit = %d, stderr=%s", code, errOut.String())
	}
	var got listJSON
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if got.Files != 1 || got.Valid != 1 || len(got.Sessions) != 1 {
		t.Fatalf("unexpected counts: %+v", got)
	}
	if got.Sessions[0].ThreadID != "abcd1111-2222-3333-4444-555566667777" {
		t.Errorf("thread id = %q", got.Sessions[0].ThreadID)
	}
	if got.Sessions[0].CWD != "/proj/a" {
		t.Errorf("cwd = %q", got.Sessions[0].CWD)
	}
}

func TestImportJSONOutput(t *testing.T) {
	tmp := t.TempDir()
	srcHome := filepath.Join(tmp, "home")
	writeSessionCWD(t, srcHome, "abcd1111-2222-3333-4444-555566667777", "/proj/a")
	bundle := filepath.Join(tmp, "p.codexbundle")

	var out, errOut bytes.Buffer
	if code := Run([]string{"export", "--all", "--codex-home", srcHome, "-o", bundle}, &out, &errOut); code != 0 {
		t.Fatalf("export exit = %d, stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	dstHome := filepath.Join(tmp, "home2")
	if code := Run([]string{"import", bundle, "--json", "--codex-home", dstHome}, &out, &errOut); code != 0 {
		t.Fatalf("import --json exit = %d, stderr=%s", code, errOut.String())
	}
	var got importJSON
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("import output is not valid JSON: %v\n%s", err, out.String())
	}
	if got.Imported != 1 || got.SessionsInBundle != 1 {
		t.Errorf("unexpected import json: %+v", got)
	}
}

func TestInspectJSONOutput(t *testing.T) {
	tmp := t.TempDir()
	srcHome := filepath.Join(tmp, "home")
	writeSessionCWD(t, srcHome, "abcd1111-2222-3333-4444-555566667777", "/proj/a")
	bundle := filepath.Join(tmp, "p.codexbundle")

	var out, errOut bytes.Buffer
	if code := Run([]string{"export", "--all", "--codex-home", srcHome, "-o", bundle}, &out, &errOut); code != 0 {
		t.Fatalf("export exit = %d, stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Run([]string{"inspect", bundle, "--json"}, &out, &errOut); code != 0 {
		t.Fatalf("inspect --json exit = %d, stderr=%s", code, errOut.String())
	}
	var got inspectJSON
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("inspect output is not valid JSON: %v\n%s", err, out.String())
	}
	if len(got.Manifest.Sessions) != 1 {
		t.Errorf("expected 1 session in manifest json, got %d", len(got.Manifest.Sessions))
	}
}
