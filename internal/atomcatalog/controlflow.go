package atomcatalog

import "codeflux.dev/codeflux/internal/atomdoc"

// ProposalSource names the document and section every entry originates from.
// It is embedded in each entry's documentation so a retrieval result carries
// its own provenance rather than arriving as an unattributed assertion.
const ProposalSource = "docs/go-program-graph.md §5.9 (proposal; not a kernel declaration — POST-002)"

// ControlFlowKind distinguishes where an entry's control-flow behaviour lives
// in the graph, because the same catalog holds constructs of three different
// shapes and a reader who cannot tell them apart will look for a node kind
// that is actually a region attribute.
type ControlFlowKind string

const (
	// KindNode is expressed as a graph node.
	KindNode ControlFlowKind = "node"
	// KindRegion is expressed as a bounded region enclosing other nodes.
	KindRegion ControlFlowKind = "region"
	// KindEdge is expressed as a control edge or an edge label.
	KindEdge ControlFlowKind = "edge"
	// KindRelationship is expressed as a typed relationship between nodes.
	KindRelationship ControlFlowKind = "relationship"
	// KindAttribute is expressed as an attribute on a node or region.
	KindAttribute ControlFlowKind = "attribute"
	// KindBoundary is expressed in the lowering boundary rather than the graph.
	KindBoundary ControlFlowKind = "boundary"
)

// Readiness records how settled an entry is, so a retrieval result cannot
// present a proposed construct with the same confidence as a frozen one. This
// is the catalog's form of the honesty rule the plan states for validation
// generally: "we did not check this" and "this passed" must never render the
// same way.
type Readiness string

const (
	// ReadinessFrozen means the frozen notation in docs/plan.md §3 contains it.
	ReadinessFrozen Readiness = "frozen"
	// ReadinessProposed means this design proposes it and no plan text does.
	ReadinessProposed Readiness = "proposed"
	// ReadinessUndefined means the construct is required but unspecified.
	ReadinessUndefined Readiness = "undefined"
)

// ControlFlowEntry is one catalog entry before it becomes documentation.
// Ordinal is the row number in §5.9's table and is stable, so a diagnostic or
// a review comment can name an entry without quoting its whole description.
type ControlFlowEntry struct {
	Ordinal      int
	Name         string
	Kind         ControlFlowKind
	Readiness    Readiness
	GraphForm    string
	Precedent    string
	GoLowering   string
	Purpose      string
	UseWhen      string
	DoNotUseWhen string
	Semantics    string
	Inputs       []string
	Outputs      []string
	Concepts     string
	OpenQuestion string
}

