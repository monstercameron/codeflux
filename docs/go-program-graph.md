# The Go Program Graph

A design for the functional program graph that composes atoms into programs, its lowering to Go, and the validator that gives it its reason to exist.

**Revision 7.** Revised against nineteen adversarial reviews across six rounds — plan fidelity, Go language reality, algorithmic correctness, experiment validity, internal consistency, and a dedicated pass over each revision's new material. Every Go claim marked *verified* was tested against a live Go 1.26.3 toolchain. §21 lists what changed.

**Revisions 6 and 7 close a gap all five prior revisions had: the notation could not express a program.** No accumulator, no composition, no reuse, and no definition of a workflow as a callable unit — four constructs the plan requires. §5.8 adds them, §11.11 defines what a generated program is, and §12.5 adds a cost layer over the complexity every atom already declares. Revision 7 then corrected revision 6, which got the accumulator mechanism wrong twice and built the cost layer on a gate belonging to a different subsystem. §19 records why five revisions and seventeen reviews missed the gap, and why the sixth introduced new errors closing it.

**Read §19 before §5–§13.** Six things in this document have now been specified wrongly more than once. They are recorded as open questions with their candidate answers, not resolved by a further guess:

| Item | Times wrong | Now at |
| --- | --- | --- |
| Rule 4's applicability test | 3 | §9.4 + §16.6, open |
| `provider_deduplication_scope`'s home | 2 | §16.9, open |
| Boundary-type restriction | 2 | §6.2 + §6.4, two passes |
| `BoundExhausted`'s declaring authority | 2 | §13.4, region-level |
| Handler-laundering control | 3 | §11.2, provenance not shape |
| The void channel's representation | 2 | §11.2, `Voided` narrowed |

**And one gap sits underneath all of it (§5.7): the control graph is not fully defined by the frozen notation.** Explicit control edges appear only in `match` blocks and `->` arrows. Nothing expresses flow from `n01` to `n02` to `n03` to `region claim`, and nothing defines how an edge targeting a region reaches a node inside it. Rule 1's reachability, Rule 4's dataflow, W6, and §5.5's dominance all walk that graph.

## 0. Status, authority, and scope

**This document has no build authority.** The capability is `POST-001` in `internal/deferred`, triggered by "prototype exit is decided, and the graph experiment is run as a disposable artifact rather than as the first version of production code." `POST-002`–`POST-005` form a serial chain behind it, each with its own trigger.

**Authority ranking.** Plan beats document; disagreements go to §16. Frozen notation (§3) beats plan prose (§5), because §3 is a freeze.

**Three authorities appear and each citation names which:** `docs/plan.md`; `internal/deferred/deferred.go`; the repository for existence claims.

**What this is.** The disposable experimental medium for Arm C (`plan.md:1129-1138`), built to be thrown away. **Not** the production graph schema, atom runtime, macro system, or Go backend — `plan.md:1270` keeps all four behind the graph decision.

## 1. Grounding in the project's own concepts

> **Atom** — a reusable unit with stable identity, descriptive name, typed signature, contract, detailed documentation, applicability, effects, bindings, and evidence. (`plan.md:400-402`)

> **Graph** — a versioned composition of identities, atoms, control, data, effects, obligations, and evidence. (`plan.md:404-406`)

> **Evidence** — version-bound support for a specific claim. Evidence can pass, fail, be waived, become stale, or be invalidated. (`plan.md:388-390`)

> **Project Knowledge** — an admitted fact, command, mapping, regression, recipe, or atom derived from attributable evidence. (`plan.md:396-398`)

Nine components. Three implemented: **stable identity** in `internal/domain/identity.go:584`, **descriptive name** in `internal/atomname`, **detailed documentation** in `internal/atomdoc`. Six not implemented: typed signature, contract, applicability, effects, bindings, evidence.

`atomdoc.ContractHash` is parsed but never computed; its only non-test producer is `storage/atom_documentation_repository.go:641`, reading a stored string back.

## 2. The design's justification is the experiment, not its elegance

The question (`plan.md:1104-1106`): is a structured functional graph a better editing and reasoning medium for coding agents than ordinary Go source text?

The kill criterion (`plan.md:1357-1366`): significantly fewer structural violations than Arm B under McNemar at 0.05 with a paired CI excluding no improvement; no more than five points worse on hidden acceptance; no more than 25% higher total economic cost including human preparation; no severe defect class regressed.

**C1. Arm B is the competitor.** `plan.md:1300-1302`: "how often does manual application forget or misapply a structural rule compared with a validator that cannot forget?" Any feature that does not make a structural rule mechanically checkable is out of scope; §17 applies that test to this document.

**C2. Modeling cost is measured cost** (`plan.md:1284-1298`), cold-start reported separately.

### 2.1 Cost accounting — pre-registered before task one

| Bucket | Contents |
| --- | --- |
| Notation authoring | Grammar (§13.5), parser, editor verbs |
| Validator implementation | Four rules, preconditions, diagnostics |
| Lowering implementation | Generator, eliminators, copiers, conformance |
| **Atom contract authoring** | Stubs for every atom the 50 tasks touch |
| **Boundary shadow types** **[R4]** | A graph `Money`, `Timestamp`, `Url` etc., because §6.2 forbids `time.Time`, `decimal.Decimal`, `net/url.URL` and every other type with internal invariants from crossing a Tier 2/3 boundary |
| Editor onboarding | Notation fluency for staff who did not design it |

All are one-shot and none amortizes: the artifact is discarded after grading, so every bucket is charged in full against the 50-task N.

**The shadow-type bucket is new and was invisible in revision 3. [R4]** A `go/types` walk against realistic candidates confirms §6.2 excludes `time.Time`, `net/url.URL`, `math/big.Int`, `strings.Builder`, and `shopspring/decimal.Decimal`; only a type with no hidden state at all (`uuid.UUID`, a bare `[16]byte`) passes. The worked example is a *payments* workflow, so money and timestamps are unavoidable, and every one of them needs a hand-authored graph type plus an adapter conversion.

**Removed from this table: the `Goexit` supervision cost. [R4]** It is 611.5 ns/op against 0.21 ns/op for a direct call — a real runtime tax recorded in §10.4, but orders of magnitude too small to move a ceiling measured in task-hours across fifty tasks. Revision 3 listed it here and broke the table's schema to do so, which mistook a measured number for a relevant one.

**Two things actually threaten the ceiling:** atom-contract plus shadow-type authoring across fifty *real* change requests in varied domains, and translation cost for editors working through the frozen editor verbs. If the pre-registered estimate exceeds 25%, cut per §17 or run the narrower payments-only experiment `plan.md:1368` provides for.

**Measurement threat.** `plan.md:1314-1321`'s triage cannot separate "the notation is bad" from "the editor was undertrained." Log onboarding hours per editor and report them alongside defect counts.

## 3. What the strict-functional standard libraries settled

Surveyed: Standard ML (Basis), OCaml (Stdlib 5.x), F# (FSharp.Core), Scala 3, Elm (`elm/core` 0.19.1), Roc. "Strict" is eager evaluation, excluding Haskell.

### 3.1 Adopted, because the frozen notation already contains them

| Stdlib construct | Notation form |
| --- | --- |
| Applicative join (`Task.map2..map5`) | `m01 merge charge <- [n06.c, n09.c] : Charge` — `provenance(phi) = union(incoming)` (`plan.md:1585`) is applicative behaviour |
| `traverse` over a finite structure | `loop map line in n01.out.lines as line#id { … }` |
| Two-channel sequencing | Control edges labelled with variant tags |
| Failure-channel transformation | `reconciles` relationships, recovery arms |

### 3.2 Adopted as design constraints the plan does not state

**`Fun.protect` and the lost original failure.** OCaml specifies that a raising `finally` replaces the original exception and the original is lost. §12 defines `ReconciliationGatesCompensation` and is silent on a failing compensator — §16.5, raised not implemented.

**Scala's `NonFatal`.** The library draws the recoverable line, not the call site. Go's `panic` is a fourth channel; §10.4 bounds what can be done.

**Elm's `Task.perform : Task Never a -> Cmd msg`.** An obligation discharged by a type at a boundary; transfers for one case (§12.3).

**SML's `General` as the kernel budget.** About a dozen names. `plan.md:1682` says thirty "**may be manageable**" — a hedge.

**OCaml's producer/consumer split.** The bound is always a runtime value, so `region reconcile [max_polls: 6]` is the only thing between a reconciliation loop and non-termination.

### 3.3 Explicitly rejected

| Idea | Rejected because |
| --- | --- |
| One channel-tagged edge | `plan.md:1146-1152` freezes control and data as separate items |
| Retry as a derived macro | `region retry` is first-class |
| Effects as ordinary atom calls | `effect` is a frozen keyword with eighteen declaration fields |
| Obligations as uninhabited types | `plan.md:1596-1610` lists obligations not expressible as value types. *The "graph-shape predicate" framing is this document's inference.* |
| A generic combinator runtime | Possible in Go, incompatible with compile-time exhaustiveness (§11.1) |

## 4. Go's enforcement boundary

| Go enforces | Go does not enforce |
| --- | --- |
| Function arity, for positional calls only | Exhaustiveness of a `switch` |
| Unexported fields → construction in-package | Immutability of any value |
| Package import graph, Go-visible imports only | Aliasing or input mutation |
| Module and toolchain pinning | Purity of any called function |
| Type identity of errors | Determinism inside a dependency |
| That an embedded field's type is not a type parameter | Absence of `panic` (§10.4) |
| Interface satisfaction (the visitor route, §11.2) | Absence of `init()` side effects |
| | Termination |
| | That an unexported-method interface has no foreign implementations |
| | That a copy of a foreign type copies its unexported internals |
| | Stability of generated symbol names across regenerations |
| | **Reclamation of a blocked goroutine — there is no primitive at all** **[R4]** |
| | **A distinguishable "no value" for a generic return type** **[R4]** |

`plan.md` §16's determinism profile governs generated Go; none of it governs dependency Go.

## 5. The intermediate representation

### 5.1 Node kinds

Six: **`input` · `pure` · `effect` · `merge` · `wait durable` · `return`**

**Accumulation is a *region*, not a node kind. [R7 — revision 6 added a seventh node kind, `fold`, and that was wrong; see §5.8.1.]**

*Reconciliation read*, *conditional durable claim*, and *effect request* from `plan.md:1547-1557` all collapse into `effect` plus context, as the molecule does it.

**`match` is treated as a labelled group of control edges leaving one out-port — provisionally, pending §16.1.** It carries no ID in the molecule while `plan.md:1154` requires permanent explicit IDs and `plan.md:2342` requires identity for every node, edge, region, relationship, and obligation; it is keyed on a port, and its arms target regions. Counter-argument: `plan.md:1551` lists match among node kinds.

### 5.2 Regions

`RegionKind ∈ { claim, issue, retry, reconcile, loop, fold, plain }` **[R7 — `fold` added, §5.8.1]**. Regions nest lexically and form a tree; each node belongs to exactly one innermost region.

**Regions related by control flow are not related by nesting. [R4]** In the frozen molecule `region claim` and `region issue` are **siblings** — the claim block closes before the `match`, and `region issue` opens as a separate top-level block, connected only by the `n04.Acquired -> region issue` control edge. This is the fact that broke Rule 4 three times, and §9.4 now names it.

### 5.3 The IR

```
NodeID EdgeID RegionID PortID RelationshipID   -- opaque, permanent, never reused

Node         = { ID, Kind, Label, Region RegionID, Atom *AtomRef, Result, Attrs }

Attrs        = { Dedup *DedupStrategy, Contract *ContractVersionRef,
                 Max *uint, MaxPolls *uint, Backoff *BackoffPolicy, Timeout *Duration }

Result       = Single { Port PortID }
             | Tagged { Variants []Variant }        -- only this is matchable

Variant      = { Tag, PayloadPorts []PortID, Disposition }
Disposition  = confirming | ambiguous | inconclusive

Port         = { ID, Node NodeID, Direction in|out, Name, Type TypeRef, Guard *VariantTag }

DataEdge     = { ID, From PortID, To PortID, Type TypeRef, Stability StabilityClass }

ControlEdge  = { ID, From PortID|RegionID, To NodeID|RegionID,                    -- [R4]
                 Label *VariantTag|RegionOutcome, Progress bool }

RegionOutcome = BoundExhausted                       -- [R4], see §13.4

Region       = { ID, Kind RegionKind, Parent *RegionID, Attrs }

Relationship = { ID, Kind reconciles|compensates, Subject NodeID, Object NodeID }

AtomRef      = { AtomID, AtomVersion, ContractRef }   -- joins ContractStub on (AtomID, AtomVersion)
```

**Effective attribute resolution.** An attribute resolves to the node's own `Attrs` if present, otherwise the innermost enclosing region declaring it, otherwise undeclared. **Declaring one attribute key at two nesting levels for the same node is a well-formedness error (W9), not a silent shadowing.** **[R4]** The molecule never exercises a genuine conflict — every node either has its own attribute or resolves through exactly one ancestor — so an inner region's stray `dedup:` would otherwise override a materially different outer strategy with no diagnostic.

**`ControlEdge.From` widened to `PortID|RegionID` and `Label` to include `RegionOutcome`. [R4]** This is what carries `BoundExhausted` out of a bounded region without making it a variant on an atom's contract (§13.4).

### 5.4 Identity, labels, and resolution

`plan.md:2344-2350` forbids deriving identity from source position, display name, content hash, parent order, or generated Go location. **`n01` and `m01` are display labels.**

The parser maintains a workflow-scoped table mapping label → `NodeID` and `<label>.<name>` → `PortID`. Labels are unique within one workflow revision and **not** across revisions — which is what makes generated symbol names unstable (§11.7). Round-tripping must preserve `NodeID`, not label spelling.

`plan.md:1251` requires rejecting deletion of a referenced node, writing a tombstone, and never reusing IDs.

### 5.5 Guarded ports

A data edge from a port with `Guard = T` is well-formed only if its consumer is control-dominated by the control edge labelled `T` leaving that port's node, *or* the consumer is a `merge`.

**Dominance is global from the workflow root, over progress edges only.** **[R5 — the reason revision 4 gave is now stale.]** That reason was "`region issue` has two entries," using *entries* to mean incoming edges — the exact conflation §5.6 identifies as revision 4's error. Under §5.6 as corrected, `region issue` has one entry node and two incoming edges, so a per-region dominator tree would in fact be well-defined for it.

The conclusion survives on a different basis: a guarded port's consumer may sit in a *different* region from the producer — `n06.c` is produced inside `region issue` and consumed by `m01` at the top level — so a per-region dominator tree cannot answer the question §5.5 asks. Dominance must range over the whole progress subgraph.

**The root is the unique node with no incoming control edge, and it must be an `input`. [R4]** Revision 3 wrote W6 as "exactly one `input` node has no incoming control edge," which quantifies only over `input` nodes and therefore could not reach the molecule's disconnected `pure` node `n02` — while §15.2 claimed it did. W6 is corrected in §9.1 and §15.2 no longer makes that claim.

Excluding non-progress back-edges from dominance is sound **given W1**; §9.1 records what collapses if the plan rejects W1.

**Coverage gap.** The only guarded-port consumer in the molecule is `m01`, the *exempted* case, so the dominance requirement itself is never exercised by the plan's example.

### 5.6 Progress edges and the cyclic graph

The molecule is not a DAG: `n09 → n10 → n09`, and `n13 → n14 → n13`.

**`Progress: false` covers three cases**, each hedged on whether the lowering materializes it:

1. The back-edge from a `wait durable` node. **Materialized** — `n10 -> n09` is explicit.
2. A `retry` region's repetition edge. **Not materialized** — `n06`'s three variants carry no "retry me" signal, so retry operates beneath the notation.
3. A `loop` region's next-element transition. **Not materialized** — the notation inlines loop bodies.

**Region entry and exit — generalized to all regions. [R5]** Revision 4 scoped the single-entry rule to retry regions because `region issue` has two incoming *edges*, which confused entry **nodes** with incoming **edges**. Every region has exactly one entry **node**, and any number of control edges may target it (W2). `region issue` is well-formed: two edges, one entry. A retry region's exit set is every node with an edge back to the entry; each such edge is non-progress.

### 5.7 The control graph — a gap the notation leaves open **[R5]**

