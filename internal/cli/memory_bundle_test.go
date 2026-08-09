package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A project's auto memory only travels when both sides ask for it, and it lands
// under the project the import mapped the sessions to — not under the folder the
// source machine happened to encode.
func TestExportImportProjectMemoryRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(tmp, "cfg"))
	srcHome := filepath.Join(tmp, "src-claude")
	dstHome := filepath.Join(tmp, "dst-claude")
	srcProj := filepath.Join(tmp, "proj-a")
	dstProj := filepath.Join(tmp, "proj-b")
	if err := os.MkdirAll(dstProj, 0o755); err != nil {
		t.Fatal(err)
	}
	const id = "aaaa1111-2222-3333-4444-555566667777"
	writeClaudeTranscript(t, srcHome, srcProj, id, "hello claude")
	writeProjectMemory(t, srcHome, srcProj, "MEMORY.md", "- [core](core.md)\n")
	writeProjectMemory(t, srcHome, srcProj, "notes/core.md", "the build needs Go 1.23\n")

	bundle := filepath.Join(tmp, "with-memory.codexbundle")
	var out, errOut bytes.Buffer
	if code := Run([]string{
		"export", "--all", "--tool", "claude", "--claude-home", srcHome, "--with-memory", "-o", bundle,
	}, &out, &errOut); code != 0 {
		t.Fatalf("export exit=%d %s", code, errOut.String())
	}

	// Without --with-memory the import writes transcripts only, and says so.
	out.Reset()
	errOut.Reset()
	if code := Run([]string{
		"import", bundle, "--claude-home", dstHome, "--map-cwd", srcProj + "=" + dstProj,
	}, &out, &errOut); code != 0 {
		t.Fatalf("import exit=%d %s", code, errOut.String())
	}
	if _, err := os.Stat(memoryPath(dstHome, dstProj, "MEMORY.md")); !os.IsNotExist(err) {
		t.Fatalf("memory was written without --with-memory (err=%v)", err)
	}
	if !strings.Contains(out.String()+errOut.String(), "--with-memory") {
		t.Errorf("the import did not mention the skipped memory:\n%s\n%s", out.String(), errOut.String())
	}

	// With the flag it lands under the mapped project, nested paths intact.
	out.Reset()
	errOut.Reset()
	if code := Run([]string{
		"import", bundle, "--claude-home", dstHome, "--map-cwd", srcProj + "=" + dstProj, "--with-memory",
	}, &out, &errOut); code != 0 {
		t.Fatalf("import --with-memory exit=%d %s", code, errOut.String())
	}
	for rel, want := range map[string]string{
		"MEMORY.md":     "- [core](core.md)\n",
		"notes/core.md": "the build needs Go 1.23\n",
	} {
		got, err := os.ReadFile(memoryPath(dstHome, dstProj, rel))
		if err != nil {
			t.Fatalf("memory file %s did not arrive: %v", rel, err)
		}
		if string(got) != want {
			t.Errorf("memory file %s = %q, want %q", rel, got, want)
		}
	}
	if _, err := os.Stat(memoryPath(dstHome, srcProj, "MEMORY.md")); !os.IsNotExist(err) {
		t.Errorf("memory landed under the source project folder (err=%v)", err)
	}

	// Re-importing is a no-op, and a locally changed memory file is never
	// overwritten.
	if err := os.WriteFile(memoryPath(dstHome, dstProj, "MEMORY.md"), []byte("changed here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := Run([]string{
		"import", bundle, "--claude-home", dstHome, "--map-cwd", srcProj + "=" + dstProj, "--with-memory", "--merge",
	}, &out, &errOut); code != 0 {
		t.Fatalf("second import exit=%d %s", code, errOut.String())
	}
	if got, _ := os.ReadFile(memoryPath(dstHome, dstProj, "MEMORY.md")); string(got) != "changed here\n" {
		t.Fatalf("a locally changed memory file was overwritten: %q", got)
	}
	if !strings.Contains(out.String()+errOut.String(), "never overwritten") {
		t.Errorf("the conflict was not reported:\n%s\n%s", out.String(), errOut.String())
	}
}

// Exporting without the flag must not put memory in the bundle at all.
func TestExportWithoutMemoryLeavesItOut(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(tmp, "cfg"))
	home := filepath.Join(tmp, "claude")
	proj := filepath.Join(tmp, "proj")
	writeClaudeTranscript(t, home, proj, "bbbb2222-3333-4444-5555-666677778888", "hi")
	writeProjectMemory(t, home, proj, "MEMORY.md", "secretless note\n")

	bundle := filepath.Join(tmp, "plain.codexbundle")
	var out, errOut bytes.Buffer
	if code := Run([]string{
		"export", "--all", "--tool", "claude", "--claude-home", home, "-o", bundle,
	}, &out, &errOut); code != 0 {
		t.Fatalf("export exit=%d %s", code, errOut.String())
	}
	out.Reset()
	if code := Run([]string{"inspect", bundle}, &out, &errOut); code != 0 {
		t.Fatalf("inspect exit=%d %s", code, errOut.String())
	}
	if strings.Contains(out.String(), "memory") {
		t.Fatalf("a plain export mentions memory:\n%s", out.String())
	}
}

// Memory is prose the agent wrote, so it must go through the same pre-egress
// secret gate as a transcript rather than around it.
func TestExportRefusesSecretInProjectMemory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(tmp, "cfg"))
	home := filepath.Join(tmp, "claude")
	proj := filepath.Join(tmp, "proj")
	writeClaudeTranscript(t, home, proj, "cccc3333-4444-5555-6666-777788889999", "hi")
	// A well-known example key, not a real credential, and deliberately in a
	// file with no extension.
	writeProjectMemory(t, home, proj, "credentials", "AKIAIOSFODNN7EXAMPLE is the deploy key\n")

	bundle := filepath.Join(tmp, "leaky.codexbundle")
	var out, errOut bytes.Buffer
	code := Run([]string{
		"export", "--all", "--tool", "claude", "--claude-home", home, "--with-memory", "-o", bundle,
	}, &out, &errOut)
	if code == 0 {
		t.Fatalf("export wrote a bundle containing a secret in memory:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "likely secret") {
		t.Fatalf("stderr does not explain the refusal: %q", errOut.String())
	}
	if _, err := os.Stat(bundle); !os.IsNotExist(err) {
		t.Fatalf("the refused bundle was left on disk (err=%v)", err)
	}
}