// controlFlowEntries is the §5.9 table, in table order.
//
// The four entries whose Precedent is "none" are the constructs no surveyed
// strict-functional standard library provides — retry, claim gate, suspend,
// and element-failure policy. Three of those four are also the entries
// carrying an OpenQuestion, which is the §3.3 finding restated: the
// constructs this design had to invent are the ones that keep needing
// decisions.
var controlFlowEntries = []ControlFlowEntry{
	{
		Ordinal: 1, Name: "SequenceGraphStepsInControlOrder",
		Kind: KindEdge, Readiness: ReadinessUndefined,
		GraphForm: "control edge", Precedent: "bind / andThen, in all six surveyed standard libraries",
		GoLowering: "statement order in the generated function",
		Purpose:    "Order one graph step after another so the second observes the first's effects.",
		UseWhen:    "Two steps must run in a fixed order.",
		DoNotUseWhen: "The order is incidental and the steps are independent; an unnecessary edge " +
			"constrains scheduling and enlarges every reachability answer.",
		Semantics: "A control edge from a source port to a target node or region means the target " +
			"becomes reachable once the source completes along that edge's label.",
		Inputs:   []string{"a source out-port", "a target node or region"},
		Outputs:  []string{"a reachability relation the validator's rules walk"},
		Concepts: "sequence, order, then, control edge, happens-before",
		OpenQuestion: "The frozen notation expresses control edges only inside match blocks and " +
			"arrow forms. No convention resolves an edge targeting a region to a node inside it, " +
			"and none sequences lexically adjacent nodes. Every other entry inherits this gap.",
	},
	{
		Ordinal: 2, Name: "BranchOnTaggedUnionVariant",
		Kind: KindNode, Readiness: ReadinessFrozen,
		GraphForm: "match on a Tagged out-port", Precedent: "match and case, in all six surveyed standard libraries",
		GoLowering: "a generated fixed-arity eliminator whose parameter count is the variant count",
		Purpose:    "Take exactly one path per declared variant of a tagged union.",
		UseWhen:    "A node's result is a tagged union and each variant needs different handling.",
		DoNotUseWhen: "The decision is on a scalar value; derive a tagged result first, because " +
			"there is no boolean conditional in the graph.",
		Semantics: "Each outgoing control edge carries one variant tag. Labels must be pairwise " +
			"distinct so at most one arm fires, and must cover the variant set so none is unhandled.",
		Inputs:   []string{"a Tagged out-port", "one labelled control edge per variant"},
		Outputs:  []string{"exactly one taken path", "payload ports bound by the taken variant"},
		Concepts: "branch, match, case, dispatch, switch, tagged union, sum type, exhaustiveness",
	},
	{
		Ordinal: 3, Name: "MergeConvergingBranchValues",
		Kind: KindNode, Readiness: ReadinessFrozen,
		GraphForm: "merge node", Precedent: "applicative join, as in Elm's Task.map2 through map5",
		GoLowering: "assignment from whichever branch executed",
		Purpose:    "Join values produced on mutually exclusive branches into one downstream value.",
		UseWhen:    "Two or more branches each produce a value the same consumer needs.",
		DoNotUseWhen: "The branches produce unrelated values; a merge asserts they are alternatives " +
			"for one purpose.",
		Semantics: "A merge takes the value from the branch that executed and contributes no " +
			"derivation of its own, so its provenance is the union of its inputs' provenance. " +
			"That union is what makes a multi-source key detectably non-singleton.",
		Inputs:   []string{"two or more guarded payload ports"},
		Outputs:  []string{"one value port", "the union of the incoming provenance sets"},
		Concepts: "merge, join, phi, converge, alternative, unify branches",
	},
	{
		Ordinal: 4, Name: "MapOverBoundedCollection",
		Kind: KindRegion, Readiness: ReadinessFrozen,
		GraphForm: "loop region with an element binder", Precedent: "traverse over a finite structure",
		GoLowering: "a for-range loop over the materialized collection",
		Purpose:    "Apply the same body to every element of an already-finite collection.",
		UseWhen:    "Each element is processed independently and no value carries between elements.",
		DoNotUseWhen: "An element depends on the previous element's result; that is a fold, and a " +
			"map has no accumulator to carry it.",
		Semantics: "The bound is the collection's length, so termination is structural rather than " +
			"a policy. The element binder gives each iteration a stable identity that may enter a " +
			"key derivation, which is what makes an effectful iteration keyable.",
		Inputs:   []string{"a materialized finite collection", "an element identity binder"},
		Outputs:  []string{"per-element results", "stable per-element iteration provenance"},
		Concepts: "map, iterate, traverse, for each, bounded loop, per-element identity",
	},
	{
		Ordinal: 5, Name: "FoldBoundedCollectionIntoAccumulator",
		Kind: KindRegion, Readiness: ReadinessProposed,
		GraphForm:  "fold region with a seed, an element binder, and a yield port",
		Precedent:  "fold, in all six surveyed standard libraries",
		GoLowering: "a for loop over the collection with an accumulator variable",
		Purpose:    "Reduce a finite collection to one value by threading an accumulator through it.",
		UseWhen: "A running total, a reduction, or any iteration whose element depends on the " +
			"prior element's result.",
		DoNotUseWhen: "Elements are independent; a map states that independence and a fold hides it.",
		Semantics: "The body is an ordinary node block, so effects inside it remain visible to the " +
			"validator's rules. The in-flight accumulator is transient and may not enter a key cone. " +
			"The fold's result may, but only when its body performs no effect, draws only on durable " +
			"or stable-iteration values, and its combinator is declared order-independent.",
		Inputs:   []string{"a seed value", "a materialized finite collection", "an element identity binder"},
		Outputs:  []string{"the final accumulator", "a per-iteration accumulator port inside the body"},
		Concepts: "fold, reduce, accumulate, running total, aggregate, sum, scan",
	},
	{
		Ordinal: 6, Name: "RetryBoundedAttemptsOnDeclaredOutcome",
		Kind: KindRegion, Readiness: ReadinessFrozen,
		GraphForm:  "retry region with a maximum attempt count and a backoff policy",
		Precedent:  "none — no surveyed standard library provides retry",
		GoLowering: "a bounded for loop around the retried node",
		Purpose:    "Repeat one operation a bounded number of times before giving up.",
		UseWhen: "A transient failure is plausibly recoverable by repeating the same request " +
			"with the same key.",
		DoNotUseWhen: "The outcome is ambiguous rather than failed; ambiguity is reconciled, never " +
			"retried, because a retry after an ambiguous outcome can duplicate a completed effect.",
		Semantics: "A retry repeats one node instance, so it never creates a second issuance for " +
			"duplicate-issuance purposes. The key must stay identical across attempts.",
		Inputs:   []string{"a maximum attempt count", "a backoff policy", "the retried node"},
		Outputs:  []string{"the last attempt's outcome, or a bound-exhausted outcome"},
		Concepts: "retry, reattempt, backoff, transient failure, bounded repetition",
		OpenQuestion: "Nothing declares which outcome triggers a repeat. An author can bound how " +
			"many times and with what backoff, but not on what.",
	},
	{
		Ordinal: 7, Name: "ReconcileAmbiguousOutcomeByBoundedPolling",
		Kind: KindRegion, Readiness: ReadinessFrozen,
		GraphForm:  "reconcile region with a maximum poll count",
		Precedent:  "unfold paired with take_while, as in OCaml's Seq",
		GoLowering: "a bounded for loop around the reconciling read",
		Purpose:    "Resolve an ambiguous outcome by reading authoritative state a bounded number of times.",
		UseWhen:    "An external write returned neither confirmed success nor confirmed failure.",
		DoNotUseWhen: "The outcome is already confirmed; a reconciliation read then adds latency " +
			"and no information.",
		Semantics: "The poll bound is the only thing between a reconciliation and non-termination, " +
			"so it is mandatory and statically present. Reconciliation may repeat bounded reads or " +
			"suspend, and may never reissue the original write outside its declared retry region.",
		Inputs:   []string{"a maximum poll count", "the effect being reconciled", "a reading operation"},
		Outputs:  []string{"a confirmed outcome, or a bound-exhausted outcome"},
		Concepts: "reconcile, poll, resolve ambiguity, confirm outcome, authoritative read",
	},
	{
		Ordinal: 8, Name: "GateIssuanceOnConfirmedAtomicClaim",
		Kind: KindRegion, Readiness: ReadinessFrozen,
		GraphForm:  "claim region declaring a deduplication strategy",
		Precedent:  "none — no surveyed standard library provides a claim gate",
		GoLowering: "a conditional admitting only the confirmed-acquisition path",
		Purpose: "Ensure every path to an issuance passes a confirmed conditional durable claim on " +
			"the same key.",
		UseWhen: "Deduplication depends on prior durable state rather than on a provider's own " +
			"idempotency key.",
		DoNotUseWhen: "The provider already deduplicates on a version-bound key contract; a claim " +
			"then adds a write and a failure mode for no additional guarantee.",
		Semantics: "The claim distinguishes at least acquisition, completion, in-progress, and " +
			"terminal failure. Only confirmed acquisition may reach a new issuance. The claim is " +
			"itself an external write, so an ambiguous claim must be reconciled before issuance.",
		Inputs:   []string{"a singleton key derivation", "a uniqueness or compare-and-set contract"},
		Outputs:  []string{"a confirmed acquisition that gates issuance, or an alternative path"},
		Concepts: "claim, atomic claim, deduplicate, gate issuance, exactly once, idempotency",
		OpenQuestion: "Whether a region's deduplication attribute describes its own node or scopes " +
			"to the issuances it gates. The readings select different nodes for checking.",
	},
	{
		Ordinal: 9, Name: "SuspendUntilDurableTimeout",
		Kind: KindNode, Readiness: ReadinessFrozen,
		GraphForm:  "durable wait node with a timeout and a resume target",
		Precedent:  "none — no surveyed standard library provides durable suspension",
		GoLowering: "an explicit state machine, a replay from journal, or a checkpointed replay",
		Purpose:    "Pause a workflow for a bounded period and resume it at a declared target.",
		UseWhen:    "Progress requires waiting for external state that polling alone cannot hurry.",
		DoNotUseWhen: "The wait is short enough to be an ordinary bounded poll; a durable suspension " +
			"costs a persistence boundary and a resumption mechanism.",
		Semantics: "The suspension may outlive the process, so the graph declares the timeout and " +
			"the durable runtime enforces it. The graph itself holds no clock.",
		Inputs:   []string{"a timeout", "a resume target"},
		Outputs:  []string{"resumption at the declared target"},
		Concepts: "wait, suspend, durable timer, resume, long running, sleep until",
		OpenQuestion: "Which resumption mechanism the generated program uses. The choice changes " +
			"the shape of every generated function, and the notation's explicit resume target fits " +
			"a state machine or a checkpointed replay better than a replay from the start.",
	},
	{
		Ordinal: 10, Name: "ExitRegionOnBoundExhaustion",
		Kind: KindEdge, Readiness: ReadinessProposed,
		GraphForm:  "a control edge leaving a bounded region, labelled with the exhaustion outcome",
		Precedent:  "boundary and break, as in Scala 3",
		GoLowering: "a labelled break out of the bounded loop",
		Purpose:    "Leave a retry or reconcile region when its declared bound is reached.",
		UseWhen:    "A bounded region needs a path for the case where the bound is exhausted.",
		DoNotUseWhen: "Never omitted: a region that declares a bound and does not handle its " +
			"exhaustion leaves a reachable path with no defined behaviour.",
		Semantics: "Exhaustion is inconclusive rather than confirming, so a path leaving a region " +
			"this way establishes nothing and cannot gate a compensation.",
		Inputs:   []string{"a bounded region"},
		Outputs:  []string{"one exhaustion edge to a handling node"},
		Concepts: "bound exhausted, give up, escape, break out, max attempts reached",
	},
	{
		Ordinal: 11, Name: "RecoverFromFailureVariantIntoAlternatePath",
		Kind: KindRelationship, Readiness: ReadinessFrozen,
		GraphForm:    "a reconciles relationship plus the recovery arms of a match",
		Precedent:    "mapError and onError, as in Elm and OCaml",
		GoLowering:   "routing on the recovering variant",
		Purpose:      "Transform a failure into a different path rather than only short-circuiting.",
		UseWhen:      "A declared failure has a meaningful alternative.",
		DoNotUseWhen: "The failure is genuinely terminal; an alternative path then hides it.",
		Semantics: "Recovery routes a declared variant, never a voided contract. A panic or a " +
			"contract void is not a failure variant and may not reach a recovery arm.",
		Inputs:   []string{"a failure or ambiguous variant", "a recovering target"},
		Outputs:  []string{"an alternative path"},
		Concepts: "recover, handle failure, fallback, alternative path, error routing",
	},
	{
		Ordinal: 12, Name: "CompensateConfirmedEffectAfterReconciliation",
		Kind: KindRelationship, Readiness: ReadinessFrozen,
		GraphForm:  "a compensates relationship from the compensating effect to the compensated one",
		Precedent:  "protect with a finally clause, as in OCaml — and only partially",
		GoLowering: "a deferred compensating call",
		Purpose:    "Undo a completed external effect once its outcome is confirmed.",
		UseWhen:    "An effect completed and downstream work then failed.",
		DoNotUseWhen: "The original outcome is ambiguous. Compensating an unconfirmed effect can " +
			"undo something that never happened, or leave a real one standing.",
		Semantics: "Every path from an ambiguous outcome to a compensator must pass a confirmed " +
			"reconciliation. Compensation for an unrelated effect stays valid.",
		Inputs:   []string{"a confirmed outcome", "a compensating operation"},
		Outputs:  []string{"a compensated state"},
		Concepts: "compensate, undo, refund, reverse, saga rollback",
		OpenQuestion: "What happens when the compensator itself fails. The original failure must " +
			"survive, or the compensation destroys the record of what it was compensating for.",
	},
	{
		Ordinal: 13, Name: "SealAtomPanicAtCallBoundary",
		Kind: KindBoundary, Readiness: ReadinessProposed,
		GraphForm:  "not a graph construct — the generated call boundary",
		Precedent:  "the recoverable-versus-fatal split, as in Scala's NonFatal",
		GoLowering: "a deferred recover, a supervising goroutine, and a mandatory timeout",
		Purpose:    "Convert a host-language panic into a distinguished voided-contract outcome.",
		UseWhen:    "Always, at every call into an atom with a native implementation.",
		DoNotUseWhen: "Never omitted, and never routed into a declared failure variant: a void " +
			"means the contract did not hold, not that the operation failed.",
		Semantics: "Seals a synchronous panic in the calling goroutine. It cannot seal a panic in a " +
			"goroutine the atom spawned, an unrecoverable runtime fault, or a goroutine exit; those " +
			"need process supervision. A blocked atom leaks its goroutine, and no host mechanism " +
			"reclaims it.",
		Inputs:   []string{"an atom call"},
		Outputs:  []string{"the atom's declared outcome, or a voided contract"},
		Concepts: "panic, seal, contract voided, supervise, recover, fatal versus recoverable",
	},
	{
		Ordinal: 14, Name: "TerminateWorkflowWithDeclaredOutcome",
		Kind: KindNode, Readiness: ReadinessFrozen,
		GraphForm: "return node", Precedent: "identity",
		GoLowering:   "returning the workflow's result union",
		Purpose:      "End a workflow path with one declared outcome.",
		UseWhen:      "A path has reached a final answer.",
		DoNotUseWhen: "Work remains on that path; a premature return silently drops it.",
		Semantics: "Each return node contributes one variant to the workflow's result union. Tags " +
			"must be pairwise distinct across return nodes or two outcomes collapse and the caller " +
			"cannot tell which path ran.",
		Inputs:   []string{"a payload value"},
		Outputs:  []string{"one variant of the workflow's result union"},
		Concepts: "return, terminate, final result, workflow outcome, exit",
		OpenQuestion: "No syntax delivers a value to a return node, though a payload-carrying " +
			"result requires one.",
	},
	{
		Ordinal: 15, Name: "ContinuePastFailedIterationElement",
		Kind: KindAttribute, Readiness: ReadinessProposed,
		GraphForm:    "an element-failure attribute on a loop or fold region",
		Precedent:    "none — no surveyed standard library provides an element-failure policy",
		GoLowering:   "continue rather than break inside the generated loop",
		Purpose:      "Decide whether a failing element aborts the iteration or is skipped.",
		UseWhen:      "Batch work should complete for the elements that succeed.",
		DoNotUseWhen: "Elements are interdependent, so continuing past one failure corrupts the rest.",
		Semantics: "Defaults to aborting, because silently continuing past failures is the more " +
			"surprising behaviour and the more damaging default.",
		Inputs:   []string{"a loop or fold region", "a policy of continue or abort"},
		Outputs:  []string{"iteration that either halts or skips on a failing element"},
		Concepts: "continue on error, skip failure, best effort, partial success, batch processing",
	},
}