**Everything downstream of this section walks a graph the frozen notation does not fully specify.** Explicit control edges appear only in `match` blocks and in `->` arrows. The molecule shows no edge for `n01 → n02`, `n02 → n03`, or `n03 → region claim`, and its only claim-to-issuance edges — `n04.Acquired -> region issue` and `n13.Acquired -> region issue` — target a *region* while `n06` sits two regions inside it.

Taken literally, no edge in the IR has `To = n06`. Rule 4's `Claimed` would find no progress-predecessors for it, and Rule 1's BFS would dead-end at a region pseudo-vertex, so `R(n04)` and `R(n13)` would never reach `n06`, `m01`, `n07`, `n08`, or either `return`. **That is a reachability undercount on the only two forward edges connecting the claim region to everything downstream, and it affects Rule 1 as much as Rule 4.**

Two conventions are required, and neither is stated anywhere in `plan.md`:

**C1 — Region-edge resolution.** A control edge with `To = RegionID(R)` contributes a progress-predecessor edge into `R`'s entry node, recursively through nested regions. Under this convention `region issue`'s entry resolves through `region retry` to `n06`, so `n06`'s progress-predecessors are `{n04, n13}` and both Rule 1 and Rule 4 behave as their sections describe.

**C2 — Implicit sequencing.** Consecutive nodes within one region, where the earlier node has a `Single` result or a `Tagged` result with no `match`, are joined by an implicit progress edge in lexical order. Without this the molecule has no control flow at all outside `match` blocks, and `n02`, `n03`, and every other non-matched node is rootless.

**C2 is the weaker of the two and its consequences are unattractive.** It makes lexical order semantically load-bearing in a notation whose §5.4 insists position is not identity, and it interacts badly with §15.2: `n02` is `Tagged` (`Valid | Invalid`) with no `match`, so under C2 it has an incoming edge from `n01` but produces **no outgoing edge**, leaving `n03` rootless anyway. Revision 4's §15.2 counted `n02` as the only extra rootless node; `n03` is a third, and W6 rejects the molecule twice over.

**A third convention, considered and rejected. [R5]** Control could be derived from *data* edges — a node's progress-predecessors being the producers it reads. This is more consistent with §5.4 than C2, since it uses dependency rather than position, and it repairs the `n03` case directly: `n03` reads `n01.out`, so `n01` becomes its predecessor without any appeal to lexical order. It is rejected because it conflates dataflow with control flow in a way that damages the rules it feeds: Rule 4's meet is a conjunction **over every progress-predecessor**, so admitting every data producer as a predecessor injects `pure` nodes with no bearing on claim-gating into that conjunction, weakening `Claimed` toward false and producing spurious `RULE4` violations. Recording it because revision 5's first draft presented C1 and C2 as the whole space, and they are not.

**Recorded as §16.14.** The alternative to C2 is that the notation requires an explicit `->` for every control transition and the frozen molecule is simply incomplete — which is consistent with §15's other findings, and is the reading this document would prefer, but it is a plan decision.

### 5.8 Program completeness **[R6 — the largest gap in every prior revision]**

The six node kinds of §5.1 express straight-line dataflow with variant branching and bounded mapping. **That is not enough to write a program**, and four constructs the plan requires are absent. Every prior revision missed this because §2's C1 test — anything that does not make a structural rule checkable is out of scope — is a scope test, not an expressiveness test, and nothing else in the document asked whether the notation can say what real programs need to say.

This matters directly to the experiment. `plan.md:1110` requires Arm C to model **fifty real change requests**, and `plan.md:1284` requires logging "representability corrections per workflow." A construct the notation cannot express is not a scope saving; it is a measured Arm C failure.

#### 5.8.1 `fold` — accumulation, as a region

**`plan.md:2195` requires `Map` *and* `Fold`.** Without an accumulator there is no running total, no reduction, no state-machine step, and no iteration in which element *N* depends on element *N−1* — which excludes most real programs.

**[R7 — revision 6 got the mechanism wrong twice.]** It made `fold` a seventh *node kind* whose body was an `AtomRef`, and both were mistakes:

- **An atom-shaped body hides effects from the rules.** `loop map` has an inline `{ … }` block; a node bound to an atom does not. Rules 1, 2, and 4 all quantify over `n.Kind = effect` **in the static graph**, so an effect inside an atom-shaped fold body is invisible to duplicate-issuance checking, key-provenance checking, and claim-gating — the three rules the design exists to enforce. Batch processing and saga workflows need exactly that combination.
- **A node creates no region**, so §8.1's "for each loop-kind ancestor" walk has nothing to walk, and W7's capability check quantifies on `n.Kind = effect` while the node's kind was `fold`. Both silently escaped.

**A `fold` region, with `RegionKind` gaining a seventh member:**

```
region fold total from n02.zero over n01.out.lines as line#id : Money
       [on_element_failure: continue] {
  n03 pure amount = LineAmount@1.0(line: line#id.elem) : Money
  n04 pure next   = AddMoney@1.0(acc: total.in, add: n03.out) : Money
  yield n04.out
}
```

- The body is an ordinary node block, so every node in it is a static node the rules already see.
- `total.in` is the accumulator's per-iteration in-port; `yield` names the out-port feeding the next iteration; the region's out-port carries the final value.
- **Revision 6's "inline at edit time" claim did not apply here and conflating them was an error.** A loop or fold *body* is a static subgraph iterated at runtime. Edit-time inlining (§5.8.2) is about substituting a *molecule's* body at a call site. Those are different operations, and a runtime-sized iteration obviously cannot be unrolled at edit time.

**Correction to revision 6's causal story. [R7]** It claimed §6's exclusion of first-order function references "silently took `Fold` with it." That is not what happened: fold binds its body the same way every `pure` node does, and §16.4's exclusion concerns function-typed *values flowing as data*, which is a different construct. `Fold` was simply omitted. `plan.md:2195` still requires it; the manufactured explanation does not survive.

**Lowering** is a `for` loop with an accumulator variable — no recursion, consistent with §11.4.

#### 5.8.1a Two control decisions the notation cannot express **[R7 — found by writing programs, not by auditing rules]**

Neither of these is a lowering gap. An author cannot state the intent at all.

**Retry trigger.** `region retry [max: 3, backoff: exp]` bounds *how many times* and *with what backoff*, but nothing says *on what*. §5.6 filed this as "retry operates beneath the notation," which is true of the mechanism and false of the specification — "retry on `ConfirmedFailure` but not on this other terminal variant" is ordinary intent with no home. **Proposed:** `Variant.Disposition` gains `retryable`, and a retry region repeats on any arm carrying it. Recorded as §16.19, since disposition is this document's construct rather than the plan's.

**Loop and fold element failure.** §5.6 says the next-element transition is not materialized, so there is no edge on which to hang "continue past this element" versus "abort the whole iteration." Program 4 of §5.8.6's sample — process a batch, continue past individual failures, summarize — cannot be written. **Proposed:** an `[on_element_failure: continue | abort]` region attribute, defaulting to `abort`. This rides the existing attribute mechanism (`plan.md:1144-1152` already freezes region attributes) rather than extending the notation's item list.

#### 5.8.2 Molecules and Tier 1 bodies — composition

`plan.md:1707-1715` says a Tier 1 graph-native atom is "implemented entirely using graph nodes, kernel atoms, and other graph-native atoms." A graph *is* an atom body, and the IR never said what a `pure` node bound to a Tier 1 atom does with that body. **That gap is real and plan-grounded; the framing below is not.**

**"Molecule" is this document's borrowing, not a plan concept. [R7]** `plan.md:1541` and `plan.md:1612` mention molecules in single clauses of prose, and **there is no Concept Vocabulary entry for it** alongside Atom, Graph, Evidence, Flow, and Episode at `plan.md:340-414`. The plan also uses the word differently elsewhere — `plan.md:1267`'s "the corrected payment molecule" means the worked example graph, not a composition tier. Revision 6 presented an invented compositional level as plan-defined. The Tier 1 body question stands on `plan.md:1707-1715` alone.

Two options, and the choice has consequences for every rule:

| | Inline at edit time | A `call` node |
| --- | --- | --- |
| Rules 1, 3, 4 | Unchanged — still intragraph reachability | Become **interprocedural**; the plan specifies nothing for this |
| Graph size | Explodes; reviewability suffers | Stays small |
| Rule 1 cost | Pairwise over the *expanded* node count | Needs a summary-based analysis |

**Recommendation: inline at edit time**, matching how `plan.md:2197` already treats templates — "expands at edit time into ordinary nodes, and the expanded graph is validated and lowered." Every rule survives untouched, which is worth the size cost for a disposable experiment.

**One consequence is an authoring rule, not a footnote. [R7]** Inlining the same Tier 1 atom at two call sites produces **two distinct derivation instances**. A key routed through a shared helper is therefore *not* singleton and fails Rule 2 — even where the two sites are provably mutually exclusive or compute from identical inputs. Revision 6 called this "surprising"; it is stronger than that, because it makes a shared key-derivation helper unusable across branches, which is the main thing Tier 1 composition exists for.

> **Derive keys before branching, never per-branch.** A key must be produced by one derivation instance that dominates every consumer. Hoisting it above the branch point is the only way a shared helper is usable, and the frozen molecule already does this — `n03 DeriveKey` sits above every match.

This is `plan.md:1588` reached from an angle the plan does not illustrate.

#### 5.8.3 Templates — reuse

`plan.md:2197`: structural macros declaring named holes with required signatures, capabilities, and obligation contracts, expanded at edit time. Absent from the IR. They are an **edit-time construct, not a node kind**; a template instance needs permanent identity because `plan.md:2342` requires it for "template instances" by name, and §11.7.2's diff suite requires a template-version diff.

#### 5.8.4 The workflow as a callable unit

The frozen header is `workflow ReceivePayment v1` plus `capabilities:` and `effect_identity:`. Nothing said what that *is* as a value.

**This definition is this document's, with no plan citation. [R7 — revision 6 set it as a blockquote, the convention this document otherwise reserves for cited plan text, and §17 and §21 then repeated it with the confidence of a sourced claim.]** `plan.md` gives only the frozen header at `1159-1161` and never defines a workflow's signature or result type. `plan.md:1110` and `plan.md:1284` establish that *some* such mechanism is needed for Arm C; they do not establish this one. Recorded as §16.20.

**Proposed:** a workflow is a callable unit with signature `(In₁ … Inₙ) -> Result`, where the inputs are its `input` nodes in declared order and `Result` is the tagged union of its `return` nodes' payload types. The molecule's `n11 return Declined` and `n12 return Success` then give `ReceivePaymentResult = Declined | Success`.

**Three things this needs that the notation does not show. [R7]**

1. **No syntax delivers a value to a `return` node.** Every example is bare — `n11 return Declined` — while `merge` shows its inputs explicitly as `<- [n06.c, n09.c]`. A payload-carrying return needs a form the notation never demonstrates.
2. **Nothing enforces distinct variant tags across `return` nodes.** W5 checks label distinctness within one `Tagged` out-port; no check spans return nodes, so two returns sharing a tag and payload type silently collapse into one variant and the caller cannot tell which path ran — the exact failure this section's own prose forbids. **W10 proposed:** return-tag distinctness, workflow-scoped.
3. **A graph with zero reachable `return` nodes** yields an uninhabited `Result` and nothing rejects it — a plausible authoring bug from a dangling match arm. W10 covers this too.

#### 5.8.5 Two things that are expressible but were never stated

**Data construction** is kernel atoms — `plan.md:1659-1660` lists "Tuple and record construction" and "Option and Result construction" — so a record is built by a `pure` node, not a dedicated node kind.

**Value predicates** are the same move: `match` dispatches on a variant, so branching on `amount > 100` is a `pure` node returning `Tagged{Over | NotOver}`. Every conditional in a graph is a tagged-union match; there is no boolean `if`.

#### 5.8.6 Representability must be assessed before task one **[R6]**

`plan.md:1284` makes representability corrections a logged Arm C cost, so they cannot be discovered during the benchmark.

> Before task one, model a pre-registered sample of the fifty change requests in the notation and log every construct that cannot be expressed. If the sample cannot be modelled, the notation is not ready and no amount of validator work compensates.

This document has never done that, and the four gaps above are what one afternoon of trying would have surfaced immediately.

### 5.9 The control-flow surface **[R7]**

Everything the design needs in order to express control flow, in one place. Seeded as retrievable atom documentation by `internal/atomcatalog` (§5.9.1).

| # | Control flow | Graph form | Strict-FP precedent (§3) | Go lowering | Status |
| --- | --- | --- | --- | --- | --- |
| 1 | **Sequence** | control edge | `bind` / `andThen` — all six | statement order | ⚠️ **undefined**, §5.7 |
| 2 | **Branch** | `match` on a `Tagged` out-port | `match` / `case` — all six | `MatchX[R]` eliminator | frozen |
| 3 | **Join** | `merge` node | `Task.map2..map5` (Elm) | assignment from the taken branch | frozen |
| 4 | **Map** | `region loop` | `traverse` | `for … range` | frozen |
| 5 | **Fold** | `region fold` | `fold` — all six | `for` + accumulator | new, §5.8.1 |
| 6 | **Retry** | `region retry [max, backoff]` | **none** | bounded `for` | trigger unauthorable, §16.19 |
| 7 | **Poll** | `region reconcile [max_polls]` | `unfold` + `take_while` (OCaml) | bounded `for` | frozen |
| 8 | **Claim gate** | `region claim [dedup]` | **none** | conditional on `Acquired` | Rule 4 open, §16.6 |
| 9 | **Suspend** | `wait durable [timeout]` | **none** | state machine \| replay \| checkpointed | mechanism open, §16.17 |
| 10 | **Bounded escape** | `BoundExhausted` region edge | `boundary` / `break` (Scala 3) | labelled break | new, §13.4 |
| 11 | **Recover** | `reconciles` relationship | `mapError` / `onError` | variant routing | frozen |
| 12 | **Compensate** | `compensates` relationship | `Fun.protect`, partially | deferred call | §16.5 |
| 13 | **Seal panic** | supervised call boundary | `NonFatal` (Scala) | `defer`/`recover` + goroutine + timeout | §10.4 |
| 14 | **Terminate** | `return` node | `identity` | `return Outcome[R]` | payload syntax missing, §5.8.4 |
| 15 | **Element-failure policy** | `[on_element_failure]` attribute | **none** | `continue` vs `break` | proposed, §16.19 |

**Four of fifteen have no precedent in any surveyed standard library** — retry, claim gate, suspend, and element-failure policy — and three of those four are also the rows carrying open plan questions. The correlation is the §3.3 finding restated: the constructs this design had to invent are the ones that keep needing decisions.

**Supporting kernel atoms.** Control flow cannot be expressed without these Tier 0 operations (`plan.md:1652-1666`): tagged-union matching (rows 2, 10, 11); comparison yielding a `Tagged` result, since §5.8.5 establishes there is no boolean `if` and every value predicate is a match; `Option`/`Result` construction (rows 11, 14); list traversal (rows 4, 5); stable ordering, which `fold_key_safe` (§7.3) depends on.

**Generated Go helpers**, monomorphic per type, so their count scales with distinct variant and boundary types (§11.1): `MatchX[R]`, `Outcome[R]`, `copyX`, the supervised call, and the per-`NodeID` fixture counter.

Row 1 deserves the most attention: **sequencing is the most basic operation here and the notation does not define it.** Every other row inherits that gap.

#### 5.9.1 Seeding

`internal/atomcatalog` declares these fifteen as schema-v1 atom documentation and returns typed records for the storage lane to persist, matching how `internal/atomdoc` and `internal/atomname` already behave. `SeedControlFlowCatalog` writes them through `storage.CreateAtomDocumentationRevision`, so they are retrievable through the same path as any other atom.

They are declared **Tier 1 graph-native**, which `atomdoc.ClassifyAtomDocumentationAuthoring` maps to SQLite-authored — correct, because a control-flow construct has no Go implementation of its own (`plan.md:1707-1715`).

> **This catalog is a proposal, not a kernel declaration.** `POST-002` in `internal/deferred` forbids claiming "that the kernel's scope is known," so every entry records itself as this document's §5.9 proposal. Seeding retrievable documentation is the shipped M21 retrieval lane; it does not implement, freeze, or scope a kernel, and nothing in the catalog may be read as doing so.

## 6. The type layer

**Included** (`plan.md:2173-2183`): primitive scalars, named records, tagged unions, `Option`, `Result`, bounded collections, effect request/response types, capability sets. **Excluded by the plan:** unrestricted higher-order functions, closures, general recursion, dependent types, unrestricted refinement types, open effect rows, implicit polymorphism. **Excluded additionally here:** first-order function references, never exercised by the notation (§16.4).

