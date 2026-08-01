// Package doctor runs the local health checks and turns each failure into
// something the user can act on (M23-026..036).
//
// A check that reports "failed" without saying what to do next is a check
// nobody can use. Every result here therefore carries a remediation, and the
// package refuses to declare a check that does not have one.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// CheckID names one health check.
type CheckID string

const (
	CheckGoToolchain       CheckID = "go-toolchain"
	CheckGit               CheckID = "git"
	CheckDatabasePath      CheckID = "database-path"
	CheckDatabaseIntegrity CheckID = "database-integrity"
	CheckCredentialStore   CheckID = "credential-store"
	CheckProvider          CheckID = "provider-connectivity"
	CheckWorktreeRoot      CheckID = "worktree-root"
	CheckLoopbackPort      CheckID = "loopback-port"
	CheckTaskStates        CheckID = "task-states"
	CheckVersions          CheckID = "versions"
)

// AllCheckIDs returns every declared check, in the order they are run.
//
// The order is cheapest-and-most-fundamental first: a missing Git makes every
// later result uninteresting, so it is reported before anything is opened.
func AllCheckIDs() []CheckID {
	return []CheckID{
		CheckVersions,
		CheckGoToolchain,
		CheckGit,
		CheckDatabasePath,
		CheckDatabaseIntegrity,
		CheckCredentialStore,
		CheckProvider,
		CheckWorktreeRoot,
		CheckLoopbackPort,
		CheckTaskStates,
	}
}

// Status is the outcome of one check.
type Status string

const (
	// StatusOK means the prerequisite is present and usable.
	StatusOK Status = "ok"
	// StatusMissing means it is absent. This is distinct from failed: nothing
	// is broken, something has not been set up.
	StatusMissing Status = "missing"
	// StatusDegraded means it works but not well: an old version, low disk,
	// a schema older than the binary expects.
	StatusDegraded Status = "degraded"
	// StatusFailed means it is present and does not work.
	StatusFailed Status = "failed"
	// StatusUnknown means the check could not run. It is reported rather than
	// coerced into ok or failed, because guessing is how a doctor becomes
	// something people stop trusting.
	StatusUnknown Status = "unknown"
)

// Blocking reports whether this status prevents work.
func (status Status) Blocking() bool {
	switch status {
	case StatusOK, StatusDegraded:
		return false
	case StatusMissing, StatusFailed, StatusUnknown:
		return true
	default:
		return true
	}
}

// Valid reports whether a status is one of the declared set.
func (status Status) Valid() bool {
	switch status {
	case StatusOK, StatusMissing, StatusDegraded, StatusFailed, StatusUnknown:
		return true
	default:
		return false
	}
}

// Result is one check's outcome (M23-036).
type Result struct {
	ID     CheckID
	Todo   string
	Status Status
	// Summary is one line: what was found.
	Summary string
	// Detail carries the specifics, and never a credential or a raw path from
	// a user's machine beyond what they already supplied.
	Detail string
	// Remediation is what to do about it. It is required for anything that is
	// not ok, because a failure with no next step is a dead end.
	Remediation string
	// Duration is how long the check took, so a doctor run that hangs can be
	// attributed to the check responsible.
	Duration time.Duration
}

// Validate rejects a result that cannot be acted on.
func (result Result) Validate() error {
	switch {
	case !slices.Contains(AllCheckIDs(), result.ID):
		return fmt.Errorf("unknown check %q", result.ID)
	case !result.Status.Valid():
		return fmt.Errorf("check %q reported unknown status %q", result.ID, result.Status)
	case strings.TrimSpace(result.Summary) == "":
		return fmt.Errorf("check %q reported no summary", result.ID)
	case !strings.HasPrefix(result.Todo, "M23-"):
		return fmt.Errorf("check %q cites %q, want an M23 TODO", result.ID, result.Todo)
	}
	// M23-036: anything other than ok must say what to do next.
	if result.Status != StatusOK && strings.TrimSpace(result.Remediation) == "" {
		return fmt.Errorf(
			"check %q reported %q with no remediation; a failure with no next step is a dead end",
			result.ID, result.Status)
	}
	if result.Duration < 0 {
		return fmt.Errorf("check %q reported a negative duration", result.ID)
	}
	return nil
}

// Check is one runnable health check.
type Check struct {
	ID   CheckID
	Todo string
	// Title is what the user sees before the result.
	Title string
	// Run performs the check. It returns a Result rather than an error,
	// because "this check could not run" is itself a result the user needs.
	Run func(context.Context, Input) Result
}

// Input is everything a check may inspect. It is a struct of values and
// functions rather than concrete dependencies, so every check is testable
// without a database, a network, or a credential store.
type Input struct {
	DatabasePath  string
	WorktreeRoot  string
	ListenAddress string

	// GoVersion returns the running toolchain version.
	GoVersion func() string
	// GitVersion returns the installed Git version, or an error if absent.
	GitVersion func(context.Context) (string, error)
	// PathWritable reports whether a directory can be written to.
	PathWritable func(string) error
	// DatabaseHealth reports integrity and schema compatibility.
	DatabaseHealth func(context.Context, string) (DatabaseHealth, error)
	// CredentialStore reports the platform store's availability.
	CredentialStore func() (bool, string)
	// ProviderReachable reports whether a configured provider answers. It must
	// never receive or return credential material.
	ProviderReachable func(context.Context) ([]ProviderReachability, error)
	// DiskFree reports free bytes at a path.
	DiskFree func(string) (uint64, error)
	// PortBindable reports whether the address can be bound.
	PortBindable func(string) error
	// TaskCounts reports active and recovery-required tasks.
	TaskCounts func(context.Context) (TaskCounts, error)
	// Versions reports application and migration versions.
	Versions func() Versions
}

