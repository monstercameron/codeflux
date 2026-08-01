// Package dogfood is the ReserveFlow trial: the control plane that keeps an
// evaluated run honest, and the harness that judges it (M24-101..130).
//
// The whole value of a dogfood run is that it could have failed. Every
// structure here exists to remove a way the run could quietly succeed for the
// wrong reason: an agent that read the hidden tests, a track that skipped a
// requirement, an intervention nobody recorded, a fixture edited afterwards.
package dogfood

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// RepositoryRole separates the two repositories a dogfood run needs
// (M24-101, M24-103).
type RepositoryRole string

const (
	// RoleScaffold is the ReserveFlow repository the agent works in.
	RoleScaffold RepositoryRole = "reserveflow-scaffold"
	// RoleEvaluator holds the hidden tests. Nothing the agent can reach may
	// see it.
	RoleEvaluator RepositoryRole = "evaluator"
)

// ScaffoldContent is what the frozen ReserveFlow scaffold must contain, and
// nothing more (M24-101).
//
// "And nothing more" is the requirement that matters. A scaffold carrying an
// extra helper, an example, or a stray test hands the agent work it was
// supposed to do, and the trial silently measures something easier.
func ScaffoldContent() []string {
	return []string{
		"go.mod",
		"README.md",
		"cmd/reserveflow/main.go",
		"internal/testsupport/testsupport.go",
		"LICENSE",
		".gitignore",
	}
}

// ForbiddenScaffoldContent is what must NOT be present.
func ForbiddenScaffoldContent() []string {
	return []string{
		"acceptance_test.go", "hidden_test.go", "solution.go",
		"reference/", "answers/", ".github/",
	}
}

// Scaffold describes the frozen ReserveFlow repository (M24-101, M24-102).
type Scaffold struct {
	Root string
	// Revision is the frozen scaffold revision.
	Revision string
	// TreeHash is the cryptographic identity of its contents (M24-102).
	TreeHash string
}

// Validate rejects a scaffold that is not frozen or not minimal.
func (scaffold Scaffold) Validate() error {
	if strings.TrimSpace(scaffold.Root) == "" {
		return errors.New("the scaffold has no root")
	}
	if err := validateHex(scaffold.Revision, 40, "scaffold revision"); err != nil {
		return err
	}
	if err := validateHex(scaffold.TreeHash, 64, "scaffold tree hash"); err != nil {
		return err
	}
	return nil
}

// VerifyScaffoldContent checks the scaffold holds exactly what §28 specifies
// (M24-101).
func VerifyScaffoldContent(root string) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("no scaffold root was supplied")
	}
	present := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		slashed := filepath.ToSlash(relative)
		// .git is the repository itself, not content.
		if strings.HasPrefix(slashed, ".git/") || slashed == ".git" {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		present[slashed] = true
		return nil
	})
	if err != nil {
		return fmt.Errorf("read scaffold: %w", err)
	}

	var missing []string
	for _, required := range ScaffoldContent() {
		if !present[required] {
			missing = append(missing, required)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("the scaffold is missing %s", strings.Join(missing, ", "))
	}

	var extra []string
	for path := range present {
		if slices.Contains(ScaffoldContent(), path) {
			continue
		}
		extra = append(extra, path)
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return fmt.Errorf(
			"the scaffold contains %s, which hands the agent work it was supposed to do",
			strings.Join(extra, ", "))
	}
	return nil
}