### 6.1 `TypeRef`

`TypeRef` is a **nominal** reference into a **project-scoped** type table. **[R4 — revision 3 said workflow-scoped, which cannot work: `ContractStub.Signature` types an atom's inputs and outputs, and atoms are shared across workflows, so two workflows would resolve the same atom's declared types against two independent tables with no rule making them denote the same type.]**

Types are equal iff their `TypeRef`s are identical: no structural equality, no subtyping, no implicit coercion. A conversion is an explicit `pure` node bound to a kernel conversion atom (`plan.md:1665`).

**Parametric types are registered instances, not constructors applied at use. [R4]** `Option<Charge>` and `Option<Failure>` are two separately registered nominal types. Nothing generically ranges over "any `Option`" — that would need parametric polymorphism, which `plan.md:2193` excludes. **This is a consequence, not a design goal, and §16.11 records the cost:** every instantiation an atom needs must be pre-registered, and the kernel's `Option`/`Result` operations must be re-declared per instantiation.

### 6.2 Boundary types

**Boundary types are drawn from the §6 type layer only. [R4 — revision 3 stated this as "no unexported field transitively" over arbitrary Go types, which is the same rule at the wrong level and reads as unusably strict.]**

A value crossing a Tier 2/3 atom boundary is a graph type — a named record of scalars, a tagged union, an `Option`/`Result` instantiation, or a bounded collection of those. Foreign Go types never cross; the Tier 3 adapter converts. That is why §2.1 budgets a shadow-type library: a graph `Money` rather than `decimal.Decimal`, a graph `Timestamp` rather than `time.Time`.

The mechanical consequence is the same restriction revision 3 stated — no channels, function values, `sync` primitives, or unexported fields anywhere in the transitive shape — but as a *property the type layer already guarantees* rather than a filter applied to arbitrary Go. Verified: a `go/types` walk over `time.Time`, `net/url.URL`, `math/big.Int`, `strings.Builder`, and `decimal.Decimal` rejects all five, which is why they are converted rather than restricted.

**Enforcement takes two passes, not one. [R5]** Go enforces nothing here — verified: an adapter signature crossing `decimal.Decimal` and `time.Time` directly compiles with zero errors.

- **Pass A, boundary signatures.** A `go/types` walk over every Tier 2/3 adapter's exported signature, checking each parameter and result against the registered graph-type whitelist. Verified to catch the direct violation above.
- **Pass B, registration-time transitive shape.** Pass A alone is defeated by the whitelist itself: a *registered* graph type whose own field smuggles a foreign type — `CorruptedTimestamp{ At time.Time }` — passes Pass A cleanly, verified. So the transitive-shape walk must run **every time a `TypeRef` is registered**, not once against a list of realistic candidates.

Revision 4 named only Pass A as what enforces §6.2, which left the whitelist itself as a laundering vector.

**Verified: Pass B closes that hole and a harder one — and has a third the document had not connected. [R5]** Pass B correctly rejects `CorruptedTimestamp{ At time.Time }` at registration, and also `type Timestamp time.Time`, which a name-only check would miss.

But Pass B must enumerate a sealed union's declared variants rather than reject every interface-typed field, or it would reject legitimate §6 tagged unions. Verified: a **package-scoped** enumeration — the natural reading of "run the walk when a `TypeRef` is registered" — cannot see a variant added from another package via §11.2's embedding trick. A `RogueVariant` embedding the sealed interface and carrying a live `time.Time` compiles, constructs, satisfies the union, and crosses the boundary unseen. Only a whole-module `types.Implements` scan finds it — **the same machinery §11.2's dispatch guard already needs, and inheriting the same "cannot reach a separate module" limit.** Those two facts belong together: Pass B's tagged-union coverage degrades exactly where Tier 3 adapters are most likely to live, in an independently versioned module.

**Contracts** (`plan.md:2215-2241`) use a restricted predicate language from kernel operations and carry one of five classes — executable, solver-supported, test-backed, assumed-external, human-reviewed — because `plan.md:2241` forbids reporting a natural-language contract as mechanically verified.

## 7. The provenance algebra

### 7.1 `Prov` as a least fixed point

```
Prov_0(p) = ∅

Prov_{k+1}(p) = case owner(p) of
    input node        →  { that node instance }
    pure node         →  { that node instance } ∪ ⋃ Prov_k(inputs)
    effect node       →  { that node instance } ∪ ⋃ Prov_k(inputs)
    merge node        →  ⋃ Prov_k(incoming)          -- no own instance; the phi rule
    loop element port →  { the loop's element binder instance }
    fold element port →  { the fold region's element binder instance }        -- [R7]
    fold acc in-port  →  Prov_k(fold seed) ∪ Prov_k(fold yield port)          -- [R7]
    fold region out   →  Prov_k(fold yield port)                              -- [R7]

Prov = the least fixpoint, reached in at most |Nodes| iterations
```

**The three `fold` cases are new. [R7 — revision 6 introduced fold and left `Prov` undefined on it**, so every rule depending on cone reachability downstream of a fold was running on an undefined term. The accumulator in-port is the genuine data cycle — it depends on its own yield port from the prior iteration — and is the clearest justification the least-fixpoint formulation has; revision 5 justified it with retry counters, which was thinner.]

Monotone unions over a finite-height lattice. Structural recursion would not terminate on a data cycle, which §7.3's `transient` class contemplates.

### 7.2 Derivation instances

```
derivation_instances(p) = the non-merge nodes reachable backwards from p
                          through merge nodes only, stopping at the first non-merge
```

**Not `Prov`.** `Prov(n03.key) = {n03, n01}` is not a singleton, so Rule 2's test would fail the plan's canonical example. `derivation_instances(n03.key) = {n03}`; a genuine two-source phi gives `{n_P, n_Q}`. Multiplicity enters at the phi, which is what `plan.md:1585-1588` describes.

### 7.3 The key cone and stability classes

The *cone* of a port is backward reachability through data edges — a visited-set walk, well-defined on cycles.

| Class | Contents |
| --- | --- |
| `durable` | Durable request identity, constants, randomness captured once and persisted before the first attempt |
| `stable-iteration` | A loop element-identity binder |
| `transient` | Post-attempt values, retry counters, transient worker identity, uncaptured time or randomness |

```
RULE2_cone_ok(port) :=
      ∀ e ∈ cone_edges(port):  e.Stability ∈ { durable, stable-iteration }
  ∧   ∀ n ∈ cone_nodes(port):  tier(n.Atom) ≠ Tier3
  ∧   ∀ n ∈ cone_nodes(port):  n.Kind ∈ { input, pure, merge }
  ∧   ∀ f ∈ cone_folds(port):  fold_key_safe(f)                    -- [R7]

fold_key_safe(f) :=
      f.Region contains no effect node
  ∧   ∀ e ∈ body_cone_edges(f):  e.Stability ∈ { durable, stable-iteration }
  ∧   f.combinator is declared order-independent
```

**A fold's *result* may enter a key cone; its *in-flight accumulator* may not. [R7 — revision 6 banned both, which is a false positive.]** The stated justification — the accumulator at iteration *N* depends on 1..*N−1* — only excludes accumulators whose per-iteration dependency crosses non-durable data. It does not support a blanket ban, and the counterexample is ordinary:

```
region fold earliest from const_max over n01.out.lines as line#id : Timestamp {
  n05 pure t = LineTimestamp@1.0(line: line#id.elem) : Timestamp
  n06 pure m = MinTimestamp@1.0(acc: earliest.in, t: n05.out) : Timestamp
  yield n06.out
}
```

This folds durable, input-derived values with a deterministic order-independent combinator. Its result is exactly as reproducible as the molecule's `n03 DeriveKey`, a plain deterministic function of durable input — yet revision 6 forbade it from a key cone on node kind alone. Note also that revision 6's ban was enforced only by `fold` being absent from the `n.Kind` set, not by the reasoning it gave.

## 8. Logical effect identity

```
LogicalEffectIdentity = operation_contract_id + provider_deduplication_scope + business_intent_key
```

Workflow-instance identity is excluded (`plan.md:1574`).

### 8.1 Three components, as frozen

Revision 1's four-component tuple was killed by two independent reviews: it contradicts `plan.md:1576`, which folds per-element identity **into** `business_intent_key`, and under §7.1 the loop element port resolves to the same static binder for every element, so a fourth component could not distinguish iterations anyway.

