package dogfood

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Task is one chronological ReserveFlow task and the independent verdict that
// judges it (M24-131..160).
//
// Each task is a pair of TODOs: one runs it, one accepts or rejects it. The
// pairing is the point — a task with no independent verdict is a task the
// system grades itself on.
type Task struct {
	// Ordinal is the packet this task implements.
	Ordinal int
	// RunTodo is the TODO that runs it.
	RunTodo string
	// VerdictTodo is the TODO that independently judges it.
	VerdictTodo string
	// Summary is what the task builds.
	Summary string
	// AcceptanceCases are the situations the independent verdict must check.
	// A case here is a way the work can be wrong while looking right.
	AcceptanceCases []string
	// WithheldFromAgent is information deliberately not supplied, where the
	// task's difficulty depends on the agent working something out.
	WithheldFromAgent string
}

// Tasks returns the chronological ReserveFlow sequence (M24-131..160).
func Tasks() []Task {
	return []Task{
		{
			Ordinal: 1, RunTodo: "M24-131", VerdictTodo: "M24-132",
			Summary: "server lifecycle, health, readiness, request identifiers, JSON " +
				"behaviour, and safe errors",
			AcceptanceCases: []string{
				"a port already in use is reported rather than silently retried",
				"cancellation stops the server without leaving a listener",
				"a termination signal shuts down cleanly",
				"a malformed path returns a safe error with no internal detail",
				"health is deterministic and does not depend on wall time",
			},
		},
		{
			Ordinal: 2, RunTodo: "M24-133", VerdictTodo: "M24-134",
			Summary: "SQLite resource persistence, capacity validation, stable identity, " +
				"timestamps, and bounded cursor pagination",
			AcceptanceCases: []string{
				"a clean migration produces the expected schema",
				"data survives a restart",
				"an invalid capacity is refused rather than clamped",
				"ordering is stable across equal timestamps",
				"a cursor does not skip or repeat across an insert",
				"a duplicate request does not create a second row",
			},
		},
		{
			Ordinal: 3, RunTodo: "M24-135", VerdictTodo: "M24-136",
			Summary: "atomic pending-reservation creation with capacity decrement",
			AcceptanceCases: []string{
				"an invalid quantity is refused",
				"an unknown resource is refused",
				"insufficient capacity is refused rather than oversubscribed",
				"a failure rolls back the decrement completely",
				"the error shape is stable and carries no internal detail",
			},
		},
		{
			Ordinal: 4, RunTodo: "M24-137", VerdictTodo: "M24-138",
			Summary: "canonical-request idempotency, original-response replay, key expiry, " +
				"and semantic-input conflict",
			AcceptanceCases: []string{
				"reordered JSON fields are the same canonical request",
				"two concurrent calls with one key produce one reservation",
				"a transport retry replays the original response, not a new one",
				"an expired key is treated as a new request",
				"the same key with different input is a conflict, not a replay",
			},
		},
		{
			Ordinal: 5, RunTodo: "M24-139", VerdictTodo: "M24-140",
			Summary: "expected-version confirm and cancel transitions with explicit " +
				"repeated-request semantics",
			AcceptanceCases: []string{
				"a valid transition succeeds",
				"a stale expected version is refused distinguishably",
				"a repeated request has stated, consistent semantics",
				"a forbidden transition is refused",
				"cancelling releases capacity exactly once",
				"a confirm racing a cancel resolves to one outcome, not both",
			},
		},
		{
			Ordinal: 6, RunTodo: "M24-141", VerdictTodo: "M24-142",
			Summary: "concurrent capacity safety across reservation creation and cancellation",
			AcceptanceCases: []string{
				"no oversubscription under in-process contention",
				"no oversubscription under multi-process contention",
				"capacity never goes negative",
				"no lost update between concurrent writers",
				"no duplicate reservation from one request",
				"no deadlock under sustained contention",
			},
		},
		{
			Ordinal: 7, RunTodo: "M24-143", VerdictTodo: "M24-144",
			Summary: "deterministic expiration, exact-once capacity release, worker " +
				"ownership, bounded scans, shutdown, and restart",
			AcceptanceCases: []string{
				"expiry happens at the boundary, not before or after",
				"multiple workers release capacity exactly once between them",
				"an injected crash mid-expiry does not double-release",
				"a repeated scan does not re-expire an already expired reservation",
				"shutdown during a scan leaves no partial state",
				"a confirmation arriving after expiry is refused, not honoured",
			},
		},
		{
			Ordinal: 8, RunTodo: "M24-145", VerdictTodo: "M24-146",
			Summary: "transactional outbox creation, bounded polling, ordering, and " +
				"publish-state transitions",
			AcceptanceCases: []string{
				"a rolled-back transaction leaves no outbox event",
				"duplicate polling does not claim one event twice",
				"a restart resumes without losing or repeating an event",
				"a poison event does not block the queue forever",
				"ordering within a resource is preserved",
				"one state transition produces exactly one event",
			},
		},
		{
			Ordinal: 9, RunTodo: "M24-147", VerdictTodo: "M24-148",
			Summary: "signed webhook delivery, stable delivery identity, bounded " +
				"retry and backoff, secret references, and disabled endpoints",
			AcceptanceCases: []string{
				"a successful delivery is attempted once",
				"an ambiguous outcome is not retried blindly",
				"a connection failure is retried with backoff",
				"a terminal 4xx stops retrying",
				"a retryable 5xx retries within the bound",
				"a duplicate receipt is detectable by delivery identity",
				"the signature verifies at the receiver",
				"a disabled endpoint receives nothing",
				"delivery output is bounded",
				"no secret appears in any delivery or its logs",
			},
		},
		{
			Ordinal: 10, RunTodo: "M24-149", VerdictTodo: "M24-150",
			Summary: "API-key authorization of administrative operations, with explicit " +
				"policy for reservation operations",
			AcceptanceCases: []string{
				"a missing key is refused distinguishably",
				"a malformed key is refused distinguishably",
				"an invalid key is refused distinguishably",
				"a revoked key is refused distinguishably",
				"a scope mismatch is refused with a different status than a bad key",
				"key comparison is constant time",
				"no key material reaches any log",
				"no error message reveals which key exists",
				"no capability leaks through an unauthenticated path",
			},
		},
		{
			Ordinal: 11, RunTodo: "M24-151", VerdictTodo: "M24-152",
			Summary: "correlated structured logs, stable error codes, local metrics, " +
				"readiness dependencies, and redacted diagnostics",
			AcceptanceCases: []string{
				"a request can be traced end to end by its identifier",
				"database activity is correlated to the request that caused it",
				"worker activity is correlated to the reservation it acted on",
				"webhook activity is correlated to the event it delivered",
				"no request or response body appears in a log",
				"no secret appears in a log or a diagnostic",
			},
		},
		{
			Ordinal: 12, RunTodo: "M24-153", VerdictTodo: "M24-154",
			Summary: "an OpenAPI contract describing only implemented behaviour, with " +
				"examples, errors, pagination, idempotency, and concurrency",
			AcceptanceCases: []string{
				"every described path exists in the runtime",
				"every runtime path is described",
				"request and response schemas match",
				"status codes match",
				"pagination is described as implemented",
				"idempotency is described as implemented",
				"concurrency headers are described as implemented",
				"the contract claims no guarantee the implementation cannot keep",
			},
		},
		{
			Ordinal: 13, RunTodo: "M24-155", VerdictTodo: "M24-156",
			Summary: "diagnose and fix a frozen defect",
			WithheldFromAgent: "the defect's root cause; the agent is given the frozen " +
				"defective revision and a symptom, and must find the cause itself",
			AcceptanceCases: []string{
				"a regression test reproducing the defect exists and failed before the fix",
				"the fix is behaviourally correct rather than symptom-suppressing",
				"the race suite still passes",
				"every prior task's suite still passes",
			},
		},
		{
			Ordinal: 14, RunTodo: "M24-157", VerdictTodo: "M24-158",
			Summary: "a domain-rule change after project memory has formed",
			WithheldFromAgent: "the list of affected files; the change's difficulty is " +
				"finding everything it touches, and supplying the list removes the task",
			AcceptanceCases: []string{
				"state transitions reflect the new rule",
				"capacity accounting reflects the new rule",
				"the HTTP contract reflects the new rule",
				"the outbox reflects the new rule",
				"webhooks reflect the new rule",
				"tests were updated rather than deleted",
				"documentation was updated",
				"the graph shows the change's reach",
				"evidence from before the change was invalidated",
				"memory items that depended on the old rule were invalidated",
			},
		},
		{
			Ordinal: 15, RunTodo: "M24-159", VerdictTodo: "M24-160",
			Summary: "a dependency upgrade with a backwards-compatible schema addition",
			AcceptanceCases: []string{
				"the migration applies exactly once",
				"no existing row is lost or altered",
				"the existing API keeps working unchanged",
				"the new dependency version is bound and recorded",
				"cached artifacts built against the old version are no longer eligible",
				"evidence produced before the upgrade was invalidated",
			},
		},
	}
}

