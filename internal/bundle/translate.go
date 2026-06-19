package bundle

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/handoff"
	"github.com/ahmojo/codex-claude-transfer/internal/safety"
)

// TranslateOptions configures a cross-agent handoff import: reading a bundle of
// one agent's sessions and writing translated, continue-from-here sessions into a
// different agent's home.
type TranslateOptions struct {
	BundlePath string
	TargetTool agent.Kind
	DryRun     bool
}

// TranslateItem is the plan/outcome for one translated session.
type TranslateItem struct {
	BundlePath string
	DestPath   string
	SessionID  string // id assigned in the target agent
	Turns      int
	Action     string // "translate" | "skip-existing" | "skip"
	Reason     string // why it was skipped, when Action == "skip"
}

// TranslateResult summarizes a cross-agent handoff import.
type TranslateResult struct {
	SourceTool      agent.Kind
	TargetTool      agent.Kind
	Translated      int
	SkippedExisting int
	Skipped         int
	Items           []TranslateItem
	Warnings        []string
	DryRun          bool
}

// TranslateImport reads the bundle (whatever agent it came from), converts each
// session into TargetTool's format via the handoff package, and writes the
// results into targetHome (whose Root is the target agent's home). It never
// overwrites: a session that already exists is skipped (translation is
// deterministic, so a re-run is an idempotent skip). Checksums of the source
// bundle are verified before anything is read, exactly like a native import.
func TranslateImport(targetHome codexhome.Home, opts TranslateOptions) (TranslateResult, error) {
	result := TranslateResult{TargetTool: agent.Normalize(opts.TargetTool), DryRun: opts.DryRun}

	zr, err := zip.OpenReader(opts.BundlePath)
	if err != nil {
		return result, fmt.Errorf("open bundle: %w", err)
	}
	defer zr.Close()

	manifest, checksums, err := readMeta(&zr.Reader)
	if err != nil {
		return result, err
	}
	if err := validateManifest(manifest); err != nil {
		return result, err
	}
	sourceKind := agent.Normalize(agent.Kind(manifest.Tool))
	result.SourceTool = sourceKind
	if sourceKind == result.TargetTool {
		return result, fmt.Errorf("bundle already contains %s sessions; use a plain import (no --to) instead", sourceKind.Label())
	}

	if err := verifyBundle(&zr.Reader, checksums); err != nil {
		return result, err
	}
	if err := verifyManifestBinding(&zr.Reader, manifest, checksums, sourceKind); err != nil {
		return result, err
	}

	for _, f := range zr.File {
		rel := f.Name
		if rel == ManifestName || rel == ChecksumsName || !isImportableEntry(sourceKind, rel) {
			continue
		}
		srcBytes, err := readEntryBytes(&zr.Reader, rel)
		if err != nil {
			return result, err
		}
		tr, err := handoff.Translate(srcBytes, sourceKind.String(), result.TargetTool.String())
		if err != nil {
			// A session we cannot translate (e.g. no recoverable conversation, or no
			// cwd for a Claude target) is skipped with a reason, not fatal.
			result.Items = append(result.Items, TranslateItem{BundlePath: rel, Action: "skip", Reason: err.Error()})
			result.Skipped++
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: not translated (%v)", rel, err))
			continue
		}

		if !isImportableEntry(result.TargetTool, tr.RelPath) {
			return result, fmt.Errorf("internal: translated path %q is not a valid %s session path", tr.RelPath, result.TargetTool.Label())
		}
		dest, err := safety.DestPath(targetHome.Root, tr.RelPath)
		if err != nil {
			return result, err
		}

		item := TranslateItem{BundlePath: rel, DestPath: dest, SessionID: tr.SessionID, Turns: tr.Turns}
		exists, err := fileExists(dest)
		if err != nil {
			return result, err
		}
		if exists {
			// Translation is deterministic, so a pre-existing destination is a prior
			// translation of the same session: skip, never overwrite.
			item.Action = "skip-existing"
			result.SkippedExisting++
			result.Items = append(result.Items, item)
			continue
		}
		item.Action = "translate"
		result.Translated++
		result.Items = append(result.Items, item)

		if opts.DryRun {
			continue
		}
		if err := safety.CopyAtomic(dest, bytes.NewReader(tr.Content)); err != nil {
			return result, fmt.Errorf("write %s: %w", tr.RelPath, err)
		}
	}

	return result, nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