**The loop requirement is a Rule 2 cone sub-predicate:** for an effect inside loop regions `L1..Lk`, `cone(n.key_port)` must contain `binder(Li)` for every `i`. Verified for nested loops (walk the region ancestor chain) and for retry-inside-loop (composed with Rule 2's transient exclusion, this is exactly `plan.md:2209`).

**"Loops need no carve-out in Rule 1"** holds because Rule 1 quantifies over distinct static nodes and one loop body is one node.

### 8.2 A static structural equivalence

Rule 1 is decidable only if LEI is structural. `plan.md:1588` grounds the derivation-instance half directly and the other two by extension.

### 8.3 Where each component comes from

**`operation_contract_id`** — from the Tier 3 atom's contract (`plan.md:1574`: "the pinned external operation rather than a wrapper function").

**`provider_deduplication_scope` — UNRESOLVED, §16.9.** Assigned wrongly twice; not guessed a third time.

**`business_intent_key`** — the Rule 2 singleton derivation instance (§7.2).

**What `contract:` is.** From `plan.md:2057-2063` — *a different passage from the eighteen-field list; the switch is flagged deliberately* — it discharges the deduplication strategy's required contract obligation. Neither operation identity nor provider scope.

### 8.4 The effect declaration splits by variability

**Intrinsic — Tier 3 atom contract:** pinned operation contract, effect kind, read/write classification, capability, request type, response variants with dispositions, ambiguous-outcome policy, key provenance requirements, security classification, observability requirements.

**Site-specific — `Node.Attrs` and region attributes:** deduplication strategy, retry policy and bounds, timeout, ordering constraints, compensation and reconciliation relationships, conditional durable-claim requirements, and the versioned contract a strategy requires.

## 9. Validation

`plan.md:1266` requires four structural rules frozen before task one. This section specifies those four plus named well-formedness preconditions.

**Preconditions are validator machinery, not graded properties. [R4]** Revision 3 wrote that "any check that blocks validation produces the structural violation the McNemar endpoint measures." That is wrong. `plan.md:1325-1327` defines the primary endpoint as "does the **modal accepted output** contain a structural-effect violation under **blind grading**" — graded by an adjudicator on output, not by the validator. A blocked graph never reaches output. **The blind adjudicator grades against the plan's four structural properties only (`plan.md:1120-1125`), identically for Arm B and Arm C.** What preconditions do feed is `plan.md:1316` — a validation failure is a modeling defect counted against Arm C — so their cost lands in the time ledger and the defect count, not the primary comparison. That asymmetry is real and must be pre-registered.

**Disposition.** A rule or precondition violation blocks lowering; the graph is rejected. A conditional warning (§9.2) does not block and is counted separately.

### 9.1 Well-formedness preconditions

| ID | Check | Source | Traced? |
| --- | --- | --- | --- |
| W1 | The progress subgraph is acyclic | **Proposed** — §16.3 | Yes |
| W2 | **Every region** has exactly one entry node; any number of edges may target it **[R5]** | **Proposed** | Yes |
| W3 | Every data edge's type equals both endpoint port types, and every merge's incoming port types equal each other and the merge's declared output type | **Proposed** | **No — §9.7** |
| W4 | Every write effect has exactly one **effectively resolved** deduplication strategy | `plan.md:2055` — a mandate ("an undeclared strategy is a validator error") | Yes |
| W5 | For each `Tagged` out-port, outgoing control edge labels are **distinct** | **Proposed** — required by Rule 1's soundness | Yes |
| W6 | Exactly one node has no incoming control edge, and its `Kind` is `input` | **Proposed** | Yes — rejects the molecule twice (§15.2) |
| W7 | Every effect's required capability is contained in the workflow header's declared set | **Proposed** — restored **[R5]** | No |
| W8 | Every `Tagged` out-port's variant set is **covered**, and every bounded region has **exactly one** `BoundExhausted` edge **[R5]** | **Proposed** — `plan.md:2091` lists `AllResponsesHandled` under "**Potential** proof obligations," a hedged vocabulary list, not a mandate | Yes |
| W9 | No attribute key is declared **at two region levels** in one node's ancestor chain **[R5]** | **Proposed** | Not exercised |
| W10 | A workflow has **at least one** `return` node, and its `return` nodes carry **pairwise distinct variant tags** **[R7]** | **Proposed** — §5.8.4 | No |

**W7 is restored, because cutting it was a double standard. [R5]** Revision 4 cut it on the grounds that `plan.md:2086` sits under "**Potential** proof obligations include" — hedge language — and then justified the replacement by pointing at §11.6, which cites `plan.md:2255`'s "**Likely** mappings include: capabilities → narrow injected interfaces." That is the same class of hedge, and unlike every other Go-enforcement claim here it carried no verification, no test, and no conformance case. A precondition may not be cut in favour of a control that is merely asserted. W7 returns as an explicit proposal; §11.6's lowering-time interfaces are defence in depth, not the primary check.

**W9 constrains regions only. [R5]** Revision 4 wrote "at two nesting levels for one node," which made §5.3's own resolution rule dead: a node carrying its own `dedup` inside a region that also declares `dedup` *is* two levels, so the "node's own wins" clause could never adjudicate anything. A node overriding its enclosing region is legitimate, so W9 now forbids only two *regions* in one ancestor chain declaring the same key.

**No node in the frozen molecule exercises this. [R5, corrected]** An earlier draft of this paragraph cited `n07` and `n08` as the overriding case. They are not: both sit at the molecule's top indent level, siblings of `region issue` and `region reconcile`, which have already closed. They carry their own attributes with no enclosing region declaring anything, so no override occurs — matching the table above, which correctly records W9 as unexercised. The reasoning stands; the example did not.

**W8 gained a uniqueness clause. [R5]** W5 quantifies over `Tagged` out-ports, and a `BoundExhausted` edge has `From = RegionID` and no port at all, so it fell outside W5's reach entirely. Two `BoundExhausted` edges from one region would make region exit nondeterministic in precisely the way W5 exists to prevent.

**Source-column honesty.** Only W4 carries a genuine mandate; `plan.md:2078` heads the obligation list "**Potential** proof obligations include." Everything else is this document's proposal and is labelled so.

**W1's provisionality is load-bearing. [R4]** §5.5's dominance soundness and §9.4's Rule 4 termination both assume W1. **If the plan rejects the §16.3 amendment, both need a different basis** — dominance would have to be computed over the full cyclic graph using an iterative dominator algorithm, and Rule 4 would need an explicit worklist fixpoint rather than a topological pass.

**W4 checks the *effective* resolved strategy, not the node's own attribute. [R4]** Revision 3 left this open; own-only would reject `n04` and `n06`, which inherit from their regions, and §15 never listed that.

### 9.2 Rule 2 — stable shared-key provenance

```
for each effect node n with a declared key input:
    d := derivation_instances(n.key_port)
    if |d| ≠ 1:                       RULE2 @ n.key: derivation set {…} is not singleton
    if ¬RULE2_cone_ok(n.key_port):    RULE2 @ n.key: cone contains <transient|Tier3|effect>
    for each loop-kind ancestor Li:
        if binder(Li) ∉ cone(n.key_port):
            RULE2 @ n.key: cone omits the element binder of loop <Li>
```

**Relationship to Rule 1, and a threat the experiment must know about. [R4]** Rule 1 stays decidable when Rule 2 fails, by comparing derivation sets. But §7.1 erases merge identity, so two merges with **overlapping** sets compare unequal and raise nothing while possibly producing identical runtime keys. A non-empty intersection raises a conditional warning — over-approximating, since it fires on any shared ancestry.

**The threat:** a genuine duplicate issuance hidden behind that ambiguity validates cleanly, is never blocked, and therefore falls to `plan.md:1319`'s bucket 4 — "a genuine acceptance failure that counts in the experiment" — when it is actually validator incompleteness. That misattributes a false negative in "a validator that cannot forget" as an Arm C acceptance failure, in the experiment measuring exactly that claim. §17 lists the warning as cuttable for noise; **cutting it removes the only signal for this case**, and that trade must be made deliberately, not by default.

### 9.3 Rule 1 — no sequential duplicate issuance

```
precompute R(a) := forward progress-reachable set, one BFS per issuance -- O(k·(V+E))

for each unordered pair (a, b), a ≠ b, of write effects:
    if LEI(a) ≡ LEI(b) ∧ ( b ∈ R(a) ∨ a ∈ R(b) ):
        RULE1 @ a, b : same logical effect identity on a sequential path
```

Pairwise is complete — reachability is transitive, so a chain is caught by its pairs. Soundness depends on W5.

### 9.4 Rule 4 — atomic claim gates issuance

**Specified three times, wrong three times. What follows is the fourth attempt, and §16.6 records the plan question it still depends on. [R4]**

```
guarded := { n | n.Kind = effect ∧ n.classification = write
               ∧ effective_dedup(n) = LocalAtomicClaim
               ∧ n.Region.Kind ≠ claim }

requires: W1

for each n ∈ guarded, with k := derivation_instances(n.key_port):

    Claimed(v) := false                    for the root and for every node
                                           with no progress-predecessors
    Claimed(v) := ⋀ over progress-predecessors u of v:
                     Claimed(u) ∨ confirmsClaim(u → v, k)

    confirmsClaim(u → v, k) :=
          ( u.Kind = effect ∧ u.Region.Kind = claim
            ∧ derivation_instances(u.key_port) = k
            ∧ label(u → v) = Acquired )
       ∨  ( ∃ Relationship{Kind = reconciles, Subject = u, Object = c}
            ∧ c.Region.Kind = claim
            ∧ derivation_instances(u.key_port) = k
            ∧ derivation_instances(c.key_port) = k                    -- [R4]
            ∧ label(u → v) = Acquired )

    if ¬Claimed(n):  RULE4 @ n : a path reaches issuance without a confirmed claim
```

**Complexity is `O(|guarded|·(V+E))`, not `O(V+E)`. [R4]** The fixpoint is nested per guarded node because `k` depends on `n` — the same shape stated correctly for Rule 1. Revision 3's header contradicted its own pseudocode three lines below.

**The second disjunct now checks the claim's key too. [R4]** Revision 3 checked only the reconciler's key, so a node reconciling an *unrelated* claim could satisfy it by key coincidence.

**On the frozen molecule, `guarded = ∅`, and revision 3's trace of `n06` was invalid. [R4]** `n06` inherits `ProviderIdempotency` from `region issue`; `LocalAtomicClaim` is declared on `region claim`, which is a **sibling** of `region issue` (§5.2), so §5.3's ancestor walk can never reach it. `n04` is the only node resolving to `LocalAtomicClaim` and is excluded as a claim. Revision 3 nonetheless printed a full `Claimed(n06)` trace as evidence its fix worked — running an algorithm on an input its own quantifier excludes. That paragraph is deleted, not corrected.

**The fixpoint logic is now verified correct — on four purpose-built graphs, not on the molecule. [R5]** Independent tracing confirms: a query-then-issue graph with no claim anywhere is correctly rejected; a correctly claim-gated issuance passes with no false positive; the reconciliation disjunct fires for an ambiguous claim reconciled to `Acquired`; two claims on different keys correctly fail the key check rather than passing spuriously; and an issuance reachable both through a claim and around it is correctly rejected via the AND over predecessors.

**This holds only under §5.7's convention C1**, without which `n06` has no progress-predecessors at all and `Claimed` returns its base case.

**Consequences, stated rather than papered over:**

- **Rule 4 is not exercised by the plan's worked example**, so §9.6's hand-trace requirement cannot be discharged against the molecule.
- **The mandated conformance case can only be built in a shape that avoids the failure mode. [R5]** Under §16.6's reading-1, *any* graph where the claim region is a control-flow sibling of the issuing region — the molecule's actual topology — yields `guarded = ∅` for that issuance. Populating `guarded` requires the issuing node or region to independently redeclare `LocalAtomicClaim`, which is a different graph: "a node carries the same dedup tag as some upstream claim region," not "a claim gates an issuance via control flow." So `plan.md:2158`'s case is constructible, but not in the shape that broke this rule three times.
- **§16.6 remains open**: does a region's `dedup:` describe its own node, or scope to the issuances it gates? This quantifier assumes the first. `plan.md:2059-2060` defines `LocalAtomicClaim`'s obligations as properties of the *gated issuance*, which argues for the second — but nesting cannot express a control-flow relationship, so the second reading needs a mechanism this design does not have. **The bullet above is the strongest argument yet that reading-1 is wrong.**

`plan.md:2134-2143` requires the claim to distinguish `Acquired`, `Completed`, `InProgress`, `TerminalFailed`, with only `Acquired` reaching new issuance.

### 9.5 Rule 3 — reconciliation gates compensation

```
requires: `effect` has a Tagged result with an ambiguous-disposition variant

for each Relationship r with Kind = compensates:
    effect := r.Object ; compensator := r.Subject
    start  := the control edge from `effect` labelled with its ambiguous-disposition variant
    G'     := progress_subgraph minus
              { control edges from nodes reconciling `effect`
                whose label's Variant.Disposition = confirming }
    if reachable(start, compensator) in G':
        RULE3 @ compensator : reachable from ambiguity without confirmed reconciliation
```

Deletion is of confirming **edges**, not nodes: whole-node deletion removes `n09`'s inconclusive continuation and the `start` edge itself. "Confirming" is a declared `Disposition` (§5.3), not a tag spelling.

**Verified on the molecule.** Start at `n06`'s ambiguous edge to `n09`; delete `n09`'s confirming arms; `n08` is unreachable — passes. `n08` stays reachable via `n07 PersistFailed(e) -> n08`, a different start, correctly out of scope.

`BoundExhausted` carries `inconclusive`, so its edge is not deleted and an exhausted reconciliation correctly still fails to gate compensation.

### 9.6 Hand-tracing obligation

All four rules and all preconditions must be hand-traced before freeze.

| Check | Status |
| --- | --- |
| Rule 1 | Traces cleanly **given §5.7's C1**; without it, reachability undercounts badly **[R5]** |
| Rule 2 | Traces cleanly |
| Rule 3 | Traces cleanly (§9.5) |
| Rule 4 | **Cannot be traced on the molecule** (`guarded = ∅`); verified on four purpose-built graphs (§9.4) |
| W1, W2, W4, W5, W6, W8 | Traced |
| **W3** | **Not traced — see §9.7** |
| W7, W9 | Not exercised by the molecule |

Revision 1 traced only Rule 3; revision 2 asserted Rule 4; revision 3 traced Rule 4 on a node its own quantifier excluded; revision 4 asserted W3.

### 9.7 W3 is unverified and may block the molecule **[R5]**

For `m01 merge charge <- [n06.c, n09.c] : Charge` to pass W3, `TypeRef(n06.c) = TypeRef(n09.c) = TypeRef(m01.charge)` must hold under §6.1's **nominal-only** equality. But `n06` (`IssueCharge@3.1`) and `n09` (`QueryCharge@1.0`) are independently contracted Tier 3 atoms, and nothing in §6.1, §14's `ContractStub`, or anywhere else requires two separately authored contracts to converge on one registered `TypeRef` for "a confirmed charge."

Under strict nominal identity, two structurally identical but separately registered `Charge` types **fail W3 and block the molecule**, requiring an explicit conversion node the notation never shows. Either the type table must be authored so both atoms reference one registered `Charge`, or nominal equality is the wrong choice for merge inputs. This is a consequence of revision 4's own nominal-`TypeRef` decision and is unresolved.

## 10. The atom boundary

### 10.1 Tier is computed, not declared

```
floor(atom) := non-empty if any of:

  IMPORTS (prefix match)
    time, os, os/exec, net, syscall, runtime, reflect, unsafe,
    sync (incl. sync/atomic), math/rand (incl. math/rand/v2), crypto/rand

  SYNTAX
    a `go` statement; a `range` over a map; a write to a package-level var
    (including qualified SelectorExpr targets); an `init()`; a call through
    a non-narrow interface

  OPAQUE TO GO ANALYSIS — automatic floor
    `import "C"` anywhere transitively; any package containing .s files
```

Per-platform, recomputed for every target in §13.6. Needs the reviewed-exception mechanism `internal/atomname` already has. Still the highest-value control here and the only one implementable before atom contracts exist.

### 10.2 Correlation controls

`plan.md:1889-1896` requires different agents for implementation and reference model, the second from upstream specification. `plan.md:1727-1729` forbids calling a modeled atom proven merely because both agree. For the experiment, a flag on the contract stub.

### 10.3 Unmapped dependency errors degrade to ambiguity

> The Tier 3 adapter's error mapping must be total by construction, and its default arm is `AmbiguousOutcome`, never `ConfirmedFailure`.

A new error type after a version bump routes to reconciliation, not compensation (`plan.md:2076`). `plan.md:2298` forbids matching on message strings.

### 10.4 Panic sealing is partial, and the goroutine leak is unclosable

**Seals:** a synchronous panic in the calling goroutine becomes `AtomContractVoided` (§11.2 for how that value is represented).

**Does not seal** — all verified:

| Escape | Behaviour | Control |
| --- | --- | --- |
| Panic in a goroutine the atom spawned | Kills the process; verified exit code 2 | Process supervision; the floor flags `go` statements |
| `fatal error:` — stack overflow, OOM | `runtime.throw`, unrecoverable | Process supervision; §11.4 removes the main self-inflicted source |
| `runtime.Goexit` | All defers run, `recover()` returns nil, caller never resumes | Supervised call, below |
| **Atom that blocks forever** | **Nothing reclaims it** | **Process supervision only** **[R4]** |

**The supervised call, and what it does not do. [R4]** Run the atom on its own goroutine with a buffered completion channel and a deferred send; classify `Goexit` and in-goroutine panics correctly; apply a timeout.

**The timeout does not close the leak.** Verified: 200 calls against a permanently-blocked atom leave 200 parked goroutines. Go has no primitive to terminate a goroutine blocked on a channel receive or a syscall — this is a language limitation, not an implementation gap. The timeout changes only *when the caller stops waiting*, and it makes the leak invisible where previously it also hung the caller. Revision 3 said the timeout was "therefore mandatory" as if it fixed this. **Long-running processes hitting blocked Tier 2/3 atoms leak goroutines without bound; only process-level supervision and restart bounds it.**

**The timeout `select` is itself a determinism hazard** — see §11.9.

**Cost:** 611.5 ns/op against 0.21 ns/op direct.

### 10.5 Boundary copying uses generated per-type copiers

Reflective deep copy silently drops unexported fields — `CanSet()` is false, no error, wrong data. Generated code cannot reach a foreign type's unexported fields either (verified: compile error). **§6.2's type-layer restriction is what actually closes this**, by ensuring foreign types never cross a boundary at all. Cyclic values need a visited set or an acyclicity declaration. `no-alias` remains an evidence-bearing claim demonstrated by differential testing.

### 10.6 Dependency bindings must bind the build

Toolchain, OS and architecture, module, version, configuration (`plan.md:1898-1904`), plus the **transitive** module-graph hash, build tags, and the platform the effect floor was computed against. For the experiment: `ContractStub.Bindings`, an opaque recorded string.

## 11. Lowering to Go, traceability, and debuggability

Priority order at `plan.md:2261-2269`: semantic fidelity, **traceability**, **debuggability**, stable generation, idiomatic presentation.

**Two of the top three are traceability and debuggability, and they outrank both stable generation and idiomatic presentation.** §11.7 and §11.10 own them. Everything this design spends on the first priority — eliminators, `Outcome[R]`, monomorphization, digests, redaction — is drawn from the third, so §11.10 keeps the running balance.

### 11.1 Sharing and exhaustiveness are a trade-off

Go has no higher-kinded types, so no generic `Bind[F[_], A, B]`. But shared generic envelopes are implementable:

> No design can have both a fully shared generic runtime **and** compile-time exhaustiveness.

**Two cost dimensions.** Verified via `go tool nm`: GC-shape stenciling collapses four pointer-shaped type arguments into one compiled body while six value-shaped arguments each get their own. But **generated source volume does not benefit** — the generator emits one call-site expression per (node, return type) pair regardless. Reviewer burden and §2.1's accounting scale with source, not object code.

### 11.2 Seal, eliminate, and guard

**Sealing is not airtight, verified.** Embedding the interface itself in a foreign struct promotes the unexported method and satisfies it with no implementation. Four forms compile: direct, through a type alias, in an anonymous struct, and at two removes through an intermediate struct. Go closes one sub-form — `embedded field type cannot be a (pointer to a) type parameter`.

**What arity buys, verified.** Adding a declared variant breaks compilation at direct calls, literal-typed function values, hand-written wrappers, and nested generic helpers. It does **not** give totality: a `func` parameter's zero value is `nil`; `var o PaymentOutcome` matches no case; `Match(o, f, f, f)` type-checks.

**The eliminator returns `Outcome[R]`, not `R`. [R4 — revision 3's guard 1 was unimplementable.]**

```go
type Outcome[R any] struct { Value R; Voided bool }

func MatchPaymentOutcome[R any](
    o PaymentOutcome,
    onConfirmedSuccess func(Charge) Outcome[R],
    onConfirmedFailure func(Failure) Outcome[R],
    onAmbiguous       func(Ambiguity) Outcome[R],
) Outcome[R]
```

The fallback must return a value of the generic type, and the only expression valid for every `R` is `var zero R`. Verified: with bare `R`, a voided contract and a legitimate `$0` payment both return `0`, and a voided contract and a genuine `false` both return `false`. Verified with the wrapper: all three of `int`, `bool`, and `string` become distinguishable, and arity exhaustiveness survives — adding a fourth sealed variant still breaks a three-handler call site with `not enough arguments in call to MatchPaymentOutcome[int]`.

**`Voided` is set only by the platform, never by a generated handler. [R5 — without this rule the mechanism collapses under composition.]** Verified: when two eliminators chain, as a graph makes them, a generator with only one bit encodes *upstream declared failure*, *downstream declared failure*, and *genuine contract void* identically as `{Value:"", Voided:true}`. One collision was traded for a worse one a level up.

> `Voided` is the platform channel. It is set by exactly two things: the eliminator's own fallback (unknown dynamic type, nil interface, nil handler) and §10.4's panic/`Goexit` boundary. **A generated handler for a matched arm must never set it.** A declared failure variant is an ordinary graph value and flows as `R`, carrying the downstream node's own `Result` type — the same intrinsic-versus-platform separation §13.4 applies to `BoundExhausted`.

**Verified to fix chain composition, with three consequences the rule does not cover. [R5]** A two-node chain — A returning `ConfirmedSuccess | ConfirmedFailure | Ambiguous`, B reached only on success — now yields four distinguishable results where round 4's collapsed three into one bit.

1. **`R` is forced to become a sealed type** whenever branches carry heterogeneous declared shapes, since every declared outcome must flow as a graph value. A homogeneous-terminal chain can keep `R` scalar; the general case cannot.
2. **The nil/`Voided` ambiguity reopens one level down.** With `R` a sealed interface, `var zero R` is `nil` — fine, because `Voided` disambiguates it. But verified: a matched-arm handler may return `Outcome[R]{Voided:false}` with `Value` left nil, producing a "successful, non-void" result carrying no business value. Nothing in the two-source rule addresses that state.
3. **One case cannot follow the rule's letter.** A downstream atom with a `Single` result has no eliminator, so its §10.4 panic seal has nowhere to live except the *upstream* matched-arm handler — which must then set `Voided` itself. The intent is forwarding a platform signal rather than declaring a business outcome, but the text does not distinguish the two.

**This is a discipline, not a guarantee, and Go will not help.** Verified: a handler returning `Outcome[int]{Value: 42, Voided: true}` on a matched arm compiles and runs, and a caller reading `.Value` while ignoring `.Voided` compiles with no warning from the compiler or `go vet`. Since all such code is generated (below), the discipline binds the generator rather than a human — which is the only reason it is acceptable.

**Guard: provenance, not shape. [R5 — the third attempt at this control, and the first that is implementable.]**

Revisions 2 and 3 banned variadic lists, `map[Tag]func`, and handler structs; round 3 verified three more forms (a tag-indexed handler slice, an embedded default-handler struct, functional options). Revision 4 replaced the blocklist with a type-level positive check — and that is **not implementable**, verified: a laundering `[]func() Outcome[int]` and an unrelated report-selector menu are byte-for-byte identical under `go/types`. Go slice and map indices carry no type-level relation to element types, and since Go forbids heterogeneous element types a handler table can never retain the per-variant payload types a checker would need. There is no signal to flag. The closest structural approximation flags every slice-or-map-of-func literal in the codebase, with false positives and no basis for auto-rejection.

Shape detection fails in both directions. The workable control is **where the code came from**:

> **All dispatch on a sealed type lives in the generated package**, whose contents are entirely generator output and whose import set is already constrained by §11.6. No hand-written code may be added to it, and the generator emits only fixed-arity positional eliminators. Enforcement is authorship and import analysis over one package — both mechanical — rather than shape analysis over the whole module.

**Verified implementable, with three limits. [R5]** A `go/packages` + `go/types` checker flagging any type-switch or type-assertion on the sealed type outside the generated package caught both hand-rolled dispatch sites in a test module and produced **zero false positives** on legitimate eliminator calls. That much is mechanically real. But:

1. **The literal rule criminalizes ordinary test code.** The checker also flagged unit tests, which must type-assert on a returned variant to assert anything meaningful. A blanket `_test.go` exemption reopens the hole, since test files hold arbitrary code. This needs a scoping decision the rule does not make.
2. **"Entirely generator output" has no technical enforcement.** A sha256 manifest detects a hand-edited file against the *old* manifest, but verified: a tamperer who re-runs the recorder and commits the new manifest verifies clean. Only re-running the actual generator in CI and diffing closes it — the guarantee is a process, not a check.
3. **Exported variant types already permit foreign dispatch** regardless of whether the eliminator is exported, because Tier 3 adapters must be able to construct variants. The residual gap — a foreign module importing the sealed interface and building its own dispatch — is therefore not closable here, and the eliminator's total fallback is the only backstop.

**Guard: whole-module semantic analysis.** Verified that a naive syntactic check misses the alias and two-removes forms while a `go/packages` + `types.Implements` walk catches all four. It needs an exclusion for a bare alias of the interface itself, and it **cannot reach a separate module** that imports the interface package. The eliminator's total fallback is the only backstop there.

**A stronger alternative worth recording. [R4]** A visitor interface with one method per variant was verified *not* to be a laundering vector: adding a method breaks every implementor at compile time, which binds implementors rather than call sites and is therefore stronger than arity. It remains subject to the same embedding hole. Not adopted — it inverts the call structure the notation implies — but it is the better mechanism if that inversion is acceptable.

### 11.3 The graph has no clock and no randomness

Both enter as `input` nodes captured once and persisted (`plan.md:2118`, `plan.md:2299`, `plan.md:2334`). `wait durable` is a declaration the durable runtime enforces.

### 11.4 No recursion in generated Go

No TCO, and stack overflow is unrecoverable. Lower bounded iteration, structurally decreasing recursion, and max-step declarations to `for` loops with explicit worklists.

### 11.5 `error` is never the graph's failure channel

It exists only inside Tier 3 adapters.

### 11.6 Generated code cannot reach impurity

Import set enforced by a CI analyzer using §10.1's list; impure access through narrow injected capability interfaces (`plan.md:2255`, whose wording is "**Likely** mappings include" — a hedge, not a mandate). **This is defence in depth for capability containment, not the primary check; W7 is restored as the primary check** (§9.1). **[R5 — revision 4 cut W7 and leaned on this, which substituted an asserted control for a verified one.]** This package's authorship constraint is also what makes §11.2's dispatch guard implementable.

### 11.7 Traceability: the identity chain, source maps, diffs, and blame

**[R5 — revision 4 covered source maps and none of `plan.md` §19's other requirements.]**

#### 11.7.1 The identity chain

Traceability means one question is answerable in both directions: *given a symptom, which graph node and which obligation produced it*, and *given a node, what does it become and what does it claim*. Every hop needs a durable key.

```
AtomID@Version ──contract──► ContractRef ──bound at──► Node.Atom
                                                           │
                                                     NodeID (permanent, §5.4)
                                                           │
                         ┌─────────────────────────────────┼──────────────────────┐
                         ▼                                 ▼                      ▼
                  generated symbol                  trace step (§13.4)     obligation (§12.1)
                         │                                 │                      │
                         ▼                                 ▼                      ▼
                   stack frame                    digest + field path      assurance + cone
                         │
                         └──► sidecar, generation-versioned (§11.7.3)
```

**The weak hop is symbol → `NodeID`**, because §5.4 permits label reuse and collision suffixes therefore shift (§11.7.3). Every other hop is keyed on a permanent identity.

**Requirement. [R5]** Every generated eliminator call and atom call site embeds its `NodeID` in a form that survives into a panic stack trace — a wrapper function whose name carries the ID, or an equivalent. Without it the one artifact a developer always has in a failure, the stack, is the one artifact that does not resolve back to the graph.

#### 11.7.2 Per-construct mapping and the diff suite

`plan.md:2380-2386` requires every generated construct to carry machine-readable mapping to **graph revision, node identity, contract identity, proof obligations, and generator version**. All five are sidecar fields.

`plan.md:2388-2398` requires nine diffs, each computable from artifacts this design already produces:

| Diff | Computed from |
| --- | --- |
| Graph semantic | IR diff over stable IDs (§5.4); tombstones distinguish deletion from replacement |
| Generated Go | AST comparison, not text (`plan.md:2434`) |
| Obligation | Validator report diff (§12.1) — added, removed, weakened, invalidated |
| Capability | Header declared set versus each effect's required set (W7) |
| Effect-trace | Trace step sequence diff (§13.4), digest-keyed |
| Data-provenance | Cone diff per port (§7.2) — which derivation instances entered or left |
| Region and merge | Region tree diff; merge in-port set diff |
| Effect-relationship | `reconciles` and `compensates` relationship-set diff |
| Pinned atom and template version | `AtomRef` diff |

`plan.md:2400-2407` requires a review to answer six questions. The first — what intent changed — is human; the other five are the diffs above. **An obligation that is *weakened* rather than removed is the one to surface loudest**, because it is the case `plan.md:2405` singles out and the one a diff most easily renders as noise.

**Blame.** `plan.md:2409` forbids assigning all generated lines to the latest generator run. Blame resolves through the generation-versioned sidecar to a `NodeID`, then to the graph revision that last changed *that node* — not to the regeneration that last rewrote the file.

#### 11.7.3 Source maps: node-ID keyed, generation-versioned

The sidecar is authoritative; comments are advisory, because comment-to-position association is purely positional and a generator assigning synthetic positions can misplace one silently.

**The sidecar keys on permanent `NodeID`, and historical sidecars are retained per generator run. [R4]** Verified: with the collision-suffix naming any realistic generator needs, an unrelated edit shifts an untouched node's symbol from `confirm_2` to `confirm_3` — and resolving the *old* symbol against the *current* reverse index returns the **wrong node**, not a miss. A bidirectional map is insufficient; the mapping must be pinned to the generation that produced the artifact being debugged, or `plan.md:2409`'s blame requirement silently resolves to the wrong construct.

**Three conformance tests:** position fidelity (generate, format, re-parse); regeneration integrity (regenerate after an unrelated edit, assert unchanged nodes still resolve); and historical resolution (resolve a symbol from generation *n* against generation *n*'s retained sidecar).

### 11.8 Generator versioning

`plan.md:2413-2440`; textual golden files must not be the primary mechanism. One generator version for the experiment, so the conformance requirement stands and the migration machinery does not.

### 11.9 Determinism hazards in the generator and runtime

All verified.

- **`sort.Slice` is not stable.** Use `sort.SliceStable` / `slices.SortStableFunc`.
- **`==` on an interface holding an uncomparable dynamic type panics.** No `==` on variant payloads containing collections; generate comparison functions.
- **`range` over a map is nondeterministic.**
- **`maps.Keys` iteration is the same hazard under new syntax [R4]** — and evades any static check grepping for `range` over a map. `slices.Sorted(maps.Keys(m))` is deterministic.
- **`sync.Map.Range` is nondeterministic [R4]** and is a distinct type from the plain-map bullet.
- **`select` with two ready cases picks uniformly at random** — verified 10059/9941 over 20000 trials. **This is the shape of §10.4's own mandated timeout `select`.** The supervised call must drain a completed result in preference to the timeout: a buffered completion channel plus a non-blocking recheck before treating a timeout as authoritative. **Verified to fully resolve genuine ties — 20000/20000 — but it narrows the hazard rather than closing it. [R5]** With the atom genuinely scheduled and its duration equal to the timeout, 80 of 2000 runs still timed out; at slightly under the timeout, 7 of 2000 did. That residual is goroutine-scheduling latency, which is what a deadline means and is not closable by any code. An `atomic.CompareAndSwapInt32`-guarded single decision is the stronger formulation. **Verified to behave exactly as claimed and no better [R5]:** it does not touch a true pre-tie, where Go's own random pick governs which case is even considered (9926/10074, unchanged); under real scheduling it leaves a residual of the same order as the non-CAS version. Instrumenting actual completion time against the nominal deadline found **0 of 215** timeout outcomes where the work had genuinely finished first — so CAS never loses to a legitimately earlier finisher, and the entire residual is scheduler latency inflating real completion past the deadline. **Deadline-adjacent completions are nondeterministic by construction, and conformance fixtures must not place an atom's duration near its timeout.**
- **`%v` on a struct with a pointer field prints the heap address**, which changes every run.
- **`time.Time`'s monotonic reading breaks `==`** — `t1 == t3` false while `t1.Equal(t3)` true after the monotonic reading is stripped.
- **Negative zero: `==`-equal but textually distinct [R4]** — `-0.0 == 0.0` is true while `%v` and JSON print `-0` versus `0`. A *disagreement* hazard: equality-based and text-based comparison give different answers about whether two trace steps match.
- **`%v` on nil and empty maps is identical (`map[]`) while JSON distinguishes them [R4]** — the same disagreement class, and directly relevant to §13.4's trace comparison.
- **Ruled out:** `fmt` sorts map keys when formatting; integer division and overflow are spec-determined; `errors.Join` preserves order; JSON struct-field order is declaration order; `range` over invalid UTF-8 is spec-deterministic.

### 11.10 Debuggability: the running balance **[R5 — new]**

`plan.md:2265` ranks debuggability **third**, above stable generation and idiomatic presentation. This design has been spending it steadily to buy the first two priorities, and until now those costs sat scattered as caveats with nothing owning the total.

| What was spent | Where | What compensates |
| --- | --- | --- |
| Eliminators deepen every branch's stack | §11.2 | §11.7.1's `NodeID`-bearing wrapper frames make the depth *informative* rather than noise |
| `Outcome[R]` wraps every value, and `R` becomes a sealed type in the general case | §11.2 | Nothing yet — a debugger sees a nested union where the graph shows a variant name. **Open.** |
| Trace comparison is digest-based, so a human sees *that* something differed | §13.4 | The field-path divergence hint (§13.4) |
| Redaction hides values, including the ones most likely to need debugging | §13.4 | Digest still proves inequality; the value itself is deliberately unavailable. **Accepted cost.** |
| Generated Go is unidiomatic by design | `plan.md:2269` | Explicitly in budget; reviewers will still feel it |
| Symbol names shift across regenerations | §11.7.3 | Generation-versioned sidecars |
| Monomorphization multiplies call sites per (node, return type) | §11.1 | One `NodeID` maps to many symbols — the sidecar must be one-to-many, and §11.7.1's reverse lookup must return the set |
| Boundary copies break value identity across an atom call | §10.5 | Nothing. A pointer-equality intuition is simply wrong here. **Accepted cost.** |

**Two are unmitigated and should be treated as real debt**, not as footnotes: `Outcome[R]`'s nesting, and boundary-copy identity loss.

#### The diagnosis path

`plan.md:1314-1321` classifies a failure into four buckets. Debuggability means each bucket has a first artifact to reach for, and the artifact exists:

| Symptom | Bucket | First artifact |
| --- | --- | --- |
| Graph fails to validate | Modeling defect (Arm C) | The rule or precondition diagnostic, which carries node and port (`plan.md:1260`) |
| Graph evaluates to the wrong trace | Notation or graph-semantics defect | Trace step diff, first divergent step, then its field path |
| Generated Go diverges from the graph trace | Lowering defect — **excluded from the comparison** | Same trace diff, plus the sidecar entry for the divergent node |
| Otherwise | Genuine acceptance failure | Obligation report (§12.1) for the failing claim, then its dependency cone |

**The path that is currently broken is the fourth-to-first one:** a panic in production gives a stack frame, and a stack frame resolves to a `NodeID` only if §11.7.1's embedding requirement is implemented and the correct generation's sidecar is retained. Both are stated; neither is built.

#### What this does not cover

`plan.md:1304` requires validator defects to be reported separately and never read as evidence about the medium. That means the validator's own diagnostics need to be trustworthy enough to distinguish "the graph is wrong" from "the validator is wrong" — and nothing in this design provides that. It is a gap in the experiment's instrumentation rather than in the graph, and §18 records it.

### 11.11 The generated program **[R6 — new]**

Every prior revision described how *constructs* lower and never what a lowered **program** is.

**Package layout.**

| Package | Contents | Constraint |
| --- | --- | --- |
| `graphtypes` | Registered §6 type-layer types and their generated copiers | Pass B verified at registration (§6.2) |
| `gen` | Sealed variants, eliminators, workflow entry functions, all dispatch | Import set enforced (§11.6); wholly generator output (§11.2) |
| `caps` | Narrow capability interfaces, one per declared capability | Hand-written, reviewed |
| `adapter/*` | One Tier 3 adapter per external operation | Pass A verified (§6.2); the only packages importing impure Go |

**The entry function**, per §5.8.4's workflow signature:

```go
func RunReceivePayment(
    ctx context.Context,
    caps ReceivePaymentCapabilities,   // narrow interfaces, §11.6
    fixtures FixtureSource,            // §13.4; nil in production
    in PaymentRequest,
) Outcome[ReceivePaymentResult]
```

`ReceivePaymentResult` is the tagged union of the workflow's `return` nodes. Capabilities are injected, never ambient, which is what makes W7's containment claim checkable at the boundary as well as in the validator.

**The durable seam is the hard part, and it is unaddressed. [R6]** `wait durable [timeout: 24h]` is a suspension that may outlive the process. A plain Go function cannot hold local state across a restart, so the generated program must be one of:

| Approach | Consequence |
| --- | --- |
| Explicit state machine | The entry function becomes a step function over a persisted state record; local variables become fields. Verbose, but resumable and inspectable. |
| Deterministic replay from a journal | Re-execute from the start, serving prior effect results from the journal. Requires the §13.6 determinism profile to hold **exactly**, since any nondeterminism diverges the replay. |
| **Checkpointed replay** **[R7]** | Replay only from the last `wait durable` boundary rather than from workflow start, bounding a determinism violation's blast radius to one segment instead of the whole history. |

**The notation's own syntax already argues against pure replay. [R7]** `n10 wait durable [timeout: 24h] -> n09` names an explicit resume target. Under replay-from-start that pointer is redundant — you re-execute from node one and never consult it. It is exactly what a state machine or a checkpointed replay uses. So the frozen syntax leans one way while `plan.md:2334-2336`'s "durable-workflow replay safety" vocabulary leans the other, and checkpointed replay is the option that fits both.

`plan.md:2334` requires the determinism profile to govern durable-runtime targets so that "evaluator-versus-Go conformance and durable-workflow replay safety share one executable suite" — which points at replay. But replay makes every §11.9 hazard a correctness bug rather than a test-flake, and this document has found ten of them. **The choice is not made here, and it changes the shape of every generated function.** Recorded as §16.17.

**Fold lowers to a `for` loop with an accumulator variable** (§5.8.1), and **inlined molecules lower to more nodes** (§5.8.2) — neither needs new lowering machinery, which is part of why inlining is the recommended composition strategy.

## 12. Obligations and guarantee provenance

### 12.1 Obligations are validator output

`plan.md:1912-1914` forbids a global label on a whole workflow; `plan.md:1941-1951` requires statement, scope, verification method, dependency cone, evidence, assumptions, model and toolchain versions, invalidation triggers, and confidence limitations. Computed into a report, not authored per node. Vocabulary at `plan.md:2080-2093`.

### 12.2 Assurance is computed per claim

```
assurance(o) := combine over o.DependencyCone of
                  Tier0 kernel        → evaluator-versus-Go conformance
                  Tier1 graph-native  → derived from constituents
                  Tier2 modeled Go    → differential evidence, degraded if correlated
                  Tier3 external      → contract-checked or runtime-observed only
```

### 12.3 The one obligation a type discharges

`AllResponsesHandled` (W8) via eliminator arity, for *declared* variants only. Every other obligation stays with the validator.

### 12.4 Request and response sides stay distinct

No obligation may claim assurance derived from response-side behaviour above `runtime observed`.

### 12.5 The cost layer **[R6 — new]**

Every atom already declares its complexity, so a graph's cost is compositionally derivable. `plan.md:1839` makes `complexity_and_limits` one of the nineteen required atom-documentation fields, implemented at `internal/atomdoc/schema.go:79`.

**The assurance class is `assumed-external`, and revision 6's claim of `test-backed` was wrong twice over. [R7]**

Revision 6 cited `plan.md:7897`'s `atom-complexity` gate — "a claimed time and space bound and a measured growth curve across input sizes, and the two agree" — as corroborating measurement. **That gate belongs to a different subsystem.** It is stage 24 of `plan.md` §33 *Pipeline Refinement*, an audit of `internal/pipeline`, the ordinary agentic Go-coding flow, whose "atom" and "molecule" vocabulary describes small verified Go functions that pipeline produces and registers. §14's experiment atoms are hand-authored stubs with "no computed hash, no embeddings" that never enter that flow, so gate 24 corroborates nothing here.

And even the citation that *is* correct cuts the other way: `plan.md:1819`, two paragraphs above the field list, says atom documentation "is descriptive evidence and embedding source material, **not a correctness-bearing contract**." `plan.md:2221-2231`'s enumeration of what contracts may express — scalar bounds, equality, tagged-union membership, collection-size bounds, state-transition legality, capability containment, effect cardinality and ordering, stable derivation rules — **never mentions complexity or time and space bounds at all.**

> **A cost bound derived from `complexity_and_limits` is `assumed-external` and cannot rise above it** until `plan.md:2221-2231` gains a complexity clause and the bound moves into the atom contract. That is a plan amendment, recorded as §16.21.

The composition rules below remain sound as *derivation*; what changes is how much the derived number is worth.

#### 12.5.1 Why composition is decidable here

Cost composition is undecidable in general. It is decidable over this graph because of three restrictions adopted for entirely unrelated reasons:

- **Every loop is bounded** — `loop map` and `fold` over materialized finite structures, `retry [max: k]`, `reconcile [max_polls: p]`, all with §13.4's `BoundExhausted` (§3.2).
- **No general recursion** — §11.4, forced by Go's lack of TCO.
- **No higher-order values or closures** — §6, per `plan.md:2185-2193`.

The constraints that make the graph feel restrictive are exactly what make it analyzable. That is worth stating plainly, because it is the strongest argument this design has for the medium being good for something ordinary Go is not.

#### 12.5.2 The composition rules

Cost is a symbolic expression over input-size variables, computed bottom-up:

```
Cost(pure n)                  = cost(n.Atom) at its input size variables
Cost(merge)                   = O(1)
Cost(match)                   = max over arms                    -- worst case
Cost(a then b)                = Cost(a) + Cost(b)
Cost(loop map over C)         = |C| · Cost(body)
Cost(fold over C)             = Σ i=1..|C| of Cost(body, acc=i)  -- [R7]
Cost(region retry [max:k])    = k · Cost(body)
Cost(region reconcile [p])    = p · Cost(body)
Cost(wait durable)            = O(1) compute; wall-clock bounded only by its declared timeout
Cost(effect n)                = NOT DERIVED — a declared budget, never a computed term
```

**The fold rule carries a free accumulator-size variable, and revision 6's did not. [R7]** It wrote `|C| · Cost(body)`, treating the body cost as size-independent. Under this design's own semantics — immutable values, and a boundary copy in and out of every Tier 2/3 crossing (§10.5) — a fold that appends is Σ O(i) = **O(N²)**, and a formula with a single multiplicative constant cannot say so. Depending on whether an implementer substituted first-iteration, last-iteration, or assumed-amortized cost, the composed bound was correct by coincidence or understated by an order. That is a failure at precisely the payoff §12.5.5 claims — catching accidental quadratics — in the section's own worked example.

**Tier 3 effects contribute a declared budget rather than a derived cost**, which is §12.4's request/response split applied to time: the request side is analyzable, the response side is not predictable. A workflow's cost expression therefore has two parts — a derived compute term and a sum of declared external budgets — and they must never be added into one number that implies uniform confidence.

#### 12.5.3 Space, and the cost this design has been hiding

The same composition gives space. It also puts a number on something §10.5 currently describes only as "safe and slow":

> **Every Tier 2/3 boundary crossing copies in and out**, so `Space(crossing) = 2 · sizeof(value)` per call, and a crossing inside a `loop map` over *N* elements is `2N · sizeof(element)`. The `no-alias` contract claim removes it.

That turns an unquantified caveat into a measurable trade, and it makes `no-alias` an optimization with a stated payoff rather than a vague escape hatch.

#### 12.5.4 A new obligation class

`WithinCostBudget(workflow, bound)` joins §12.1's vocabulary. **Its derivation is static; its assurance is `assumed-external`** (§12.5 opening), and those are different properties — revision 6 blurred them and produced a contradiction with §12.5.6, which said the layer "makes no structural rule mechanically checkable." **[R7]** Both are true once separated: the obligation is statically *derived*, and it is not one of the plan's four structural rules, so C1 still excludes it.

**Invalidation is already wired.** An atom version bump whose `complexity_and_limits` changes invalidates every cost obligation whose cone contains it — `plan.md:1757`, "an atom upgrade creates a graph revision and re-derives affected obligations" **[R7 — revision 6 cited `plan.md:1906`, which is about third-party *dependency* upgrades under correlation controls, a different mechanism]** — and surfaces in §11.7.2's obligation diff. A **weakened** cost bound is precisely the case §11.7.2 says to surface loudest.

#### 12.5.5 What it does not give

Stated because the layer is easy to oversell.

- **It is not a proof, and not even test-backed.** `assumed-external` is the ceiling until `plan.md:2221-2231` admits complexity as a contract-expressible property and the bound moves out of documentation (§16.21).
- **Asymptotic classes compose; constant factors do not.** Predicting latency needs measured constants from gate 24's growth curve, and those are platform-bound — they belong to §10.6's per-target bindings and must be re-measured across §13.6's matrix.
- **Tier 3 latency dominates real workflows.** A gateway call is milliseconds while everything around it is microseconds, so the compute term rarely predicts wall-clock. Its real value is **space bounds** and **catching accidental quadratics** — an atom whose declared cost is linear used inside a `loop map`, giving a quadratic no reviewer notices in a diff.

#### 12.5.6 Scope verdict

**This fails §2's C1 test and is out of scope for the disposable experiment.** It makes no structural rule mechanically checkable. It belongs in the production design, and §17 records it as such.

That is a different answer from §5.8's gaps: `fold` and molecules are *representability*, which `plan.md:1110` and `plan.md:1284` make a measured Arm C requirement. The cost layer is capability the experiment does not need. Both are worth building; only one is worth building first.

## 13. Evaluator, notation, and conformance

### 13.1 Preconditions before task one

`plan.md:1263-1270` and `plan.md:1306-1312`.

### 13.2 Conformance cases

**Frozen** at `plan.md:2149-2163`.

**Proposed additions — pending §16, not part of the frozen set:** progress-cycle detection (W1) · retry multiple-entry (W2) · data-edge and merge type disagreement (W3) · missing dedup strategy (W4) **[R4]** · duplicate control-edge labels (W5) **[R4]** · zero or multiple roots (W6) **[R4]** · uncovered variant and unhandled `BoundExhausted` (W8) **[R4]** · attribute declared at two nesting levels (W9) **[R4]** · **a dedicated Rule 4 graph, since the molecule cannot exercise it** **[R4]** · claim confirmed via reconciliation reaching issuance · notation round-trip `NodeID` preservation · guarded-port escape by a non-merge consumer · effect-floor mismatch including cgo and per-platform · unmapped dependency error · panic sealing including `Goexit`, spawned-goroutine, and blocked-atom timeout · source-map position fidelity, regeneration integrity, and historical resolution · foreign variant smuggled through interface embedding, all four forms · handler-set laundering, all six verified forms · trace redaction and bounding **[R4]**.

### 13.3 Blind adjudication triage

`plan.md:1314-1321`. Exclusions pre-registered; **more than ten percent excluded or dropped tasks in any arm voids the comparison.**

**Risk.** §11's lowering pipeline is a from-scratch compiler backend whose conformance suite is written before anyone sees the fifty tasks, which are seeded with adversarial shapes (`plan.md:1272`). Six lowering defects in fifty is twelve percent and voids the comparison. Mitigations to decide before task one: an explicit lowering-hardening pass with exit criteria, and/or bounding the corpus's variant-type diversity.

### 13.4 Evaluator semantics and the trace

**A trace is an ordered sequence of steps.** Each records: `NodeID` entered; variant tag taken; **a bounded, redacted rendering of each out-port binding**; `EdgeID` followed; and the iteration index of every enclosing loop region.

**Traces are bounded, and credential-redacted — but business-data minimization is an open gap. [R5]** `plan.md:3141` requires secrets never be written to task transcripts or diagnostics, and `plan.md:3240-3246` mandates one redaction pipeline. **That pipeline is credential-scoped**: it "combines exact currently loaded credential values with provider-key, bearer/header, and private-key patterns." A gateway token trips it; a `PaymentRequest` carrying amounts and customer fields does not, and is not credential-shaped by any pattern. Revision 4 cited this pipeline as though it covered arbitrary domain payloads. It does not, and **no plan mechanism currently minimizes business data in traces** (§16.15).

Port bindings are therefore recorded as a content digest plus a bounded rendering — never a verbatim value — with credential redaction from the cited pipeline and *field-level* minimization still to be specified. A `loop map` over a large input would otherwise produce an unbounded trace.

**Comparison uses the digest, and the digest needs a canonical encoding that does not yet exist. [R5]** §11.9's disagreement hazards — negative zero, nil versus empty map — mean text and value comparison give different answers, so the digest must be canonical. But the digest is compared *across representations*: the evaluator holds graph values, generated Go holds Go values. Verified that the obvious choices all fail — the same logical `Money(USD, 500)` digests differently as `{"cents":500,"currency":"USD"}` and `{"Currency":"USD","Cents":500}`; gob is type-tagged and encodes map and struct through different schemas outright; and even Go-to-Go, struct field *declaration order* changes the JSON bytes for an identical value.

A canonical encoding must be defined over §6 type-layer values, and both the evaluator and the generated adapter must encode through it. **This is tractable precisely because §6.2 restricts boundary values to the graph type layer**, which is a narrower domain than arbitrary Go.

> **The encoding, in full. [R5 — an earlier draft gave the first four and stopped, missing the three that matter most for its own purpose.]**
>
> 1. **The value's `TypeRef` is part of the digest input**, before any content. Without this the digest is purely structural while §6.1's equality is strictly nominal, so two distinct registered types of identical shape would digest equal — a false pass in differential testing that contradicts the type layer's own rule.
> 2. Record fields are emitted in **sorted field-name order**, each name length-prefixed.
> 3. Each primitive type has **one fixed rendering**: integers as two's-complement fixed width by declared type (so a 32-bit and a 64-bit 500 differ); floats in a canonical decimal form with `-0.0` normalized to `0.0` and NaN and each infinity given a distinct reserved token; strings as length-prefixed UTF-8, rejected rather than substituted if ill-formed.
> 4. Tagged unions emit an **explicit variant tag** before the payload.
> 5. Collections are **length-prefixed**, and **absent is distinct from empty** — a separate marker, not a zero length. This is exactly §11.9's nil-versus-empty hazard, which the encoding exists to neutralize rather than inherit.
> 6. Collections without inherent order are emitted in **canonical sorted order of their elements' encodings**, so §11.9's map-iteration hazard cannot reach the digest.
> 7. **A type-kind discriminator precedes every composite.** **[R5]** Verified: without one, an empty record and an empty list encode to byte-identical output — both reduce to a single `uint32(0)` — so the collision is structural, not a hash accident.

**Verified to work for a representative type, and to leave six scalar questions open. [R5]** A record containing scalars, a `Result` union, and a bounded collection digests identically from a map-based graph representation and a reflected Go struct. But every scalar edge case tested breaks something the six rules above do not settle:

| Case | Behaviour |
| --- | --- |
| NaN payloads | Two NaN bit patterns digest differently; needs a normalization pass |
| `-0.0` vs `0.0` | `==`-equal, digest-different — §11.9's disagreement recurring inside the encoder |
| float32 promoted to float64 | Bit-different from a native float64 of the same literal; graph float width is unspecified |
| `uint64` near `MaxUint64` | Silently becomes `-1` when widened to a canonical int64 — a live overflow bug |
| Unicode NFC vs NFD | Visually identical, digest-different; no normalization specified |
| Absent vs empty collection | Reconcilable only by a choice that conflicts with the map representation's natural encoding of an absent field — a §6 modeling decision, not an encoder one |

Invalid UTF-8 and nested unions both work correctly. The encoding does not exist yet, is a prerequisite for differential testing, and needs the six rows above resolved before it is written.

**Digesting costs debuggability, and the trace must compensate.** Verified: two trace steps can have byte-identical redacted renderings — `{GatewayToken: [REDACTED], Amount: 500}` — while their digests correctly differ, so a human sees *that* something diverged and not *what*, for exactly the values most likely to need debugging. The trace therefore records the **field path** at which two digests first diverge, without the value.

**Stepping.** A deterministic single-threaded interpreter over the progress subgraph from the W6 root. At a `Tagged` node it evaluates the atom, takes the returned variant, and follows the uniquely-labelled control edge (W5 for uniqueness, W8 for existence). At a `merge` it takes the value from whichever incoming branch executed; **reaching a merge with no executed incoming branch is an evaluator error, not a silent zero** **[R4]**. At a `loop` it iterates the finite input directly, binding element identity per iteration.

**Effect fixtures are ordered scripts, not functions. [R4 — this was a blocking gap.]** `n09`'s declared input is `key: n03.key`, unchanged across polls, and Rule 2 forbids a poll counter from entering the key cone — so a fixture modelled as a pure function of inputs must return the same variant on every invocation. That makes `region reconcile [max_polls: 6]` either never terminate or resolve on the first pass, and **"bounded reconciliation reads" (`plan.md:2157`) — a frozen conformance case — could not be authored at all.**

> An effect fixture is a **sequence** keyed by `(NodeID, invocation ordinal)`. The graph evaluator and the generated Go's Tier 3 adapter consult the **same** fixture in the **same** order, which is what makes `plan.md:1311`'s differential execution meaningful for a stateful mock.

Verified authorable: `n09: [1..5: StillUnknown, 6: ConfirmedSuccess(c)]` steps correctly through `region reconcile`, and the ordinal is harness metadata that never enters `n09`'s key cone, so Rule 2 is not violated.

**The ordinal counter is shared per `NodeID`, and this is a generator obligation. [R5]** `n09` is targeted by three match arms (`n06.Ambiguous`, `n05.InProgress`, `n13.InProgress`), and §11.1 states the generator emits one call-site expression per (node, return type) pair — so a node's logic may be duplicated across call sites. Independent per-call-site counters would desynchronize the graph and Go traces. The generated code must therefore maintain **one counter per `NodeID`**, not one per emitted call site.

**`wait durable` in evaluation.** The evaluator does not sleep. It records a `suspend` step carrying the declared timeout and resume target, then follows the resume edge — so the frozen expected trace contains the suspend step and a lowering that fails to suspend is detectable without waiting 24 hours. Termination comes from the enclosing region's bound, and the fixture sequence supplies the differing responses that make progress observable.

**Bound exhaustion is a region outcome, not an atom variant. [R4 — revision 3 made it a variant on "the region's governing effect node," which violates §8.4: response variants are intrinsic to the atom contract while a bound is site-specific, so a fixed contract cannot grow a variant that depends on which region wraps it.]**

> When a `retry` or `reconcile` region reaches its declared bound, control leaves the region by a `ControlEdge` whose `From` is the **region** and whose `Label` is the `RegionOutcome` `BoundExhausted` (§5.3). W8 requires every bounded region to have such an edge. For Rule 3's purposes it is treated as `inconclusive`, so an exhausted reconciliation does not gate compensation.

This keeps the site-specific bound producing a site-specific outcome, leaves `ContractStub.ResponseVariants` untouched, and leaves §11.2's eliminator arity unchanged.

**Scope limit. [R4]** The evaluator replays one workflow instance on one scripted path. `plan.md:1574` excludes workflow-instance identity from LEI precisely because the domain concern is cross-instance races, which this apparatus cannot represent. That is acceptable — Rule 1 is a static reachability check, not a runtime property — but the trace's scope is narrower than the domain problem, and nothing else in the experiment covers the gap.

### 13.5 The notation grammar is an undelivered prerequisite

**This section states requirements; it is not a grammar, and nothing here can be implemented from it. [R4 — revision 3's changelog claimed grammar and editor verbs were "specified as deliverables, with `wrap-region` and `annotate` resolved." Both overstated what was delivered.]**

Before task one the grammar must specify token rules, how `[…]` attribute lists bind to a node versus a region (§5.3's resolution depends on it), comment syntax, and continuation rules.

**`plan.md:1154` says "one node per line," but `n03`, `n06`, `n09`, and `n13` span multiple lines** (§16.10).

**The frozen editor verbs cannot build the frozen molecule. [R4]** `plan.md:1243-1248` lists `add-node`, `delete-node`, `rewire <node>.<input> <- <source>`, `wrap-region`, `bind-atom <node> <atom>@<version>`, `annotate <node> <relationship> <target>`. **None of the six sets a region's attributes**, so no sequence of them produces `region retry [max: 3, backoff: exp]` or `region reconcile [max_polls: 6]` — meaning the documented editor cannot construct the worked example, or the "declared retry" and "bounded reconciliation reads" conformance cases. Recorded as §16.12. Note also that `wrap-region` is frozen **without parameters**; revision 3 invented `wrap-region <nodes>` without flagging the deviation.

**Verb semantics, so far as they can be settled:** `wrap-region` leaves edges crossing the new boundary in place as cross-region edges and fails if the selection would violate the one-innermost-region invariant or W2. `annotate` mints a `Relationship` with a fresh ID; **a duplicate (same kind, subject, object) is a validator precondition failure, not an editor-time rejection** **[R4]** — revision 3 said "rejected" without saying by what, and it appeared in neither the W list nor §9's taxonomy.

### 13.6 Determinism conformance

`plan.md:2304-2336`, minimum `amd64` and `arm64`. The effect floor is recomputed per target.

## 14. What the experiment actually needs from atoms

Hand-authored contract **stubs**, one per atom the fifty tasks touch, discarded with the notation.

```
ContractStub = {
    AtomID, AtomVersion          -- ContractRef joins on this pair
    Signature                    -- input/output TypeRefs (§6.1, project-scoped)
    Tier                         -- declared; checked against the computed floor
    OperationContractID          -- Tier 3 only
    ResponseVariants             -- each with its Disposition
    Capability
    Determinism
    Correlated
    Bindings                     -- opaque recorded string (§10.6)
}
```

No migration, no computed hash, no transitive module graph, no embeddings.

**For production, after the graph decision:** `plan.md:2653-2667` specifies thirteen atom tables. **Four exist under their planned names** (`atom_documentation_revisions`, `atom_documentation_fields`, `atom_names`, `atom_name_aliases`), **one exists under a drifted name** (`atom_documentation_embeddings` for `atom_embeddings`, §16.8), **and eight do not exist**: `atoms`, `atom_versions`, `atom_signatures`, `atom_contracts`, `atom_implementations`, `atom_dependencies`, `atom_capabilities`, `atom_evidence`. That gap blocks production, not the experiment.

## 15. Defects in the plan's own frozen molecule

`plan.md:1267` requires the corrected molecule frozen before task one.

**15.1 `n08` has no result variants** — no `: Variant | Variant`, no `match n08.out`. Under §5.3 it is `Single`, i.e. always succeeding, contradicting `plan.md:2067-2074` for a gateway refund. Rule 3's precondition fails for any relationship naming `n08` as the effect half.

**15.2 Validation is structurally disconnected, and the molecule has at least two rootless nodes — possibly three. [R5]** `n02`'s `Valid | Invalid` has no `match`, and `n03` reads `n01.out`. The molecule does not demonstrate `NoEffectOnInvalidInput` (`plan.md:2092`). **W8 rejects it for the uncovered variant set.** W6 also rejects it, and the count depends on §5.7: without convention C2 nothing gives `n02` or `n03` an incoming control edge, so both are rootless alongside `n01`; with C2, `n02` inherits an edge from `n01` but — being `Tagged` with no `match` — emits none, leaving `n03` rootless regardless. Revision 4 named only `n02`.

**15.3 Neither bounded region models exhaustion.** `region reconcile [max_polls: 6]` **and** `region retry [max: 3, backoff: exp]` both declare bounds with no exhaustion outcome; §13.4's W8 clause rejects both. **[R4 — revision 3 flagged only the reconcile region.]**

**15.4 Node declarations span multiple lines** against `plan.md:1154` (§16.10).

**15.5 The molecule cannot be built with the frozen editor verbs** (§13.5, §16.12). **[R4]**

## 16. Conflicts requiring a plan decision

**16.1 `match` as a node.** `plan.md:1551` versus the notation's lack of identity for it.

**16.2 `evidence_version` pinning.** `plan.md:1748-1757` pins it; `bind-atom` supplies two of four components. Recommend pinning atom plus contract and resolving evidence at validation time against a policy floor. Out of scope for the experiment.

**16.3 Cycles and Rule 1.** §5.6's progress partition proposed as an amendment to Rule 1's text, with W1 as precondition. **If rejected, §5.5's dominance and §9.4's Rule 4 both need different bases** (§9.1).

**16.4 First-order function references.** Permitted by the plan, unused by the notation.

**16.5 Failed compensator.** A plan gap, deliberately not implemented as a fifth rule; currently unfalsifiable against the molecule (§15.1).

**16.6 Rule 4's applicability test — OPEN, three failed specifications.** Does a region's `dedup:` describe its own node, or scope to the issuances it gates? §9.4 assumes the first, which makes `guarded = ∅` on the molecule. `plan.md:2059-2060` defines `LocalAtomicClaim`'s obligations as properties of the *gated issuance*, favouring the second — but claim and issuance are related by a control edge, not by nesting (§5.2), and this design has no mechanism to express that. **Whichever reading is chosen, Rule 4 needs its own conformance graph.**

**16.7 The frozen diagnostic format cannot name a merge**, limiting §9.2's overlap warning.

**16.8 `atom_embeddings` naming drift.** Recorded, not resolved; §14's count reflects it.

**16.9 Where `provider_deduplication_scope` lives — OPEN, two failed assignments.** `plan.md:1574` defines it as endpoint, tenant, account, or declared idempotency scope; `plan.md:2034-2045` lists it among the eighteen fields **each effect declaration includes**. No node in the molecule carries it. The workflow header's `effect_identity: (operation_contract_id, provider_scope, key)` uses the same generic role names as the abstract formula, so it documents the tuple's *shape* — and cannot be assigning values, since the molecule's six effects do not share one provider and would collide on Rule 1. **Candidates: a `Node.Attrs` key with no syntax yet, or the Tier 3 atom contract.** One third of the LEI tuple is currently uncomputable.

**16.10 "One node per line" versus the example** (`plan.md:1154`). The grammar cannot be written until this is settled.

**16.11 Parametric types under nominal `TypeRef`.** `plan.md:2178-2180` includes `Option`, `Result`, and bounded collections — parametric constructs. §6.1 registers each instantiation as a distinct nominal type, so `Option<Charge>` and `Option<Failure>` are unrelated and every kernel operation over them must be re-declared per instantiation: a real cost multiplier on the kernel budget (§3.2), and an argument for dropping `Option`/`Result` from the disposable type layer.

**This is a consequence of this document's own nominal-`TypeRef` choice, not something the plan forces. [R5]** Revision 4 cited `plan.md:2193`'s exclusion of *implicit* polymorphism as the cause. That is a different thing from *explicit* type parameters, which the plan does not exclude and which §11.2's own `Outcome[R any]` and `MatchPaymentOutcome[R any]` depend on. A type layer with explicit parametric constructors is available if the cost multiplier is judged too high.

**16.12 The frozen editor verbs cannot set region attributes. [R4]** None of `plan.md:1243-1248`'s six verbs produces `region retry [max: 3]` or `region reconcile [max_polls: 6]`, so the editor cannot build the molecule or two frozen conformance cases. Either a seventh verb is needed or region attributes are authored outside the editor — and the second breaks the premise that the six verbs are the whole editing surface.

**16.13 Trace format and `BoundExhausted` are inventions.** `plan.md` uses "trace" only informally (`1317`, `1318`, `2330`, `2366`, `2394`) and never defines its structure; nothing specifies bound-exhaustion behaviour. §13.4 invents both, and W8 makes unhandled exhaustion a validation failure.

**16.14 The control graph is underspecified — the most consequential open item. [R5]** The frozen notation shows explicit control edges only in `match` blocks and `->` arrows. Two conventions are required and neither is stated: **C1**, that an edge targeting a region resolves to that region's entry node recursively; and **C2**, that lexically consecutive nodes are implicitly sequenced. Without C1, Rule 1's reachability and Rule 4's dataflow are both undefined on the molecule's only claim-to-issuance edges. C2 is the weaker convention — it makes lexical order semantically load-bearing in a notation whose §5.4 insists position is not identity — and this document's preferred reading is instead that **every control transition requires an explicit `->` and the frozen molecule is incomplete**, consistent with §15's other findings. A plan decision either way unblocks §13.5's grammar.

**16.15 No plan mechanism minimizes business data in traces. [R5]** `plan.md:3240-3246`'s redaction pipeline is credential-scoped — exact credential values plus provider-key, bearer, and private-key patterns. Traces carry domain payloads (amounts, customer fields, gateway responses) that match no such pattern, are compared and logged, and are shown to a blind adjudicator. Field-level minimization needs specifying, and it is not a restatement of the existing pipeline.

**16.16 Nominal `TypeRef` may block the merge in the frozen molecule. [R5]** See §9.7. Two independently contracted Tier 3 atoms have no reason to share a registered `Charge` type, and W3 under strict nominal equality would then reject `m01`. Either the type table is authored so both reference one type, or merge inputs need a different equality rule.

**16.17 Durable suspension: state machine or replay. [R6]** `wait durable` may outlive the process, so the generated program is either an explicit state machine over a persisted record or a deterministic replay from a journal. `plan.md:2334` points at replay by requiring one determinism suite to serve both evaluator conformance and "durable-workflow replay safety" — but replay promotes every §11.9 hazard from test-flake to correctness bug, and this document has found ten of them. The choice changes the shape of every generated function and is not made here (§11.11).

**16.22 No external signal can enter a suspended workflow. [R7]** Every demonstrated `wait durable` resumes to a node that **re-polls the same query** (`n10 -> n09`, `n14 -> n13`), and `input` nodes bind once at workflow start (§5.8.4). Nothing delivers *new* data into a suspended instance. A long-running graph awaiting an external actor — an order moving Draft → Submitted → Approved → Shipped, where each transition arrives as a separate call — is therefore inexpressible as one graph and must be decomposed into one workflow per transition with state in a database. That decomposition works and may be the right answer, but it is a modelling constraint nobody has stated, and it interacts with §11.11's durable seam: if workflows never span external signals, the suspension mechanism only ever has to survive polling, which is a much weaker requirement.

**16.19 Retry trigger and element-failure policy. [R7]** Neither is authorable (§5.8.1a). Proposed: a `retryable` member of `Variant.Disposition`, and an `[on_element_failure: continue | abort]` region attribute. The first extends a construct this document invented; the second rides the frozen attribute mechanism.

**16.20 The workflow's signature and result type are uncited. [R7]** `plan.md` defines neither (§5.8.4). `plan.md:1110` and `plan.md:1284` establish that Arm C needs *some* such mechanism; the specific one proposed here is this document's.

**16.21 Complexity is not contract-expressible. [R7]** `plan.md:2221-2231` enumerates what contracts may express and omits time and space bounds, while `plan.md:1819` bars documentation from bearing correctness. A cost obligation (§12.5) therefore cannot rise above `assumed-external` without a plan amendment admitting complexity to the contract language.

**16.18 Molecule composition: inline or call. [R6]** `plan.md:1541` and `plan.md:1707-1715` require graph-native composition; §5.8.2 recommends edit-time inlining because it leaves Rules 1, 3, and 4 as intragraph reachability, where a `call` node would make them interprocedural with nothing in the plan specifying how. The cost is graph size and the derivation-instance trap in §5.8.2. A plan decision would settle it.

## 17. What to cut, if §2.1's estimate exceeds the ceiling

| Candidate | Gates a rule? | Note |
| --- | --- | --- |
| Generator-version migration (§11.8) | No | One version, ever |
| Cross-revision tombstones (§5.4) | No | IDs need stability for one run |
| Correlation flags (§10.2) | No | Assurance reporting |
| Transitive module hashing (§10.6) | No | Invalidation hygiene for a discarded artifact |
| Full obligation report (§12.1) | No | Rules produce pass/fail |
| Merge-overlap warning (§9.2) | No | **But cutting it removes the only signal for Rule 1's known false-negative class — see §9.2** |
| `Goexit` supervision (§10.4) | No | Does not close the leak anyway; process supervision is the real control |
| Per-target floor recomputation (§13.6) | Partially | Only if multi-platform |
| `Option`/`Result` in the type layer (§16.11) | No | Each instantiation multiplies kernel declarations |
| **The cost layer (§12.5)** **[R6]** | No | Post-experiment capability; fails C1 outright, and the data it needs already exists whenever it is built |
| **Templates (§5.8.3)** **[R6]** | No | Reuse convenience; the experiment can hand-expand |

**Not cuttable for representability, though C1 would cut them. [R6]** `fold` (§5.8.1), molecule inlining (§5.8.2), and the workflow signature (§5.8.4) gate no rule. They survive because `plan.md:1110` requires Arm C to model fifty *real* change requests and `plan.md:1284` counts representability corrections as Arm C cost. A notation that cannot express an accumulator does not save scope — it loses the arm.

**Not cuttable, each gating a rule or precondition:** the six node kinds and the seven region kinds; `Node.Attrs` (Rule 4's quantifier); guarded-port dominance, §5.5 — *the data-edge typing constraint, distinct from W5's label distinctness, which is what Rule 1's soundness needs*; progress-edge marking (Rule 1, Rule 4, W1); stability classes (Rule 2); the provenance fixpoint and `derivation_instances` (Rules 1, 2, 4); `Variant.Disposition` (Rule 3); `RegionOutcome` (W8); contract stubs; the `Outcome[R]` eliminator (W8); the effect floor (Rule 2); W1–W10.

## 18. Known residual risks

**Blocking, and within this document's reach — the largest item. [R6]**

0. **Representability was never assessed** (§5.8.6). `plan.md:1110` requires Arm C to model fifty real change requests and `plan.md:1284` counts representability corrections as its cost. Four constructs the plan requires were absent from every prior revision — `fold`, molecule composition, templates, and the workflow as a callable unit — and one afternoon of trying to model a real request would have surfaced all four. **Until a pre-registered sample is modelled in the notation, no claim about Arm C's viability is supported**, and further validator work compensates for nothing.

**Blocking on a plan decision — outside this document's authority to settle.**

1. **Rule 4's applicability reading** (§9.4, §16.6). Specified wrongly three times; its mandated conformance case is only constructible in a shape that avoids the failure mode; and the alternative reading needs a mechanism relating regions by control flow that the notation lacks.
2. **`provider_deduplication_scope` has no home** (§16.9), leaving a third of the LEI tuple uncomputable.
3. **The grammar is blocked** on `plan.md:1154` contradicting its own worked example (§13.5, §16.10).
4. **The frozen molecule fails validation** under W6 (twice), W8, and Rule 3's precondition, and cannot be built with the frozen editor verbs (§15). Two frozen plan artifacts are mutually inconsistent.
5. **Whether every control transition requires an explicit `->`** — the C2 half of §5.7/§16.14. C1 is adopted and works; only this half is a plan question.

**Unresolved but within this document's reach:**

6. **The canonical encoding needs six scalar decisions** (§13.4) — NaN normalization, signed zero, float width, unsigned-64 range, Unicode normalization, and absent-versus-empty collections. The structure is verified correct for a representative type; the scalars are not settled. Revision 5's first draft called this plan-blocking; it is not.
7. **The provenance guard's scope for test code is undecided** (§11.2). The checker flags ordinary unit tests, which must dispatch to assert anything, and a blanket `_test.go` exemption reopens the hole.
8. **`Voided`'s rule does not cover three cases it creates** (§11.2): `R` forced to a sealed type in the heterogeneous case, a non-void result carrying a nil value, and a `Single`-result downstream atom whose panic seal must live in an upstream matched-arm handler.

**Unresolved but not blocking:**

7. **The 25% cost ceiling is threatened by three unsized drivers** — atom-contract authoring, shadow-type authoring, and translation cost (§2.1).
8. **A lowering defect rate above ten percent voids the comparison** (§13.3).
9. **W3 is unverified and may reject the molecule's merge** under nominal typing (§9.7, §16.16).
10. **Rule 1's merge blind spot misattributes to the wrong triage bucket** (§9.2), and the compensating warning is on the cut list.
11. **W1 is used as settled in two derivations while listed as proposed** (§9.1, §16.3).
12. **No plan mechanism minimizes business data in traces** (§13.4, §16.15).
13. **Per-instantiation kernel declarations multiply under nominal `TypeRef`** (§16.11).

**Accepted — no fix exists:**

14. **Blocked Tier 2/3 atoms leak goroutines without bound**; Go offers no reclamation, and only process supervision bounds it (§10.4).
15. **Deadline-adjacent completions are nondeterministic by construction** (§11.9).
16. **Guard 3 cannot reach another module** (§11.2), and **§6.2's Pass B inherits the same limit** — a foreign-module variant added by interface embedding, carrying a foreign field, crosses the boundary unseen (§6.2). The eliminator's total fallback is the only backstop for both.
17. **"Entirely generator output" is a CI process, not a check** (§11.2). A manifest detects tampering only until the tamperer re-runs the recorder; verified.
18. **`Outcome[R]`'s nesting and boundary-copy identity loss are unmitigated debuggability debt** (§11.10).
19. **Validator diagnostics cannot distinguish "the graph is wrong" from "the validator is wrong"** (§11.10), which `plan.md:1304` requires the experiment to separate.
17. **`Outcome[R]`'s two fields carry unenforced trust** (§11.2); the discipline binds the generator because nothing else can bind it.
18. **The evaluator cannot represent cross-instance concurrency** (§13.4), which is the domain concern LEI's instance-exclusion exists for.
19. **The failed-compensator gap is unfalsifiable against the molecule** (§16.5).

## 19. Where this document has been wrong

Recorded because a design that has failed repeatedly in one place should say so rather than present its latest attempt with the same confidence as everything else.

| Item | Revisions wrong | Why it kept failing |
| --- | --- | --- |
| Rule 4's quantifier and algorithm | 1, 2, 3 | Own-versus-inherited attribute resolution, and regions related by control flow rather than nesting |
| Handler-laundering control | 2, 3, 4 | Enumerating shapes, then detecting shapes; Go admits neither |
| `provider_deduplication_scope` | 1, 2 | Two plausible homes, neither supported; the notation has no syntax for it |
| Boundary-type restriction | 2, 3 | Stated as a filter on arbitrary Go types instead of a property of the graph type layer |
| `BoundExhausted` authority | 3 (twice within) | A site-specific bound cannot produce an atom-intrinsic variant |
| Panic and `Goexit` mitigation | 2, 3 | Successive claims that a mechanism closed a hole Go cannot close |
| The void channel | 3, 4 | No representation in a bare generic return; then one bit for three meanings |

**A third pattern, and the worst one: a whole dimension was never checked. [R6]** Five revisions and seventeen adversarial passes examined whether the rules were *correct* and never whether the notation could *express a program*. `fold` was dropped by a side effect of excluding function references; molecules, templates, and the workflow signature were never raised at all. Every reviewer inherited the document's own framing, and §2's C1 test — a scope test — was doing work no expressiveness test was doing. **A review loop converges on the questions it is pointed at**, which is an argument for choosing adversarial lenses that the document did not choose for itself.

**A second pattern, behavioural rather than technical. [R5]** Four separate times a revision declared something resolved that was not: revision 3's grammar and editor verbs "specified as deliverables, with `wrap-region` and `annotate` resolved"; its `Goexit` timeout "therefore mandatory" as though the leak were closed; its full `Claimed(n06)` trace offered as evidence a fix worked, on a node the quantifier excluded; and its W7/W8 source labels implying parity with a genuine mandate. Revision 4 added a fifth — §20 claiming the risk list was "completed" when gaps it had just introduced were missing. **The failure mode is claiming completion, and it is more dangerous than any single technical error**, because it removes the item from review.

**Revision 4's own claim that "Rules 1–3 have been stable" is retracted. [R5]** Revision 4's widening of `ControlEdge.To` to accept a `RegionID` reopened Rule 1's reachability without anyone re-examining it (§5.7). A change in one revision can un-settle material that earlier rounds had cleared, so "settled" is a statement about review coverage, not a property of the text.

Four rounds found criticals in every round, always concentrated in whatever that revision had just introduced. Round 4 differed in character: no algorithm was wrong and no Go claim was false — the fixpoints, the rule logic, and the verified Go behaviours all held. What round 4 found instead was **representation left undefined** (§5.7), **mechanisms that collapse under composition** (`Outcome[R]`), and **controls that cannot be implemented as described** (shape detection, digest comparison). Anything written here in one pass should be assumed to carry that defect rate.

## 20. Convergence status

Four review rounds, fifteen adversarial passes. Findings by round, and their character:

| Round | Character of findings | Verdict |
| --- | --- | --- |
| 1 | Algorithms wrong (Rule 4 quantifier, `Prov` recursion); Go claims false (sealing, arity, panic, deep copy); scope contradictions | Not converged |
| 2 | Fixes wrong (Rule 4 again, `Prov` fixpoint gaps); new mechanisms unspecified (`derivation_instances`, disposition) | Not converged |
| 3 | Rule 4 wrong a third time; new material (evaluator, `TypeRef`, W-list) untested; two frozen plan artifacts found mutually inconsistent | Not converged |
| 4 | **No algorithm wrong. No Go claim false.** Representation undefined (§5.7); mechanisms collapse under composition (`Outcome[R]`); controls unimplementable as described (shape detection, digest comparison) | Converged on logic, not on representation |
| 5 | Fixes largely sound, but **a new factual error in the fix material** (a W9 example that does not exist in the molecule), a stale justification left behind by revision 5's own edit (§5.5), an unsurveyed third convention (§5.7), an incomplete encoding spec, and two "blocking" items overstated | **Not a clean pass** |

**Round 5 did not clear the bar round 4 set. [R5]** A final verification round that finds a fresh, checkable error inside the fixes is evidence against declaring the document done — and it found exactly the failure mode §19 names, a revision asserting more than it had established. The convergence claim below is therefore about *where the remaining work lives*, not about the document being finished.

**The remaining blockers are plan-level, not document-level.** Rule 4 has failed three times because §16.6 is an open plan question with no notation mechanism to express one of its two readings. `provider_deduplication_scope` has no home because the plan defines it in two places that cannot both be assignments. The trace format and `BoundExhausted` are inventions filling a hole the plan leaves. The control graph (§5.7) is undefined by the frozen notation. And two *frozen* plan artifacts — the worked molecule and the six editor verbs — are mutually inconsistent: the molecule cannot be built with the verbs, and it fails the validator this document derives from the plan's own rules.

**Further review rounds have sharply diminishing returns, but they are not yet worthless.** Rounds 4 and 5 found no wrong algorithm and no false Go claim; what they found were unsurveyed alternatives, stale cross-references, incomplete specifications, and overstated confidence. Those are real and worth catching — round 5 caught four — but they are editorial quality, not design correctness, and they do not resolve a question the plan has not answered.

**Recommended next action, in order:**

1. **Decide §16.6** (Rule 4's applicability) and **§16.9** (`provider_deduplication_scope`). Together they determine whether Rule 4 and LEI are computable at all, and both have defeated three and two attempts respectively from this side.
2. **Repair or replace the worked molecule** (§15) so it passes the rules derived from the plan's own text, and **add a seventh editor verb** or move region attributes outside the editor (§16.12). Two frozen artifacts contradicting each other cannot be reconciled downstream.
3. **Decide the C2 half of §16.14** and `plan.md:1154`'s "one node per line" (§16.10), which together unblock the grammar.
4. **Then** revise this document against those answers, and run a further adversarial round on the result — the loop is useful again once the inputs change.

Until then this is a design under review with its open questions recorded, which is the state §0 says it is allowed to be in.

## 21. What changed in revision 7

Revision 6 added program completeness and a cost layer; two adversarial reviews — one that tried to *write five ordinary programs* in the notation, one that checked the new citations — found substantial errors in both.

**`fold` was wrong twice and is now a region, not a node kind.** An atom-shaped body hid effects from Rules 1, 2, and 4, which quantify over `n.Kind = effect` in the static graph — so the construct added to make programs expressible could not carry the one thing batch and saga workflows need from an accumulator. A node also creates no region, so §8.1's loop-ancestor walk and W7's capability check both silently escaped. `RegionKind` gains `fold`; node kinds return to six. Revision 6's claim that inlining resolves this was a conflation: a loop body is a static subgraph iterated at runtime, while edit-time inlining substitutes a *molecule* body at a call site, and a runtime-sized iteration cannot be unrolled at edit time.

**Two control decisions were unauthorable** (§5.8.1a) — no way to say which outcome triggers a retry, and no way to say whether a failing element continues or aborts the iteration. §5.6 had filed both as lowering gaps, which is true of the mechanism and false of the specification.

**`Prov` had no `fold` case**, so every rule depending on cone reachability downstream of a fold ran on an undefined term. Three cases added; the accumulator in-port turns out to be the clearest justification the least-fixpoint formulation has.

**"The accumulator can never enter a key cone" was a false positive.** A fold over durable inputs with an order-independent combinator is as stable as the molecule's own `DeriveKey`. Replaced by `fold_key_safe`, conditioned on what is folded rather than on node kind.

**The cost layer's foundation was wrong.** Revision 6 cited `plan.md:7897`'s `atom-complexity` gate as corroborating measurement; that gate is stage 24 of §33 *Pipeline Refinement*, an audit of `internal/pipeline` — a different subsystem whose "atom" means a small verified Go function, and which §14's hand-authored stubs never enter. Worse, `plan.md:1819` bars documentation from bearing correctness and `plan.md:2221-2231` never admits complexity as contract-expressible. The class drops from `test-backed` to `assumed-external` (§16.21). The `fold` cost rule was also wrong rather than loose — `|C| · Cost(body)` cannot express a growing accumulator's Σ O(i) = O(N²), failing at exactly the quadratic-catching §12.5.5 claims as its payoff.

**Uncited inventions now say so.** "Molecule" has no Concept Vocabulary entry and the plan uses the word differently elsewhere; the workflow signature has no plan support at all. Both were set as blockquotes, the convention this document reserves for cited plan text. Recorded as §16.20.

**Also:** W10 added for return-tag distinctness and non-empty result; the return-payload syntax gap named; the inlining trap promoted from "surprising" to a stated authoring rule (derive keys before branching); checkpointed replay added as a third durable option the notation's own resume-target syntax fits better than either previously listed; hazard count corrected from eleven to ten; `plan.md:1906` corrected to `1757`; `2334` to `2334-2336`.

## 21b. What changed in revision 6

**Program completeness (§5.8, new — the largest gap in every prior revision).** A `fold` node, restoring the accumulator that `plan.md:2195` requires and that §6 had silently dropped along with function references; without it there is no running total, no reduction, and no iteration where element *N* depends on *N−1*. Molecule and Tier 1 body composition, with edit-time inlining recommended and the derivation-instance trap it creates stated. Templates as an edit-time construct. The workflow defined as a callable unit whose result is the tagged union of its `return` nodes. Data construction and value predicates named as kernel atoms rather than node kinds. And a requirement that representability be assessed against a pre-registered sample of the fifty change requests **before task one**, since `plan.md:1284` makes representability corrections a measured Arm C cost.

**The generated program (§11.11, new).** Package layout, the entry function's signature, capability injection — and the durable seam, where `wait durable` may outlive the process, so the generated program is either an explicit state machine or a deterministic replay. `plan.md:2334` points at replay, which promotes all ten §11.9 hazards from test-flake to correctness bug. Recorded as §16.17.

**The cost layer (§12.5, new).** Every atom already declares `complexity_and_limits` (`plan.md:1839`, `internal/atomdoc/schema.go:79`), and `plan.md:7897`'s gate requires a claimed bound corroborated by a measured growth curve — test-backed, not hearsay. Composition rules over the IR, decidable precisely because every loop is bounded, there is no general recursion, and there are no higher-order values. Space composition puts a number on §10.5's boundary copies, which the design had carried as an unquantified "safe and slow." A `WithinCostBudget` obligation joining §12.1's vocabulary, with invalidation already wired through `plan.md:1906`. And an honest limit: asymptotic classes compose but constant factors do not, and Tier 3 latency dominates real workflows, so the layer's real value is space bounds and catching accidental quadratics. **It fails C1 and is post-experiment**, unlike §5.8's gaps, which the experiment requires.

**Honesty.** §19 gained a third pattern: five revisions and seventeen reviews checked whether the rules were correct and never whether the notation could express a program. A review loop converges on the questions it is pointed at.

## 21b. What changed in revision 5

**The control graph (§5.7, new).** Named the gap underneath Rule 1, Rule 4, W6, and dominance: the notation defines control edges only in `match` blocks and `->` arrows, and nothing resolves a region-targeted edge to a node. Stated both required conventions and this document's preferred reading. Generalized region entry from retry-only to all regions, correcting revision 4's confusion of entry *nodes* with incoming *edges*.

**Rule 4.** Recorded that the fixpoint logic is now verified correct on four purpose-built graphs, that it depends on convention C1, and that under §16.6's reading-1 the mandated conformance case cannot be built in the shape that broke the rule three times.

**Preconditions.** Restored W7 — cutting it substituted an asserted control for a verified one, using the same hedged citation class the cut was justified by. Narrowed W9 to region-to-region shadowing, which had made §5.3's own resolution rule dead. Added a uniqueness clause to W8 for `BoundExhausted` edges, which fell outside W5's port-scoped reach. Marked W3 untraced and added §9.7 explaining how nominal typing may reject the molecule's merge.

**Go.** `Voided` narrowed to the platform channel only, after verification showed one bit encoding three distinct meanings once eliminators chain. Replaced the unimplementable shape-detection guard with provenance — all dispatch lives in the generated package — after verification showed a laundering handler table and an innocuous one are type-identical. Stated the `select` fix's verified residual. Split §6.2 enforcement into two passes after the whitelist itself proved to be a laundering vector.

**Evaluator.** Specified that the trace digest needs a canonical encoding over §6 type-layer values, and that JSON, gob, and `fmt` are all disqualified. Corrected the redaction claim: the plan's pipeline is credential-scoped and does not cover business data. Added the field-path divergence hint, and the shared per-`NodeID` fixture counter.

**Traceability and debuggability, on request.** §11 retitled, since two of `plan.md:2261-2269`'s top three priorities live there. §11.7 expanded from source maps alone to the full §19 requirement: an identity chain from `AtomID` through `NodeID` to symbol, trace step, and obligation; the requirement that a `NodeID` survive into a panic stack trace; all nine required diffs mapped to the artifacts that compute them; and blame resolving to the revision that changed a node rather than the run that rewrote the file. §11.10 added, consolidating every debuggability cost this design has incurred, what compensates for each, the two that are unmitigated, and a diagnosis path per triage bucket.

**Corrections from the final round.** Removed a W9 justification citing an override that does not occur in the molecule. Replaced §5.5's stale dominance reason. Recorded a third control-flow convention as considered-and-rejected rather than unmentioned. Completed the canonical encoding with `TypeRef` binding, absent-versus-empty, ordering, and a type-kind discriminator, plus the six scalar questions verification left open. Added the verified limits on `Voided` under composition, on the provenance guard, and on §6.2's Pass B.

**Honesty.** Retracted revision 4's "Rules 1–3 are stable" claim. Added the behavioural pattern of premature completion claims to §19. Restructured §18 into blocking, within-reach, unresolved, and accepted, and reclassified two items revision 5 had overstated as plan-blocking. Added §20 with a convergence verdict that records round 5 as not a clean pass. Corrected §16.11's conflation of implicit with explicit polymorphism.