// ValidateTasks checks the sequence covers M24-131..160 exactly.
func ValidateTasks() error {
	tasks := Tasks()
	if len(tasks) != PacketCount {
		return fmt.Errorf("%d tasks are declared for %d packets", len(tasks), PacketCount)
	}
	todos := map[string]bool{}
	for index, task := range tasks {
		if err := task.Validate(); err != nil {
			return err
		}
		if task.Ordinal != index+1 {
			return fmt.Errorf("task %d is out of order", task.Ordinal)
		}
		for _, todo := range []string{task.RunTodo, task.VerdictTodo} {
			if todos[todo] {
				return fmt.Errorf("%s is claimed twice", todo)
			}
			todos[todo] = true
		}
	}
	for number := 131; number <= 160; number++ {
		todo := fmt.Sprintf("M24-%d", number)
		if !todos[todo] {
			return fmt.Errorf("no task claims %s", todo)
		}
	}
	return nil
}

// Validate rejects a task that could not be run or judged.
func (task Task) Validate() error {
	switch {
	case task.Ordinal < 1 || task.Ordinal > PacketCount:
		return fmt.Errorf("task ordinal %d is outside 1..%d", task.Ordinal, PacketCount)
	case !strings.HasPrefix(task.RunTodo, "M24-"):
		return fmt.Errorf("task %d has no run TODO", task.Ordinal)
	case !strings.HasPrefix(task.VerdictTodo, "M24-"):
		return fmt.Errorf("task %d has no verdict TODO", task.Ordinal)
	case task.RunTodo == task.VerdictTodo:
		return fmt.Errorf(
			"task %d runs and judges itself under one TODO; the pairing is what makes the "+
				"verdict independent", task.Ordinal)
	case strings.TrimSpace(task.Summary) == "":
		return fmt.Errorf("task %d has no summary", task.Ordinal)
	case len(task.AcceptanceCases) == 0:
		return fmt.Errorf(
			"task %d declares no acceptance case, so its verdict would be an opinion",
			task.Ordinal)
	}
	for index, acceptanceCase := range task.AcceptanceCases {
		if strings.TrimSpace(acceptanceCase) == "" {
			return fmt.Errorf("task %d acceptance case %d is empty", task.Ordinal, index)
		}
	}
	return nil
}

