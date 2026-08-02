// Package plantrace declares the prototype's feature and deferral manifest as
// machine-readable data, so M00-G01 and M00-G02 can be checked rather than
// asserted.
//
// The plan states two rules about scope in prose: every prototype capability
// maps to a user journey and a required measurement, and no deferred feature
// has a dependency edge into that prototype journey. Prose cannot be checked.
// A table in a document drifts from the milestones that implement it, and a
// deferred item can quietly acquire a dependent without anyone noticing,
// because nothing reads the sentence forbidding it.
//
// This package is the vocabulary those rules quantify over. It follows the
// precedent of internal/pipeline: the plan explains intent, and the Go
// declaration is what the checks execute against. It stores no runtime state
// and is not a sidecar for one.
package plantrace

// Feature is one prototype capability the product ships inside the frozen
// boundary. Every field is required; the point of the manifest is that a
// capability cannot enter it without naming what it is for and how it is
// measured.
type Feature struct {
	// Name is the capability as the plan's feature-to-journey table names it.
	Name string
	// Journey is the user journey it serves.
	Journey string
	// Measurement is the required measurement that accepts it.
	Measurement string
	// Milestones are the TODO milestones that implement it. More than one is
	// normal: a journey usually crosses several.
	Milestones []string
	// DependsOn names other manifest entries this capability requires. It is
	// the field M00-G02 reads: a prototype feature naming a deferred entry is
	// a dependency edge into the prototype journey, which the plan forbids.
	DependsOn []string
}

// Deferred is one capability the plan explicitly holds back, with the gate
// that may activate it. A deferred entry is not a plan for later work; it is a
// prohibition with a named condition for lifting.
type Deferred struct {
	// ID is the TODO identifier, so the manifest and the checklist agree.
	ID string
	// Name is the capability being withheld.
	Name string
	// BranchGate is the condition the plan requires before it may become
	// active. An empty gate is rejected: a deferral nobody can lift is
	// indistinguishable from a capability nobody documented.
	BranchGate string
}

// PrototypeFeatures is the frozen prototype boundary as data. It mirrors the
// feature-to-journey and measurement map in docs/plan.md; the trace check
// requires the two to agree, so neither can be edited alone.
var PrototypeFeatures = []Feature{
	{
		Name:        "Local install, provider setup, repository open",
		Journey:     "Install-to-ready, repository-open, credential-boundary checks",
		Measurement: "Clean-install-to-ready time, repository-open time, credential boundary",
		Milestones:  []string{"M01", "M04", "M08", "M23"},
	},
	{
		Name:        "Requirement, scope, forecast, plan, approval",
		Journey:     "Time-to-plan, clarification/approval burden, forecast coverage",
		Measurement: "Time-to-plan, clarification and approval counts, forecast coverage",
		Milestones:  []string{"M13", "M14", "M17"},
		DependsOn:   []string{"Local install, provider setup, repository open"},
	},
	{
		Name:        "Worktree, safe edits, mediated commands",
		Journey:     "Concurrent-edit preservation, unauthorized-action and escape counts",
		Measurement: "Unauthorized-action count, workspace escape count, edit preservation",
		Milestones:  []string{"M09", "M10"},
		DependsOn:   []string{"Local install, provider setup, repository open"},
	},
	{
		Name:        "Fixed provider policy and hard budget",
		Journey:     "Exact usage/cost, cap enforcement, no silent switching",
		Measurement: "Exact decimal minor-unit cost, cap enforcement, provider-switch authority",
		Milestones:  []string{"M12", "M13"},
		DependsOn:   []string{"Local install, provider setup, repository open"},
	},
	{
		Name:        "Live thread, controls, reconnect, recovery",
		Journey:     "State comprehension, pause/resume, zero loss and zero duplication",
		Measurement: "Lost correctness-bearing events, duplicate effect intents, resume rate",
		Milestones:  []string{"M05", "M15", "M18"},
		DependsOn:   []string{"Worktree, safe edits, mediated commands"},
	},
	{
		Name:        "Diff, validation, evidence, repair, rollback",
		Journey:     "Acceptance, review findings, regressions, rollback success",
		Measurement: "Hidden-acceptance pass rate, review findings by severity, rollback success",
		Milestones:  []string{"M20"},
		DependsOn:   []string{"Live thread, controls, reconnect, recovery"},
	},
	{
		Name:        "Read-only task graph",
		Journey:     "Graph usefulness, confusion, accessibility, responsiveness",
		Measurement: "Rated usefulness, state/authority confusion rate, patch commit time",
		Milestones:  []string{"M19"},
		DependsOn:   []string{"Live thread, controls, reconnect, recovery"},
	},
	{
		Name:        "Deterministic project memory",
		Journey:     "Retrieval influence, rejection, invalidation, correctness and marginal effort",
		Measurement: "Retrieval influence and rejection counts, invalidation, marginal effort",
		Milestones:  []string{"M21"},
		DependsOn:   []string{"Diff, validation, evidence, repair, rollback"},
	},
	{
		Name:        "Packaging, diagnostics, updates",
		Journey:     "Clean-install exit, doctor accuracy, redaction and recovery",
		Measurement: "Clean-install exit, doctor accuracy, redaction and recovery success",
		Milestones:  []string{"M22", "M23"},
	},
}

// DeferredFeatures is the withheld set. Each entry names the gate that may
// activate it, matching the POST items in TODOS.md.
var DeferredFeatures = []Deferred{
	{ID: "POST-001", Name: "Production semantic graph engineering",
		BranchGate: "the disposable graph-medium experiment passes"},
	{ID: "POST-002", Name: "Tier-zero kernel scope freeze",
		BranchGate: "the graph experiment passes"},
	{ID: "POST-003", Name: "Graph-native atoms",
		BranchGate: "kernel scope is accepted"},
	{ID: "POST-004", Name: "Modeled Go atoms and reference models",
		BranchGate: "correlation controls are specified"},
	{ID: "POST-005", Name: "Go lowering and source maps",
		BranchGate: "the lowering conformance suite is frozen"},
	{ID: "POST-006", Name: "Determinism conformance across architecture matrices",
		BranchGate: "the deep-verification track is authorized"},
	{ID: "POST-007", Name: "Request-side effect proof obligations",
		BranchGate: "the medium and validator gates pass"},
	{ID: "POST-008", Name: "Semantic graph diff",
		BranchGate: "immutable semantic revisions exist"},
	{ID: "POST-009", Name: "Learned routing",
		BranchGate: "fixed-policy telemetry and shadow calibration pass"},
	{ID: "POST-010", Name: "Advisory patterns",
		BranchGate: "clean-room evaluation and lineage independence hold"},
	{ID: "POST-011", Name: "Promoted mechanical rules",
		BranchGate: "replay, false-positive, expiry, override, and demotion governance exist"},
	{ID: "POST-012", Name: "ANN/vector infrastructure",
		BranchGate: "SQLite brute-force retrieval is a measured bottleneck"},
	{ID: "POST-013", Name: "Multi-agent orchestration",
		BranchGate: "the single-agent baseline exposes a measured bottleneck"},
	{ID: "POST-014", Name: "Hosted sync, teams, enterprise identity, audit export",
		BranchGate: "hobbyist product evidence exists"},
	{ID: "POST-015", Name: "Direct graph editing",
		BranchGate: "user studies show conversational revisions are insufficient"},
}
