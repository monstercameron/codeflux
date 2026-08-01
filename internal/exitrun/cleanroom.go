// Package exitrun is the prototype exit protocol: the clean-room conditions,
// the journeys an evaluator walks, the scorecard, and the decision
// (M24-001..100).
//
// It exists as code rather than as a document because an exit run is worth
// exactly as much as its conditions are verifiable. A protocol nobody can
// check is a protocol that quietly drifts toward the result its author wanted.
package exitrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// PreconditionID names one clean-room requirement (M24-001..009).
type PreconditionID string

const (
	PreCleanProfile       PreconditionID = "clean-profile"
	PrePackagedArtifact   PreconditionID = "packaged-artifact"
	PreNoDevelopmentLeak  PreconditionID = "no-development-configuration"
	PreNoPriorDatabase    PreconditionID = "no-prior-database"
	PreProviderConfigured PreconditionID = "provider-configured"
	PreFrozenRepository   PreconditionID = "frozen-repository"
	PreFrozenRevision     PreconditionID = "frozen-revision"
	PreHiddenTestsSealed  PreconditionID = "hidden-tests-sealed"
	PreObservationStarted PreconditionID = "observation-started"
)

// AllPreconditions returns every requirement, in the order it must be checked.
//
// The order matters: verifying a frozen revision inside a profile that still
// carries development configuration proves nothing about the run.
func AllPreconditions() []PreconditionID {
	return []PreconditionID{
		PreCleanProfile,
		PrePackagedArtifact,
		PreNoDevelopmentLeak,
		PreNoPriorDatabase,
		PreProviderConfigured,
		PreFrozenRepository,
		PreFrozenRevision,
		PreHiddenTestsSealed,
		PreObservationStarted,
	}
}

// Precondition is one clean-room requirement.
type Precondition struct {
	ID   PreconditionID
	Todo string
	// Requirement is what must be true.
	Requirement string
	// WhyItMatters says what an exit run would falsely conclude without it.
	// A precondition whose absence changes no conclusion is not a
	// precondition, it is a preference.
	WhyItMatters string
	// Blocking marks a requirement the run cannot proceed without.
	Blocking bool
}

// Preconditions returns the declared clean-room requirements (M24-001..009).
func Preconditions() []Precondition {
	return []Precondition{
		{
			ID: PreCleanProfile, Todo: "M24-001", Blocking: true,
			Requirement: "the run happens in a user profile or virtual machine that has " +
				"never run CodeFlux",
			WhyItMatters: "a profile that has run it before carries settings, credentials, " +
				"and habits the evaluator did not choose, so a smooth run may be measuring " +
				"the previous setup rather than the product",
		},
		{
			ID: PrePackagedArtifact, Todo: "M24-002", Blocking: true,
			Requirement: "the artifact under test is a packaged release, verified by checksum " +
				"and signature, not a build from the working tree",
			WhyItMatters: "a working-tree build can contain changes nobody reviewed and " +
				"assets nobody packaged, so the run would test something that will never " +
				"be shipped",
		},
		{
			ID: PreNoDevelopmentLeak, Todo: "M24-003", Blocking: true,
			Requirement: "no development environment variable, configuration file, or PATH " +
				"entry from the development machine is visible to the run",
			WhyItMatters: "a leaked development setting can supply a credential, a database " +
				"path, or a feature switch the real user would not have, which turns a " +
				"failure into a pass",
		},
		{
			ID: PreNoPriorDatabase, Todo: "M24-004", Blocking: true,
			Requirement: "no CodeFlux database exists anywhere the run could find one",
			WhyItMatters: "an existing database skips the first-run journey entirely, " +
				"which is one of the things the exit run is supposed to measure",
		},
		{
			ID: PreProviderConfigured, Todo: "M24-005", Blocking: true,
			Requirement: "exactly one provider is configured, through the documented journey " +
				"and no other way",
			WhyItMatters: "configuring it by hand tests a path no user takes, and hides " +
				"whatever is wrong with the path they do take",
		},
		{
			ID: PreFrozenRepository, Todo: "M24-006", Blocking: true,
			Requirement: "the demonstration repository is a copy of the frozen one, with no " +
				"local modification",
			WhyItMatters: "a modified repository makes the run unrepeatable, and makes any " +
				"comparison with a baseline meaningless",
		},
		{
			ID: PreFrozenRevision, Todo: "M24-007", Blocking: true,
			Requirement: "the repository is checked out at the exact frozen base revision",
			WhyItMatters: "a different revision changes what the tasks are, so two runs " +
				"would not be measuring the same work",
		},
		{
			ID: PreHiddenTestsSealed, Todo: "M24-008", Blocking: true,
			Requirement: "the hidden acceptance tests are not present in, reachable from, or " +
				"referenced by anything the agent can read",
			WhyItMatters: "an agent that can read the acceptance tests can satisfy them " +
				"without solving the problem, which is the single easiest way for an exit " +
				"run to produce a result that means nothing",
		},
		{
			ID: PreObservationStarted, Todo: "M24-009", Blocking: false,
			Requirement: "screen recording or structured observer notes are running, where " +
				"the benchmark protocol allows it",
			WhyItMatters: "without a record, a surprise during the run is reconstructed from " +
				"memory afterwards, and the details that mattered are the ones that get lost",
		},
	}
}