// ControlFlowEntries returns the catalog in table order. The slice is copied
// so a caller cannot mutate the package's declarations.
func ControlFlowEntries() []ControlFlowEntry {
	entries := make([]ControlFlowEntry, len(controlFlowEntries))
	copy(entries, controlFlowEntries)
	return entries
}

// Document renders one entry as schema-v1 atom documentation.
//
// Fields the catalog cannot honestly fill are recorded as an explained absence
// rather than left blank, because the schema permits "None" only with a reason
// and a silently empty field is never a valid state. Preconditions,
// determinism, security, complexity, and verification are absences of exactly
// that kind: a control-flow construct has no Go implementation to measure, and
// claiming otherwise would assert evidence that does not exist.
func (entry ControlFlowEntry) Document() atomdoc.Document {
	const noImplementation = "None. This entry describes a graph construct, not a Go implementation, " +
		"so there is nothing here to measure or verify. " + ProposalSource

	document := atomdoc.Document{
		Purpose:      atomdoc.Field{Text: entry.Purpose},
		UseWhen:      atomdoc.Field{Text: entry.UseWhen},
		DoNotUseWhen: atomdoc.Field{Text: entry.DoNotUseWhen},
		Semantics:    atomdoc.Field{Text: entry.Semantics},
		Inputs:       atomdoc.Field{Items: entry.Inputs},
		Outputs:      atomdoc.Field{Items: entry.Outputs},
		Preconditions: atomdoc.Field{Items: []string{
			"the graph is well-formed under the validator's structural preconditions",
		}},
		Postconditions: atomdoc.Field{Items: []string{
			"control has advanced exactly as this entry's semantics describe",
		}},
		Effects: atomdoc.Field{Items: []string{
			"none of its own; an entry describes control flow, and any effect belongs to the " +
				"nodes it encloses or connects",
		}},
		FailureSemantics: atomdoc.Field{Items: []string{
			failureSemantics(entry),
		}},
		Determinism: atomdoc.Field{
			Text: "Deterministic as a graph construct. The determinism of what it encloses is that " +
				"material's own property, not this entry's.",
		},
		IdempotencyAndRetry: atomdoc.Field{Text: idempotencyNote(entry)},
		ReconciliationAndCompensation: atomdoc.Field{
			None:   true,
			Reason: "This entry is control flow. Reconciliation and compensation attach to effects. " + ProposalSource,
		},
		SecurityAndPrivacy: atomdoc.Field{
			None:   true,
			Reason: noImplementation,
		},
		DependenciesAndBindings: atomdoc.Field{
			Text: "Graph form: " + entry.GraphForm + ". Lowering: " + entry.GoLowering +
				". Precedent: " + entry.Precedent + ".",
		},
		ComplexityAndLimits: atomdoc.Field{
			None:   true,
			Reason: noImplementation,
		},
		Examples: atomdoc.Field{Items: []string{
			"Use when: " + entry.UseWhen,
			"Do not use when: " + entry.DoNotUseWhen,
		}},
		Verification: atomdoc.Field{
			None:   true,
			Reason: noImplementation,
		},
		RetrievalConcepts: atomdoc.Field{Text: entry.Concepts},
	}
	return document
}

// failureSemantics states an entry's failure behaviour, including the open
// question where one exists, so a reader retrieving the entry sees the
// unresolved part rather than a description that reads as settled.
func failureSemantics(entry ControlFlowEntry) string {
	base := "Readiness: " + string(entry.Readiness) + ". "
	switch entry.Readiness {
	case ReadinessFrozen:
		base += "The frozen notation contains this construct."
	case ReadinessProposed:
		base += "This design proposes this construct; no plan text authorises it yet."
	case ReadinessUndefined:
		base += "This construct is required and unspecified. Treat any behaviour as unsettled."
	}
	if entry.OpenQuestion != "" {
		base += " Open question: " + entry.OpenQuestion
	}
	return base + " " + ProposalSource
}

// idempotencyNote reports whether repeating the construct is safe, which for
// control flow is a question about what it encloses rather than about itself.
func idempotencyNote(entry ControlFlowEntry) string {
	if entry.Kind == KindRegion {
		return "A region may repeat its body under its own declared bound. Repetition safety is a " +
			"property of the enclosed effects and their keys, not of this construct."
	}
	return "Repeating this construct is meaningless in isolation; it carries no effect of its own."
}
