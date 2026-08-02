package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newRef(t *testing.T, dir string) Reference {
	t.Helper()
	ref, err := NewReference(dir, "git@github.com:me/cct-sessions.git", "", EncryptionPlain, "")
	if err != nil {
		t.Fatalf("new reference: %v", err)
	}
	return ref
}

func TestNewReferenceDerivesProjectAndPath(t *testing.T) {
	ref := newRef(t, filepath.Join(t.TempDir(), "My App_v2"))
	if ref.Project != "my-app-v2" {
		t.Fatalf("project = %q", ref.Project)
	}
	if ref.Path != "projects/my-app-v2" {
		t.Fatalf("path = %q", ref.Path)
	}
	if ref.Version != RefVersion || ref.Note == "" {
		t.Fatalf("unexpected reference %+v", ref)
	}
}

func TestNewReferenceRejectsBadInput(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj")
	// A remote-helper transport can execute a command; it must never reach git.
	if _, err := NewReference(dir, "ext::sh -c 'evil'", "", EncryptionPlain, ""); err == nil {
		t.Error("ext:: remote was accepted")
	}
	if _, err := NewReference(dir, "--upload-pack=evil", "", EncryptionPlain, ""); err == nil {
		t.Error("flag-like remote was accepted")
	}
	if _, err := NewReference(dir, "git@h:r.git", "", EncryptionAge, ""); err == nil {
		t.Error("encrypted mode without a recipient was accepted")
	}
	if _, err := NewReference(dir, "git@h:r.git", "", "rot13", "x"); err == nil {
		t.Error("unknown encryption mode was accepted")
	}
	if _, err := NewReference(dir, "git@h:r.git", "???", EncryptionPlain, ""); err == nil {
		t.Error("a project name with no usable characters was accepted")
	}
}

// A reference arrives from a repository, so Validate is the trust boundary.
func TestValidateRejectsHostileReferences(t *testing.T) {
	base := newRef(t, filepath.Join(t.TempDir(), "proj"))
	cases := map[string]func(*Reference){
		"future version":   func(r *Reference) { r.Version = RefVersion + 1 },
		"helper transport": func(r *Reference) { r.Repo = "ext::sh -c 'evil'" },
		"ansi escape":      func(r *Reference) { r.Repo = "https://h/r.git\x1b]0;pwned\a" },
		"newline":          func(r *Reference) { r.Repo = "https://h/r.git\nrm -rf /" },
		"path mismatch":    func(r *Reference) { r.Path = "projects/../../etc" },
		"unslugged name":   func(r *Reference) { r.Project = "../escape" },
		"unknown mode":     func(r *Reference) { r.Encryption = "rot13" },
		"private key":      func(r *Reference) { r.Encryption = EncryptionAge; r.Recipient = "AGE-SECRET-KEY-1XYZ" },
	}
	for name, mutate := range cases {
		ref := base
		mutate(&ref)
		if err := ref.Validate(); err == nil {
			t.Errorf("%s: Validate accepted %+v", name, ref)
		}
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("the unmodified reference should be valid: %v", err)
	}
}

func TestWriteAndReadReference(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ref := newRef(t, dir)

	res, err := WriteReference(dir, ref, Options{})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if res.Status != StatusInstalled {
		t.Fatalf("status = %q", res.Status)
	}

	got, err := ReadReference(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != ref {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, ref)
	}

	// The README is generated next to it and names the store.
	readme, err := os.ReadFile(RefReadmePath(dir))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	for _, want := range []string{ref.Repo, ref.Project, "cct import", "cct export", "MUST be private"} {
		if !strings.Contains(string(readme), want) {
			t.Errorf("README does not mention %q", want)
		}
	}

	// Writing the same reference again is a no-op.
	if res, err = WriteReference(dir, ref, Options{}); err != nil || res.Status != StatusUnchanged {
		t.Fatalf("rewrite: status=%q err=%v", res.Status, err)
	}
}