// Validate rejects a precondition that could not be checked or acted on.
func (precondition Precondition) Validate() error {
	switch {
	case !slices.Contains(AllPreconditions(), precondition.ID):
		return fmt.Errorf("unknown precondition %q", precondition.ID)
	case !strings.HasPrefix(precondition.Todo, "M24-"):
		return fmt.Errorf("precondition %q cites %q, want an M24 TODO",
			precondition.ID, precondition.Todo)
	case strings.TrimSpace(precondition.Requirement) == "":
		return fmt.Errorf("precondition %q states no requirement", precondition.ID)
	case strings.TrimSpace(precondition.WhyItMatters) == "":
		return fmt.Errorf(
			"precondition %q does not say what an exit run would wrongly conclude without it",
			precondition.ID)
	}
	return nil
}

// ValidatePreconditions checks the declared set covers M24-001..009.
func ValidatePreconditions() error {
	declared := Preconditions()
	if len(declared) != len(AllPreconditions()) {
		return fmt.Errorf("%d preconditions declared for %d ids",
			len(declared), len(AllPreconditions()))
	}
	todos := map[string]PreconditionID{}
	seen := map[PreconditionID]bool{}
	for index, precondition := range declared {
		if err := precondition.Validate(); err != nil {
			return err
		}
		if seen[precondition.ID] {
			return fmt.Errorf("precondition %q is declared twice", precondition.ID)
		}
		seen[precondition.ID] = true
		if precondition.ID != AllPreconditions()[index] {
			return fmt.Errorf("precondition %d is %q, the order declares %q",
				index, precondition.ID, AllPreconditions()[index])
		}
		if other, clash := todos[precondition.Todo]; clash {
			return fmt.Errorf("preconditions %q and %q both claim %s",
				other, precondition.ID, precondition.Todo)
		}
		todos[precondition.Todo] = precondition.ID
	}
	for number := 1; number <= 9; number++ {
		todo := fmt.Sprintf("M24-%03d", number)
		if _, ok := todos[todo]; !ok {
			return fmt.Errorf("no precondition claims %s", todo)
		}
	}
	return nil
}

// CleanRoom is the environment an exit run is checked against.
type CleanRoom struct {
	// ProfileDirectory is the user profile the run happens in.
	ProfileDirectory string
	// DatabaseSearchPaths are every location a CodeFlux database could be
	// found. All are checked: finding none in the expected place while one
	// sits somewhere else would be a false clean bill.
	DatabaseSearchPaths []string
	// RepositoryRoot is the demonstration repository.
	RepositoryRoot string
	// ExpectedRevision is the frozen base revision.
	ExpectedRevision string
	// HiddenTestPaths are the acceptance tests that must not be reachable.
	HiddenTestPaths []string
	// ArtifactVerified records that the packaged artifact passed checksum and
	// signature verification.
	ArtifactVerified bool
	// ConfiguredProviders is what the documented journey configured.
	ConfiguredProviders []string
	// ObservationActive records whether a recording or notes are running.
	ObservationActive bool
	// Environment is the environment the run will see.
	Environment map[string]string
}