// TaskFor returns one declared task.
func TaskFor(ordinal int) (Task, error) {
	for _, task := range Tasks() {
		if task.Ordinal == ordinal {
			return task, nil
		}
	}
	return Task{}, fmt.Errorf("no task %d", ordinal)
}

// Verdict is one independent acceptance decision.
type TaskVerdict struct {
	Ordinal int
	// Accepted is the decision.
	Accepted bool
	// CaseResults maps each declared acceptance case to whether it held.
	CaseResults map[string]bool
	// Note explains the decision.
	Note string
}

// Validate rejects a verdict that did not actually judge the task.
func (verdict TaskVerdict) Validate() error {
	task, err := TaskFor(verdict.Ordinal)
	if err != nil {
		return err
	}
	if len(verdict.CaseResults) == 0 {
		return fmt.Errorf("task %d was judged with no case results", verdict.Ordinal)
	}
	var unjudged []string
	for _, acceptanceCase := range task.AcceptanceCases {
		if _, judged := verdict.CaseResults[acceptanceCase]; !judged {
			unjudged = append(unjudged, acceptanceCase)
		}
	}
	if len(unjudged) > 0 {
		sort.Strings(unjudged)
		return fmt.Errorf(
			"task %d was judged without checking %d case(s): %s",
			verdict.Ordinal, len(unjudged), strings.Join(unjudged, "; "))
	}
	// Accepting while a declared case failed is the failure mode this whole
	// structure exists to prevent.
	if verdict.Accepted {
		for acceptanceCase, held := range verdict.CaseResults {
			if !held {
				return fmt.Errorf(
					"task %d was accepted although %q did not hold",
					verdict.Ordinal, acceptanceCase)
			}
		}
	}
	if !verdict.Accepted && strings.TrimSpace(verdict.Note) == "" {
		return fmt.Errorf("task %d was rejected with no note", verdict.Ordinal)
	}
	return nil
}

