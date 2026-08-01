package testfixtures

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnsafeCleanupTarget is returned when a cleanup target fails validation.
var ErrUnsafeCleanupTarget = errors.New("unsafe cleanup target")

// ValidateCleanupTarget is M22-112.
//
// Test cleanup recursively deletes directories. A bug in a fixture path — an
// empty string, a root, a home directory, a path assembled from a variable
// that was never set — turns that convenience into data loss on a developer's
// machine. Every recursive delete goes through this check first, and the check
// refuses anything it cannot positively identify as a temporary fixture
// directory.
func ValidateCleanupTarget(target string) error {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return fmt.Errorf("%w: empty path", ErrUnsafeCleanupTarget)
	}
	if trimmed != target {
		return fmt.Errorf("%w: path has surrounding whitespace", ErrUnsafeCleanupTarget)
	}
	if !filepath.IsAbs(target) {
		return fmt.Errorf("%w: %q is not absolute", ErrUnsafeCleanupTarget, target)
	}
	cleaned := filepath.Clean(target)
	if cleaned != target {
		return fmt.Errorf("%w: %q is not canonical", ErrUnsafeCleanupTarget, target)
	}
	if strings.Contains(target, "..") {
		return fmt.Errorf("%w: %q contains a parent traversal", ErrUnsafeCleanupTarget, target)
	}

	volume := filepath.VolumeName(cleaned)
	if cleaned == volume+string(filepath.Separator) || cleaned == string(filepath.Separator) {
		return fmt.Errorf("%w: %q is a filesystem root", ErrUnsafeCleanupTarget, target)
	}

	// The target must live under the OS temporary directory. Nothing outside
	// it was created by a fixture, so nothing outside it may be deleted by
	// one.
	temporary, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		temporary = os.TempDir()
	}
	resolved := cleaned
	if evaluated, evalErr := filepath.EvalSymlinks(cleaned); evalErr == nil {
		resolved = evaluated
	}
	if !withinDirectory(temporary, resolved) {
		return fmt.Errorf(
			"%w: %q is outside the temporary directory %q, so it was not created by a fixture",
			ErrUnsafeCleanupTarget, target, temporary)
	}
	if sameDirectory(temporary, resolved) {
		return fmt.Errorf("%w: %q is the temporary directory itself", ErrUnsafeCleanupTarget, target)
	}

	// Refuse anything that is not a directory, so a stray file path cannot be
	// handed to a recursive delete.
	info, err := os.Lstat(cleaned)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Already gone is a successful cleanup, not an unsafe one.
			return nil
		}
		return fmt.Errorf("%w: inspect %q: %v", ErrUnsafeCleanupTarget, target, err)
	}
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return fmt.Errorf(
			"%w: %q is a link or reparse point; deleting through it could reach outside the fixture",
			ErrUnsafeCleanupTarget, target)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %q is not a directory", ErrUnsafeCleanupTarget, target)
	}
	return nil
}

// RemoveFixtureDirectory validates a target and then removes it (M22-112).
//
// PreserveOnFailure is deliberately an explicit flag rather than a default:
// leaving artifacts behind by default fills a developer's disk with
// directories nobody reads, while deleting them unconditionally throws away
// the evidence of the one run that mattered.
func RemoveFixtureDirectory(target string, preserve bool) error {
	if err := ValidateCleanupTarget(target); err != nil {
		return err
	}
	if preserve {
		return nil
	}
	return os.RemoveAll(target)
}

func withinDirectory(parent, candidate string) bool {
	relative, err := filepath.Rel(parent, candidate)
	if err != nil {
		return false
	}
	if relative == "." {
		return true
	}
	return relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(relative)
}

func sameDirectory(left, right string) bool {
	relative, err := filepath.Rel(left, right)
	return err == nil && relative == "."
}