// DevelopmentEnvironmentPrefixes are the variables that would leak a
// development machine's setup into a clean run (M24-003).
func DevelopmentEnvironmentPrefixes() []string {
	return []string{
		"CODEFLUX_", "GOFLAGS", "GOPATH", "GOROOT", "GOCACHE",
		"PLAYWRIGHT_", "STATICCHECK_",
	}
}

// CheckResult is one precondition's verdict.
type CheckResult struct {
	ID PreconditionID
	// Satisfied is whether the requirement holds.
	Satisfied bool
	// Detail says what was found.
	Detail string
	// Blocking mirrors the precondition, so a caller need not look it up.
	Blocking bool
}

// Report is the complete clean-room verdict.
type Report struct {
	Results []CheckResult
	// Blocking counts unsatisfied blocking preconditions.
	Blocking int
}

// Ready reports whether the exit run may begin.
func (report Report) Ready() bool { return report.Blocking == 0 }

// Unsatisfied returns every requirement that did not hold.
func (report Report) Unsatisfied() []CheckResult {
	var failures []CheckResult
	for _, result := range report.Results {
		if !result.Satisfied {
			failures = append(failures, result)
		}
	}
	return failures
}

// Verify checks every clean-room precondition (M24-001..009).
func Verify(ctx context.Context, room CleanRoom, revisionOf func(context.Context, string) (string, error)) (Report, error) {
	if err := ValidatePreconditions(); err != nil {
		return Report{}, err
	}
	report := Report{}
	for _, precondition := range Preconditions() {
		result := CheckResult{ID: precondition.ID, Blocking: precondition.Blocking}
		switch precondition.ID {
		case PreCleanProfile:
			result.Satisfied, result.Detail = checkCleanProfile(room)
		case PrePackagedArtifact:
			result.Satisfied = room.ArtifactVerified
			result.Detail = "the artifact was verified by checksum and signature"
			if !result.Satisfied {
				result.Detail = "the artifact under test was not verified as a packaged release"
			}
		case PreNoDevelopmentLeak:
			result.Satisfied, result.Detail = checkNoDevelopmentLeak(room)
		case PreNoPriorDatabase:
			result.Satisfied, result.Detail = checkNoPriorDatabase(room)
		case PreProviderConfigured:
			result.Satisfied = len(room.ConfiguredProviders) == 1
			result.Detail = fmt.Sprintf("%d provider(s) configured",
				len(room.ConfiguredProviders))
		case PreFrozenRepository:
			result.Satisfied, result.Detail = checkRepositoryPresent(room)
		case PreFrozenRevision:
			result.Satisfied, result.Detail = checkFrozenRevision(ctx, room, revisionOf)
		case PreHiddenTestsSealed:
			result.Satisfied, result.Detail = checkHiddenTestsSealed(room)
		case PreObservationStarted:
			result.Satisfied = room.ObservationActive
			result.Detail = "observation is running"
			if !result.Satisfied {
				result.Detail = "no recording or observer notes are running"
			}
		}
		if !result.Satisfied && result.Blocking {
			report.Blocking++
		}
		report.Results = append(report.Results, result)
	}
	return report, nil
}

func checkCleanProfile(room CleanRoom) (bool, string) {
	if strings.TrimSpace(room.ProfileDirectory) == "" {
		return false, "no profile directory was supplied"
	}
	// A profile that already holds CodeFlux state is not clean, whatever it is
	// called.
	marker := filepath.Join(room.ProfileDirectory, "codeflux")
	if _, err := os.Stat(marker); err == nil {
		return false, "the profile already contains CodeFlux state"
	}
	return true, "the profile has never run CodeFlux"
}

func checkNoDevelopmentLeak(room CleanRoom) (bool, string) {
	var leaked []string
	for name := range room.Environment {
		upper := strings.ToUpper(name)
		for _, prefix := range DevelopmentEnvironmentPrefixes() {
			if strings.HasPrefix(upper, prefix) {
				leaked = append(leaked, name)
				break
			}
		}
	}
	if len(leaked) == 0 {
		return true, "no development configuration is visible to the run"
	}
	sort.Strings(leaked)
	return false, "development configuration is visible: " + strings.Join(leaked, ", ")
}