// Progression is the whole chronological run.
type Progression struct {
	Verdicts []TaskVerdict
}

// Validate checks a progression advanced through the sequence properly.
func (progression Progression) Validate() error {
	if err := ValidateTasks(); err != nil {
		return err
	}
	byOrdinal := map[int]TaskVerdict{}
	for _, verdict := range progression.Verdicts {
		if err := verdict.Validate(); err != nil {
			return err
		}
		if _, duplicate := byOrdinal[verdict.Ordinal]; duplicate {
			return fmt.Errorf("task %d was judged twice", verdict.Ordinal)
		}
		byOrdinal[verdict.Ordinal] = verdict
	}
	// A task may only be attempted once its dependencies were accepted. A run
	// that skipped ahead is not chronological, and its later results describe
	// a codebase that never existed.
	packets := Packets()
	for ordinal := 1; ordinal <= PacketCount; ordinal++ {
		verdict, attempted := byOrdinal[ordinal]
		if !attempted {
			continue
		}
		for _, dependency := range packets[ordinal-1].DependsOn {
			previous, ok := byOrdinal[dependency]
			if !ok {
				return fmt.Errorf(
					"task %d was attempted although task %d it depends on was never run",
					ordinal, dependency)
			}
			if !previous.Accepted {
				return fmt.Errorf(
					"task %d was attempted although task %d it depends on was not accepted",
					ordinal, dependency)
			}
		}
		_ = verdict
	}
	return nil
}

// Completed reports how many tasks were accepted.
func (progression Progression) Completed() int {
	count := 0
	for _, verdict := range progression.Verdicts {
		if verdict.Accepted {
			count++
		}
	}
	return count
}

// FirstRejection returns the earliest rejected task.
func (progression Progression) FirstRejection() (TaskVerdict, bool) {
	rejected := TaskVerdict{Ordinal: PacketCount + 1}
	found := false
	for _, verdict := range progression.Verdicts {
		if verdict.Accepted {
			continue
		}
		if verdict.Ordinal < rejected.Ordinal {
			rejected = verdict
			found = true
		}
	}
	return rejected, found
}

// WithheldInformation returns the tasks where something was deliberately not
// supplied, and what.
//
// These are the tasks whose difficulty is the finding rather than the doing.
// Supplying the withheld information would leave a much easier task wearing
// the same name.
func WithheldInformation() (map[int]string, error) {
	withheld := map[int]string{}
	for _, task := range Tasks() {
		if strings.TrimSpace(task.WithheldFromAgent) == "" {
			continue
		}
		withheld[task.Ordinal] = task.WithheldFromAgent
	}
	if len(withheld) == 0 {
		return nil, errors.New(
			"no task withholds anything; a sequence where everything is supplied does not " +
				"test diagnosis at all")
	}
	return withheld, nil
}
