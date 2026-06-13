package bundle

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// InspectResult is the read-only view of a bundle: its manifest, its checksum
// map, and the names of the files it contains. Inspecting never extracts or
// writes anything.
type InspectResult struct {
	Manifest  Manifest
	Checksums Checksums
	Entries   []string
}

// Inspect opens a .codexbundle and reads manifest.json and checksums.json
// without extracting any session files. It validates that the bundle has the
// expected format version.
func Inspect(bundlePath string) (InspectResult, error) {
	zr, err := zip.OpenReader(bundlePath)
	if err != nil {
		return InspectResult{}, fmt.Errorf("open bundle: %w", err)
	}
	defer zr.Close()

	var res InspectResult
	var sawManifest, sawChecksums bool

	for _, f := range zr.File {
		res.Entries = append(res.Entries, f.Name)
		switch f.Name {
		case ManifestName:
			data, err := readZipFile(f)
			if err != nil {
				return InspectResult{}, fmt.Errorf("read %s: %w", ManifestName, err)
			}
			if err := json.Unmarshal(data, &res.Manifest); err != nil {
				return InspectResult{}, fmt.Errorf("parse %s: %w", ManifestName, err)
			}
			sawManifest = true
		case ChecksumsName:
			data, err := readZipFile(f)
			if err != nil {
				return InspectResult{}, fmt.Errorf("read %s: %w", ChecksumsName, err)
			}
			if err := json.Unmarshal(data, &res.Checksums); err != nil {
				return InspectResult{}, fmt.Errorf("parse %s: %w", ChecksumsName, err)
			}
			sawChecksums = true
		}
	}

	if !sawManifest {
		return InspectResult{}, fmt.Errorf("bundle is missing %s", ManifestName)
	}
	if !sawChecksums {
		return InspectResult{}, fmt.Errorf("bundle is missing %s", ChecksumsName)
	}
	if err := validateManifest(res.Manifest); err != nil {
		return InspectResult{}, err
	}
	return res, nil
}

// validateManifest checks the bundle format version is one this build accepts.
func validateManifest(m Manifest) error {
	if m.FormatVersion == "" {
		return fmt.Errorf("manifest missing format_version")
	}
	if !strings.HasPrefix(m.FormatVersion, "codex-sync-bundle-") {
		return fmt.Errorf("unrecognized bundle format_version %q", m.FormatVersion)
	}
	if m.FormatVersion != FormatVersion {
		return fmt.Errorf("unsupported bundle format_version %q (this build supports %q)", m.FormatVersion, FormatVersion)
	}
	return nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