func checkNoPriorDatabase(room CleanRoom) (bool, string) {
	if len(room.DatabaseSearchPaths) == 0 {
		// Searching nowhere and finding nothing is not evidence.
		return false, "no locations were searched for an existing database"
	}
	var found []string
	for _, path := range room.DatabaseSearchPaths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.IsDir() || info.Size() == 0 {
			continue
		}
		found = append(found, filepath.Base(path))
	}
	if len(found) == 0 {
		return true, fmt.Sprintf("no database exists in any of %d searched locations",
			len(room.DatabaseSearchPaths))
	}
	sort.Strings(found)
	return false, "a CodeFlux database already exists: " + strings.Join(found, ", ")
}

func checkRepositoryPresent(room CleanRoom) (bool, string) {
	if strings.TrimSpace(room.RepositoryRoot) == "" {
		return false, "no demonstration repository was supplied"
	}
	if _, err := os.Stat(filepath.Join(room.RepositoryRoot, ".git")); err != nil {
		return false, "the demonstration repository is not a Git repository"
	}
	return true, "the demonstration repository is present"
}

func checkFrozenRevision(
	ctx context.Context,
	room CleanRoom,
	revisionOf func(context.Context, string) (string, error),
) (bool, string) {
	if strings.TrimSpace(room.ExpectedRevision) == "" {
		return false, "no frozen base revision was declared"
	}
	if revisionOf == nil {
		return false, "the repository revision could not be read"
	}
	actual, err := revisionOf(ctx, room.RepositoryRoot)
	if err != nil {
		return false, "the repository revision could not be read"
	}
	if !strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(room.ExpectedRevision)) {
		return false, "the repository is not at the frozen base revision"
	}
	return true, "the repository is at the frozen base revision"
}

// checkHiddenTestsSealed is M24-008.
//
// It checks the acceptance tests are absent from the repository the agent can
// read. Presence is checked by path AND by name anywhere in the tree: moving a
// hidden test to a different directory would defeat a path-only check while
// leaving it just as readable.
func checkHiddenTestsSealed(room CleanRoom) (bool, string) {
	if len(room.HiddenTestPaths) == 0 {
		return false, "no hidden acceptance tests were declared, so sealing cannot be verified"
	}
	if strings.TrimSpace(room.RepositoryRoot) == "" {
		return false, "no repository was supplied"
	}

	names := map[string]bool{}
	for _, path := range room.HiddenTestPaths {
		names[strings.ToLower(filepath.Base(path))] = true
		full := filepath.Join(room.RepositoryRoot, filepath.FromSlash(path))
		if _, err := os.Stat(full); err == nil {
			return false, "a hidden acceptance test is present in the repository"
		}
	}

	var found []string
	_ = filepath.WalkDir(room.RepositoryRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if names[strings.ToLower(entry.Name())] {
			relative, relErr := filepath.Rel(room.RepositoryRoot, path)
			if relErr != nil {
				relative = entry.Name()
			}
			found = append(found, filepath.ToSlash(relative))
		}
		return nil
	})
	if len(found) > 0 {
		sort.Strings(found)
		return false, "a hidden acceptance test is reachable under another path"
	}
	return true, fmt.Sprintf("none of the %d hidden acceptance tests is reachable",
		len(room.HiddenTestPaths))
}

// ErrCleanRoomNotReady reports a run attempted without its preconditions.
var ErrCleanRoomNotReady = errors.New("the clean room is not ready")

// RequireReady refuses to begin an exit run whose conditions do not hold.
//
// The refusal is the point: a run that proceeded anyway would produce a
// scorecard that looked exactly like a valid one.
func RequireReady(report Report) error {
	if report.Ready() {
		return nil
	}
	var reasons []string
	for _, result := range report.Unsatisfied() {
		if !result.Blocking {
			continue
		}
		reasons = append(reasons, fmt.Sprintf("%s: %s", result.ID, result.Detail))
	}
	sort.Strings(reasons)
	return fmt.Errorf("%w: %s", ErrCleanRoomNotReady, strings.Join(reasons, "; "))
}