// Repointing a project at a different store is exactly the change that must not
// happen by accident.
func TestWriteReferenceNeedsForceToRepoint(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj")
	ref := newRef(t, dir)
	if _, err := WriteReference(dir, ref, Options{}); err != nil {
		t.Fatalf("write: %v", err)
	}

	other := ref
	other.Repo = "git@github.com:someone-else/sessions.git"
	if _, err := WriteReference(dir, other, Options{}); err == nil {
		t.Fatal("a differing reference was written without --force")
	}
	if got, _ := ReadReference(dir); got.Repo != ref.Repo {
		t.Fatalf("the reference changed anyway: %q", got.Repo)
	}

	res, err := WriteReference(dir, other, Options{Force: true})
	if err != nil || res.Status != StatusUpdated {
		t.Fatalf("forced write: status=%q err=%v", res.Status, err)
	}
	if got, _ := ReadReference(dir); got.Repo != other.Repo {
		t.Fatalf("forced write did not repoint: %q", got.Repo)
	}
}

func TestWriteReferenceDryRunWritesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj")
	ref := newRef(t, dir)
	if _, err := WriteReference(dir, ref, Options{DryRun: true}); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if _, err := os.Stat(RefPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote the reference (err=%v)", err)
	}
	if _, err := os.Stat(RefReadmePath(dir)); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote the README (err=%v)", err)
	}
}

func TestReadReferenceRejectsGarbage(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(filepath.Join(dir, RefSubdir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RefPath(dir), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadReference(dir); err == nil {
		t.Fatal("invalid JSON was accepted")
	}

	hostile, _ := json.Marshal(Reference{Version: 1, Repo: "ext::sh -c evil", Project: "p", Encryption: EncryptionPlain})
	if err := os.WriteFile(RefPath(dir), hostile, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadReference(dir); err == nil {
		t.Fatal("a hostile reference was accepted")
	}
}

func TestStorePathsAndEncryptionSuffix(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-app")
	ref := newRef(t, dir)
	store := filepath.Join(t.TempDir(), "store")

	want := filepath.Join(store, "projects", "my-app", "claude", "claude-all.codexbundle")
	if got := BundlePathIn(store, ref, "claude"); got != want {
		t.Fatalf("bundle path = %q, want %q", got, want)
	}
	wantGroup := filepath.Join(store, "projects", "my-app", "claude", "groups", "auth-refactor.codexbundle")
	if got := GroupPathIn(store, ref, "claude", "Auth Refactor"); got != wantGroup {
		t.Fatalf("group path = %q, want %q", got, wantGroup)
	}

	enc := ref
	enc.Encryption = EncryptionAge
	enc.Recipient = "age1abc"
	if got := BundlePathIn(store, enc, "codex"); !strings.HasSuffix(got, ".codexbundle.age") {
		t.Fatalf("encrypted bundle path = %q", got)
	}
}

func TestStoreDirDefaults(t *testing.T) {
	if got := StoreDir("/tmp/x"); got != "/tmp/x" {
		t.Fatalf("explicit dir = %q", got)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got := StoreDir(""); got != filepath.Join(home, "cct-sessions") {
		t.Fatalf("default dir = %q", got)
	}
	if got := StoreDir("~/elsewhere"); got != filepath.Join(home, "elsewhere") {
		t.Fatalf("~ expansion = %q", got)
	}
}

func TestExplainDescribesTheEncryptedCase(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj")
	ref, err := NewReference(dir, "https://example.com/sessions.git", "", EncryptionAge, "age1recipient")
	if err != nil {
		t.Fatalf("new reference: %v", err)
	}
	doc := Explain(ref)
	for _, want := range []string{"age1recipient", "--identity", "--encrypt-to", "claude-all.codexbundle.age"} {
		if !strings.Contains(doc, want) {
			t.Errorf("explanation does not mention %q", want)
		}
	}
	if strings.Contains(doc, "MUST be private") {
		t.Error("the plaintext warning should not appear in encrypted mode")
	}
}
