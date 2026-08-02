package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The skill is instructions for an agent, so its value depends on the document
// actually saying the things the workflow relies on. These assertions are a
// tripwire for edits that quietly drop a safety rule or rename a flag.
func TestDocumentIsAWellFormedSkill(t *testing.T) {
	// .gitattributes keeps the document LF-only, but normalize anyway so the
	// assertions survive a checkout that rewrote the endings regardless.
	doc := strings.ReplaceAll(Document(), "\r\n", "\n")
	if !strings.HasPrefix(doc, "---\n") {
		t.Fatalf("document must start with YAML frontmatter, got:\n%.80s", doc)
	}
	if !strings.Contains(doc, "name: "+Name) {
		t.Errorf("frontmatter does not declare name: %s", Name)
	}
	if !strings.Contains(doc, "\ndescription: ") {
		t.Error("frontmatter has no description, so the agent cannot decide when to use it")
	}
	for _, want := range []string{
		"cct export --project .",   // save
		"--map-cwd-here",           // restore under the clone's path
		"--merge",                  // repeat imports stay incremental
		"cct diff",                 // preview before writing
		"cct undo",                 // way back out
		"--allow-secrets",          // named so the agent knows never to pass it
		"git push",                 // the ask-first rule
		"cct config get repo-sync", // setup check
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("document no longer mentions %q", want)
		}
	}
}

func TestBodyDropsFrontmatter(t *testing.T) {
	body := Body()
	if strings.HasPrefix(body, "---") {
		t.Fatalf("body still starts with frontmatter:\n%.80s", body)
	}
	if strings.Contains(body, "name: "+Name) {
		t.Error("body still contains the frontmatter name field")
	}
	if !strings.HasPrefix(body, "# cct session sync") {
		t.Fatalf("body should start at the first heading, got:\n%.80s", body)
	}
}

func TestInstallWritesThenReportsUnchanged(t *testing.T) {
	root := filepath.Join(t.TempDir(), "claude")

	res, err := Install(root, Options{})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if res.Status != StatusInstalled {
		t.Fatalf("status = %q, want %q", res.Status, StatusInstalled)
	}
	got, err := os.ReadFile(Path(root))
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if string(got) != Document() {
		t.Error("installed file does not match the embedded document")
	}

	// Installing again is a no-op, not a rewrite.
	res, err = Install(root, Options{})
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if res.Status != StatusUnchanged {
		t.Fatalf("second install status = %q, want %q", res.Status, StatusUnchanged)
	}
}

func TestInstallDryRunWritesNothing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "claude")
	res, err := Install(root, Options{DryRun: true})
	if err != nil {
		t.Fatalf("dry-run install: %v", err)
	}
	if res.Status != StatusInstalled || !res.DryRun {
		t.Fatalf("unexpected result %+v", res)
	}
	if _, err := os.Stat(Path(root)); !os.IsNotExist(err) {
		t.Fatalf("dry run created %s (err=%v)", Path(root), err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("dry run created the Claude home")
	}
}

func TestInstallKeepsAModifiedFileUnlessForced(t *testing.T) {
	root := filepath.Join(t.TempDir(), "claude")
	if _, err := Install(root, Options{}); err != nil {
		t.Fatalf("install: %v", err)
	}
	edited := Document() + "\n<!-- my own note -->\n"
	if err := os.WriteFile(Path(root), []byte(edited), 0o644); err != nil {
		t.Fatalf("edit: %v", err)
	}

	if _, err := Install(root, Options{}); err == nil {
		t.Fatal("install replaced an edited skill without --force")
	}
	if got, _ := os.ReadFile(Path(root)); string(got) != edited {
		t.Fatal("the edited file was modified by the refused install")
	}

	res, err := Install(root, Options{Force: true})
	if err != nil {
		t.Fatalf("forced install: %v", err)
	}
	if res.Status != StatusUpdated {
		t.Fatalf("status = %q, want %q", res.Status, StatusUpdated)
	}
	if got, _ := os.ReadFile(Path(root)); string(got) != Document() {
		t.Error("forced install did not write the shipped document")
	}
	if res.Backup == "" {
		t.Fatal("no backup recorded")
	}
	if got, err := os.ReadFile(res.Backup); err != nil || string(got) != edited {
		t.Fatalf("backup does not hold the edited file (err=%v)", err)
	}
}

// A checkout or editor that rewrote the file's line endings must not look like
// a user edit, or every install on such a machine would demand --force.
func TestInstallTreatsCRLFAsUnchanged(t *testing.T) {
	root := filepath.Join(t.TempDir(), "claude")
	if _, err := Install(root, Options{}); err != nil {
		t.Fatalf("install: %v", err)
	}
	crlf := strings.ReplaceAll(Document(), "\n", "\r\n")
	if err := os.WriteFile(Path(root), []byte(crlf), 0o644); err != nil {
		t.Fatalf("rewrite with CRLF: %v", err)
	}
	res, err := Install(root, Options{})
	if err != nil {
		t.Fatalf("install over CRLF copy: %v", err)
	}
	if res.Status != StatusUnchanged {
		t.Fatalf("status = %q, want %q", res.Status, StatusUnchanged)
	}
}

func TestInstallRefusesNonRegularTarget(t *testing.T) {
	root := filepath.Join(t.TempDir(), "claude")
	if err := os.MkdirAll(Path(root), 0o755); err != nil { // a directory where the file goes
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := Install(root, Options{Force: true}); err == nil {
		t.Fatal("install overwrote a non-regular path")
	}
}

func TestInstallNeedsARoot(t *testing.T) {
	if _, err := Install("", Options{}); err == nil {
		t.Fatal("install accepted an empty Claude home")
	}
}
