// Package git provides best-effort discovery of git metadata for a project
// directory. Everything here is optional: if git is not installed or the
// directory is not a repository, the functions return an empty Info and no
// error, so callers can include git metadata when available without failing.
package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Info holds the git facts we record in a bundle manifest.
type Info struct {
	Branch    string `json:"branch,omitempty"`
	CommitSHA string `json:"commit_sha,omitempty"`
	RemoteURL string `json:"remote_url,omitempty"`
	// Dirty reports that the working tree had uncommitted changes when the
	// bundle was exported, so HEAD does not capture the exact state.
	Dirty bool `json:"dirty,omitempty"`
	// Unpushed reports that the HEAD commit is not reachable from any
	// remote-tracking branch, so the other machine cannot fetch it yet.
	Unpushed bool `json:"unpushed,omitempty"`
}

// Empty reports whether no git information was discovered.
func (i Info) Empty() bool {
	return i.Branch == "" && i.CommitSHA == "" && i.RemoteURL == ""
}

// Available reports whether the git executable is on PATH.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// IsRepo reports whether dir is inside a git working tree. It is best-effort:
// a missing git binary or any failure yields false.
func IsRepo(dir string) bool {
	if dir == "" || !Available() {
		return false
	}
	return isRepo(dir)
}

// Discover returns best-effort git metadata for dir. It never returns an error;
// missing git or a non-repository simply yields an empty Info.
func Discover(dir string) Info {
	if dir == "" {
		return Info{}
	}
	if _, err := exec.LookPath("git"); err != nil {
		return Info{}
	}
	if !isRepo(dir) {
		return Info{}
	}
	return Info{
		Branch:    run(dir, "rev-parse", "--abbrev-ref", "HEAD"),
		CommitSHA: run(dir, "rev-parse", "HEAD"),
		RemoteURL: run(dir, "config", "--get", "remote.origin.url"),
		Dirty:     isDirty(dir),
		Unpushed:  isUnpushed(dir),
	}
}

// Clone clones remote into dir and, when commit is non-empty, checks it out.
// It is only ever invoked when the user explicitly opts in (e.g. --clone), so
// it surfaces errors rather than swallowing them like Discover does.
func Clone(remote, dir, commit string) error {
	if remote == "" {
		return fmt.Errorf("no remote URL to clone")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git not found in PATH")
	}
	if err := runErr("", "clone", remote, dir); err != nil {
		return fmt.Errorf("git clone %s: %w", remote, err)
	}
	if commit != "" {
		if err := runErr(dir, "checkout", commit); err != nil {
			return fmt.Errorf("git checkout %s: %w", commit, err)
		}
	}
	return nil
}

func isRepo(dir string) bool {
	return run(dir, "rev-parse", "--is-inside-work-tree") == "true"
}

// isDirty reports whether the working tree has staged or unstaged changes.
func isDirty(dir string) bool {
	return run(dir, "status", "--porcelain") != ""
}

// isUnpushed reports whether HEAD has commits not reachable from any
// remote-tracking branch. With no remotes configured, every commit counts as
// unpushed (the other machine has nowhere to fetch from).
func isUnpushed(dir string) bool {
	return run(dir, "rev-list", "HEAD", "--not", "--remotes", "--max-count=1") != ""
}

// run executes a git command in dir and returns its trimmed stdout, or "" on
// any failure. A short timeout guards against a hung git process.
func run(dir string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// runErr executes a git command and returns its error, with a longer timeout
// suited to network operations like clone. Unlike run, it does not swallow
// failures: callers that opt into an action want to know when it fails.
func runErr(dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}