// HashTree computes a stable content identity for a directory (M24-102,
// M24-130).
//
// It hashes paths AND contents in sorted order, so a file moved, renamed, or
// edited after the fact all change the result. Hashing contents alone would
// let a rename go undetected.
func HashTree(root string, skip func(string) bool) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("no root was supplied")
	}
	type entry struct {
		path string
		sum  [32]byte
	}
	var entries []entry
	err := filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		slashed := filepath.ToSlash(relative)
		if skip != nil && skip(slashed) {
			if item.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if item.IsDir() {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		entries = append(entries, entry{path: slashed, sum: sha256.Sum256(content)})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("hash tree: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].path < entries[right].path
	})
	digest := sha256.New()
	for _, item := range entries {
		digest.Write([]byte(item.path))
		digest.Write([]byte{0})
		digest.Write(item.sum[:])
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// SkipGit is the usual skip predicate for HashTree.
func SkipGit(path string) bool {
	return path == ".git" || strings.HasPrefix(path, ".git/")
}

// Isolation describes what a component may reach (M24-104).
type Isolation struct {
	// Component is the thing being checked.
	Component string
	// ReachablePaths are the paths it can read.
	ReachablePaths []string
}

// EvaluatorReachabilityViolation reports a component that can see the hidden
// tests (M24-104).
type EvaluatorReachabilityViolation struct {
	Component string
	Path      string
}

// IsolatedComponents are the parts of the system that must not reach the
// evaluator repository (M24-104).
func IsolatedComponents() []string {
	return []string{
		"coordinator", "worker", "tool-execution",
		"provider-context", "reserveflow-worktree",
	}
}

// VerifyEvaluatorIsolation checks nothing the agent controls can read the
// evaluator repository (M24-104).
//
// This is the single most important check in the trial. An agent that can read
// the hidden tests can satisfy them without solving anything, and every number
// the run produces afterwards is meaningless.
func VerifyEvaluatorIsolation(
	evaluatorRoot string,
	components []Isolation,
) ([]EvaluatorReachabilityViolation, error) {
	if strings.TrimSpace(evaluatorRoot) == "" {
		return nil, errors.New("no evaluator repository was supplied")
	}
	if len(components) == 0 {
		return nil, errors.New(
			"no components were checked, so isolation was not actually verified")
	}
	checked := map[string]bool{}
	for _, component := range components {
		checked[component.Component] = true
	}
	for _, required := range IsolatedComponents() {
		if !checked[required] {
			return nil, fmt.Errorf("component %q was not checked for isolation", required)
		}
	}

	absoluteEvaluator, err := filepath.Abs(evaluatorRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve evaluator root: %w", err)
	}
	var violations []EvaluatorReachabilityViolation
	for _, component := range components {
		for _, path := range component.ReachablePaths {
			absolute, absErr := filepath.Abs(path)
			if absErr != nil {
				continue
			}
			if withinDirectory(absoluteEvaluator, absolute) {
				violations = append(violations, EvaluatorReachabilityViolation{
					Component: component.Component, Path: filepath.ToSlash(path),
				})
			}
		}
	}
	sort.Slice(violations, func(left, right int) bool {
		if violations[left].Component != violations[right].Component {
			return violations[left].Component < violations[right].Component
		}
		return violations[left].Path < violations[right].Path
	})
	return violations, nil
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

// DatabaseAllocation is one track's storage (M24-105, M24-106).
type DatabaseAllocation struct {
	Track string
	// CodefluxDatabase is the coordinator's runtime database.
	CodefluxDatabase string
	// ApplicationDatabase is ReserveFlow's own database.
	ApplicationDatabase string
}

// Validate rejects an allocation that would let two things share state.
//
// The two databases must be separate files. Sharing one would let the
// application's data influence the coordinator's — or, worse, let a reset of
// one silently reset the other.
func (allocation DatabaseAllocation) Validate() error {
	switch {
	case strings.TrimSpace(allocation.Track) == "":
		return errors.New("an allocation requires a track")
	case strings.TrimSpace(allocation.CodefluxDatabase) == "":
		return fmt.Errorf("track %q has no CodeFlux database", allocation.Track)
	case strings.TrimSpace(allocation.ApplicationDatabase) == "":
		return fmt.Errorf("track %q has no application database", allocation.Track)
	}
	if filepath.Clean(allocation.CodefluxDatabase) ==
		filepath.Clean(allocation.ApplicationDatabase) {
		return fmt.Errorf(
			"track %q shares one file between the coordinator and the application",
			allocation.Track)
	}
	return nil
}

// ValidateAllocations checks every track has its own pair (M24-105).
func ValidateAllocations(allocations []DatabaseAllocation) error {
	if len(allocations) == 0 {
		return errors.New("no database allocations were declared")
	}
	seen := map[string]string{}
	for _, allocation := range allocations {
		if err := allocation.Validate(); err != nil {
			return err
		}
		for _, path := range []string{
			allocation.CodefluxDatabase, allocation.ApplicationDatabase,
		} {
			cleaned := filepath.Clean(path)
			if owner, taken := seen[cleaned]; taken {
				return fmt.Errorf(
					"tracks %q and %q share a database; a fresh database per track is what "+
						"makes their results comparable", owner, allocation.Track)
			}
			seen[cleaned] = allocation.Track
		}
	}
	return nil
}

// ResetPlan is what a track reset does (M24-107).
type ResetPlan struct {
	// RestoreCommit is the exact accepted commit to return to.
	RestoreCommit string
	// RemovePaths are the run-scoped application state to clear.
	RemovePaths []string
	// FreshCodefluxDatabase is the new coordinator database to create.
	FreshCodefluxDatabase string
	// PreservePaths must survive the reset.
	PreservePaths []string
}

// Validate rejects a reset that would destroy something it should not.
func (plan ResetPlan) Validate() error {
	if err := validateHex(plan.RestoreCommit, 40, "reset restore commit"); err != nil {
		return err
	}
	if len(plan.RemovePaths) == 0 {
		return errors.New(
			"a reset that removes nothing leaves run-scoped state behind, and the next " +
				"run starts from a state nobody chose")
	}
	if strings.TrimSpace(plan.FreshCodefluxDatabase) == "" {
		return errors.New("a reset must create a fresh CodeFlux database")
	}
	// A reset must never remove something it also promises to preserve.
	for _, removal := range plan.RemovePaths {
		for _, preserved := range plan.PreservePaths {
			if filepath.Clean(removal) == filepath.Clean(preserved) {
				return fmt.Errorf("the reset both removes and preserves %q", removal)
			}
		}
		// Removing the whole repository would destroy the accepted commit
		// chain the trial depends on.
		if filepath.Clean(removal) == "." || removal == "" {
			return errors.New("the reset would remove the whole repository")
		}
	}
	return nil
}

// RunManifest freezes everything that could change a result (M24-108).
//
// A trial comparing two tracks is only a comparison if everything except the
// thing under test is identical. Each field here is something that has
// silently changed a benchmark result before.
type RunManifest struct {
	GoVersion         string
	DependencyLock    string
	OperatingSystem   string
	Architecture      string
	CodefluxVersion   string
	ProviderName      string
	ModelIdentity     string
	ReasoningEffort   string
	ToolSchemaVersion string
	PricingSnapshot   string
	ValidationPolicy  string
	RoutingPolicy     string
	FrozenAt          time.Time
}

// ManifestFields returns the field names a manifest must populate, so a
// missing one is named rather than merely absent.
func ManifestFields() []string {
	return []string{
		"go-version", "dependency-lock", "operating-system", "architecture",
		"codeflux-version", "provider", "model", "reasoning-effort",
		"tool-schema-version", "pricing-snapshot", "validation-policy",
		"routing-policy",
	}
}

// Validate rejects an incomplete manifest.
func (manifest RunManifest) Validate() error {
	values := map[string]string{
		"go-version": manifest.GoVersion, "dependency-lock": manifest.DependencyLock,
		"operating-system": manifest.OperatingSystem, "architecture": manifest.Architecture,
		"codeflux-version": manifest.CodefluxVersion, "provider": manifest.ProviderName,
		"model": manifest.ModelIdentity, "reasoning-effort": manifest.ReasoningEffort,
		"tool-schema-version": manifest.ToolSchemaVersion,
		"pricing-snapshot":    manifest.PricingSnapshot,
		"validation-policy":   manifest.ValidationPolicy,
		"routing-policy":      manifest.RoutingPolicy,
	}
	var missing []string
	for _, field := range ManifestFields() {
		if strings.TrimSpace(values[field]) == "" {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf(
			"the run manifest does not freeze %s; anything unfrozen can change a result "+
				"without anyone noticing", strings.Join(missing, ", "))
	}
	if manifest.FrozenAt.IsZero() {
		return errors.New("the run manifest has no freeze time")
	}
	return nil
}

// Identity is the manifest's content identity, so two tracks can be shown to
// have run under the same conditions.
func (manifest RunManifest) Identity() string {
	fields := []string{
		manifest.GoVersion, manifest.DependencyLock, manifest.OperatingSystem,
		manifest.Architecture, manifest.CodefluxVersion, manifest.ProviderName,
		manifest.ModelIdentity, manifest.ReasoningEffort, manifest.ToolSchemaVersion,
		manifest.PricingSnapshot, manifest.ValidationPolicy, manifest.RoutingPolicy,
	}
	digest := sha256.Sum256([]byte(strings.Join(fields, "\x00")))
	return hex.EncodeToString(digest[:])
}

func validateHex(value string, length int, label string) error {
	if len(value) != length {
		return fmt.Errorf("%s must be %d hex characters, got %d", label, length, len(value))
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("%s must be hexadecimal", label)
	}
	if strings.ToLower(value) != value {
		return fmt.Errorf("%s must be lower case", label)
	}
	return nil
}
