// Package git provides best-effort discovery of git metadata for a project
// directory. Everything here is optional: if git is not installed or the
// directory is not a repository, the functions return an empty Info and no
// error, so callers can include git metadata when available without failing.
package git

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Info holds the git facts we record in a bundle manifest.
type Info struct {
	Branch    string `json:"branch,omitempty"`
	CommitSHA string `json:"commit_sha,omitempty"`
	RemoteURL string `json:"remote_url,omitempty"`
}

// Empty reports whether no git information was discovered.
func (i Info) Empty() bool {
	return i.Branch == "" && i.CommitSHA == "" && i.RemoteURL == ""
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
	}
}

func isRepo(dir string) bool {
	return run(dir, "rev-parse", "--is-inside-work-tree") == "true"
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
