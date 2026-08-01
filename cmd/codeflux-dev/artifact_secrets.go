package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"codeflux.dev/codeflux/internal/testfixtures"
)

// maximumScannedArtifactBytes bounds one scanned file. A profile or a database
// can be very large, and reading it whole would make the check cost more than
// the build. Credential material is short and appears near the text that
// mentions it, so a bounded prefix is where a leak actually shows up.
const maximumScannedArtifactBytes = 8 << 20

// artifactSecretFinding is one credential-shaped hit in a retained artifact.
type artifactSecretFinding struct {
	Path string
	// Shape names WHICH forbidden value matched. The matched text itself is
	// never carried: a report that quoted the leak would leak it again, into a
	// terminal and probably into CI logs.
	Shape string
}

// scannedArtifactExtensions are the retained artifact kinds worth scanning.
//
// Binary formats are excluded deliberately rather than accidentally: a heap
// profile can contain anything the process held, and scanning it for a
// substring would produce neither confidence nor an actionable finding. The
// protection for those is that they are never committed, which
// checkArtifactPolicy already enforces.
func scannedArtifactExtensions() []string {
	return []string{
		".txt", ".log", ".json", ".jsonl", ".md", ".html", ".xml", ".csv", ".yaml", ".yml",
	}
}

// forbiddenArtifactMaterial is every credential shape a fixture can seed.
//
// It is sourced from internal/testfixtures rather than duplicated, so adding a
// new fixture credential automatically extends the scan instead of silently
// creating an unscanned one.
func forbiddenArtifactMaterial() []string {
	material := testfixtures.FixtureCredentialShapes()
	sort.Strings(material)
	return material
}

// checkArtifactSecrets is M22-122.
//
// Replay fixtures, logs, profiles, screenshots, and failure artifacts are
// exactly the files a developer attaches to a bug report, so a seeded
// credential surviving into one of them is a leak with a delivery mechanism.
func checkArtifactSecrets(root string) error {
	artifactRoot := filepath.Join(root, ".artifacts")
	info, err := os.Stat(artifactRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// No artifacts is a clean result, not a skipped check.
			return nil
		}
		return fmt.Errorf("inspect artifact root: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", artifactRoot)
	}

	forbidden := forbiddenArtifactMaterial()
	if len(forbidden) == 0 {
		return errors.New(
			"no forbidden credential material is declared, so the artifact scan would pass vacuously")
	}
	extensions := scannedArtifactExtensions()

	var findings []artifactSecretFinding
	scanned := 0
	walkErr := filepath.WalkDir(artifactRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// A file that vanished mid-walk is not a finding: artifacts are
			// written and cleaned concurrently by other runs.
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if !contains(extensions, extension) {
			return nil
		}
		contents, readErr := readBoundedFile(path, maximumScannedArtifactBytes)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("read %s: %w", path, readErr)
		}
		scanned++
		text := string(contents)
		for _, secret := range forbidden {
			if strings.Contains(text, secret) {
				relative, relErr := filepath.Rel(root, path)
				if relErr != nil {
					relative = path
				}
				findings = append(findings, artifactSecretFinding{
					Path:  filepath.ToSlash(relative),
					Shape: describeSecretShape(secret),
				})
			}
		}
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("scan artifacts: %w", walkErr)
	}
	if len(findings) == 0 {
		return nil
	}
	sort.Slice(findings, func(left, right int) bool {
		if findings[left].Path != findings[right].Path {
			return findings[left].Path < findings[right].Path
		}
		return findings[left].Shape < findings[right].Shape
	})
	descriptions := make([]string, 0, len(findings))
	for _, finding := range findings {
		descriptions = append(descriptions,
			fmt.Sprintf("%s contains %s", finding.Path, finding.Shape))
	}
	return fmt.Errorf(
		"seeded credential material reached %d of %d scanned artifacts: %s",
		len(findings), scanned, strings.Join(descriptions, "; "))
}

// describeSecretShape names a credential without reproducing it.
func describeSecretShape(secret string) string {
	if len(secret) <= 8 {
		return fmt.Sprintf("fixture credential material (%d bytes)", len(secret))
	}
	// Only the leading marker is shown. It identifies which fixture leaked
	// without printing anything usable.
	return fmt.Sprintf("fixture credential material beginning %q (%d bytes)",
		secret[:8], len(secret))
}

func readBoundedFile(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	buffer := make([]byte, maximum)
	read, err := file.Read(buffer)
	if err != nil && read == 0 {
		if err.Error() == "EOF" {
			return nil, nil
		}
		return nil, err
	}
	return buffer[:read], nil
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
