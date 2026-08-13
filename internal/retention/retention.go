// Package retention answers one question about a Claude Code home: which of its
// transcripts is Claude Code's own cleanup about to delete?
//
// Claude Code removes session files older than `cleanupPeriodDays` (30 by
// default) from ~/.claude/settings.json. The deletion is silent — no prompt, no
// trash — and users have reported losing hundreds of transcripts to it,
// including one case where an explicit `cleanupPeriodDays: 99999` was ignored
// (anthropics/claude-code#41458, #85466, #86280). cct cannot stop that cleanup
// and does not try: it reads the setting, projects it onto the transcripts that
// are actually on disk, and says what is about to go, early enough to archive it.
//
// Everything here is read-only. The package never writes to the Claude home, and
// it deliberately reads nothing from settings.json except the retention number:
// that file also holds API keys and environment values that are none of cct's
// business.
package retention

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

// SettingsFileName is the Claude Code settings file inside the home.
const SettingsFileName = "settings.json"

// DefaultDays is the cleanup period Claude Code applies when settings.json does
// not set one. It is the documented default, and the value most reports of
// unexpected deletion were running under.
const DefaultDays = 30

// MaxSettingsBytes caps how much of settings.json is read. The file is small;
// the cap keeps a corrupt or hostile file from being pulled into memory whole.
const MaxSettingsBytes = 1 << 20

// Policy is the retention rule cct resolved for a Claude Code home.
type Policy struct {
	// Days is the retention period in days.
	Days int
	// Configured reports whether settings.json set it explicitly. When false,
	// Days is DefaultDays and the user may not know a cleanup is happening.
	Configured bool
	// SettingsPath is the file consulted, whether or not it existed.
	SettingsPath string
}

// Cutoff is the instant a transcript must be newer than to survive.
func (p Policy) Cutoff(now time.Time) time.Time {
	return now.AddDate(0, 0, -p.Days)
}

// LoadPolicy reads the retention setting from a Claude Code home. A missing or
// unreadable settings.json is not an error: Claude Code applies its default in
// that case, and so does cct. A settings.json that exists but cannot be parsed
// is reported, because then cct cannot tell which rule is really in force.
func LoadPolicy(claudeRoot string) (Policy, error) {
	p := Policy{Days: DefaultDays, SettingsPath: filepath.Join(claudeRoot, SettingsFileName)}

	info, err := os.Lstat(p.SettingsPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return p, nil
	case err != nil:
		return p, nil
	case !info.Mode().IsRegular():
		return p, fmt.Errorf("%s is not a regular file", p.SettingsPath)
	case info.Size() > MaxSettingsBytes:
		return p, fmt.Errorf("%s is unexpectedly large (%d bytes); not parsed", p.SettingsPath, info.Size())
	}

	data, err := os.ReadFile(p.SettingsPath)
	if err != nil {
		return p, nil
	}
	// Only the retention number is decoded. settings.json also holds API keys
	// and environment values, which cct has no reason to read.
	var parsed struct {
		CleanupPeriodDays *int `json:"cleanupPeriodDays"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return p, fmt.Errorf("%s is not valid JSON: %w", p.SettingsPath, err)
	}
	if parsed.CleanupPeriodDays == nil {
		return p, nil
	}
	if *parsed.CleanupPeriodDays <= 0 {
		return p, fmt.Errorf("%s sets cleanupPeriodDays to %d; cct cannot tell what that means for deletion",
			p.SettingsPath, *parsed.CleanupPeriodDays)
	}
	p.Days = *parsed.CleanupPeriodDays
	p.Configured = true
	return p, nil
}

// AtRisk is one transcript the cleanup is about to remove (or already may have).
type AtRisk struct {
	ThreadID string
	Path     string
	CWD      string
	// Deadline is when this transcript falls outside the retention window. It is
	// in the past for transcripts already eligible for deletion.
	Deadline time.Time
	// DaysLeft is the whole days until Deadline, negative once it has passed.
	DaysLeft int
}

// Report is the projection of a retention policy onto the transcripts on disk.
type Report struct {
	Policy Policy
	// Expired are transcripts already older than the retention period. Claude
	// Code may remove them at any time, and may already have removed others.
	Expired []AtRisk
	// Soon are transcripts that fall out of the window within the warning
	// horizon passed to Assess.
	Soon []AtRisk
	// Total is how many transcripts were considered.
	Total int
	// NextDeadline is the earliest upcoming deadline among Soon, zero when none.
	NextDeadline time.Time
}

// Any reports whether anything is expired or about to expire.
func (r Report) Any() bool { return len(r.Expired) > 0 || len(r.Soon) > 0 }

// Assess projects a policy onto scanned sessions. A session's last modification
// time is what the cleanup goes by, so that is what this compares — cct does not
// re-date anything. Sessions are returned oldest-first, so the caller can name
// the most urgent one without sorting again.
func Assess(list []sessions.Session, policy Policy, now time.Time, warnWithin time.Duration) Report {
	rep := Report{Policy: policy, Total: len(list)}
	cutoff := policy.Cutoff(now)
	horizon := now.Add(warnWithin)

	for _, s := range list {
		if s.ModTime.IsZero() {
			continue
		}
		deadline := s.ModTime.AddDate(0, 0, policy.Days)
		item := AtRisk{
			ThreadID: s.ThreadID,
			Path:     s.Path,
			CWD:      s.CWD,
			Deadline: deadline,
			DaysLeft: int(deadline.Sub(now).Hours() / 24),
		}
		switch {
		case s.ModTime.Before(cutoff):
			rep.Expired = append(rep.Expired, item)
		case deadline.Before(horizon):
			rep.Soon = append(rep.Soon, item)
		}
	}

	sort.Slice(rep.Expired, func(i, j int) bool { return rep.Expired[i].Deadline.Before(rep.Expired[j].Deadline) })
	sort.Slice(rep.Soon, func(i, j int) bool { return rep.Soon[i].Deadline.Before(rep.Soon[j].Deadline) })
	if len(rep.Soon) > 0 {
		rep.NextDeadline = rep.Soon[0].Deadline
	}
	return rep
}