// DatabaseHealth is what a database check learns.
type DatabaseHealth struct {
	IntegrityOK            bool
	SchemaVersion          int
	SupportedSchemaVersion int
	FailedMigrations       uint64
}

// ProviderReachability is one provider's connectivity result (M23-031).
//
// It deliberately carries no credential and no endpoint: a doctor output is
// routinely pasted into a chat, and an endpoint plus a failure mode is more
// than a user intends to share.
type ProviderReachability struct {
	Name string
	// Reachable is whether the provider answered at all.
	Reachable bool
	// Authorized is whether the credential was accepted. It is separate from
	// Reachable because "no network" and "wrong key" need different fixes.
	Authorized bool
	// FailureKind is an enumerated reason, never a raw error string.
	FailureKind string
}

// TaskCounts is the durable task situation (M23-034).
type TaskCounts struct {
	Active           int
	RecoveryRequired int
	Paused           int
}

// Versions is the identity a doctor reports (M23-035).
type Versions struct {
	Application            string
	Commit                 string
	SchemaVersion          int
	SupportedSchemaVersion int
	AppliedMigrations      uint64
}

// MinimumFreeBytes is the free space below which the worktree root is degraded.
//
// A task worktree is a full checkout plus build output. Half a gigabyte is
// where an ordinary Go repository starts failing in ways that look like
// unrelated build errors rather than like a full disk.
const MinimumFreeBytes uint64 = 512 << 20

// Report is a complete doctor run.
type Report struct {
	Results []Result
	// Blocking is the count of results that prevent work.
	Blocking int
	// Ran is when the report was produced.
	Ran time.Time
}

// Healthy reports whether nothing blocks work.
func (report Report) Healthy() bool { return report.Blocking == 0 }

// FirstBlocking returns the first result preventing work, which is the one a
// user should address first.
func (report Report) FirstBlocking() (Result, bool) {
	for _, result := range report.Results {
		if result.Status.Blocking() {
			return result, true
		}
	}
	return Result{}, false
}

// Run executes every declared check in order.
func Run(ctx context.Context, input Input, now func() time.Time) (Report, error) {
	if now == nil {
		return Report{}, errors.New("a doctor run requires a clock")
	}
	report := Report{Ran: now()}
	for _, id := range AllCheckIDs() {
		check, ok := CheckFor(id)
		if !ok {
			return Report{}, fmt.Errorf("check %q is ordered but not declared", id)
		}
		started := now()
		result := check.Run(ctx, input)
		result.ID = check.ID
		result.Todo = check.Todo
		if result.Duration == 0 {
			result.Duration = now().Sub(started)
		}
		if err := result.Validate(); err != nil {
			return Report{}, err
		}
		if result.Status.Blocking() {
			report.Blocking++
		}
		report.Results = append(report.Results, result)
	}
	return report, nil
}

// CheckFor returns one declared check.
func CheckFor(id CheckID) (Check, bool) {
	for _, check := range Checks() {
		if check.ID == id {
			return check, true
		}
	}
	return Check{}, false
}

// ValidateChecks verifies the declared set covers M23-026..035 exactly.
func ValidateChecks() error {
	checks := Checks()
	if len(checks) != len(AllCheckIDs()) {
		return fmt.Errorf("%d checks are declared for %d ids", len(checks), len(AllCheckIDs()))
	}
	todos := map[string]CheckID{}
	seen := map[CheckID]bool{}
	for _, check := range checks {
		if !slices.Contains(AllCheckIDs(), check.ID) {
			return fmt.Errorf("check %q is declared but not ordered", check.ID)
		}
		if seen[check.ID] {
			return fmt.Errorf("check %q is declared twice", check.ID)
		}
		seen[check.ID] = true
		if check.Run == nil {
			return fmt.Errorf("check %q has no implementation", check.ID)
		}
		if strings.TrimSpace(check.Title) == "" {
			return fmt.Errorf("check %q has no title", check.ID)
		}
		if other, clash := todos[check.Todo]; clash {
			return fmt.Errorf("checks %q and %q both claim %s", other, check.ID, check.Todo)
		}
		todos[check.Todo] = check.ID
	}
	for number := 26; number <= 35; number++ {
		todo := fmt.Sprintf("M23-%03d", number)
		if _, ok := todos[todo]; !ok {
			return fmt.Errorf("no check claims %s", todo)
		}
	}
	return nil
}

// CheckTodos returns the cited TODOs, sorted.
func CheckTodos() []string {
	todos := make([]string, 0, len(Checks()))
	for _, check := range Checks() {
		todos = append(todos, check.Todo)
	}
	sort.Strings(todos)
	return todos
}
