package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitEnv sets a deterministic identity via env so commits work without touching
// the machine's global git config.
func gitEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "codex-sync-test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "codex-sync-test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func skipNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func TestDiscoverCleanRepoUnpushed(t *testing.T) {
	skipNoGit(t)
	gitEnv(t)
	dir := t.TempDir()
	mustGit(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "first")

	gi := Discover(dir)
	if gi.Empty() {
		t.Fatalf("expected non-empty info")
	}
	if gi.CommitSHA == "" {
		t.Errorf("missing commit sha")
	}
	if gi.Dirty {
		t.Errorf("clean repo reported dirty")
	}
	if !gi.Unpushed {
		t.Errorf("repo with no remote should be reported unpushed")
	}
}

func TestDiscoverDirty(t *testing.T) {
	skipNoGit(t)
	gitEnv(t)
	dir := t.TempDir()
	mustGit(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "first")
	// Introduce an uncommitted change.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !Discover(dir).Dirty {
		t.Errorf("expected dirty working tree")
	}
}

func TestDiscoverNonRepoEmpty(t *testing.T) {
	skipNoGit(t)
	if !Discover(t.TempDir()).Empty() {
		t.Errorf("non-repo dir should yield empty info")
	}
}

func TestCloneRoundTrip(t *testing.T) {
	skipNoGit(t)
	gitEnv(t)
	// Build a source repo and push it to a bare "remote".
	src := t.TempDir()
	mustGit(t, src, "init")
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustGit(t, src, "add", ".")
	mustGit(t, src, "commit", "-m", "first")

	bare := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, ".", "init", "--bare", bare)
	mustGit(t, src, "remote", "add", "origin", bare)
	mustGit(t, src, "push", "origin", "HEAD")

	commit := Discover(src).CommitSHA

	dest := filepath.Join(t.TempDir(), "clone")
	if err := Clone(bare, dest, commit); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "file.txt")); err != nil {
		t.Errorf("cloned file missing: %v", err)
	}
}

func TestCloneNoRemoteErrors(t *testing.T) {
	if err := Clone("", t.TempDir(), ""); err == nil {
		t.Errorf("expected error cloning empty remote")
	}
}

func TestPushRoundTrip(t *testing.T) {
	skipNoGit(t)
	gitEnv(t)
	// A source repo with a remote whose HEAD has NOT been pushed yet.
	src := t.TempDir()
	mustGit(t, src, "init")
	mustGit(t, src, "checkout", "-b", "work")
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustGit(t, src, "add", ".")
	mustGit(t, src, "commit", "-m", "first")

	bare := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, ".", "init", "--bare", bare)
	mustGit(t, src, "remote", "add", "origin", bare)

	// Before push, Discover reports the commit as unpushed.
	if !Discover(src).Unpushed {
		t.Fatalf("precondition: commit should be unpushed before Push")
	}

	remote, branch, err := Push(src)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if remote != "origin" || branch != "work" {
		t.Errorf("push reported remote=%q branch=%q, want origin/work", remote, branch)
	}
	// The bare remote must now contain the branch at the same commit.
	want := Discover(src).CommitSHA
	got := strings.TrimSpace(runOut(t, ".", "ls-remote", bare, "refs/heads/work"))
	if got == "" || !strings.HasPrefix(got, want) {
		t.Errorf("remote ref = %q, want commit %q present", got, want)
	}
	// And Discover should now consider HEAD pushed.
	if Discover(src).Unpushed {
		t.Errorf("after Push, HEAD should no longer be unpushed")
	}
}

func TestPushNoRemoteErrors(t *testing.T) {
	skipNoGit(t)
	gitEnv(t)
	dir := t.TempDir()
	mustGit(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "first")

	if _, _, err := Push(dir); err == nil {
		t.Errorf("expected an error pushing with no remote configured")
	}
}

// runOut runs a git command and returns stdout, failing the test on error.
func runOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
