The refined plan treats **Codeflux as a correctness-constrained adaptive coding platform**, **past validated work as reusable project capital**, **Go as the first deep verification target**, and **the graph as an optional, unproven correctness medium**.

# Codeflux: Correctness-Constrained Adaptive Coding Platform

**Canonical reading and build order:** Section 0 below is authoritative. The numbered sections that follow are stable reference chapters, not an instruction to build in numeric order. In particular, Sections 6–20 are an optional Deep Go Verification branch and do not precede the code-first prototype.

# Frozen Prototype Contract

This contract is the concise prototype boundary. It freezes the decisions needed
by Milestone 00; later chapters explain the design in greater detail. A change to
this contract requires an explicit plan decision and a corresponding TODO update.

## Promise and user

Codeflux helps one technically capable hobbyist or independent developer make
correct, reviewable changes to a local repository they control. It first enforces
the task's authority, validation, and acceptance requirements; inside that
correctness boundary it shortens time to an accepted change; among equally safe
options it reduces model, tool, infrastructure, and human cost. Forecasts,
contract checks, tests, and evidence remain bounded claims rather than proof of
all repository or external-system behavior. (`M00-001`, `M00-002`, `M00-G04`)

## Supported prototype boundary

The first internal build may target the current development host, Windows 11 on
ARM64, provided filesystem, process, credential, browser, and provider behavior
remain behind portable interfaces. Prototype exit requires Windows 11
(`amd64` and `arm64`), macOS 14 or later (`arm64`), and Ubuntu 24.04 LTS or a
compatible glibc Linux (`amd64`) to pass their declared CI and installation
gates. A platform without passing CI is experimental, not supported.
(`M00-003`, `M00-004`)

The supported workspace is a local Git repository. A clean worktree is the
default. A dirty worktree is accepted only after the user sees and acknowledges
the exact pre-existing changes; Codeflux must preserve them and perform task
edits in a dedicated worktree or equivalent isolated branch workspace.
(`M00-005`)

Repository intelligence is Go-first. The prototype must support scoped bug
fixes, features, tests and reproductions, refactors, dependency or configuration
changes, and documentation changes whose acceptance is tied to repository
behavior. Non-Go repositories may be opened only as explicitly labeled
experimental inputs until a language-specific mapping and validation contract
exists. (`M00-006`, `M00-007`)

One coordinator may run at most four active tasks, with at most one active task
per repository. Multiple inactive threads may exist for one repository and are
stored durably; every list and UI query remains paginated and bounded rather
than loading all inactive history. (`M00-008`, `M00-009`)

The local GoWebComponents interface is primary. A minimal CLI remains available
for installation, start/stop, diagnostics, scripted task submission, status,
safe export, and recovery; it is not a second independent source of workflow
policy. (`M00-010`)

Ordinary source code is the only editable program medium in the prototype. The
graph is derived, task-scoped, optional, read-only, and limited to explaining
Program, Execution, and Evidence identities. Direct visual programming, graph
editing, semantic Go generation, and deep verification are excluded unless
their later research gates pass. (`M00-011`, `M00-012`, `M00-013`, `M00-017`)

The prototype does not embed a general code editor, full repository file
explorer, or terminal. It does not provide multi-user collaboration, hosted
accounts, repository upload, managed execution, or enterprise administration.
It may open a resolved file in the user's external editor and show bounded,
redacted command output. (`M00-014`, `M00-015`, `M00-018`)

Routing uses a versioned fixed model-and-effort policy through prototype exit.
Forecast recommendations may run in shadow mode only after the fixed-policy
evidence gate below passes; shadow output has no execution authority.
(`M00-016`)

## Acceptance definitions

A coding task is **successfully completed** only when the requested observable
behavior exists at the exact candidate revision, all required validation and
security checks pass, no unresolved blocker or unauthorized action exists, the
diff and evidence are internally consistent, and the task reaches
`AwaitingAcceptance` within its approved hard budget. A model stopping or
claiming completion is not task completion. (`M00-019`)

A result is **user accepted** only when the user approves the exact diff,
evidence, validation, limitations, cost, and repository base/candidate
revisions presented for review. Stale approval is rejected. Benchmark
acceptance additionally requires the separately controlled evaluator to pass
without the implementing agent seeing hidden tests. (`M00-020`)

An **agent-caused regression** is a previously passing, supported behavior or
required quality gate that fails at the candidate revision, where the failure
is attributable to the task's edits, generated output, dependency change,
configuration change, or commanded side effect. Pre-existing failures recorded
before the task are not agent-caused unless the agent conceals, worsens, or
misclassifies them. Delayed defects and rollbacks update this classification
retroactively. (`M00-021`)

An **unauthorized action** is any read, write, command, network request,
credential access, installation, destructive operation, external message,
publication, deployment, provider switch, budget increase, validation waiver,
or scope expansion that lacked the exact authority required at execution time.
An action remains unauthorized when it happens to succeed or is approved only
afterward. (`M00-022`)

## Frozen measurement contract

All metrics bind to task ID, accepted base, candidate revision, Codeflux
version, provider/model revision, execution policy, validation profile, host,
and UTC timestamps. Counts report numerators and denominators; retries, failed
cheap attempts, contaminated runs, and manual intervention remain included.

Correctness metrics are hidden-acceptance pass rate, required-validation pass
rate, substantive review finding rate by severity, escaped-defect rate,
rollback rate, unauthorized-action count, secret-exposure count, and
agent-caused regression count. Speed and cost comparisons are invalid when the
compared policy violates its correctness floor. (`M00-023`)

Latency timestamps are requirement accepted, plan available, first authorized
tool or edit action, first non-empty diff, validation complete, review ready,
and accepted or terminated. Report time-to-plan, time-to-first-action,
time-to-first-diff, and time-to-accepted-completion with median, P90, range, and
failure/censoring counts. Provider, queue, tool, validation, review, and human
clarification time are also reported separately. (`M00-024`)

Cost records uncached input, cache-write, cache-read, output, and reasoning
tokens when exposed; provider charges; paid tool charges; local compute time;
and measured human review/intervention minutes. Monetary amounts use exact
decimal minor units and the price snapshot effective at request time. Unknown
usage or price remains unknown, never zero. Report total cost per accepted
change and cost of all failed attempts. (`M00-025`)

Usability metrics are clean-install-to-ready time, repository-open time,
first-task completion time, completion without rescue, number and duration of
clarifications and approvals, pause/resume success, review decision time,
rollback success, recovery success, user-reported state/authority/cost/next-step
comprehension, keyboard completion, and repeat-use intent. (`M00-026`)

Persistence metrics are recovery-point age, replayed event count, replay
duration, state checksum agreement, worktree/base binding agreement, duplicate
external-effect intent count, lost correctness-bearing event count, ambiguous
recovery count, and successful resume/rollback rate for every injected durable
boundary failure. (`M00-027`)

On the ordinary reference laptop (8 logical CPU cores, 16 GiB RAM, SSD, current
supported browser), a 10,000-event thread must show an interactive viewport in
at most 2 seconds; input and focus feedback must remain under 100 ms at P95;
ordered streamed state must appear under 250 ms at P95 after coordinator
receipt; a 300-visible-node graph patch must commit under 100 ms at P95; and
pan, zoom, selection, and typing must not exhibit a frame over 50 ms at P95
during the measured stream scenario. No performance optimization may drop,
reorder, or invent correctness-bearing state. (`M00-028`)

The maximum acceptable lost correctness-bearing event count across reconnect is
zero. The maximum acceptable duplicate correctness-bearing command or external
effect intent after retry, reconnect, or replay is zero. Any non-zero result
fails the prototype gate. (`M00-029`, `M00-030`)

## Unsupported assurance claims

The prototype does not claim that generated or edited software is bug-free,
secure, compliant, production-ready, or correct beyond the named tests and
evidence. It does not prove external services honor idempotency, atomicity,
availability, ordering, delivery, authentication, or compensation contracts.
Workspace confinement is not a perfect sandbox. A passed contract or graph
check is not proof of runtime behavior. Forecasts are estimates, local secret
redaction is not a guarantee that arbitrary secrets are discoverable, webhook
delivery is not exactly once, and accepted work may later be invalidated by a
regression or incident. (`M00-031`)

## Frozen demonstration and independent acceptance

The initial end-to-end demonstration is **ReserveFlow Task 1: Server and
Health**, using the separate repository
`C:\Users\mreca\Desktop\reserveflow` at Git revision
`1d14bb1b01ceb8ecb691e58b10998f37e66ee8d9`. The frozen task contract is in
that repository's `README.md`. The implementing system must configure, build,
start, probe, review, and stop the service without manual source edits.
(`M00-032`, `M00-033`)

Independent black-box acceptance is stored outside both repositories at
`C:\Users\mreca\Desktop\reserveflow-evaluator`, frozen at revision
`c7179fd32d9b010877db2c3102bce327cf980bbf`. The evaluator builds only the
public command, starts it through the documented environment contract, and
checks health, readiness, JSON/content type, request identity, safe errors,
loopback binding failure, and prompt non-zero exit. It neither imports
ReserveFlow internals nor exposes its tests to an evaluated Codeflux run.
(`M00-034`, `M00-G03`)

## Frozen baselines and price snapshot

The direct current-agent baseline is OpenAI Codex using `gpt-5.6-sol` at `max`
reasoning effort with the same repository instructions, clean starting
revision, authority, hard budget, visible tests, and local tools as Codeflux.
The run records the exact provider-returned model revision or fingerprint; a
changed or unavailable revision starts a new baseline stratum rather than being
silently pooled. Its allowed tools are bounded file read/search/edit, Git, Go
build/test/format/vet, local process execution, and loopback HTTP probing.
Network access, dependency installation, hidden evaluator access, credential
access, external messaging, and publication are denied unless the frozen task
explicitly authorizes the same scope for every arm. (`M00-035`, `M00-036`)

The 2026-07-30 OpenAI API price snapshot for GPT-5.6 Sol is USD $5.00 per
million uncached input tokens, $0.50 per million cached input tokens, and
$30.00 per million output tokens; cache writes are 1.25 times uncached input.
The authoritative snapshot source is the
[GPT-5.6 Sol model page](https://developers.openai.com/api/docs/models/gpt-5.6-sol).
Subscription credits are recorded separately from API-equivalent economic
cost.

The second named code-first comparison for the later Code-First Agent gate is
Claude Code using `claude-fable-5`, its frozen default agent harness settings,
and the identical authority/tool envelope. The 2026-07-30 API snapshot is USD
$10.00 per million input tokens and $50.00 per million output tokens with a
90% prompt-cache input discount, from Anthropic's
[Claude Fable 5 page](https://www.anthropic.com/claude/fable). A manual expert
track may be recorded for human-time context but is not substituted for either
named coding-agent baseline.

## Authorization gates for later branches

Shadow forecasting may begin only after all M24 fixed-policy gates pass, the
clean Track B rerun passes, and at least 30 eligible fixed-policy tasks across
at least three repositories have complete correctness, latency, usage, cost,
risk, and intervention records. P90 time and cost intervals must cover at least
80% of held-out outcomes, acceptance calibration must meet its frozen
tolerance, and out-of-distribution cases must abstain safely. Shadow output
cannot change model, effort, tools, validation, authority, or budget.
Automatic adaptive routing still requires the 100-task paired experiment and
Section 30 thresholds. (`M00-037`)

The prototype's read-only Program, Execution, and Evidence views may add richer
bounded explanations only after M19 performance/accessibility gates pass and
30 independent task sessions show at least 70% user-rated usefulness with no
correctness regression and at most 10% state/authority confusion attributable
to the graph. Direct graph editing or semantic Go generation remains forbidden
until the 50-task graph-medium experiment, lowering conformance, incident
surface, kernel, proof-coverage, and review gates all pass. (`M00-038`)

## Feature-to-journey and measurement map

| Prototype capability | User journey or required measurement |
|---|---|
| Local install, provider setup, repository open | Install-to-ready, repository-open, credential-boundary checks |
| Requirement, scope, forecast, plan, approval | Time-to-plan, clarification/approval burden, forecast coverage |
| Worktree, safe edits, mediated commands | Concurrent-edit preservation, unauthorized-action and escape counts |
| Fixed provider policy and hard budget | Exact usage/cost, cap enforcement, no silent switching |
| Live thread, controls, reconnect, recovery | State comprehension, pause/resume, zero loss and zero duplication |
| Diff, validation, evidence, repair, rollback | Acceptance, review findings, regressions, rollback success |
| Read-only task graph | Graph usefulness, confusion, accessibility, responsiveness |
| Deterministic project memory | Retrieval influence, rejection, invalidation, correctness and marginal effort |
| Packaging, diagnostics, updates | Clean-install exit, doctor accuracy, redaction and recovery |

Deferred enterprise, adaptive, vector, multi-agent, direct graph-editing, and
deep-verification features have no dependency edge into this table's prototype
journey. A deferred item may become active only at its named branch gate.
(`M00-G01`, `M00-G02`)

## Major subsystem declaration rule

Before a new major subsystem enters `TODOS.md`, its plan section must declare:

1. the smallest prerequisite concepts from the Section 0 vocabulary;
2. its owning product layer and milestone;
3. typed inputs and their authority/revision scope;
4. observable outputs and durable events;
5. the validation or measurement that accepts the subsystem;
6. forbidden forward dependencies on later optional layers;
7. a stop, narrow, or removal condition.

The plan trace check rejects a major subsystem declaration that omits these
fields or maps to a nonexistent layer or reference section. (`M00-046`)

## Milestone-to-layer map

| Milestone | Canonical layer |
|---|---|
| M00 | Layer 0 |
| M01 | Layer 1 |
| M02 | Layer 2 |
| M03 | Layer 3 |
| M04 | Layer 4 |
| M05 | Layer 5 |
| M06 | Layer 6 |
| M07 | Layer 7 |
| M08 | Layer 8 |
| M09 | Layer 8 |
| M10 | Layer 9 |
| M11 | Layer 9 |
| M12 | Layer 10 |
| M13 | Layer 10 |
| M14 | Layer 11 |
| M15 | Layer 12 |
| M16 | Layer 13 |
| M17 | Layer 13 |
| M18 | Layer 13 |
| M19 | Layer 14 |
| M20 | Layer 15 |
| M21 | Layer 16 |
| M22 | Layer 17 |
| M23 | Layer 17 |
| M24 | Layer 17 |

This map is executable documentation: `docs/check-plan-trace.ps1` verifies that
all milestones and layers exist, every milestone has exactly one mapping, and
the Section 0 reference chapters exist. (`M00-045`, `M00-G05`)

# 0. Linear Concept and Build Order

The plan is organized around one rule:

> Establish small, testable, durable concepts first. Compose them into local flows. Build the user experience over those flows. Add learning only after outcomes are trustworthy. Add adaptive and deep-verification ambitions only after the simpler system earns them.

The implementation queue in [`../TODOS.md`](../TODOS.md) follows this exact order.

## Smallest Concepts First

The system grows through the following conceptual stack:

```text
typed value
-> stable identity
-> immutable revision
-> validated state transition
-> command or query
-> durable event
-> entity projection
-> transactional application function
-> local service
-> chronological backend flow
-> frontend reducer and component
-> complete user journey
-> validation and evidence
-> factual episode
-> reusable project knowledge
-> effort forecast and routing policy
-> optional atom/graph verification
-> measured compounding platform
```

Each layer may depend only on earlier layers or on an explicitly defined port. Later ambition must not leak backward into foundational code.

## Concept Vocabulary

### Typed Value

A small immutable value with validation and domain meaning, such as money, token usage, repository-relative path, sequence, risk, assurance, or policy.

Typed values prevent invalid combinations before storage or transport exists.

### Stable Identity

A durable identity for one conceptual entity: project, repository, thread, task, run, event, checkpoint, graph, node, atom, evidence item, or memory artifact.

Identity answers `which thing?` It is not a name, version, path, or current state.

### Immutable Revision

A versioned description of an entity at one point in history.

Revision answers `which state of the thing?` Updates create a new revision or state transition rather than rewriting historical evidence.

### State Transition

A validated movement between explicit states. Transitions reject impossible, stale, unauthorized, or terminal-state mutations.

### Command

A request to change state. A command carries typed input, authority, idempotency identity, and expected revision when necessary.

### Query

A bounded read that declares project/task scope, revision, pagination, and result limits. Queries do not change authority or state.

### Durable Event

An immutable fact committed in the same transaction as the state change it describes. Events drive replay, UI projection, graph projection, recovery, and later learning.

### Projection

A deterministic view derived from authoritative records and ordered events: task state, timeline, graph slice, budget summary, validation report, or frontend store.

### Application Function

One transactional use case that coordinates domain rules, repositories, external-effect intent, events, and typed failures. gRPC handlers call application functions; they do not replace them.

### Flow

An ordered composition of application functions and effects that produces one user-visible outcome, including failure, cancellation, reconnect, and recovery branches.

### Evidence

Version-bound support for a specific claim. Evidence can pass, fail, be waived, become stale, or be invalidated.

### Episode

The immutable chronological record of one completed or terminated task: intent, plan, actions, interventions, validation, decision, time, cost, and outcome.

### Project Knowledge

An admitted fact, command, mapping, regression, recipe, or atom derived from attributable evidence. Knowledge has scope, bindings, lineage, maturity, and invalidation.

### Atom

A reusable unit with stable identity, descriptive name, typed signature, contract, detailed documentation, applicability, effects, bindings, and evidence. An atom is larger than a helper function and smaller than a workflow.

### Graph

A versioned composition of identities, atoms, control, data, effects, obligations, and evidence. The prototype graph first explains tasks; the optional deep graph later attempts stronger program semantics.

### Execution Policy

A versioned choice of model, effort, context, tools, budget, validation floor, escalation, and stopping behavior.

### Pattern or Rule

A later generalization across independent episodes. It cannot become authority merely because it was generated, retrieved, or frequently used.

## Linear Product Layers

### Layer 0: Freeze the Promise

Reference:

* §1 Product Constraints;
* §2 Revised Product Thesis;
* §27 Initial Product Scope;
* §29 Phase 0;
* §30 Kill and Pivot Criteria.

Decide:

```text
who the prototype serves
which task classes it supports
what correctness means
what is measured
what is explicitly deferred
what result kills or narrows the idea
```

Output: a frozen hobbyist prototype promise and independent acceptance task.

Do not design implementation details to rescue an unfrozen product claim.

### Layer 1: Establish the Repository and Engineering Contract

Reference:

* `AGENTS.md`;
* `CLAUDE.md`;
* `TODOS.md`;
* §27D Prototype Developer Experience.

Build:

```text
Go module and package boundaries
cross-platform development helper
generation and lint checks
agent instructions
deterministic test conventions
```

Output: a fresh clone that can bootstrap, generate, lint, and run fast tests through documented commands.

### Layer 2: Define Domain Primitives

Reference:

* §18 Stable Graph Identity for the general identity model;
* §22 Correctness and Assurance Gates;
* §23 Core Operational Entities;
* §27B Backend Design Rules.

Build:

```text
typed IDs
exact money and usage
policy and risk values
task/run/approval/checkpoint/validation states
typed errors
transition validators
```

Output: pure domain code with exhaustive transition and serialization tests.

No database, provider, worker, browser, graph, or adaptive policy is needed yet.

### Layer 3: Add Durable Local State

Reference:

* §23 Storage;
* §23 Transactions, Migrations, and Recovery.

Build:

```text
SQLite lifecycle
migrations and backup
domain repositories
foreign keys and constraints
transaction runner
integrity and recovery primitives
```

Output: real-SQLite tests proving atomic writes, conflicts, backup, restore, and migration behavior.

SQLite stores facts; it does not yet make an agent.

### Layer 4: Add Configuration, Credentials, and Redaction

Reference:

* §27 Provider Credentials;
* §27 Commands, Secrets, and Malicious Repository Content;
* §27A Local Security.

Build:

```text
settings precedence
OS credential-store port
secret-free credential references
redaction pipeline
repository-content trust boundary
```

Output: configuration and provider testing with no credential in SQLite, logs, events, child environments, or exports.

### Layer 5: Add Events and Replay

Reference:

* §27A Unified Session Stream;
* §27B Transaction and Event Functions;
* §27 Persistence, Recovery, Diagnostics, and Updates.

Build:

```text
session event envelope
monotonic per-session sequence
atomic state-plus-event commit
post-commit publication
replay and snapshot
replay-to-live join
backpressure policy
```

Output: deterministic reconstruction after disconnect or process restart with no lost or duplicated correctness-bearing event.

Events become the spine for every larger concept.

### Layer 6: Prove the Browser Transport

Reference:

* §27A Framework and Transport Spike;
* §27A Rendering and Performance;
* §27A Local Security.

Build only the bounded GoWebComponents v5/gRPC bridge spike.

Output: pinned framework/transport versions, authenticated loopback delivery, cancellation, reconnect, and measured simultaneous chat/graph update behavior.

Do not build the complete frontend on an assumed v5 API.

### Layer 7: Define the Application and gRPC Surface

Reference:

* §27A Service Contracts;
* §27B Prototype Backend Function and Flow Specification.

Build:

```text
application service commands, queries, and results
authority and idempotency
expected revisions
transaction/event ownership
typed error mapping
thin gRPC handlers
generated clients
```

Output: the complete synthetic user journey through generated clients and deterministic fakes.

### Layer 8: Understand and Isolate the Repository

Reference:

* §5 Workspace Intelligence;
* §27 Repository Indexing and Context Selection;
* §27 Local Runtime and Repository Isolation;
* §19 Review and Source Mapping.

Build in this order:

```text
read-only repository discovery
deterministic Go map
bounded explainable context selection
isolated Git worktree
safe path resolution
expected-hash edits
diff and checkpoint binding
```

Output: no agent edit can escape the worktree or silently overwrite concurrent user changes.

### Layer 9: Add Mediated Tools and Worker Lifecycle

Reference:

* §21 Coordinator and Coding Agent;
* §27 Commands, Secrets, and Malicious Repository Content;
* §27B Tool, Permission, Command, and Worker Functions.

Build:

```text
typed tools
authority classification
approval records
bounded commands
one worker per active task
leases and heartbeat
pause/cancel/checkpoint
crash classification
```

Output: an isolated fake task can inspect, edit, run a command, request authority, checkpoint, and stop safely.

### Layer 10: Add Providers, Fixed Policy, Forecast, and Budget

Reference:

* §27 Initial Model Providers;
* §21 Effort Forecaster, Model and Effort Router, and Routing Safety;
* §25 Cost and Forecast and Routing Quality.

Build:

```text
normalized provider port
OpenAI, Anthropic, and local-compatible adapters
exact usage and price snapshots
bounded retry and explicit switching
fixed model/effort baseline
transparent heuristic P50/P90 forecast
atomic reservations and hard budget
```

Output: model work is attributable, cancellable, budget-bound, and comparable. Adaptive routing remains disabled.

### Layer 11: Build the Smallest Agent Loop

Reference:

* §5 Human Intent, Task Fingerprint and Retrieval, Execution and Review;
* §21 Agent Architecture;
* §27B Task, Plan, and Execution Functions.

Compose prior layers into:

```text
requirement
-> deterministic context
-> fixed-policy forecast
-> plan
-> approval
-> edit/tool loop
-> bounded repair
-> completion candidate
```

Output: a deterministic fake provider completes the entire state machine before real-model quality is evaluated.

### Layer 12: Make Interruption Normal

Reference:

* §23 Transactions, Migrations, and Recovery;
* §27 Persistence, Recovery, Diagnostics, and Updates;
* §27B Pause, Cancel, Resume, and Crash flows.

Build checkpoint, pause, cancel, resume, divergence detection, ambiguous-effect handling, and patch preservation.

Output: failure at every tested durable boundary yields a safe user choice.

### Layer 13: Build the Frontend from Shell to Journey

Reference:

* §27A Local Frontend and Tooling;
* §27C Prototype Frontend Component and UX Specification.

Build in this order:

```text
design tokens and primitives
session bootstrap and routes
application shell
stores and pure reducers
thread rail
virtual timeline and cards
composer
live task controls
review surface
settings, memory, diagnostics, and first-run
```

Every component receives explicit loading, empty, partial, error, denied, incompatible, disconnected, keyboard, focus, and accessibility behavior before polish.

Output: the full code-first task journey works with the graph pane collapsed.

### Layer 14: Add the Explanatory Task Graph

Reference:

* §18 Stable Graph Identity;
* §23 Graph Storage;
* §27A Graph Modes and Rendering Rules;
* §27C Graph Component Contracts.

Build:

```text
event-derived task graph
immutable revisions
bounded slices
stable layout
Program/Execution/Evidence modes
node inspector
chat-to-graph identity links
```

Output: the graph explains a task without becoming the source-editing medium.

This is not authorization to build Sections 6–20.

### Layer 15: Add Validation, Review, and Evidence

Reference:

* §22 Correctness and Assurance Gates;
* §19 Review and Source Mapping;
* §27B Validation and Review Functions;
* §27C Review Drawer Contracts.

Build:

```text
risk classification
required validation profiles
test selection
diff-bound validation
baseline comparison
claim-level evidence
stale-review detection
accept/repair/reject/rollback
```

Output: accepted work binds to the exact reviewed diff and evidence revision.

### Layer 16: Add Deterministic Project Memory

Reference:

* §31 Evidence-Driven Reuse and Learning;
* §23 Atom and Vector Storage;
* §7 Atom Naming and Documentation as retrieval material.

Build in this order:

```text
chronological episodes
repository facts and reviewed commands
file-to-test mappings
versioned task fingerprints
exact retrieval
compatibility/applicability/evidence/assurance gates
atom name and documentation admission
optional SQLite vector candidate discovery
lineage and invalidation
```

Output: accepted prior work may reduce later context or effort without similarity becoming authority.

Vector retrieval is added only after deterministic retrieval has a measured miss.

### Layer 17: Harden, Package, and Run the Prototype Exit

Reference:

* §24 Specification Review;
* §25 Metrics;
* §26 Benchmark Timing;
* §27D Prototype Developer Experience;
* §28 Initial Demonstrations;
* §28 ReserveFlow Dogfood API Refinement Trial;
* §30 Kill and Pivot Criteria.

Build deterministic fakes, replay, fault injection, security cases, accessibility tests, packaging, first run, doctor, backup, update, and clean-machine vertical-slice evaluation.

Then use the packaged tool—not manual source editing—to build the chronological ReserveFlow API from a frozen clean scaffold. Run independent hidden acceptance after every requirement. When Codeflux fails, freeze the run, reproduce the defect at the lowest responsible layer, add a regression test, implement the smallest general repair, and rerun from the original clean task boundary with memory first disabled and then enabled.

Output: a working ReserveFlow API, an attributable defect/refinement ledger, a clean final rerun, and a reproducible continue, narrow, redesign, defer, or stop decision.

### Layer 18: Add Adaptive Routing Only After the Baseline

Reference:

* §3 Adaptive and Compounding-Effort Experiment;
* §21 Effort Forecaster and Router;
* §29 Phases 3–5;
* §30 Adaptive Routing and Compounding-Memory Failure.

Progression:

```text
fixed policy
-> shadow forecasts
-> calibration and counterfactual subset
-> limited model/effort routing
-> rollback on regression
-> broader memory only after independent value
```

The router does not select validation floors or multi-agent topology in its first adaptive form.

### Layer 19: Run the Optional Deep Go Verification Branch

Reference:

* §3 Graph-Medium Experiment;
* §4 Incident Archaeology;
* §§6–20 Deep Go Verification;
* §28 Go Structural-Verification Demonstration;
* §29 Phase 6;
* §30 Graph and Kernel kill criteria.

Only begin if:

```text
the code-first prototype works
the addressable incident surface passes
the graph medium beats the frozen alternative
kernel scope remains bounded
the proof wedge covers meaningful request-side failures
```

Then progress from kernel primitives to atom tiers, obligations, effects, types, contracts, lowering, determinism, identity, review, and generator migration.

This branch is a later composition over stable runtime, storage, evidence, and review primitives. It must not redefine those foundations.

### Layer 20: Add Mechanical Rules and Advisory Patterns

Reference:

* §31 Mechanical-Rule Governance;
* §31 Regression Oracle and Execution Policy;
* §31 Permanent Clean Room;
* §29 Phase 7.

Only independent replay evidence can promote a mechanical rule. Advisory patterns remain experimentally available, not self-sealing preferences.

## Dependency Invariants

The following dependencies are prohibited:

```text
domain -> SQLite, provider, gRPC, browser, or Git implementation
storage -> frontend presentation
gRPC handler -> hidden domain policy
frontend reducer -> durable state invention
graph visualization -> required source editing
vector similarity -> applicability or authority
forecast -> lowered correctness floor
memory item -> self-validation
agent self-report -> accepted outcome
generated Go -> independent semantic authority
optional verifier -> prototype runtime prerequisite
enterprise backlog -> hobbyist prototype critical path
```

## Branch Points and Stop Gates

The linear path has four deliberate branch points:

1. vector discovery remains off unless deterministic retrieval has a measured recall problem;
2. adaptive routing remains off unless fixed-policy data supports calibrated shadow forecasts;
3. multi-agent execution remains off unless single-agent bottlenecks and incremental value are measured;
4. deep graph verification remains off unless its incident, medium, kernel, proof-coverage, and review gates pass.

At each branch, `not yet justified` means stop, not add more design prose.

## Canonical Reading Sequence

For a builder reading the full plan:

```text
0
-> 1
-> 2
-> 27
-> 5
-> 23
-> 27B
-> 27A
-> 27C
-> 27D
-> 21
-> 22
-> 24
-> 31
-> 25
-> 26
-> 28
-> 29
-> 30
-> 3 and 4 when preparing experiments
-> 6 through 20 only after the optional graph branch is authorized
-> 32 as the final consistency check
```

For implementation, follow `TODOS.md` milestones M00 through M24 rather than numeric plan sections.

---

# 1. Product Constraints

Codeflux competes with agentic coding tools such as terminal coding agents and AI-native editors. Its primary artifact is a correct, reviewable change to an existing repository—not a graph.

The platform must:

* work code-first with ordinary repositories, tools, tests, and pull requests;
* make correctness policy a hard constraint rather than a cost-speed preference;
* estimate task effort and uncertainty before and during work;
* choose models, reasoning effort, tool budgets, validation depth, and agent topology dynamically;
* convert validated work into durable project knowledge that can reduce later effort;
* store all Codeflux-managed atoms, graphs, vectors, evidence, and memory in one local SQLite database rather than artifact files;
* preserve human review, provenance, and override authority.

Go is the first language receiving deep semantic verification, deterministic lowering, and request-side effect analysis. The orchestration, routing, memory, telemetry, and review architecture must not be Go-specific.

The graph remains an experimental medium for protected Go workflows. Ordinary source code remains the default source of truth unless the graph wins its pre-registered medium experiment.

Historical artifacts are not trusted merely because they exist. Reuse requires compatibility, evidence, version bindings, and lineage-aware validation.

---

# 2. Revised Product Thesis

The primary thesis is:

> Codeflux improves correctness, speed, and total cost by adapting execution to the requirement, repository, risk, and evidence from prior validated work.

The optimization order is:

```text
1. Meet the task's required correctness and assurance policy.
2. Meet the user's latency target when safely feasible.
3. Minimize total model, tool, infrastructure, and human cost among policies satisfying 1 and 2.
4. If the latency target is infeasible, select the fastest safe policy and disclose the expected overrun.
```

User modes such as `fast`, `balanced`, and `economical` change the latency target and willingness to escalate. They never lower correctness, authority, security, or protected-path floors.

Conceptually:

```text
minimize   ExpectedTotalCost
subject to ActualValidationPolicy ≥ required policy
           PredictedAcceptance ≥ required threshold
           PredictedLatency ≤ user latency target
           RequiredValidationGates pass
           Protected assurance obligations do not regress
```

The prediction is calibrated evidence, not a guarantee. When uncertainty is high, the router escalates validation or model capability rather than claiming false precision.

## Compounding-Effort Thesis

Every accepted task should leave candidate evidence; validated and matured outcomes may create reusable project capital:

* repository structure and dependency facts;
* validated requirements and acceptance examples;
* validated plans and scoped change strategies;
* build, test, and debugging knowledge;
* exact reusable atoms and project conventions;
* regression cases and mechanical rules;
* model-performance and effort observations;
* review decisions and failure counterexamples.

Before new work begins, Codeflux retrieves compatible evidence, estimates the required effort distribution, and selects an execution policy. During work, it monitors progress and may escalate, de-escalate, re-plan, or stop.

Task and artifact maturity is explicit:

```text
Accepted  = immediate task review and required checks passed
Validated = CI, integration, and applicable independent review passed
Matured   = delayed observation window closed without rollback, incident, or attributed defect
```

Low-risk deterministic workspace facts may be admitted after validation. Reusable strategies, routing evidence, and broader recommendations carry maturity state and may require the delayed window. A later defect retroactively invalidates dependent memory and routing evidence.

The compounding claim must be measured:

> At equal or better correctness, marginal time and cost per comparable task should decline as validated project evidence accumulates.

## Deep Correctness Wedge

For high-risk Go workflows, Codeflux may additionally verify structural properties such as single logical issuance structure, effect ordering, exhaustive response handling, compensation coverage, stable request construction, and capability containment.

Codeflux complements database constraints, vendor idempotency APIs, durable-execution systems, and operational controls. It proves that a workflow uses its declared strategy correctly; it does not provide at-most-once execution or predict external responses.

The graph and verifier are correctness subsystems inside the broader adaptive coding platform. They are not the product identity and must not delay a useful code-first agent.

---

# 3. Load-Bearing Experiments

**Linear position:** Freeze experiment definitions and thresholds in Layer 0, but run the adaptive experiment only after the fixed prototype baseline and run the graph experiment only before entering Layer 19. This chapter is a gate specification, not an instruction to build experimental subsystems before the runtime.

The platform has two independent load-bearing claims:

1. adaptive routing and accumulated project evidence reduce effort at equal or better correctness;
2. a structured graph can improve enforcement of high-risk workflow properties beyond disciplined source editing.

The first claim defines the product. The second defines an optional correctness subsystem.

## Adaptive and Compounding-Effort Experiment

Freeze chronological task streams containing at least one hundred eligible paired tasks across multiple repositories. Preserve order within each repository because the hypothesis concerns improvement from accumulated project evidence.

Every arm receives the same canonical starting revision for each task. When tasks depend on previous changes, all arms advance along one canonical accepted commit chain while memory histories remain isolated. Task `n` may access only artifacts timestamped and admitted before task `n`.

Run four policies with the same model pool, tools, repositories, acceptance tests, correctness floors, and maximum authority:

### Platform Arm A1: Strong Fixed Baseline

Uses the strongest static model and effort policy selected on a separate tuning set. The policy is frozen before evaluation and receives no persistent Codeflux project memory.

### Platform Arm A2: Static Risk-Tier Baseline

Uses a simple frozen mapping from immutable task-risk tiers to model and reasoning effort. It represents the strongest cheap alternative to learned routing.

### Platform Arm B: Adaptive Execution

Adds task fingerprinting, effort forecasting, model and reasoning-effort routing, progress monitoring, and dynamic escalation. The first router does not select validation profiles or multi-agent topology. It receives no reusable evidence from earlier benchmark tasks.

### Platform Arm C: Compounding Execution

Adds admitted project memory and reusable artifacts from earlier validated or matured tasks in its own isolated history.

Interpret:

```text
Arm B - best(A1, A2) = value of effort estimation and adaptive routing
Arm C - Arm B = value of accumulated project evidence
```

On a separate calibration subset, execute a small safe matrix of model × reasoning-effort policies. This supplies counterfactual outcomes for calibration and regret estimates. Do not infer unselected-policy performance from ordinary router logs alone.

Measure:

* hidden acceptance and required-validation pass rate;
* human review, delayed defect, rollback, and incident outcome;
* wall-clock time to accepted change;
* model tokens and tool calls;
* model, infrastructure, and estimated human cost;
* number and timing of escalations;
* effort-forecast calibration;
* reuse frequency and verified contribution;
* marginal cost and latency over chronological task index.

Correctness is a gate, not a weighted metric. A cheaper arm fails when it falls below the pre-registered acceptance, security, or assurance policy.

Report immediate accepted outcomes separately from validated and matured outcomes. Later rollbacks or attributed defects retroactively update the task outcome and invalidate dependent memory evidence.

The effort forecaster reports distributions rather than point guesses:

```text
P(accepted without escalation)
P50 and P90 wall-clock time
P50 and P90 model/tool cost
expected tool calls
required validation profile
recommended model and reasoning effort
```

The initial sample is not large enough to calibrate every repository × task class × risk × model interaction independently. Evaluate global calibration plus pre-registered coarse risk and novelty groups, report sparse groups without claiming calibration, and route out-of-distribution tasks conservatively.

Analyze paired `C - B` outcomes over task index, clustered by repository and stratified by pre-registered task class. Include capture, curation, invalidation, retrieval, and human-review cost.

The compounding claim passes only when Arm C maintains correctness and the late benchmark block improves total cost or accepted-change latency by at least the frozen material threshold relative to Arm B. If the advantage does not grow with eligible evidence, stop describing project memory as compounding.

## Graph-Medium Experiment

Before building the graph language, run a pre-registered medium experiment.

The core question is:

> Is a structured functional graph a better editing and reasoning medium for coding agents than ordinary Go source text?

## Experiment Design

Select fifty real change requests before designing the experimental graph notation. Use the same starting revision, hidden acceptance tests, model family, tool access, and resource limits for every arm.

The experimental notation is deliberately disposable. It requires only enough serialization, stable identity, and provisional kernel semantics to run the experiment. The people editing the tasks must not be the people who designed the notation, and acceptance grading must be blind.

### Arm A: Ordinary Go

The agent edits Go with the project's normal tools, tests, and instructions.

### Arm B: Go with Structural-Effect Protocol

The agent edits ordinary Go but must demonstrate:

* no sequential duplicate issuance for one logical effect identity;
* stable shared-key provenance across claim, query, issuance, retry, and reconciliation;
* reconciliation gating every path from an ambiguous outcome to compensation;
* a confirmed atomic durable claim gating issuance whenever deduplication depends on prior state.

This arm estimates the value of explicit effect discipline without a new editing medium.

### Arm C: Functional Graph

The agent receives the same required structural-effect properties as Arm B but edits the disposable graph. The graph is validated, lowered to Go, and tested with the same tools and acceptance suite.

Interpret the paired differences as:

```text
Arm B - Arm A = value of explicit structural-effect discipline
Arm C - Arm B = incremental value of the graph medium
```

If Arm B wins but Arm C does not improve on Arm B, the product becomes effect and refinement checking over ordinary Go rather than a graph compiler.

## Disposable Graph Experiment Freeze

The disposable notation contains only:

1. sum-typed effect responses;
2. provenance-carrying data edges;
3. labeled control edges;
4. explicit merges and regions;
5. effect relationships;
6. bounded loops with iteration provenance;
7. conditional durable-claim nodes.

The notation is line-oriented text with one node per line, permanent explicit IDs, lexical regions, pinned atom versions, and provenance expressed by stable references.

Example:

```text
workflow ReceivePayment v1
  capabilities: [payments.charge, payments.read, db.write:payments]
  effect_identity: (operation_contract_id, provider_scope, key)

  n01 input req : PaymentRequest
  n02 pure valid = ValidateRequest@1.2(req: n01.out) : Valid | Invalid
  n03 pure key = DeriveKey@2.0(req: n01.out) -> IdempotencyKey
      requires provenance_cone subset_of [n01.out]

  region claim [dedup: LocalAtomicClaim] {
    n04 effect claim = InsertIntent@1.0(key: n03.key)
                     : Acquired | AlreadyExists | AmbiguousClaim
  }

  match n04.out {
    Acquired       -> region issue
    AlreadyExists  -> n05
    AmbiguousClaim -> n13
  }

  n05 effect prior = QueryPriorState@1.0(key: n03.key)
                   : Completed(r) | InProgress | TerminalFailed(f)
  match n05.out {
    Completed(r)     -> n12
    InProgress       -> n09
    TerminalFailed(f)-> n11
  }

  region issue [dedup: ProviderIdempotency, contract: gateway-2026-07] {
    region retry [max: 3, backoff: exp] {
      n06 effect charge = IssueCharge@3.1(
            key: n03.key,
            amount: n01.out.amount
          ) : ConfirmedSuccess(c) | ConfirmedFailure(f) | Ambiguous(a)
    }
  }
  match n06.out {
    ConfirmedSuccess(c) -> m01
    ConfirmedFailure(f) -> n11
    Ambiguous(a)        -> n09
  }

  region reconcile [max_polls: 6] {
    n09 effect probe = QueryCharge@1.0(key: n03.key)
                     : ConfirmedSuccess(c) | ConfirmedFailure(f) | StillUnknown
                     reconciles n06
    n10 wait durable [timeout: 24h] -> n09
  }
  match n09.out {
    ConfirmedSuccess(c) -> m01
    ConfirmedFailure(f) -> n11
    StillUnknown        -> n10
  }

  m01 merge charge <- [n06.c, n09.c] : Charge
  n07 effect persist [dedup: NaturallyIdempotent, contract: result-upsert-1.0]
      = WriteResult@1.0(key: n03.key, charge: m01.charge)
      : Persisted | PersistFailed(e)
  n08 effect refund [dedup: ProviderIdempotency, contract: refund-2026-07]
      = RefundCharge@1.0(key: n03.key, charge: m01.charge) compensates n06
  match n07.out {
    Persisted        -> n12
    PersistFailed(e) -> n08
  }

  n11 return Declined
  n12 return Success
  n13 effect claim_probe = QueryIntent@1.0(key: n03.key)
                         : Acquired | Completed(r) | InProgress |
                           TerminalFailed(f) | StillUnknown
                         reconciles n04
  match n13.out {
    Acquired          -> region issue
    Completed(r)      -> n12
    InProgress        -> n09
    TerminalFailed(f) -> n11
    StillUnknown      -> n14
  }
  n14 wait durable [timeout: 24h] -> n13
```

The experimental editor supports only:

```text
add-node
delete-node
rewire <node>.<input> <- <source>
wrap-region
bind-atom <node> <atom>@<version>
annotate <node> <relationship> <target>
```

Deletion of a referenced node is rejected. Successful deletion writes a tombstone row to SQLite, and IDs are never reused. Loops bind stable element identity explicitly:

```text
loop map line in n01.out.lines as line#id { ... }
```

Validator diagnostics identify the rule, graph location, and evidence:

```text
RULE2 @ n06.key: derivation set {n03, n14} is not singleton
```

Before any benchmark task runs:

* define logical effect identity;
* freeze the four validator rules and worked examples;
* freeze the corrected payment molecule;
* pass a hand-authored positive and negative conformance suite for every rule;
* implement only the minimal validator and disposable Go execution projection required by the experiment;
* keep the production graph schema, atom runtime, macro system, and Go backend behind the graph decision.

Seed the benchmark with unannounced ambiguous outcomes, in-progress prior state, missing durable claims, unstable cross-instance keys, loop-key errors, retry-key errors, and merge reissuance. Approximately eight tasks should contain ambiguous outcomes.

## Experimental Controls

For each task:

* randomize arm order;
* run every task-arm cell at least three times;
* increase to five repetitions exactly when the first three binary primary outcomes disagree;
* analyze paired per-task differences rather than arm means alone;
* use an independent adjudicator named before execution;
* ensure the notation designers do not hand-model the fifty workflows;
* log modeling time and representability corrections per workflow;
* record graph-translation and notation-learning time as graph-arm cost.

Measure:

* hidden acceptance-test pass rate;
* human review minutes;
* wall-clock time to accepted change;
* number of specification clarifications;
* escaped defects;
* total model, tool, review, preparation, and onboarding cost;
* change locality;
* merge and review complexity.

Report cold-start cost separately from estimated steady-state cost. Hand-building or translating a graph is part of the graph arm's economic cost.

The central medium hypothesis is mechanical repeatability:

> How often does manual application forget or misapply a structural rule compared with a validator that cannot forget?

Validator defects are reported separately and cannot be interpreted as evidence that the graph medium won or lost.

## Disposable Lowering Conformance

Before task one, the disposable projection must pass:

* golden generated-Go cases for each notation construct;
* differential graph-versus-Go execution against a mocked gateway;
* every confirmed-success, confirmed-failure, ambiguous, reconciliation, claim, loop, retry, and merge path used by the benchmark.

A blind adjudicator applies this triage order without knowing the arm:

1. If the graph does not validate, classify a modeling defect that counts against Arm C.
2. If the graph does not evaluate to the frozen expected trace, classify a notation or graph-semantics defect.
3. If generated Go diverges from the graph trace, classify a lowering defect, exclude it from the arm comparison, and log it separately.
4. Otherwise classify a genuine acceptance failure that counts in the experiment.

Lowering exclusions are pre-registered. More than ten percent excluded or dropped tasks in any arm voids the comparison.

## Statistical Protocol

The primary endpoint is binary per task and arm:

> Does the modal accepted output contain a structural-effect violation under blind grading?

Each task-arm cell runs three times initially. When the three outcomes disagree, run two additional times. Collapse repetitions to one modal outcome before analysis. Run-level spread is reported as reliability evidence and never increases the effective task sample above fifty.

The primary comparison is Arm C versus Arm B using McNemar's exact test on paired discordant tasks. Report the paired effect size and a paired-bootstrap confidence interval. Arm B versus Arm A is a pre-specified secondary comparison measuring the value of the structural rules themselves.

Rank secondary economic endpoints in advance:

1. median review minutes;
2. graph-modeling minutes;
3. total economic cost.

Report secondary endpoints with confidence intervals as exploratory results.

Invalid runs are re-run once and then dropped with the reason recorded. Task difficulty is controlled through pairing rather than post-hoc stratification.

A tie favors the established Go medium. An inconclusive Arm C versus Arm B result does not authorize Phase 1; it triggers a payments-only narrow experiment. More than ten percent dropped tasks in an arm voids the comparison.

## Intent and Acceptance Authority

Name three distinct roles before choosing the corpus:

* **Intent author:** derives the fifty specifications and hidden tests from real changes.
* **Editors:** perform arm work and are neither the intent author nor notation designer.
* **Adjudicator:** grades blindly, resolves ambiguity, and alone may declare a hidden test invalid.

Specifications and hidden tests are frozen before notation implementation and task modeling. A discovered ambiguity receives one written clarification issued identically to all arms and logged. A wrong hidden test voids that task for every arm rather than being corrected during a run.

Seeded cases are known only to the intent author and adjudicator.

## Graph Kill Criterion

The graph proceeds only when all of the following hold:

1. Arm C has fewer primary structural violations than Arm B, McNemar's exact test is significant at the pre-registered 0.05 level, and the paired confidence interval excludes no improvement.
2. Arm C is not more than five percentage points worse than Arm B on hidden acceptance.
3. Total economic cost, including human preparation, is no more than twenty-five percent higher.
4. No severe defect class materially regresses.

If the graph fails, stop graph-first editing and retain useful contract, capability, refinement, and effect analysis over ordinary Go.

If the result is inconclusive rather than a pass or clear failure, do not proceed to Phase 1. Run the pre-registered payments-only narrow experiment.

---

# 4. Incident Archaeology

**Linear position:** This work belongs only to the optional Layer 19 graph-verification branch. It does not block Layers 1–18 of the code-first product.

Before implementation, freeze and classify one hundred real defects or postmortems from Go services. The sampling frame must be recorded before classification and must include both target-domain incidents and general consequential Go-service incidents.

Use the following categories:

* specification defect;
* intent translation defect;
* state-model defect;
* contract defect;
* effect ordering defect;
* retry or idempotency defect;
* capability violation;
* concurrency defect;
* implementation defect;
* environmental defect;
* operational defect;
* external dependency defect;
* defect already detectable by the Go compiler or current static analysis.

For every incident, determine whether it was already preventable by:

* the Go compiler;
* `go vet`;
* `staticcheck`;
* existing linters;
* conventional tests;
* database constraints;
* vendor idempotency facilities;
* durable-execution systems;
* an established architectural control the team failed to apply.

An incident enters the Codeflux-addressable set only when Codeflux could establish a valuable structural property not adequately covered by those mechanisms. Runtime failures caused solely by clients, workers, networks, timing, or external systems do not qualify.

For every incident, identify whether the relevant property could have been:

* fully evaluated;
* kernel-derived;
* model-verified;
* contract-checked;
* runtime-observed only.

Request-side and response-side properties must be classified separately.

## Classification Integrity

Before accessing the corpus:

* write two passing and two failing examples for every check class;
* use two independent classifiers blinded to each other's labels;
* use a third adjudicator for disagreements;
* report raw agreement and Cohen's kappa;
* require a kappa of at least 0.70.

If agreement is below 0.70, revise the rubric and reclassify the entire corpus rather than selectively revisiting disagreements.

Record financial loss, affected users, duration, security impact, and operational blast radius separately from addressability. Report both the unweighted and severity-weighted results, but do not move the decision thresholds after seeing severity.

## Addressable-Surface Decision

* Twenty or more qualifying incidents: proceed with the request-side product.
* Ten through nineteen: do not begin compiler construction; run a narrower domain study and classify another one hundred incidents.
* Fewer than ten: stop the graph platform.

Exactly eighteen incidents follows the ten-through-nineteen path and is not rounded into a pass.

---

# 5. Program Architecture

The platform's primary loop is code-first:

```text
Requirement and constraints
    ↓
Workspace intelligence and task fingerprint
    ↓
Compatible project evidence and prior outcomes
    ↓
Effort forecast and execution-policy selection
    ↓
Plan, edit, test, inspect, and review
    ↓
Correctness and assurance gates
    ↓
Accepted repository change
    ↓
Factual episode, reusable artifacts, and routing outcome
```

## Human Intent

Contains:

* requirements;
* examples;
* business rules;
* constraints;
* preferences;
* unresolved ambiguity.

Human intent is neither complete nor automatically correct.

## Workspace Intelligence

Maintains a versioned view of:

* repository structure and ownership;
* symbols, dependencies, build targets, and test topology;
* language and framework conventions;
* changed-file and dependency cones;
* available tools and execution environments;
* historical failures, accepted changes, and review decisions.

This layer supports ordinary source work regardless of whether a graph exists.

## Task Fingerprint and Retrieval

Transforms the requirement and workspace state into structured features for:

* scope and risk classification;
* similar-task retrieval;
* effort and success forecasting;
* model and validation routing;
* exact artifact compatibility;
* uncertainty detection.

Retrieval supplies evidence and candidates, not authority.

## Adaptive Execution Policy

Selects:

* model and model version;
* reasoning-effort level;
* single-agent or multi-agent topology;
* initial context budget;
* tool-call and wall-clock budget;
* validation profile;
* escalation and stopping thresholds.

The policy may change during execution when observed progress diverges from forecast. Required correctness gates cannot be removed to meet a cost or latency target.

## Execution and Review

Agents operate on ordinary source by default, producing inspectable diffs, test results, review evidence, and an explicit acceptance decision. The system records factual execution events so later routing decisions can be calibrated against observed outcomes.

## Optional Go Structural Verification Subsystem

For protected Go workflows, the platform may route work through the functional graph and obligation system below. This subsystem is activated only if the graph-medium experiment passes.

## Functional Graph

Represents:

* typed operation nodes;
* provenance-carrying data edges;
* labeled control edges;
* explicit merges and regions;
* effect requests and sum-typed responses;
* non-local retry, reconciliation, compensation, claim, and ordering relationships;
* bounded loops with stable iteration provenance;
* contracts;
* capability boundaries.

The graph is executable only to the extent supported by its semantic dependencies.

The graph is region- and obligation-centric rather than node-centric. Atoms provide local behavior, while molecules and regions close path-scoped obligations. Type compatibility alone is not sufficient evidence of safe composition.

## Core Graph Entities

Every node, edge, region, effect relationship, obligation, and template instance receives stable identity.

Node kinds initially include:

* input;
* pure atom call;
* match;
* effect request;
* reconciliation read;
* conditional durable claim;
* explicit merge;
* durable wait or escalation boundary;
* terminal result.

Data edges record source, destination, value type, provenance identity, stability classification, and iteration scope. Control edges are labeled with tagged-union variants.

Regions group nodes under a shared purpose and obligation set, such as validation, retry, effect execution, reconciliation, compensation, or bounded iteration.

## Logical Effect Identity

A logical external effect is identified by:

```text
LogicalEffectIdentity =
    operation_contract_id
  + provider_deduplication_scope
  + business_intent_key
```

`operation_contract_id` identifies the pinned external operation rather than a wrapper function. The provider scope captures endpoint, tenant, account, or other declared idempotency scope. Workflow-instance identity is deliberately excluded so independent requests and workers can derive the same identity for the same business intent.

Inside an effectful loop, the business-intent key must include stable per-element identity.

## Provenance Semantics

Every value carries a provenance set. Deterministic transformations retain their own identity plus the provenance of their inputs.

At a merge or phi:

```text
provenance(phi) = union(incoming provenance sets)
```

A shared key derivation is established only when the relevant derivation set is a singleton. Two equivalent expressions or atoms with identical signatures do not count as the same derivation node instance.

## Proof Obligations

Represent individual claims that the platform attempts to establish.

Examples:

```text
No two sequential issuance nodes share one logical effect identity.

Every repository conflict reaches the conflict response branch.

The handler cannot issue administrative database effects.

Claim, query, issuance, retry, and reconciliation share one stable key derivation.

Ambiguous outcomes cannot reach compensation without confirmed reconciliation.

A confirmed atomic claim gates issuance when prior state provides deduplication.

The success response contains a normalized email address.
```

Guarantees attach to these obligations, not to entire atoms, molecules, or applications.

## Generated Go

The graph lowers to versioned Go source.

Generated Go is a projection of the graph, not an independent source of truth.

## Runtime Evidence

Contains:

* test results;
* traces;
* property checks;
* integration observations;
* production failures;
* external responses;
* human-reported specification defects.

---

# 6. Semantic Tier Zero: The Kernel

**Status: optional Deep Go Verification backlog.** Sections 6 through 20 do not block the code-first agent, deterministic project memory, or initial routing experiments. Production implementation begins only after the gates in Phase 6 pass.

The evaluator requires trusted primitive semantics.

These primitives form the semantic kernel and are the actual trusted computing base of the platform.

A kernel atom is:

* implemented directly in the graph evaluator;
* implemented independently in the Go backend;
* covered by cross-implementation conformance tests;
* reviewed as part of the language definition;
* versioned with the graph language.

Examples may include:

```text
Boolean operations
Integer arithmetic
Integer comparison
String equality
String length
Byte operations
Tuple and record construction
Option and Result construction
Tagged-union matching
List construction
List traversal
Stable ordering
Explicit numeric conversion
```

The initial kernel must be enumerated before the type system is designed.

## Kernel Scoping Rule

The kernel should remain deliberately small.

Each kernel addition creates:

* one evaluator implementation;
* one Go lowering implementation;
* one semantic specification;
* one cross-platform conformance obligation;
* one permanent compatibility commitment.

A kernel of approximately thirty carefully selected operations may be manageable.

A kernel of hundreds of convenience operations becomes a general-purpose runtime and materially changes the project scope.

Kernel additions require explicit review and cannot be introduced casually by agents.

---

# 7. Semantic Atom Categories

The platform supports four implementation categories.

## Tier 0: Kernel Atom

Semantics are defined directly by the language implementation.

Examples:

* integer comparison;
* tagged-union matching;
* stable list traversal;
* explicit numeric conversion.

Kernel atoms are trusted but continuously tested for evaluator-versus-Go agreement.

## Tier 1: Graph-Native Atom

Implemented entirely using:

* graph nodes;
* kernel atoms;
* other graph-native atoms.

Its behavior is evaluable whenever its inputs are known.

## Tier 2: Modeled Go Atom

Contains:

* a native Go implementation;
* an independently authored graph reference model;
* tests comparing both implementations;
* dependency-version bindings;
* provenance identifying each authoring agent.

Modeled atoms must not be described as formally proven merely because the two implementations agree.

Agreement provides differential evidence, not certainty.

## Tier 3: External Atom

Contains:

* a typed signature;
* effect classification;
* contract;
* capability requirements;
* possible result variants;
* runtime implementation.

The evaluator does not claim to predict the external response.

It can still analyze the request, effect ordering, capability use, retry rules, and response handling.

## Pinned Atom Contract

Every atom reference pins:

```text
atom_id
atom_version
evidence_version
dependency_bindings
```

References never float to a new version. An atom upgrade creates a graph revision and re-derives affected obligations.

An atom contract contains:

* input and output types;
* purity or effect classification;
* operation contract identity;
* capability requirements;
* determinism tier;
* possible response variants;
* stability and provenance requirements;
* dependency and platform bindings;
* local proof obligations;
* assurance evidence.

For key derivation, the contract also states what constitutes the same business intent, which durable fields contribute to identity, how missing client identifiers are handled, how loop elements are distinguished, and which external key scope and expiration assumptions apply.

Atom boundaries should be drawn where meaningful local obligations close. Finer granularity is not automatically better: it creates more edges, larger proof cones, higher contract-comprehension cost, and more invalidation points.

## Atom Naming and Retrieval Identity

Atom names are descriptive semantic identifiers, not short implementation labels. They must retain enough context to be understood when shown alone in a graph node, retrieval result, trace, review report, or generated Go call.

The preferred naming grammar is:

```text
<Verb><DomainObject><ImportantQualifier><ObservableOutcome>
```

Names may be somewhat long when the additional words distinguish real domain behavior. `ReconcileAmbiguousGatewayChargeOutcome` is preferable to `HandlePayment`; `PersistSessionEventWithMonotonicSequence` is preferable to `SaveEvent`.

Names must:

* begin with a concrete action for executable atoms;
* identify the domain object rather than only the Go representation;
* include business-intent, lifecycle, identity, effect-boundary, or outcome qualifiers when they distinguish nearby atoms;
* use full domain language except for well-established abbreviations;
* remain meaningful as a standalone graph label and retrieval candidate;
* avoid claiming guarantees unsupported by the contract and evidence.

Names must not include filler such as `Helper`, `Utility`, `Manager`, `Processor`, `Handler`, `Thing`, or `Impl`. Version numbers, evidence levels, hashes, source locations, and temporary implementation mechanisms belong in bindings and metadata rather than the name. Provider names appear only when behavior is genuinely provider-specific.

Each atom stores:

```text
canonical_name
display_name
normalized_name_phrase
name_schema_version
name_rationale
search_aliases
prior_names
```

The canonical Go identifier and human-readable display name derive from the same semantic phrase. The normalized phrase splits identifier words without keyword repetition and is included once in embedding input.

A semantic-preserving rename keeps the stable atom ID, creates a new documentation revision, records the prior name as an alias, and invalidates or regenerates derived embeddings. A semantic change requires a new compatible atom version or new atom identity according to the atom-version rules; it cannot be disguised as a rename.

Two active atoms within the same project and semantic scope cannot share the same normalized canonical name. Aliases assist discovery but never bypass exact identity, compatibility, applicability, evidence, or assurance gates.

## Atom Documentation as Retrieval Material

Every atom realized in Go must carry a detailed, structured Go doc comment. The documentation exists to make the atom reviewable by humans and richly discoverable by later retrieval. It is descriptive evidence and embedding source material, not a correctness-bearing contract.

The required atom-documentation schema is versioned and includes:

```text
purpose
use_when
do_not_use_when
semantics
inputs
outputs
preconditions
postconditions
effects
failure_semantics
determinism
idempotency_and_retry
reconciliation_and_compensation
security_and_privacy
dependencies_and_bindings
complexity_and_limits
examples
verification
retrieval_concepts
```

The opening sentence follows Go documentation conventions and begins with the atom's Go identifier. The structured body explains domain meaning rather than merely repeating the signature. Important near-matches and exclusions are required because retrieval precision matters as much as recall.

Each field must contain substantive text or `None` with a reason. Comments must not contain credentials, customer data, private URLs, or real sensitive examples. Retrieval concepts contain concise authentic domain aliases and requirement language; keyword repetition intended only to manipulate similarity is rejected.

The complete comment must describe:

* the domain outcome and when the atom should or should not be selected;
* semantic behavior, ordering, successful outcomes, preconditions, and postconditions;
* the meaning, units, valid ranges, identity roles, and sensitivity of inputs;
* result variants and what callers may rely on;
* effects, capabilities, cardinality, logical operation identity, and externally visible mutations;
* failure, partial-work, ambiguity, retry, reconciliation, and compensation behavior;
* determinism boundaries and relevant time, randomness, concurrency, architecture, dependency, and external-response influences;
* security, privacy, authorization, and redaction assumptions;
* semantic dependencies, version/configuration bindings, operational limits, representative examples, non-examples, and supporting verification.

For SQLite-native atoms, the structured database record is authoritative and generated Go receives the corresponding comment as a projection. For source-authored modeled Go atoms, Codeflux parses the structured comment with the Go syntax tree, validates it, and admits an immutable documentation revision into SQLite before it can influence retrieval.

The stored documentation revision binds:

```text
atom_id
atom_version
documentation_schema_version
documentation_revision
source_repository_revision
source_comment_hash
contract_hash
dependency_bindings
validation_status
```

The normalized embedding input favors semantic fields such as purpose, selection and exclusion conditions, semantics, input/output meaning, effects, failures, and retrieval concepts. It excludes incidental whitespace, source locations, timestamps, evidence run IDs, and repeated boilerplate.

Every derived vector binds to the exact documentation revision, atom version, contract hash, repository revision, embedding model, dimensions, normalization rule, and embedding-input schema. A comment, contract, binding, or embedding-schema change invalidates or regenerates the vector before it can influence retrieval.

Rich comments improve candidate discovery only. Before an atom can be reused, Codeflux still requires exact project boundaries, version compatibility, executable applicability predicates, obligation preservation, current evidence, and sufficient assurance. A comment never promotes itself into authority.

---

# 8. Correlation Controls for Modeled Go Atoms

A native wrapper and its graph model can reproduce the same misunderstanding.

To reduce correlated error:

* the Go implementation and reference model should be authored by different agents;
* the second agent should derive behavior from an upstream specification or authoritative documentation;
* the reference model should not be generated from the wrapper source;
* co-authored or same-context pairs must be marked as correlated;
* known edge cases should be sourced independently;
* dependency updates must invalidate or re-open relevant verification obligations.

Every modeled atom must bind its claims to:

* Go toolchain version;
* operating system and architecture constraints;
* dependency module;
* dependency version or version range;
* relevant configuration.

A dependency upgrade changes the semantic evidence and triggers re-verification.

---

# 9. Proof Obligations as the Unit of Assurance

Guarantees must be attached to individual claims.

A workflow touching a database should not receive a meaningless global label such as `Runtime-only`.

Instead, it may produce a report like:

```text
Obligation: Payment request is issued at most once.
Status: Fully evaluated
Dependencies: Control-flow graph, retry graph, idempotency-key derivation

Obligation: Idempotency key is stable.
Status: Fully evaluated
Dependencies: Kernel string and byte operations

Obligation: Gateway returns the documented conflict variant.
Status: Runtime observed
Dependencies: External payment gateway

Obligation: Conflict result reaches compensation branch.
Status: Contract checked
Dependencies: Gateway result contract, graph exhaustiveness

Obligation: Unicode email normalization matches upstream behavior.
Status: Model verified
Dependencies: Reference model, Go dependency v1.8.x
Correlation risk: Low
```

Each obligation records:

* statement;
* scope;
* verification method;
* dependency cone;
* evidence;
* assumptions;
* model and toolchain versions;
* invalidation triggers;
* confidence limitations.

---

# 10. Guarantee Provenance

The assurance level of an obligation is computed from the dependencies required to establish that specific claim.

It is not computed as the minimum assurance of every node in the containing graph.

For example, a database-backed workflow may still fully establish that:

* exactly one insert request can be emitted;
* validation occurs before insertion;
* administrative capabilities are unreachable;
* a stable key accompanies every retry.

The database response itself remains external.

This provenance computation should be deterministic.

A reviewer should be able to inspect:

* which nodes support the obligation;
* which contracts are assumed;
* which kernel semantics are used;
* which external behaviors remain unverified.

---

# 11. Request-Side and Response-Side Semantics

Every effect must be split into two semantic phases.

## Request Side

Potentially analyzable properties include:

* whether the effect is issued;
* how many times it can be issued;
* ordering relative to other effects;
* payload construction;
* capability authorization;
* idempotency-key construction;
* retry policy;
* compensation requirements;
* timeout declarations;
* branch conditions preceding the effect.

## Response Side

Properties include:

* actual external result;
* timing;
* availability;
* latency;
* third-party behavior;
* data returned by the external system;
* undocumented failure modes.

The response side may be modeled through contracts, mocks, or reference models, but it is not automatically predictable.

The platform should make this boundary visually and semantically explicit.

---

# 12. Effect Discipline

Effects are represented as typed requests.

```text
Pure decision graph
    ↓
Effect request
    ↓
Interpreter
    ↓
Effect response
    ↓
Continuation graph
```

Each effect declaration includes:

* pinned operation contract;
* logical effect identity expression;
* effect kind and read/write classification;
* capability;
* request type;
* response variants;
* ambiguous-outcome policy;
* retry policy;
* idempotency policy;
* provider deduplication scope;
* key provenance requirements;
* ordering constraints;
* timeout;
* compensation;
* reconciliation;
* conditional durable-claim requirements;
* observability requirements;
* security classification.

Every write effect declares exactly one deduplication strategy. An undeclared strategy is a validator error.

| Strategy | Required obligations |
| --- | --- |
| `ProviderIdempotency` | Stable key provenance, a version-bound provider key contract, and a key cone contained by durable request identity |
| `LocalAtomicClaim` | Confirmed conditional claim before issuance, the same singleton key provenance, and a uniqueness or compare-and-set contract |
| `DurableRuntime` | A durable execution region plus replay-determinism obligations on generated Go |
| `NaturallyIdempotent` | A versioned idempotence contract plus a regression case demonstrating repeat safety |
| `NoDeduplication` | Explicit human waiver; prohibited on protected paths |

This declaration makes the applicability of the atomic-claim rule deterministic rather than a reviewer judgment.

Potentially ambiguous external writes return:

```text
EffectOutcome<Success, Failure> =
    ConfirmedSuccess(Success)
  | ConfirmedFailure(Failure)
  | AmbiguousOutcome(Ambiguity)
```

Ambiguity is not failure. It cannot be routed directly to compensation or treated as permission to create a new logical issuance.

Potential proof obligations include:

```text
NoSequentialDuplicateIssuance(logicalEffectIdentity)
AtLeastOnce(effect)
OrderedBefore(effectA, effectB)
CompensatedBy(effectA, effectC)
ReconciledBy(effectA, effectR)
CapabilityContained(handler, capabilitySet)
StableIdempotencyKey(effect)
SharedKeyProvenance(claim, query, issue, retry, reconcile)
AtomicClaimBeforeIssuance(effect)
ReconciliationGatesCompensation(effect, compensator)
AllResponsesHandled(effect)
NoEffectOnInvalidInput(effect)
```

Effect discipline is the primary initial product surface.

## Initial Structural Rules

### Rule 1: No Sequential Duplicate Issuance

For every pair of distinct issuance nodes with the same logical effect identity, neither node may be reachable from the other.

This catches sequential duplicates and reissuance after merges. The initial graph excludes parallel concurrency; adding concurrency later requires may-coexecute or partial-order analysis rather than simple reachability.

A declared retry repeats one issuance node inside a retry region. A normal bounded loop may repeat an issuance node only when its key depends on stable identity from every relevant enclosing iteration.

### Rule 2: Stable Shared-Key Provenance

Keys reaching the conditional claim, prior-state query, initial issuance, retries, and reconciliation must trace to the same derivation node instance.

The key-provenance cone must:

* contain only deterministic operations;
* contain no Tier 3 atom;
* depend only on durable request identity, constants, and stable iteration identity;
* exclude post-attempt values, retry counters, transient workers, uncaptured time, and uncaptured randomness.

A random value is allowed only when captured once, persisted before the first attempt, and subsequently treated as durable input.

### Rule 3: Reconciliation Gates Compensation

For each effect-compensator pair:

1. begin at the effect's ambiguous-outcome edge;
2. remove nodes that establish a reconciliation-confirmed result for that effect;
3. verify that the compensator is unreachable.

This is a per-pair reachability-after-deletion check, not a general entry-dominator check. Compensation for an unrelated effect remains valid.

### Rule 4: Atomic Claim Gates Issuance

For an effect declaring `LocalAtomicClaim`, every path to issuance must pass through a confirmed successful conditional durable claim on the same singleton key provenance.

The claim distinguishes at least:

```text
Acquired
Completed
InProgress
TerminalFailed
```

Only `Acquired` may reach a new issuance. `Completed` returns the existing result, `InProgress` reconciles or waits, and `TerminalFailed` terminates or begins a separately identified business operation.

The claim is itself an external write and may have an ambiguous outcome. An ambiguous claim must be reconciled before issuance.

## Disposable Graph Validator Conformance Cases

The frozen suite includes positive and negative cases for:

* duplicate issuance after a merge, mutually exclusive branches, declared retry, and effectful bounded loops;
* one shared derivation versus two equivalent derivations;
* fresh keys per retry, one key for all loop elements, and stable per-element keys;
* singleton and multi-source phi provenance;
* clock, randomness, or Tier 3 values entering a key cone;
* direct compensation from ambiguity versus compensation after confirmed reconciliation;
* unrelated compensation paths and bounded reconciliation reads;
* query-then-issue without a claim;
* a claim using a different key;
* `InProgress` or an ambiguous claim reaching issuance;
* confirmed acquisition reaching issuance.

Expected results are frozen independently of the validator implementation.

---

# 13. Type-System Scope

The first version must avoid silently becoming Idris or Koka implemented as a side project.

The initial type system should support only what the first obligation set requires.

Possible initial features:

* primitive scalar types;
* named records;
* tagged unions;
* `Option`;
* `Result`;
* bounded collections;
* first-order function references;
* explicit effect request and response types;
* capability sets.

The first version should avoid or restrict:

* unrestricted higher-order functions;
* arbitrary closures;
* general recursion;
* dependent types;
* unrestricted refinement types;
* open effect rows;
* implicit polymorphism.

`Map` and `Fold` should initially accept references to statically declared graph functions rather than arbitrary runtime closures.

Templates are structural macros rather than higher-order graph values. A template declares named holes with required signatures, capabilities, and obligation contracts. It expands at edit time into ordinary nodes, and the expanded graph is validated and lowered.

Template and atom versions are pinned. An upgrade is an explicit graph revision that re-derives provenance and obligations.

Termination should be established through:

* bounded iteration;
* structurally decreasing recursion;
* explicit maximum-step declarations.

General termination proving is outside the initial scope.

Inside an effectful bounded loop, the key must depend on stable per-element identity. Across retries of the same element, it must remain unchanged.

Reconciliation may repeat bounded reads or suspend through a durable wait. It may not reissue the original write except through its declared idempotent retry region.

---

# 14. Contracts and Invariants

Contracts should be first-class entities.

Initial contracts should use a deliberately restricted predicate language built from kernel operations.

Contracts may express:

* scalar bounds;
* equality;
* tagged-union membership;
* collection-size bounds;
* state-transition legality;
* capability containment;
* effect cardinality;
* effect ordering;
* stable derivation rules.

The platform should distinguish:

* executable contract;
* solver-supported contract;
* test-backed contract;
* assumed external contract;
* human-reviewed requirement.

An unrestricted natural-language contract must never be reported as mechanically verified.

---

# 15. Go Lowering Strategy

Go lowering should preserve semantics while producing code that remains practical to inspect and debug.

Likely mappings include:

* tagged unions → tagged structs or sealed-style interfaces;
* records → Go structs;
* `Result` → generated result types or explicit value/error variants;
* effects → typed interfaces;
* capabilities → narrow injected interfaces;
* immutable graph values → ordinary Go values with controlled mutation;
* graph functions → deterministic Go functions.

The generated code may not always look maximally handwritten.

Priority order:

1. semantic fidelity;
2. traceability;
3. debuggability;
4. stable generation;
5. idiomatic presentation.

The project should not promise perfectly idiomatic Go when that conflicts with semantic preservation.

---

# 16. Go Determinism Profile

The platform must define an executable deterministic Go subset.

Potentially unstable behavior includes:

* map iteration order;
* `select` with multiple ready channels;
* goroutine scheduling;
* floating-point reassociation;
* fused multiply-add differences;
* architecture-dependent conversions;
* dependence on `runtime.NumCPU`;
* matching error strings;
* unstable external timing;
* unordered traversal hidden by formatting;
* dependency behavior changing across versions.

Rules may include:

* maps cannot be iterated without explicit stable ordering;
* monetary values use integer minor units or a specified decimal implementation;
* floating-point obligations require explicit precision and rounding semantics;
* effect ordering cannot depend on goroutine scheduling;
* randomized `select` behavior is prohibited in verified regions;
* error contracts match typed identities or variants, never messages;
* environment-derived values are explicit inputs;
* runtime parallelism cannot alter logical results.

---

# 17. Determinism Conformance Suite

The determinism profile must be executable, not merely documented.

Run the conformance suite against every supported combination of:

* Go version;
* architecture;
* operating system;
* compiler configuration;
* relevant dependency versions.

At minimum, target:

* `amd64`;
* `arm64`.

The suite should test:

* numeric behavior;
* rounding;
* FMA-sensitive expressions;
* collection ordering;
* concurrency restrictions;
* error identity;
* generated control flow;
* evaluator-versus-Go traces.

A toolchain or architecture combination cannot be declared supported until the suite passes.

The same determinism profile should govern durable-runtime targets. Wall-clock reads, unstable map iteration, uncontrolled goroutines, randomized `select`, uncaptured randomness, and environment-derived values are prohibited or captured through explicit runtime APIs.

This allows evaluator-versus-Go conformance and durable-workflow replay safety to share one executable suite rather than creating separate determinism systems.

---

# 18. Stable Graph Identity

Every graph node, data edge, control edge, region, effect relationship, obligation, and template instance receives a permanent identity when created.

Identity must not be derived from:

* source position;
* display name;
* content hash;
* parent order;
* generated Go location.

Stable identity allows the system to distinguish:

* rename;
* move;
* modification;
* replacement;
* deletion;
* duplication.

Without stable identity, semantic graph diffs degrade into delete-and-create noise.

Stable identities must propagate into:

* generated Go source maps;
* execution traces;
* proof-obligation provenance;
* provenance sets and phi merges;
* effect and compensation relationships;
* template expansion origins;
* review tooling;
* blame history.

---

# 19. Review and Source Mapping

Generated Go is a review projection, not the only review surface.

Each generated construct should include machine-readable mapping to:

* graph revision;
* node identity;
* contract identity;
* proof obligations;
* generator version.

The system should support:

* graph semantic diff;
* generated Go diff;
* obligation diff;
* capability diff;
* effect-trace diff;
* data-provenance diff;
* region and merge diff;
* effect-relationship diff;
* pinned atom and template-version diff.

A review should answer:

* what intent changed;
* which nodes changed;
* which generated Go changed;
* which obligations were added, removed, weakened, or invalidated;
* which effects changed;
* which runtime assumptions changed.

Blame must resolve through graph-to-Go source mappings rather than assigning all generated lines to the latest generator run.

---

# 20. Generator Versioning

The code generator is versioned per project.

A generator upgrade is an explicit migration.

It must not silently rewrite the project.

Generator migration includes:

* before-and-after generated output;
* semantic comparison;
* obligation comparison;
* runtime regression tests;
* source-map migration;
* human-readable migration summary.

Textual golden files should not be the primary correctness mechanism.

Generator tests should prefer:

* parsed Go AST comparison;
* evaluator-versus-Go behavior;
* proof-obligation preservation;
* generated test execution;
* normalized structural comparison.

Text snapshots may still be used for narrow formatting expectations.

---

# 21. Agent Architecture

The agent architecture is policy-driven. It should use the least expensive execution policy predicted to satisfy the task's correctness requirements, then escalate when evidence contradicts that forecast.

## Effort Forecaster

Predicts:

* task scope and novelty;
* probability of acceptance by candidate model and effort level;
* P50 and P90 time, tokens, tool calls, and cost;
* likely files, build targets, tests, and specialists;
* expected value of retrieved project artifacts;
* uncertainty and out-of-distribution risk.

Forecasts bind to repository revision, task fingerprint, model version, tool configuration, and validation profile.

## Model and Effort Router

Chooses an initial policy from an allowed pool:

```text
model
reasoning effort
context budget
tool budget
wall-clock budget
immutable required validation profile
escalation thresholds
```

The first router varies only model and reasoning effort. Validation floors come from repository and user policy. Adaptive topology, context budget, and tool budget remain shadow recommendations until separate evidence authorizes them.

The router begins conservatively when evidence is sparse. Cost optimization never overrides a protected correctness gate.

## Coordinator

Builds the task plan, scopes work, controls shared context, assigns specialists, tracks budgets, and decides whether to continue, re-plan, escalate, or request human authority.

Multi-agent execution remains disabled in the initial adaptive router. It may be enabled later only when controlled trials show incremental value after coordination cost.

## Coding Agent

Works code-first and may:

* inspect repository and runtime state;
* edit source and tests;
* reuse compatible project artifacts;
* invoke builds, tests, static analysis, and external tools;
* propose graph changes and obligations for protected Go workflows;
* produce an evidence-backed change summary.

## Specialist Agents

Optional roles include:

* repository mapper;
* implementation worker;
* test and reproduction worker;
* security or domain specialist;
* Go structural-verification specialist;
* review and regression specialist.

## Adversarial Agent

Challenges:

* requirement interpretation;
* unsupported guarantees;
* missing response variants;
* effect duplication;
* capability leakage;
* retry safety;
* compensation completeness;
* graph complexity;
* correlation between models and implementations;
* dependency-version assumptions.

The critic should not apply a universal ever-growing checklist. It is invoked when task risk, uncertainty, or expected defect cost justifies its expense.

Checks are selected from:

* task risk;
* changed obligation categories;
* effect types;
* dependency changes;
* security classification.

## Progress Monitor and Dynamic Escalation

Observed signals include:

* elapsed cost and time against forecast;
* repeated edit-test cycles;
* unchanged or expanding failure sets;
* unexpected dependency-cone growth;
* tool or environment failures;
* conflicting retrieved evidence;
* low-confidence requirement interpretation;
* validation regressions;
* critical-path or security-sensitive changes.

Available responses include:

1. continue under the current policy;
2. increase reasoning effort;
3. expand context or retrieve additional evidence;
4. switch to a more capable model;
5. add a targeted specialist or independent reviewer;
6. strengthen validation;
7. re-plan from the original revision;
8. stop and request human input.

Every escalation records its trigger, incremental cost, and outcome. This makes routing policy measurable rather than an opaque orchestration preference.

## Routing Safety

The forecaster and router do not grade their own success. Acceptance tests, validators, human review, and runtime evidence supply outcomes.

Protected tasks define minimum model, review, and validation policies. The router may exceed those minima but may not undercut them to save time or cost.

---

# 22. Correctness and Assurance Gates

Every task receives a validation profile derived from repository policy, task risk, changed dependency cone, and explicit user requirements.

Repository and user policy establish immutable floors from owned paths, file types, capabilities, protected resources, and declared task class before the cost router runs. The router may strengthen these floors but cannot weaken them.

Unknown, conflicting, or out-of-distribution fingerprints receive a conservative profile. Measure false-negative risk classification separately from effort-forecast calibration.

Profiles may include:

```text
Routine:
    targeted tests
    formatting and static checks
    diff review

Elevated:
    broader tests
    regression analysis
    independent review
    stronger model or effort minimum

Protected:
    mandatory acceptance suite
    security or domain review
    required proof obligations
    no unresolved external assumptions
    deployment-blocking assurance policy
```

The router optimizes only inside the feasible policies for the selected profile. It may not trade away a required test, reviewer, obligation, or security check for lower cost.

Correctness and assurance reports must influence CI.

For each critical path, projects define required obligations and minimum acceptable evidence.

Example:

```text
Payment submission:
    No sequential duplicate logical issuance: Fully evaluated
    Stable idempotency key: Fully evaluated
    Capability containment: Fully evaluated
    All gateway variants handled: Contract checked or better
    Amount normalization: Model verified or better
```

CI fails when:

* a required obligation disappears;
* its evidence level drops below policy;
* a dependency binding becomes invalid;
* an external-only dependency enters a prohibited proof cone;
* the runtime-only fraction rises on protected critical paths;
* an expired modeled atom remains in use.

This prevents deadline pressure from silently converting all atoms into external atoms.

---

# 23. Storage

SQLite is the sole authoritative persistence layer for Codeflux-managed state.

Do not create separate JSON, YAML, Markdown, graph, atom, embedding, trace, memory, or vector-index files. The repository retains its ordinary source files because agents must edit and compile real code, but Codeflux metadata and semantic artifacts live only in SQLite.

For a hobbyist installation, use one local database in the operating-system application-data directory. Repositories are identified inside the database by stable repository identity, canonical location, and Git history rather than by committing a `.codeflux` directory.

## Database Authority

Store in SQLite:

* tasks, plans, policies, approvals, budgets, events, checkpoints, and outcomes;
* repository facts, instructions, known commands, context selections, and cache metadata;
* atoms, atom versions, signatures, contracts, implementations, evidence, and dependency bindings;
* graphs, immutable graph revisions, nodes, ports, edges, regions, relationships, provenance, and obligations;
* model catalogs, effort forecasts, routing decisions, escalations, costs, and calibration outcomes;
* episodes, lineage, regression cases, rules, claim overlays, and assurance advisories;
* embedding vectors, embedding-model metadata, source-artifact identity, and vector-index state.

SQLite records are authoritative. Text notation, diagrams, generated Go, reports, and diffs are projections. They may be materialized temporarily into an isolated task worktree for compilation or review, but they are regenerated from database state and are not independent sources of truth.

## Atom Storage

Use stable atom identity plus immutable versions:

```text
atoms
atom_versions
atom_signatures
atom_contracts
atom_names
atom_name_aliases
atom_implementations
atom_dependencies
atom_capabilities
atom_evidence
atom_documentation_revisions
atom_documentation_fields
atom_embeddings
```

Graph-native atom bodies reference graph revisions. Modeled Go atom source and reference-model data are stored as versioned database content. External atoms store contracts and runtime adapter bindings.

No atom reference floats. A version upgrade creates new rows and re-derives affected obligations.

Atom naming records store the canonical identifier, display name, normalized semantic phrase, naming-schema version, rationale, aliases, and prior-name lineage. Atom documentation stores its schema version, immutable revision, source comment hash, contract hash, repository revision, dependency bindings, validation status, and normalized embedding-input hash. Generated Go comments project this record. Source-authored modeled Go comments are parsed and admitted into it. Atom vectors always reference the exact name and documentation revisions that produced them.

## Graph Storage

Use normalized immutable graph revisions:

```text
graphs
graph_revisions
graph_nodes
graph_ports
graph_data_edges
graph_control_edges
graph_regions
graph_region_members
graph_relationships
graph_provenance_sets
graph_phi_bindings
graph_obligations
graph_source_maps
```

Editing creates a new graph revision transaction. Stable identities survive revisions; deletions create tombstone rows rather than deleting identity history.

The line-oriented graph notation is an editor and review projection over these tables. Loading, editing, validating, and saving the notation reads and writes SQLite transactions rather than graph files.

## Vector Storage

Vectors remain inside the same SQLite database:

```text
embedding_models
embedding_spaces
artifact_embeddings
embedding_index_state
```

Every vector stores:

* source artifact and immutable version;
* embedding provider, model, and model version;
* dimensions, numeric encoding, and normalization;
* source-content hash;
* creation time and validity state;
* project and security scope.

Store vector values as a compact binary column or through an SQLite vector extension. Any approximate-nearest-neighbor index must also remain inside SQLite and be rebuildable from `artifact_embeddings`.

Vector similarity discovers candidates only. Structured compatibility, scope, evidence, and version checks decide whether a candidate may influence work.

## Core Operational Entities

Core entities include:

```text
projects
repositories
repository_revisions
workspace_facts
task_requests
task_fingerprints
task_risk_profiles
effort_forecasts
execution_policies
model_catalog
model_performance_bindings
routing_decisions
escalation_events
validation_profiles
acceptance_outcomes
delayed_outcomes
rollbacks
defect_attributions
artifact_maturity
graph_revisions
nodes
node_revisions
edges
regions
provenance_sets
phi_bindings
logical_effect_contracts
effect_relationships
template_instances
kernel_atoms
graph_atoms
modeled_go_atoms
external_atoms
contracts
proof_obligations
obligation_evidence
effects
capabilities
source_maps
generator_versions
dependency_bindings
executions
traces
failures
agent_runs
human_feedback
benchmark_tasks
benchmark_runs
episodes
episode_events
artifacts
artifact_lineage
artifact_evidence_versions
claim_status_overlays
assurance_advisories
advisory_affected_ranges
atom_reuse_decisions
mechanical_rules
rule_exposures
rule_overrides
regression_cases
advisory_candidates
retrieval_exposures
clean_room_assignments
dependency_watch_events
scheduled_conformance_runs
runtime_contradictions
```

Phase 1 implements only repositories, tasks, runs, events, policies, costs, validations, outcomes, artifacts, and versioned bindings. Routing, broad learning, and graph-specific entities are added only in their gated phases.

## Transactions, Migrations, and Recovery

Use SQLite transactions for:

* task-state transitions;
* graph-revision creation;
* atom-version admission;
* evidence invalidation;
* routing and cost recording;
* artifact maturity changes.

Enable foreign keys and integrity checks. Use WAL mode for local concurrency, while treating its temporary sidecars as SQLite implementation details rather than artifact storage.

Schema migrations are versioned in application code. Before migration, use the SQLite backup API to create a user-controlled recovery snapshot. A failed migration restores the previous database and application version.

Retention policies are implemented as database lifecycle operations rather than filesystem cleanup.

Not all agent conversation or intermediate trace data should be preserved forever.

Storage classes may include:

* permanent semantic history;
* permanent validated or matured evidence;
* bounded failure traces;
* temporary agent scratch data;
* compacted historical execution data.

Raw episodes and tool traces are project-local high-sensitivity data. Embeddings are created from sanitized semantic artifacts rather than raw traces. Cross-project promotion requires explicit review, sanitization, and provenance retention.

Credentials remain in the operating-system credential store and are referenced by opaque credential identity. They are never stored in SQLite.

Export is an explicit user action for backup, diagnostics, or interoperability. Exported files are not authoritative and must never be silently re-imported as trusted state.

---

# 24. Specification Review

Specification defects are expected to remain a dominant failure class.

The platform should support explicit specification review artifacts:

* examples;
* counterexamples;
* acceptance tables;
* state-transition tables;
* unresolved assumptions;
* domain-owner decisions;
* scenario coverage;
* conflict records.

Agents may propose requirements, but mechanically verified behavior must remain distinguishable from human-approved intent.

The system should report:

```text
Mechanically consistent: Yes
Matches approved examples: Yes
Matches complete business intent: Unknown
Open assumptions: 3
```

---

# 25. Metrics

Metrics follow the product objective.

## Correctness Gates

Track:

* hidden acceptance-test pass rate;
* required validation and proof-obligation pass rate;
* escaped-defect and rollback rate;
* security and protected-path regressions;
* human acceptance and substantive review findings.

Speed and cost comparisons are invalid when an arm falls below its required correctness policy.

## Speed

Track:

* wall-clock time to first useful change;
* wall-clock time to accepted change;
* edit-test-repair rounds;
* queue and tool latency;
* human review and clarification minutes.

## MVP Usability

Track:

* repository setup and first-task time;
* task-completion rate without human rescue;
* interruption, resume, and checkpoint recovery success;
* concurrent-edit conflict rate;
* patch rollback success;
* plan and diff approval burden;
* repeat-use rate.

## Cost

Track:

* model tokens and price;
* tool and infrastructure cost;
* agent coordination overhead;
* estimated human modeling, review, and intervention cost;
* total cost per accepted change.

Token counts are diagnostic inputs, not the final economic objective.

## Forecast and Routing Quality

Track:

* calibration of acceptance probability;
* P50 and P90 time and cost coverage;
* model-selection regret on the controlled counterfactual calibration subset;
* escalation precision, recall, timing, and incremental value;
* unnecessary high-capability model use;
* tasks where routing violated or approached a correctness minimum.

## Compounding Value

Track:

* marginal time and cost over chronological validated tasks;
* retrieval hit, use, rejection, and verified-contribution rates;
* project-memory maintenance and invalidation cost;
* cold-start versus warm-project performance;
* useful exact reuse and plan reuse;
* regressions caused by stale or misleading evidence.

## Deep Verification and Learning

Track:

* graph-translation and notation-learning time;
* graph versus text editing outcomes;
* obligation coverage and assurance regressions;
* effect-discipline defects prevented;
* mechanical-rule warning-ignore and override rates;
* regression-case discrimination;
* advisory exposure and clean-room outcomes;
* assurance-evidence detection and resolution time;
* historical claims invalidated or downgraded.

Immediate benchmark acceptance means:

> Hidden benchmark tests and required proof obligations pass.

It does not mean the critic agreed with the coder, and it is not permanent proof of correctness. Validated and matured outcome states incorporate later CI, integration, rollback, incident, and defect evidence.

---

# 26. Benchmark Timing

The benchmark and its decision thresholds must be frozen before implementation.

It should come from:

* real historical changes;
* real incident classes;
* externally authored tasks where possible;
* domains not selected specifically for graph suitability.

Maintain three benchmark categories:

## Adaptive Platform Benchmark

Measures fixed baseline, adaptive routing, and compounding execution over a frozen chronological stream of real repository tasks.

The benchmark must:

* isolate each arm's history;
* bind every result to model, model version, price, tools, and repository revision;
* score correctness before speed and cost;
* report cold-start and warm-project results separately;
* include routine, unfamiliar, cross-cutting, failing-test, refactor, dependency, and high-risk tasks;
* measure forecast calibration and routing regret;
* prevent benchmark answers and hidden tests from entering project memory.

## Medium Benchmark

Measures three paired arms:

* ordinary Go;
* Go with an explicit structural-effect protocol;
* functional-graph editing with the same protocol.

Each task-arm cell runs at least three times. Human preparation, onboarding, translation, review, model, and tool costs are included.

Arm B and Arm C receive the same four frozen structural rules. Arm B applies them as a checklist; Arm C receives mechanical validation. Arm A receives neither.

The fifty tasks include unannounced cases involving ambiguous outcomes, in-progress prior state, missing atomic claims, cross-instance key instability, loop and retry keys, and merge reissuance. Approximately eight tasks contain ambiguous outcomes.

## Verification Benchmark

Measures whether the platform catches defects beyond:

* compiler;
* `go vet`;
* `staticcheck`;
* conventional tests.

The benchmark should include deliberately difficult request-side effect cases.

Before the medium benchmark runs, a separate validator conformance suite must pass hand-authored positive and negative cases for all four rules, including loop, merge, phi-provenance, claim, and reconciliation cases.

The benchmark also supplies the frozen corpus for mechanical-rule replay and false-positive measurement once the minimum corpus requirements are met.

---

# 27. Initial Product Scope

## Hobbyist-First User

The initial user is a technically capable hobbyist or independent developer working on repositories they control from their own machine.

The first product is a local-first, single-user coding agent for existing Go repositories with:

* installation and first task in minutes;
* terminal and editor task entry;
* bring-your-own model API keys;
* no mandatory hosted account or repository upload;
* repository mapping and targeted context assembly;
* planning, editing, command execution, testing, and repair;
* pre-work effort and uncertainty forecast;
* visible per-task budget and hard spending limits;
* selectable cost, speed, and correctness policies;
* dynamic model and reasoning-effort routing;
* project-local memory from validated work;
* inspection, correction, export, and deletion of local memory;
* inspectable diffs, validation evidence, and review handoff;
* safe command permissions and explicit approval for risky actions;
* git isolation, checkpointing, cancellation, rollback, and crash recovery.

Initial task classes include:

* bug fixes;
* scoped features;
* tests and reproductions;
* refactors;
* dependency and configuration changes;
* documentation tied to repository behavior.

High-risk request-side Go workflows provide the first deep-verification wedge:

* payments;
* authorization;
* migrations;
* account provisioning;
* stateful command handling;
* retries and compensation;
* orchestration across external services.

Adoption is incremental. Ordinary code remains the working medium. Users enable stronger validation or the graph only for protected workflows where the additional assurance justifies its cost.

The graph subsystem does not replace database uniqueness, gateway idempotency, durable execution, or runtime observation.

## Hobbyist MVP Decisions

### Local Runtime and Repository Isolation

Ship one local CLI binary. It starts a local coordinator and one subprocess worker per active task:

```text
CLI or editor client
    ↓
Local coordinator
    ├─ provider requests
    ├─ SQLite task journal
    └─ permission decisions
          ↓
Task worker subprocess
          ↓
Dedicated Git worktree and task temp directory
```

No hosted Codeflux service is required. Each task receives a dedicated Git branch or worktree. Source edits occur only there until the user accepts or applies the patch.

The default runner:

* allows reads inside the repository and task temporary directory;
* allows ordinary edits inside the task worktree;
* denies writes outside those locations without approval;
* asks before destructive, privileged, installation, or network-capable commands;
* supports an optional user-provided container for stronger isolation.

The MVP does not promise a perfect cross-platform security sandbox. It provides workspace confinement, mediated commands, explicit approvals, and optional container isolation.

### Provider Credentials

Store provider credentials in the operating-system credential store:

* Windows Credential Manager;
* macOS Keychain;
* Linux Secret Service or an equivalent keyring.

Environment variables are an explicit fallback. Secrets must never be written to SQLite, task transcripts, prompts, diffs, diagnostics, or project memory.

The local coordinator performs provider calls so task workers never receive raw credentials. Logs and tool output pass through redaction before persistence or model context.

### Initial Model Providers

Support a small provider interface with:

```text
list models and capabilities
stream a response
cancel a request
report usage
estimate price
identify model and provider version
```

The initial release should support:

* one OpenAI provider adapter;
* one Anthropic provider adapter;
* one OpenAI-compatible local endpoint adapter.

Every run records the exact adapter, model identifier, capability metadata, and pricing snapshot.

On provider slowness or failure:

1. apply bounded retries for transient transport errors;
2. preserve the latest local task checkpoint;
3. pause when the retry budget is exhausted;
4. offer resume, retry, or user-approved provider switching.

Codeflux must not silently switch providers because doing so can change cost, privacy, capability, and output behavior.

### Repository Indexing and Context Selection

Start with deterministic repository intelligence rather than embedding the entire repository.

For Go repositories, collect:

* file and package structure;
* symbols and references through Go tooling;
* imports and dependency relationships;
* build targets and test locations;
* Git status, recent relevant history, and changed files;
* user-approved project instructions and known commands.

Context selection proceeds in stages:

1. requirement terms and explicit file references;
2. matching symbols and files;
3. direct dependency and caller/callee neighbors;
4. nearby tests and configuration;
5. additional context only when tools or failures justify it.

Every context artifact binds to a repository revision. Cache entries invalidate when supporting files change. Enforce a context budget, deduplicate repeated content, and show the user which files were selected.

Embeddings remain optional until deterministic retrieval has a measured recall problem they can improve.

### Commands, Secrets, and Malicious Repository Content

Treat repository files, issues, comments, generated text, dependency output, and tool output as untrusted data. They cannot modify system policy or grant permissions.

Command policy tiers are:

```text
Automatic:
    repository reads
    diff inspection
    approved formatter, build, and test commands

Allowed within task scope:
    source edits inside the task worktree
    task-local temporary files

Approval required:
    network access
    dependency installation
    writes outside task scope
    credential access
    external messages or deployments
    destructive or privileged commands
```

Maintain local secret-pattern scanning before prompts, logs, and diagnostic exports. Repository-provided Codeflux configuration is displayed and requires first-use approval before it can add commands or permissions.

### Plugins and Custom Commands

Do not load arbitrary plugins into the coordinator process.

Plugins run as subprocesses through a small protocol such as MCP or JSON-RPC. Each plugin provides a manifest declaring:

* executable and version;
* tools exposed;
* filesystem scope;
* network requirements;
* secrets requested;
* expected side effects.

Permissions are approved per plugin and can be revoked. Versions are pinned in task evidence.

Custom project commands are stored as reviewed SQLite records with argument arrays rather than unrestricted shell-string interpolation. Repository-provided command suggestions require first-use approval before being admitted to the database.

### Persistence, Recovery, Diagnostics, and Updates

Use local SQLite in WAL mode as an append-only task journal. Persist after every material state transition:

* plan created or revised;
* permission requested or granted;
* command started or completed;
* patch applied;
* validation completed;
* model request completed;
* checkpoint created;
* task accepted, stopped, or failed.

Checkpoints bind the task state to the Git base revision, worktree state, execution policy, model version, and tool configuration.

After a crash, Codeflux verifies the worktree and repository revision before offering resume. If verification fails, it preserves the patch and starts recovery from the last valid checkpoint.

Provide:

* `codeflux doctor` for environment and provider checks;
* locally readable redacted logs;
* an explicit redacted diagnostic-bundle export;
* manual updates by default;
* signed release artifacts;
* a database backup before migrations;
* rollback to the previous application and schema version when migration fails.

An active task remains bound to the Codeflux version that created it unless the user explicitly migrates it.

### Honest Cost Display

Before work, display:

```text
recommended model and effort
P50 and P90 expected cost
P50 and P90 expected time
hard task spending limit
cost assumptions and pricing timestamp
```

During work, track provider-reported input, output, reasoning, and cached-token usage when available. Mark estimated usage and unknown costs explicitly.

The cost view separates:

* model charges;
* paid tool or hosted-service charges;
* local execution time;
* optional estimated human review cost.

Pause before exceeding the hard spending limit and require explicit approval for a new limit. Final reporting compares forecast with actual cost and explains escalation-related overruns.

These decisions target a practical hobbyist product. Multi-tenant infrastructure and organizational administration are not MVP requirements.

## Enterprise Research Backlog

Retain, but do not build, the following until the hobbyist product demonstrates correctness, repeat usage, and a meaningful compounding advantage:

* organizations, teams, repository groups, roles, and centralized policy;
* SSO, SAML, SCIM, RBAC, code-owner enforcement, and approval workflows;
* multi-tenant isolation, managed hosting, private networking, and air-gapped deployment;
* shared versus personal memory, cross-project promotion, and organizational knowledge governance;
* centralized model, provider, tool, plugin, spend, and data-retention controls;
* data residency, customer-managed encryption, deletion, export, and legal-hold requirements;
* SOC 2, GDPR, CCPA, subprocessor disclosure, and audit evidence;
* generated-code ownership, license provenance, attribution, and cross-customer contamination policy;
* enterprise audit logs, incident response, disaster recovery, SLAs, and support diagnostics;
* procurement, enterprise pricing, billing administration, and contractual model-provider guarantees;
* team collaboration, concurrent editing, PR governance, and organization-wide rollout.

Enterprise research should validate customer demand and operating cost before these capabilities enter the roadmap.

Avoid initially:

* autonomous production deployment;
* managed multi-tenant hosting;
* team administration and enterprise identity;
* unrestricted external writes without user policy;
* broad multi-language semantic verification;
* automatic fine-tuning on project episodes;
* unconstrained agent swarms;
* arbitrary visual programming;
* unconstrained concurrency;
* distributed consensus;
* general-purpose numerical computing;
* highly dynamic reflection;
* graph conversion of an entire handwritten codebase.

---

# 27A. Local Frontend and Tooling

## Product Surface

The initial graphical client is a local-first GoWebComponents v5 application served by the local Go coordinator. It is not a browser IDE and does not attempt to replace the user's editor or terminal.

The interface has two primary surfaces:

1. the chat thread is the control surface through which the user states intent, reviews the plan, grants authority, observes progress, and accepts or redirects work;
2. the graph is the semantic surface through which the user inspects program structure, current execution, and correctness evidence.

Chat drives the work. The graph explains what Codeflux intends to do, what it is doing, and why the result should be trusted. Code diffs, tool output, and validation reports appear as compact expandable cards with an action to open the relevant source in the user's editor.

Do not expose private model reasoning. Display user-visible plans, assumptions, actions, evidence, uncertainty, and decisions.

## Framework and Transport Spike

GoWebComponents v5 is the fixed frontend framework. Before production implementation, run a bounded spike that pins the exact v5 release and verifies:

* WebAssembly build, asset serving, routing, state, and list virtualization;
* typed client generation from the chosen gRPC contracts;
* streaming cancellation and reconnect behavior;
* the exact full-duplex gRPC/WebSocket bridge API;
* browser history, clipboard, file-link, and external-editor integration;
* performance under simultaneous token, task-event, cost, and graph updates.

Publicly documented earlier GoWebComponents releases include a typed gRPC bridge over WebSocket, but the plan must not assume that the v5 API is identical. The spike either confirms that bridge or selects the smallest compatible transport without changing the service contracts.

Native browser gRPC-Web is not assumed to provide client-side or bidirectional streaming. The preferred transport is a same-origin typed gRPC bridge over WebSocket embedded in the local Go server. If the v5 bridge is unsuitable, use unary commands plus one resumable server stream through gRPC-Web or an equivalent embedded Go bridge.

Do not add a separate Envoy process to the hobbyist installation unless the spike proves that an embedded bridge is infeasible.

## Application Layout

The desktop layout is:

```text
+----------------------------------------------------------------------------------+
| Codeflux | repository / branch | local health | search | help | settings         |
+--------------+---------------------------------------------+---------------------+
| Navigation   | task title / state / pause / stop / review | Assurance / context |
| and tasks    +---------------------------------------------+ task details        |
|              | task summary: evidence / progress / cost / gates                 |
|              +--------------------------+------------------+ correctness gates   |
|              | Conversation             | Graph            | measured metrics    |
|              | chronological cards      | task-scoped      | related evidence    |
|              | and composer             | explanation      | or node inspector   |
+--------------+--------------------------+------------------+---------------------+
```

The normal workspace gives approximately 55–60% of the elastic center to conversation and 40–45% to the graph. The left task rail and right assurance rail are independently collapsible. The conversation/graph split is resizable. A deliberate Graph Focus mode expands the graph without making it the only way to operate the task. During active execution, the user may choose an Execution Focus arrangement with the graph above and timeline, live output, or chat below. Layout does not switch underneath the user after they have interacted with a pane.

On narrow screens, the task rail becomes an overlay, the assurance rail becomes a drawer, and conversation/graph become state-preserving tabs rather than compressed columns.

The global application bar shows:

* Codeflux identity;
* repository, branch, and isolated-worktree access;
* coordinator, database, provider, and stream health without collapsing them into one misleading green state;
* search, shortcut help, settings, and local profile controls.

The task header directly below it shows task title, task state, pause/resume, stop, request-review, and overflow actions. The task summary strip shows the selected correctness profile or achieved evidence, calibrated forecast confidence, progress, model/effort when decision-relevant, estimated time remaining, actual and estimated cost, hard budget, and required gate completion.

## Conversation Model

The virtualized thread renders typed events rather than treating the transcript as an undifferentiated stream. Initial presentation types are:

* user and agent messages;
* requirement and ambiguity cards;
* P50/P90 time, cost, and uncertainty forecasts;
* plan and scope revisions;
* summarized tool activity;
* permission and approval requests;
* diff and artifact summaries;
* validation and assurance reports;
* cost and budget updates;
* errors, recovery choices, and completion summaries.

Tool output is collapsed by default. The primary view might say `41 tests run; 39 passed; 2 failed`, with raw redacted output available on demand. Large code views, persistent file trees, and an embedded terminal are not part of the MVP.

Approval cards state the exact action, relevant scope, and likely consequence. They support:

```text
allow once
allow for this task
deny
```

Messages can refer to graph nodes through stable clickable identity chips. Activating a chip focuses the node. Selecting a node highlights the messages, actions, and evidence that created, executed, or validated it.

The composer supports:

* task instructions and follow-up corrections;
* send, pause, stop, and resume;
* cost/speed/correctness policy;
* task budget;
* optional model or effort override;
* attachment of explicit repository files or symbols.

## Graph Modes

Do not combine all graph concerns into one network. The same task-scoped graph pane has three modes:

### Program

Shows the semantic structure relevant to the current task:

```text
requirements
-> plan regions
-> atoms and branches
-> external effects
-> validation obligations
-> generated or changed artifacts
```

### Execution

Shows the live path through the task:

* pending, active, completed, failed, blocked, and retried work;
* current tool or model activity;
* checkpoints and recovery;
* reconciliations and compensations;
* time and cost attributable to the visible path.

Execution is the default mode while the agent is running.

### Evidence

Shows why the result is considered acceptable:

* obligations and guarantee levels;
* test, validator, review, and runtime evidence;
* dependency cones and provenance;
* invalidated or runtime-only claims;
* the evidence attached to the final diff.

Evidence is the default mode after task completion.

## Graph Rendering Rules

The default view is a task-scoped slice rather than the entire repository or database. It uses a stable left-to-right directed layout. Do not use an unstable force-directed layout as the primary view.

Initial node classes are:

* requirement or intent;
* plan or semantic region;
* pure atom or operation;
* external effect;
* branch, match, or merge;
* validation obligation;
* artifact or result.

Initial status classes are:

```text
pending
active
passed
warning
failed
blocked
invalidated
```

Color cannot be the only status indicator. Use shape, icon, border, and text labels so the graph remains usable with color-vision differences and high-contrast themes.

Edges distinguish:

* data and identity provenance;
* control flow;
* evidence dependency;
* retry;
* reconciliation;
* compensation.

Collapse semantic regions, strongly connected components, and repeated template instances before rendering large slices. Stable node identities produce stable placement hints across revisions so nodes do not jump whenever the graph changes.

The MVP graph is read-only. It supports:

* pan, zoom, search, focus, and selection;
* expand neighbors;
* isolate a dependency or evidence cone;
* compare the current and proposed revision;
* explain a node in chat;
* jump to related messages, validation, diff, or source.

Graph changes are requested through chat. Codeflux proposes an immutable graph revision and semantic diff, and the user accepts or rejects it. Direct manipulation and drag-and-drop programming remain deferred until evidence shows they improve task outcomes.

## Node Inspector

Selecting a node opens a side inspector containing:

* stable identity, revision, and node class;
* contract, inputs, outputs, and effects;
* execution status;
* supporting evidence and guarantee level;
* relevant time, token, and monetary cost;
* related messages, tools, source, and graph revisions;
* commands to explain, expand, isolate, compare, or open in the editor.

The inspector presents source excerpts only when they answer the current question. It is not a permanent code editor.

## Client, Server, and Storage Boundary

```text
GoWebComponents v5 client in Go/WASM
        |
typed commands and one resumable event stream
        |
local Go coordinator and embedded bridge
        +-- workspace and repository service
        +-- thread and task service
        +-- graph and evidence service
        +-- review and validation service
        +-- agent coordinator and task workers
        +-- model-provider adapters
        `-- SQLite repositories
```

SQLite and the local server are authoritative for:

* threads, messages, plans, tasks, and task events;
* graph identities, immutable revisions, layouts, and patches;
* approvals, permission decisions, and budgets;
* forecasts, usage, actual costs, and routing evidence;
* validation, assurance evidence, atoms, and outcomes.

No graph, atom, vector, transcript, or task-state sidecar files are created. Source code and ordinary repository files remain in Git.

Browser-local state is limited to ephemeral interaction state such as:

* selected node and graph viewport;
* pane sizes and expanded cards;
* unsubmitted composer draft;
* transient query and render caches.

The client never loads the full database graph. It requests a bounded task slice and expands it on demand.

## Service Contracts

The initial logical gRPC services are:

```text
WorkspaceService
    OpenWorkspace
    GetWorkspaceState
    ListRepositories

ThreadService
    CreateThread
    ListThreads
    GetThreadPage
    SendMessage

TaskService
    StartTask
    PauseTask
    ResumeTask
    CancelTask
    ApproveAction
    SetBudget

GraphService
    GetGraphSlice
    ExpandGraph
    GetNode
    ExplainNode

ReviewService
    GetDiffSummary
    GetValidationReport
    AcceptChange
    RejectChange
    OpenInEditor

SettingsService
    GetModels
    SetPolicy
    SetBudgetDefaults
    TestProvider

SessionService
    SubscribeSession
```

Keep these as logical domain boundaries even if the MVP combines them in one Go process and one generated client package.

Commands carry an idempotency key and, where relevant, an expected revision. Duplicate delivery must not duplicate a message, approval, task transition, or external action.

## Unified Session Stream

Use one ordered session stream for chat, graph, task state, validation, and cost instead of coordinating multiple independently ordered streams.

```text
SessionEvent
    sequence
    thread_id
    task_id
    timestamp
    kind
    revision
    payload
```

Initial event kinds are:

```text
message_delta
message_final
plan_changed
tool_started
tool_completed
approval_requested
task_state_changed
cost_updated
validation_updated
graph_patch
checkpoint_created
error
```

Material events are committed to SQLite with a monotonic per-session sequence before they are considered delivered. On reconnect, the client sends `after_sequence` and the server replays missing events before returning to live delivery.

Backpressure may batch token deltas and summarize verbose tool chunks. It must not discard approvals, budget changes, checkpoints, validation results, graph revisions, task transitions, or errors.

## Rendering and Performance

Go/WASM does not remove the cost of browser DOM updates. The UI therefore:

* virtualizes long thread histories;
* batches token deltas at approximately 30-50 millisecond intervals;
* batches graph patches at approximately 50-100 millisecond intervals;
* separates chat and graph component update boundaries;
* computes initial ranks and stable layout hints on the server;
* keeps pan, zoom, focus, and selection responsive on the client;
* uses accessible SVG for task slices up to a measured practical threshold;
* collapses or virtualizes larger slices before considering Canvas or WebGL.

Performance thresholds must be measured on an ordinary hobbyist laptop. The initial target is smooth interaction with a long thread and approximately 300 visible graph nodes while model and task events are streaming.

## Local Security

The frontend server binds to loopback by default. It uses a random per-launch session secret, same-origin checks, and explicit opt-in before accepting non-loopback connections.

The browser never receives provider credentials. All provider calls, credential-store access, tool execution, SQLite writes, and permission enforcement remain in the coordinator or task worker boundary.

Raw tool output is redacted before persistence and display. Opening a file in an external editor is an explicit local action with a resolved repository path, not an arbitrary URL or command supplied by model output.

## Primary Interaction Journey

```text
Open repository
-> select or create a thread
-> state the requirement in chat
-> inspect P50/P90 forecast, selected policy, model, effort, and budget
-> approve or redirect the plan
-> observe concise live events while the execution graph highlights the current path
-> grant or deny scoped authority inline
-> inspect the diff, validation summary, and evidence graph
-> accept, request repair, or roll back
-> preserve the accepted result and factual episode in SQLite
```

## Frontend MVP Boundary

Build:

* thread rail and resumable conversations;
* streaming chat with typed cards;
* plan, tool, approval, diff, validation, recovery, and completion presentations;
* always-visible forecast, model, effort, cost, and budget state;
* one reconnectable ordered session stream;
* task-scoped program, execution, and evidence graph modes;
* stable directed graph layout and node inspector;
* bidirectional chat-to-graph references;
* responsive graph drawer;
* external-editor handoff.

Defer:

* embedded code editor, terminal, and full file explorer;
* direct graph editing or visual programming;
* full-repository graph rendering;
* collaborative presence and multi-user permissions;
* plugin-provided UI surfaces;
* elaborate force layouts;
* multiple simultaneous graph canvases;
* animated historical playback;
* mobile-first task editing.

## Frontend Acceptance Criteria

The frontend is ready for the hobbyist MVP when:

* a user can complete the full interaction journey without opening a terminal;
* the same task can be paused, reloaded, replayed from SQLite, and resumed without duplicated actions;
* every approval, cost change, validation result, and graph revision is visibly attributable;
* graph selection and chat selection resolve to the same stable identities;
* a reconnect loses no correctness-bearing event;
* the interface remains responsive at the measured long-thread and task-graph target;
* the user can always stop work, inspect the diff, open source externally, accept, request repair, or roll back;
* the graph remains optional for ordinary code-first tasks.

The product-level rule is:

```text
Chat controls the work.
The graph explains structure, execution, and correctness.
Neither surface hides authority, evidence, cost, or uncertainty.
```

---

# 27B. Prototype Backend Function and Flow Specification

This section defines the complete application-level backend surface for the prototype. It fixes domain ownership, public functions, transaction boundaries, durable events, and chronological failure behavior. Private helpers may evolve as long as these contracts and invariants remain intact.

## Backend Design Rules

Every mutating backend operation must declare:

```text
typed input
authorization requirement
idempotency key
expected entity revision when concurrent mutation is possible
SQLite transaction boundary
durable state written
events emitted after commit
external side effects
cancellation behavior
typed failure outcomes
```

The backend follows these rules:

* gRPC handlers validate transport input and delegate to application services; they do not contain domain policy or SQL;
* application services own use-case ordering and transaction boundaries;
* repositories expose domain operations rather than generic table CRUD;
* only committed state produces correctness-bearing events;
* an event is published to live subscribers only after its database transaction commits;
* provider calls, commands, Git operations, and editor launches are explicit external effects and never occur inside a long SQLite transaction;
* each external effect is preceded by durable intent when replay or ambiguity matters and followed by a durable outcome;
* functions return typed conflicts, stale revisions, policy denials, budget exhaustion, cancellation, retryable transport failure, corruption, and recovery-required outcomes;
* `context.Context` controls deadlines and cancellation but cancellation never rewrites a committed success into failure;
* retries reuse the original idempotency identity;
* logs are diagnostic projections, never authority.

## Process and Package Ownership

```text
cmd/codeflux
    process startup, CLI, version, doctor, browser launch

internal/app
    dependency construction, lifecycle, graceful shutdown

internal/transport
    protobuf conversion, authentication, validation, safe error mapping

internal/coordinator
    task use cases, execution policy, orchestration, recovery decisions

internal/worker
    isolated task loop and mediated tool execution

internal/domain
    identities, state machines, money, policies, errors, immutable values

internal/storage
    migrations, transactions, domain repositories, backup, integrity

internal/events
    append, sequence, replay, subscription, snapshots, projections

internal/workspace
    repository discovery, Go map, context selection, revision binding

internal/gitwork
    worktree lifecycle, safe edits, diff, checkpoint binding, acceptance

internal/providers
    provider adapters, streaming normalization, usage, price, cancellation

internal/executor
    tool schemas, permission checks, approvals, commands, redaction

internal/agent
    requirement intake, planning, bounded execution, repair, completion

internal/review
    risk, validation selection, validation runs, evidence and acceptance

internal/graph
    event projection, revisions, slices, layout, patches, node explanation

internal/memory
    episodes, facts, fingerprints, atom admission, retrieval, embeddings
```

Dependencies point inward toward domain types. Storage, providers, Git, process execution, and browser transport implement ports consumed by application services. Provider adapters, gRPC handlers, and SQLite repositories do not import frontend code.

## Application Lifecycle Functions

The long-lived process exposes:

```go
func NewApplication(deps Dependencies) (*Application, error)
func (a *Application) Start(ctx context.Context, opts StartOptions) (StartResult, error)
func (a *Application) Health(ctx context.Context) (HealthReport, error)
func (a *Application) Shutdown(ctx context.Context) error
```

`NewApplication` validates dependency completeness without opening external resources. `Start` acquires the process lock, creates the application-data directory, opens and migrates SQLite, initializes repositories, checks recovery-required tasks, starts the loopback server, and returns the authenticated local URL. `Shutdown` stops new mutations, asks workers to checkpoint, drains subscribers, closes the server, checkpoints the WAL, and releases the process lock.

Supporting lifecycle operations are:

```go
func ResolveApplicationPaths(osEnv OSEnvironment) (ApplicationPaths, error)
func AcquireInstanceLock(path string) (InstanceLock, error)
func OpenDatabase(ctx context.Context, cfg DatabaseConfig) (*sql.DB, error)
func ApplyMigrations(ctx context.Context, db *sql.DB, catalog MigrationCatalog) (MigrationReport, error)
func DiscoverRecoveryCandidates(ctx context.Context) ([]RecoveryCandidate, error)
func StartLoopbackServer(ctx context.Context, cfg ServerConfig) (ServerHandle, error)
func BuildHealthReport(ctx context.Context) (HealthReport, error)
```

Startup failure must identify whether retry, repair, restore, or a newer binary is required. It must not expose raw secrets or execute recovery automatically.

## Transaction and Event Functions

All repositories share:

```go
type TransactionRunner interface {
    WithinTransaction(ctx context.Context, fn func(ctx context.Context, tx Transaction) error) error
}

type EventJournal interface {
    Append(ctx context.Context, tx Transaction, event NewSessionEvent) (SessionEvent, error)
    Replay(ctx context.Context, sessionID SessionID, after Sequence, limit int) ([]SessionEvent, error)
    CurrentSequence(ctx context.Context, sessionID SessionID) (Sequence, error)
}

type EventHub interface {
    PublishCommitted(event SessionEvent)
    Subscribe(ctx context.Context, sessionID SessionID, after Sequence) (EventSubscription, error)
}
```

`Append` allocates one monotonic per-session sequence inside the same transaction as the state transition represented by the event. `PublishCommitted` accepts only a committed event value. `Subscribe` first establishes a replay boundary, replays missing committed events, then joins live delivery without a gap.

Snapshot functions are:

```go
func BuildSessionSnapshot(ctx context.Context, sessionID SessionID) (SessionSnapshot, error)
func ReduceTaskEvents(base TaskSnapshot, events []SessionEvent) (TaskSnapshot, error)
func CompactEphemeralEvents(ctx context.Context, policy CompactionPolicy) (CompactionReport, error)
```

Compaction may summarize token deltas and verbose progress. It cannot remove approvals, budgets, task transitions, checkpoints, validations, graph revisions, final messages, errors, or acceptance decisions.

## Workspace Functions

```go
type WorkspaceService interface {
    OpenRepository(ctx context.Context, cmd OpenRepositoryCommand) (WorkspaceSnapshot, error)
    InspectRepository(ctx context.Context, repositoryID RepositoryID) (RepositoryInspection, error)
    RefreshRepositoryMap(ctx context.Context, cmd RefreshRepositoryMapCommand) (RepositoryMapRevision, error)
    SelectContext(ctx context.Context, query ContextQuery) (ContextManifest, error)
    ExplainContextSelection(ctx context.Context, manifestID ContextManifestID) (ContextExplanation, error)
}
```

Internal workspace ports are:

```go
func ResolveRepositoryRoot(path string) (CanonicalRepositoryPath, error)
func InspectGitState(ctx context.Context, root CanonicalRepositoryPath) (GitState, error)
func DiscoverGoWorkspace(ctx context.Context, root CanonicalRepositoryPath) (GoWorkspace, error)
func BuildGoRepositoryMap(ctx context.Context, input MapInput) (RepositoryMap, error)
func RankContextCandidates(query ContextQuery, candidates []ContextCandidate) []RankedContext
func EnforceContextBudget(items []RankedContext, budget ContextBudget) ContextManifest
func ScrubContext(manifest ContextManifest) (ContextManifest, ScrubReport, error)
```

`OpenRepository` is read-only with respect to repository files. It canonicalizes the path, establishes stable repository identity, records Git state, and returns warnings for dirty, detached, conflicted, nested, submodule, or unsupported states. `SelectContext` is deterministic for the same requirement, repository revision, mapping revision, and selection-policy version.

## Thread and Message Functions

```go
type ThreadService interface {
    CreateThread(ctx context.Context, cmd CreateThreadCommand) (Thread, error)
    ListThreads(ctx context.Context, query ListThreadsQuery) (ThreadPage, error)
    GetThreadPage(ctx context.Context, query ThreadPageQuery) (MessagePage, error)
    AppendUserMessage(ctx context.Context, cmd AppendUserMessageCommand) (Message, error)
    RenameThread(ctx context.Context, cmd RenameThreadCommand) (Thread, error)
    ArchiveThread(ctx context.Context, cmd ArchiveThreadCommand) (Thread, error)
}
```

`AppendUserMessage` stores the user text and attachment identities atomically, emits `message_final`, and optionally creates a draft task. It never starts model work merely because transport delivery succeeded. Duplicate idempotency keys return the original committed message.

Thread titles are user-editable. Automatic title suggestions are non-authoritative and cannot overwrite a user title.

## Task, Plan, and Execution Functions

```go
type TaskService interface {
    CreateTask(ctx context.Context, cmd CreateTaskCommand) (TaskSnapshot, error)
    ForecastTask(ctx context.Context, cmd ForecastTaskCommand) (TaskForecast, error)
    BuildPlan(ctx context.Context, cmd BuildPlanCommand) (PlanRevision, error)
    RevisePlan(ctx context.Context, cmd RevisePlanCommand) (PlanRevision, error)
    ApprovePlan(ctx context.Context, cmd ApprovePlanCommand) (TaskSnapshot, error)
    StartTask(ctx context.Context, cmd StartTaskCommand) (TaskSnapshot, error)
    PauseTask(ctx context.Context, cmd PauseTaskCommand) (TaskSnapshot, error)
    ResumeTask(ctx context.Context, cmd ResumeTaskCommand) (TaskSnapshot, error)
    CancelTask(ctx context.Context, cmd CancelTaskCommand) (TaskSnapshot, error)
    RequestRepair(ctx context.Context, cmd RequestRepairCommand) (PlanRevision, error)
    RollbackTask(ctx context.Context, cmd RollbackTaskCommand) (TaskSnapshot, error)
}
```

Planning functions are:

```go
func InterpretRequirement(ctx context.Context, input RequirementInput) (RequirementInterpretation, error)
func ClassifyTaskRisk(input RiskInput) (RiskClassification, error)
func BuildTaskFingerprint(input FingerprintInput) (TaskFingerprint, error)
func RetrievePreWorkEvidence(ctx context.Context, query RetrievalQuery) (RetrievalSet, error)
func EstimateTask(input ForecastInput) (TaskForecast, error)
func ConstructPlan(ctx context.Context, input PlanInput) (PlanDraft, error)
func ValidatePlan(plan PlanDraft, policy EffectivePolicy) (PlanValidation, error)
```

Execution functions are:

```go
func StartRun(ctx context.Context, cmd StartRunCommand) (Run, error)
func ExecuteNextStep(ctx context.Context, runID RunID) (StepOutcome, error)
func ObserveProgress(ctx context.Context, runID RunID) (ProgressAssessment, error)
func DecideNextAction(input ProgressAssessment) (ExecutionDecision, error)
func CompleteRun(ctx context.Context, cmd CompleteRunCommand) (CompletionResult, error)
```

`StartTask` verifies an approved current plan, valid worktree binding, available provider, sufficient budget, and absence of unresolved approval or recovery state. It creates a run and worker ownership record before launching the worker. Launch failure transitions the run to a typed failed or recovery-required state without losing the plan.

`ExecuteNextStep` is bounded by current plan, policy, budget, authority, cancellation, and validation floors. It cannot invent an unregistered tool or silently expand scope.

## Worktree and Edit Functions

```go
type WorktreeService interface {
    CreateTaskWorktree(ctx context.Context, cmd CreateTaskWorktreeCommand) (WorktreeBinding, error)
    VerifyTaskWorktree(ctx context.Context, taskID TaskID) (WorktreeVerification, error)
    ApplyEditBatch(ctx context.Context, cmd ApplyEditBatchCommand) (EditBatchResult, error)
    GetTaskDiff(ctx context.Context, taskID TaskID) (TaskDiff, error)
    CreateCheckpoint(ctx context.Context, cmd CreateCheckpointCommand) (Checkpoint, error)
    RestoreCheckpoint(ctx context.Context, cmd RestoreCheckpointCommand) (RestoreResult, error)
    AcceptTaskChange(ctx context.Context, cmd AcceptTaskChangeCommand) (AcceptanceResult, error)
    AbandonTaskWorktree(ctx context.Context, cmd AbandonTaskWorktreeCommand) (AbandonResult, error)
    CleanupTaskWorktree(ctx context.Context, cmd CleanupTaskWorktreeCommand) error
}
```

Supporting safe file functions are:

```go
func ResolveTaskPath(binding WorktreeBinding, relativePath string) (ResolvedTaskPath, error)
func ReadFileAtRevision(ctx context.Context, req FileReadRequest) (FileSnapshot, error)
func ApplyFileMutation(ctx context.Context, req FileMutation) (FileMutationResult, error)
func ComputeDiffIdentity(ctx context.Context, binding WorktreeBinding) (DiffIdentity, error)
func DetectConcurrentUserChanges(before FileSnapshot, current FileSnapshot) ConflictResult
```

Every mutation carries an expected content hash or non-existence precondition. Path resolution rejects traversal and external symlink targets. Checkpoints bind the plan, event sequence, Git base, worktree state, diff identity, budget position, and ambiguous external-effect state.

## Provider and Model Functions

```go
type ModelProvider interface {
    ProviderIdentity() ProviderIdentity
    ListModels(ctx context.Context) ([]ModelDescriptor, error)
    Stream(ctx context.Context, req ModelRequest) (ModelStream, error)
    Cancel(ctx context.Context, requestID ModelRequestID) error
}

type ProviderService interface {
    ConfigureProvider(ctx context.Context, cmd ConfigureProviderCommand) (ProviderConfiguration, error)
    TestProvider(ctx context.Context, providerID ProviderID) (ProviderTestResult, error)
    GetModelCatalog(ctx context.Context) (ModelCatalog, error)
    ExecuteModelRequest(ctx context.Context, cmd ExecuteModelRequestCommand) (ModelRequestResult, error)
}
```

Model request orchestration performs:

```go
func BuildModelRequest(input ModelRequestInput) (ModelRequest, error)
func ReserveRequestBudget(ctx context.Context, estimate RequestCostEstimate) (BudgetReservation, error)
func PersistRequestIntent(ctx context.Context, request ModelRequest, reservation BudgetReservation) error
func ConsumeModelStream(ctx context.Context, stream ModelStream, sink ModelEventSink) (ModelResponse, error)
func ReconcileUsage(ctx context.Context, response ModelResponse) (UsageReconciliation, error)
func ClassifyProviderFailure(err error) ProviderFailure
```

Physical retries are recorded separately and consume budget. A provider/model switch requires a new explicit decision and user authority when cost, privacy, or behavior changes.

## Budget Functions

```go
type BudgetService interface {
    CreateBudget(ctx context.Context, cmd CreateBudgetCommand) (Budget, error)
    Reserve(ctx context.Context, cmd ReserveBudgetCommand) (BudgetReservation, error)
    CommitUsage(ctx context.Context, cmd CommitUsageCommand) (BudgetSnapshot, error)
    ReleaseReservation(ctx context.Context, reservationID BudgetReservationID) (BudgetSnapshot, error)
    RaiseLimit(ctx context.Context, cmd RaiseBudgetLimitCommand) (BudgetSnapshot, error)
    GetBudget(ctx context.Context, taskID TaskID) (BudgetSnapshot, error)
}
```

Reservations and usage posting use exact arithmetic and optimistic revision checks. No new billable request begins when the hard cap cannot cover its approved bound. Unknown pricing remains unknown and cannot be treated as zero.

## Tool, Permission, and Command Functions

```go
type ToolService interface {
    ListTools(ctx context.Context, taskID TaskID) ([]ToolDescriptor, error)
    RequestToolExecution(ctx context.Context, cmd ToolExecutionCommand) (ToolExecutionDecision, error)
    ResolveApproval(ctx context.Context, cmd ResolveApprovalCommand) (ApprovalResolution, error)
    CancelToolExecution(ctx context.Context, executionID ToolExecutionID) error
}

func ClassifyAuthority(req ToolRequest, policy PermissionPolicy) AuthorityClassification
func MatchExistingGrant(req ToolRequest, grants []PermissionGrant) GrantMatch
func BuildApprovalRequest(req ToolRequest, classification AuthorityClassification) (ApprovalRequest, error)
func ExecuteAuthorizedTool(ctx context.Context, req AuthorizedToolRequest) (ToolResult, error)
func RedactToolResult(result ToolResult, policy RedactionPolicy) (RedactedToolResult, error)
```

`RequestToolExecution` either executes an automatic/task-scoped action, returns a durable pending approval, or denies the action. Approval resolution is idempotent, revision-checked, and scoped. Denial never triggers a hidden alternative tool.

Commands use argument arrays, a resolved task working directory, a minimal environment, bounded output, timeout, cancellation, and process-tree cleanup.

## Worker Functions

```go
type WorkerManager interface {
    Spawn(ctx context.Context, spec WorkerSpec) (WorkerHandle, error)
    Pause(ctx context.Context, workerID WorkerID) error
    Resume(ctx context.Context, workerID WorkerID) error
    Cancel(ctx context.Context, workerID WorkerID) error
    Checkpoint(ctx context.Context, workerID WorkerID) (WorkerCheckpointResult, error)
    Status(ctx context.Context, workerID WorkerID) (WorkerStatus, error)
}

func AcquireRunOwnership(ctx context.Context, runID RunID, workerID WorkerID) (RunLease, error)
func RenewRunOwnership(ctx context.Context, lease RunLease) (RunLease, error)
func ReleaseRunOwnership(ctx context.Context, lease RunLease) error
func HandleWorkerHeartbeat(ctx context.Context, heartbeat WorkerHeartbeat) error
func ClassifyLostWorker(ctx context.Context, runID RunID) (RecoveryClassification, error)
```

Only one live lease owns a run. Lease expiry does not automatically repeat the last action. It transitions to recovery analysis.

## Validation and Review Functions

```go
type ValidationService interface {
    SelectValidationPlan(ctx context.Context, input ValidationSelectionInput) (ValidationPlan, error)
    RunValidation(ctx context.Context, cmd RunValidationCommand) (ValidationRun, error)
    GetValidationReport(ctx context.Context, taskID TaskID) (ValidationReport, error)
    InvalidateStaleValidation(ctx context.Context, diff DiffIdentity) (InvalidationReport, error)
}

type ReviewService interface {
    BuildReviewBundle(ctx context.Context, taskID TaskID) (ReviewBundle, error)
    AcceptChange(ctx context.Context, cmd AcceptChangeCommand) (AcceptanceResult, error)
    RequestRepair(ctx context.Context, cmd ReviewRepairCommand) (PlanRevision, error)
    RejectChange(ctx context.Context, cmd RejectChangeCommand) (RejectionResult, error)
    OpenInEditor(ctx context.Context, cmd OpenInEditorCommand) error
}
```

Supporting functions are:

```go
func SelectTests(input TestSelectionInput) (SelectedTests, error)
func CompareWithBaseline(input BaselineComparisonInput) (BaselineComparison, error)
func BuildEvidenceReport(input EvidenceReportInput) (EvidenceReport, error)
func AssignClaimGuarantees(input ClaimEvidenceInput) (ClaimGuaranteeSet, error)
func VerifyAcceptanceRevision(input AcceptanceInput) error
```

Validation results bind to the exact diff identity. Any subsequent semantic edit invalidates affected results. Acceptance fails with a stale-review conflict if the diff or evidence revision changed after the user opened the review.

## Graph Functions

```go
type GraphService interface {
    GetGraphSlice(ctx context.Context, query GraphSliceQuery) (GraphSlice, error)
    ExpandGraph(ctx context.Context, query ExpandGraphQuery) (GraphSlice, error)
    GetNode(ctx context.Context, query GetNodeQuery) (GraphNodeDetails, error)
    ExplainNode(ctx context.Context, query ExplainNodeQuery) (NodeExplanation, error)
    CompareGraphRevisions(ctx context.Context, query CompareGraphQuery) (GraphDiff, error)
}

func ProjectSessionEvent(graph GraphProjection, event SessionEvent) (GraphProjection, GraphChange, error)
func CommitGraphRevision(ctx context.Context, change GraphChange) (GraphRevision, error)
func BuildGraphPatch(previous GraphRevision, current GraphRevision) (GraphPatch, error)
func ComputeStableLayout(input LayoutInput) (LayoutResult, error)
func IsolateDependencyCone(graph GraphSlice, root NodeID, limits ConeLimits) GraphSlice
func IsolateEvidenceCone(graph GraphSlice, root NodeID, limits ConeLimits) GraphSlice
```

Projection is deterministic for the same ordered event history and projection-schema version. Token deltas do not create graph revisions. Queries enforce task/project scope and node/edge limits before layout.

## Memory, Atom, and Retrieval Functions

```go
type MemoryService interface {
    FinalizeEpisode(ctx context.Context, cmd FinalizeEpisodeCommand) (Episode, error)
    ExtractDeterministicFacts(ctx context.Context, episodeID EpisodeID) (FactExtractionResult, error)
    RetrieveBeforeWork(ctx context.Context, query PreWorkRetrievalQuery) (RetrievalSet, error)
    RecordInfluence(ctx context.Context, cmd RecordInfluenceCommand) error
    InvalidateArtifact(ctx context.Context, cmd InvalidateArtifactCommand) (InvalidationReport, error)
}

func ValidateAtomName(input AtomNameInput) (ValidatedAtomName, error)
func ParseAtomDocumentation(input GoDocInput) (AtomDocumentation, error)
func ValidateAtomDocumentation(doc AtomDocumentation) (DocumentationValidation, error)
func AdmitAtomRevision(ctx context.Context, input AtomAdmissionInput) (AtomRevision, error)
func BuildEmbeddingInput(input EmbeddingInputSource) (NormalizedEmbeddingInput, error)
func EmbedArtifact(ctx context.Context, input NormalizedEmbeddingInput) (EmbeddingRecord, error)
func RetrieveExact(ctx context.Context, query ExactRetrievalQuery) ([]RetrievalCandidate, error)
func RetrieveVectorCandidates(ctx context.Context, query VectorRetrievalQuery) ([]RetrievalCandidate, error)
func CheckApplicability(candidate RetrievalCandidate, task TaskFingerprint) ApplicabilityDecision
func CheckAssurance(candidate RetrievalCandidate, required AssuranceLevel) AssuranceDecision
```

Exact and deterministic retrieval runs before vector discovery. Candidate discovery and eligibility are separate functions. Atom comments and names enrich discovery; only compatibility, applicability, evidence, assurance, and authority permit influence.

## Credentials, Settings, and Diagnostics Functions

```go
type CredentialStore interface {
    Put(ctx context.Context, ref CredentialRef, value SecretValue) error
    Get(ctx context.Context, ref CredentialRef) (SecretValue, error)
    Delete(ctx context.Context, ref CredentialRef) error
    TestAvailability(ctx context.Context) CredentialStoreStatus
}

type SettingsService interface {
    GetEffectiveSettings(ctx context.Context, scope SettingsScope) (EffectiveSettings, error)
    UpdateUserSettings(ctx context.Context, cmd UpdateSettingsCommand) (SettingsRevision, error)
    ApproveRepositorySettings(ctx context.Context, cmd ApproveRepositorySettingsCommand) (SettingsRevision, error)
}

type DiagnosticsService interface {
    RunDoctor(ctx context.Context) (DoctorReport, error)
    BackupDatabase(ctx context.Context, cmd BackupCommand) (BackupResult, error)
    CheckIntegrity(ctx context.Context) (IntegrityReport, error)
    BuildDiagnosticExport(ctx context.Context, cmd DiagnosticExportCommand) (DiagnosticExport, error)
}
```

Secret values cannot be formatted or serialized through ordinary diagnostic paths. Diagnostic export begins with a manifest and preview; source, prompts, and task content are excluded by default.

## Chronological Backend Flows

### Startup

```text
resolve application paths
-> acquire instance lock
-> open SQLite
-> back up if migration requires it
-> apply migrations
-> initialize repositories and services
-> inspect incomplete tasks and workers
-> bind loopback server
-> create per-launch browser session
-> return local URL and health
```

Any failure before server bind produces no browser session. Recovery candidates are reported but not resumed automatically.

### Open Repository

```text
OpenWorkspace RPC
-> authenticate local session
-> canonicalize and validate path
-> inspect Git state
-> establish stable repository identity
-> discover Go workspace and tools
-> build or load revision-bound repository map
-> persist workspace snapshot and warnings
-> emit workspace_opened
-> return snapshot
```

Repository inspection performs no source writes, dependency installation, or network fetch.

### Submit Requirement, Forecast, and Plan

```text
SendMessage RPC with idempotency key
-> commit user message and event
-> create draft task
-> select deterministic context
-> build fingerprint
-> retrieve compatible project evidence
-> classify risk
-> create fixed-policy forecast and budget proposal
-> call planner under reserved budget
-> validate and commit immutable plan revision
-> emit forecast and plan events
-> transition to awaiting-plan-approval
```

If planning fails, the user message remains committed and the task becomes retryable or paused; it does not disappear.

### Approve and Start Task

```text
ApprovePlan with expected plan revision
-> validate current revision and policy floor
-> commit approval
-> create isolated worktree at recorded base revision
-> verify provider and budget
-> create run and worker ownership intent
-> spawn worker
-> commit running state
-> emit task and execution-graph events
```

If worktree or worker creation fails, the task retains its approved plan and exposes a specific recovery or retry choice.

### Agent Tool Step

```text
worker requests next model action
-> reserve model budget
-> persist model request intent
-> stream and batch descriptive message deltas
-> validate final tool call
-> classify authority
-> execute automatically, request approval, or deny
-> persist tool intent before effect
-> execute within worktree/process boundary
-> redact and commit result
-> update plan step and graph revision
-> checkpoint after material edit
-> assess progress and choose continue/re-plan/stop
```

No model text directly invokes a process, filesystem write, network request, or credential read.

### Approval

```text
tool requires authority
-> commit approval request and paused action identity
-> emit approval_requested
-> user resolves with idempotency key and expected revision
-> commit scoped grant or denial
-> emit approval_resolved
-> if granted, revalidate exact pending action
-> execute once using the original action identity
```

An edited command, changed path, expanded scope, expired task, or changed policy requires a new approval.

### Pause, Cancel, and Resume

```text
pause request
-> commit pause_requested
-> stop starting new work
-> cancel or finish current action according to action policy
-> create checkpoint
-> commit paused

resume request
-> verify worktree, diff, repository, policy, provider, budget, and ambiguity
-> reconcile user edits or require recovery choice
-> acquire new worker lease
-> commit running
```

Cancel is terminal for the active run but preserves the worktree and patch until explicit cleanup.

### Provider Failure and Budget Exhaustion

```text
provider failure
-> classify
-> retry only transient failures within retry and budget bounds
-> record every physical attempt
-> preserve partial output as incomplete
-> pause after retry exhaustion
-> offer retry or explicit provider switch

hard budget reached
-> allow already-billed in-flight response to settle
-> block new billable requests
-> commit budget_exhausted
-> checkpoint
-> offer raise-budget, finish-with-current-state, or stop
```

Unknown price never becomes a zero-cost authorization.

### Validation and Review

```text
implementation reports complete
-> compute exact diff identity
-> select risk-based validation plan
-> create validation intents
-> execute required checks
-> commit results and evidence
-> repair within bounded policy if authorized
-> recompute diff and invalidate stale checks
-> build review bundle and claim-level guarantees
-> transition to awaiting-review
```

### Accept, Repair, Reject, and Roll Back

```text
accept
-> verify expected task, diff, plan, validation, and evidence revisions
-> commit acceptance decision
-> apply/commit result according to chosen acceptance mode
-> finalize factual episode
-> extract eligible deterministic facts
-> emit completed

request repair
-> bind feedback to hunks/failures
-> create new plan revision and checkpoint lineage
-> resume bounded execution

reject
-> commit rejection reason
-> preserve patch until cleanup choice

roll back
-> verify target checkpoint
-> restore worktree
-> create new state revision
-> never erase historical events
```

### Browser Reconnect

```text
client reconnects with after_sequence
-> authenticate per-launch session
-> establish live-boundary sequence
-> replay committed events after client sequence
-> send snapshot if range is unavailable or projection is stale
-> join live delivery
-> client acknowledges applied sequence
```

Correctness-bearing controls remain disabled while the client has an unresolved sequence gap.

### Coordinator or Worker Crash

```text
startup discovers incomplete run
-> inspect last event and checkpoint
-> inspect worker lease/process
-> verify repository and worktree
-> compare diff and file hashes
-> detect unresolved external-effect intent
-> classify safe-resume, reconcile-required, patch-only, or unrecoverable
-> commit recovery assessment
-> wait for user choice
```

Neither process automatically repeats an action with an ambiguous outcome.

### Pre-Work Memory and Atom Retrieval

```text
build versioned task fingerprint
-> retrieve exact compatible project facts, commands, recipes, and atoms
-> evaluate project/version/applicability/evidence/assurance gates
-> if measured need justifies it, retrieve vector candidates
-> run the same eligibility gates
-> present eligible evidence and provenance
-> record agent use, adaptation, or rejection
```

### Atom Admission and Embedding

```text
discover source-authored or SQLite-authored atom
-> validate detailed canonical name
-> parse schema-versioned documentation
-> scrub and validate every field
-> bind atom identity, version, contract, repository revision, and dependencies
-> commit immutable name and documentation revisions
-> build normalized embedding input
-> create vector with exact model/schema bindings
-> enable candidate discovery only after applicability and evidence status are valid
```

## Backend Flow Acceptance

The backend design is ready for implementation when:

* every gRPC mutation maps to one application function with explicit idempotency and expected-revision behavior;
* every external effect has durable intent/outcome and cancellation semantics;
* every state transition identifies its transaction and emitted event;
* every replay path is deterministic from SQLite state;
* every failure ends in a typed user-presentable choice rather than silent fallback;
* no transport handler, provider adapter, or repository owns hidden domain policy;
* the complete user journey can be executed using deterministic fakes before the real frontend is attached.

---

# 27C. Prototype Frontend Component and UX Specification

This section is the implementation contract for the GoWebComponents v5 client. Component names are conceptual until the v5 spike confirms exact APIs, but ownership, inputs, actions, state, empty/loading/error behavior, and accessibility requirements are fixed.

## UX Principles

The interface follows these principles:

### Conversation First

The user starts with intent, not a file tree or graph. The conversation remains the primary chronological explanation of what happened. Graph and review views deepen understanding without becoming prerequisites for routine tasks.

### Progressive Disclosure

Show the decision-relevant summary first:

```text
what is happening
why it is happening
what authority or cost it needs
what succeeded or failed
what the user can do next
```

Raw tool output, full diffs, provenance cones, contract details, and diagnostics remain one deliberate expansion away.

### Calm but Interruptible

Routine progress must not create toast spam, modal churn, or constant layout movement. Pause and Stop remain visible during active work. Approval, budget, validation failure, recovery, and user-input requirements are the only states allowed to demand immediate attention.

### Reversible by Default

Plan redirection, pause, repair, rollback, rejection, and patch preservation are first-class actions. Destructive cleanup and external effects require confirmation proportional to consequence.

### Honest State

The UI distinguishes:

* planned from running;
* streamed from durably committed;
* estimated from measured;
* unknown cost from zero cost;
* passed from waived or unavailable;
* interrupted from failed;
* candidate retrieval from eligible reuse;
* contract-checked from fully evaluated;
* disconnected presentation from stopped backend work.

### Context Without Noise

Every card answers a user question. Internal events that do not affect understanding, control, evidence, cost, or recovery may be grouped into a summary while remaining durable in SQLite.

### Identity Everywhere

Thread messages, plan steps, graph nodes, tool actions, diffs, validations, evidence, and atoms link through stable identities. The user can move from a claim to its source without searching manually.

## Canonical Visual Direction and Design References

The following six images are tracked source references for the frontend. They are illustrative design inputs, not evidence that their example task data, correctness claims, percentages, routes, or controls already exist.

| Reference | Tracked asset | Adopt | Do not copy literally |
|---|---|---|---|
| Dashboard overview | [`codeflux-dashboard-overview.png`](../design/references/ui/codeflux-dashboard-overview.png) | overall density, clear panel hierarchy, compact assurance rail, graph/status relationship | overly tall single-page composition and unsupported correctness/confidence claims |
| Balanced task graph | [`codeflux-task-graph-balanced.png`](../design/references/ui/codeflux-task-graph-balanced.png) | clean three-zone shell, readable graph, subdued controls, compact composer | making the graph the default control surface |
| Graph with live progress | [`codeflux-graph-live-progress.png`](../design/references/ui/codeflux-graph-live-progress.png) | active-node progress, graph minimap, live chat/log coexistence | simultaneous prominence of every metric and panel |
| Conversation/graph split | [`codeflux-conversation-graph-split.png`](../design/references/ui/codeflux-conversation-graph-split.png) | canonical normal workspace: chronological cards beside a contextual graph | fixed equal-height cards or automatic pane changes during work |
| Graph with live output | [`codeflux-graph-live-output.png`](../design/references/ui/codeflux-graph-live-output.png) | optional graph-focus mode and bounded live-output panel | decorative glow around every panel or a permanently exposed terminal substitute |
| Streaming execution | [`codeflux-execution-streaming.png`](../design/references/ui/codeflux-execution-streaming.png) | execution-focus mode, filtered streaming log, current-action summary | constant flow particles, excessive neon, and motion that obscures graph meaning |

### Chosen Product Character

Codeflux should feel:

```text
local
quiet
precise
technical
fast
inspectable
correctness-conscious
```

It should not feel like:

```text
a game HUD
a crypto trading dashboard
a generic admin template
a terminal wrapped in cards
a visual-programming toy
a wall of unverifiable green success states
```

The visual system is dark-first because the product is a sustained technical workspace and the references consistently use a near-black canvas. A complete light theme remains required by the token contract, but the dark theme is the first reference-quality implementation.

### Reference Priority

When the images disagree:

1. the product principles and correctness rules in this plan win;
2. the conversation/graph split is the canonical normal workspace;
3. the balanced graph and live-output compositions define user-selected focus modes;
4. the streaming execution composition defines transient active-work presentation;
5. decorative effects are reduced before information, control, accessibility, or performance is reduced.

### Canvas and Surface System

Use a cool near-black canvas rather than pure black:

| Token role | Dark-theme starting value | Use |
|---|---:|---|
| canvas | `#050A0F` | page background |
| shell | `#071019` | global bars and rails |
| surface-1 | `#0A131D` | primary panels |
| surface-2 | `#0E1924` | raised cards, selected rows |
| surface-3 | `#132231` | hover, active disclosure, inspector sections |
| border-subtle | `#182734` | normal panel and row division |
| border-strong | `#294052` | selected or structurally important boundary |
| text-primary | `#F1F6F3` | headings and primary values |
| text-secondary | `#A4B0BB` | body and explanations |
| text-muted | `#6F7F8D` | timestamps and low-priority metadata |

Values are starting points, not exemptions from contrast testing. Implement them as semantic tokens rather than literal colors in components.

Surfaces use:

* one-pixel borders before shadows;
* eight-to-twelve-pixel radii on panels and six-to-eight-pixel radii on controls;
* no glass blur, transparent frosted layers, or large floating shadows;
* subtle elevation through value and border change;
* a maximum of one visually dominant active surface at a time.

### Accent and State Color System

Brand and semantic colors must not collapse into one meaning:

| Role | Starting value | Meaning |
|---|---:|---|
| brand/action | `#55DE68` | Codeflux identity and primary safe action |
| success/passed | `#4FD36B` | completed and independently passed |
| active/running | `#3C97FF` | currently executing or selected live path |
| plan | `#A76BFA` | planned or proposed structure |
| evidence | `#E5B94B` | evidence, provenance, or cautionary proof state |
| warning | `#F0A84A` | attention without failure |
| failure/blocked | `#F06070` | failed, denied, unsafe, or blocked |
| pending/unknown | `#748392` | not started, unavailable, or unknown |
| focus ring | `#8AC7FF` | keyboard focus independent of component status |

Use color as a secondary encoding:

* every status includes text and an icon, ring, border pattern, or shape;
* semantic node class and runtime status are separate encodings;
* a plan node remains identifiable as a plan when it passes;
* waived, skipped, unavailable, and unknown never use passed styling;
* disconnected never looks like stopped;
* confidence and forecast quality never inherit success green merely because the number is high.

### Typography and Numeric Presentation

Use local system stacks:

```text
UI:
ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif

Code and logs:
ui-monospace, "Cascadia Code", "SFMono-Regular", Consolas, monospace
```

The initial type scale is:

| Role | Size/line height | Weight |
|---|---|---|
| task title | `20/28` | 600 |
| panel heading | `14/20` | 600 |
| body | `13/20` | 400 |
| compact body | `12/18` | 400 |
| metadata | `11/16` | 500 |
| metric value | `16/22` | 600 with tabular numerals |
| code/log | `12/18` | 400 |

Rules:

* no remote font request;
* use tabular numerals for time, tokens, money, progress, sequence, and test counts;
* sentence case for navigation, buttons, tabs, and headings;
* reserve uppercase for very short log-level or live-state labels;
* keep body lines near 60–85 characters in conversation cards;
* visually truncate long repository, branch, atom, and file labels only when the full value remains available through accessible name, tooltip, details, and copy.

### Spacing, Density, and Geometry

Use a four-pixel base with the primary spacing scale:

```text
4, 8, 12, 16, 24, 32
```

Desktop geometry targets:

```text
global application bar: 56px
task command header: 64–72px
task summary strip: 64–76px
expanded left rail: 232–248px
collapsed left rail: 56–64px
assurance rail: 304–336px
minimum conversation width: 480px
minimum graph width: 480px
panel gap: 8–12px
outer workspace gutter: 12–16px
```

Dense does not mean cramped:

* rows align on a consistent compact rhythm;
* panel headings, controls, and content use the same horizontal inset;
* dividers organize lists before nested cards do;
* avoid cards inside cards unless the inner boundary represents a different action or authority;
* no body text smaller than the accessible compact token;
* long task histories virtualize rather than shrinking.

### Shell and Navigation

The shell has four conceptual regions:

1. global application bar;
2. left navigation/task rail;
3. elastic task workspace;
4. right assurance/context rail.

The left rail contains:

```text
New Task
Tasks
Graphs, when a cross-task graph index exists
Memory
Repositories
Settings
recent task list
local coordinator/database health
```

`Experiments` is hidden until an actual experiment feature is enabled. Evidence remains task-contextual until a cross-task evidence route has a real use case.

The selected navigation item uses a surface and leading bar, not only a green label. Recent task rows show title, state icon, attention state, and relative activity. The local-data footer distinguishes coordinator, database, and provider health rather than asserting `All Systems Operational` from one Boolean.

### Task Header and Summary Strip

The task command header is the stable control zone:

```text
task title
short requirement summary
task-state badge
Pause or Resume
Stop
Request Review when eligible
overflow for secondary actions
```

Stop remains available for every active phase but is not styled as the primary action. Request Review never appears available when the diff or required checks are stale.

The summary strip contains at most six groups. Candidate groups are:

```text
correctness profile before validation or achieved evidence after validation
calibrated forecast confidence with explanation
progress or current phase
model/effort when it affected the plan
estimated remaining time and measured elapsed time
actual cost / estimated final cost / hard cap
required gates passed / total
```

Do not show all groups merely because data exists. Use one consistent label/value/help pattern. Sparklines appear only when a meaningful time series exists; a single forecast is not converted into decorative chart noise.

### Workspace Modes

The same route supports three user-controlled compositions.

#### Conversation Mode

Default for requirement, planning, approval, review, repair, and ordinary follow-up.

```text
conversation 55–60%
graph 40–45%
assurance rail visible when width permits
composer anchored to conversation
```

Forecast, Plan, Execution, Validation, and Evidence are chronological full-width cards inside the conversation. The graph remains contextual and never displaces a blocking approval.

#### Graph Focus Mode

Selected explicitly when topology, execution path, or evidence provenance is the current question.

```text
graph occupies the elastic workspace
conversation becomes a compact lower pane or drawer
assurance rail remains available
node inspector replaces assurance panels rather than creating a fourth column
```

The mode must still expose Pause, Stop, approval attention, budget warnings, and the composer.

#### Execution Focus Mode

Available while work is running:

```text
graph in the upper elastic region
timeline, filtered execution log, current-action card, or compact conversation below
active command output summarized and bounded
```

Raw output is opt-in and is not an embedded general-purpose terminal. The mode may be suggested on first active entry, but it must not replace a layout the user already chose.

Mode, split, collapsed rails, selected graph mode, viewport, and selected node are ephemeral preferences preserved across compatible replay. They are not correctness-bearing task state.

### Conversation and Card Design

The conversation is a chronological work record, not a sequence of oversized chat bubbles.

* user messages use a quiet bounded container or aligned text block;
* agent narrative uses readable text without a mascot header on every message;
* system activity uses typed full-width cards;
* Forecast, Plan, Execution, Validation, Evidence, Approval, Recovery, and Completion cards each have one recognizable icon and status;
* cards use a compact heading row, decision-relevant summary, and progressive disclosure;
* repeated progress updates mutate the active card through ordered events rather than stacking duplicate cards;
* card actions align consistently at the lower or upper trailing edge;
* timestamps and sequence identity remain secondary but inspectable;
* raw tool output and large diffs stay collapsed by default.

The composer is a calm persistent control:

```text
multiline input
explicit file/symbol/node attachment
policy or budget controls behind disclosure
send
state-aware Pause/Resume/Stop access
```

The green send action appears only when the draft is valid and the command can be issued. During reconnect uncertainty, the draft remains editable but mutation clearly waits for sequence certainty.

### Graph Visual Language

The graph uses a dark dotted or low-contrast grid only when it improves orientation. The grid must disappear in high-contrast mode if it reduces clarity.

Node visual class:

```text
operation/code: restrained green family
external adapter or active test/tool boundary: blue family
plan or proposed strategy: purple family
evidence or provenance: amber family
memory: neutral/teal family
unknown or opaque: neutral gray
```

Runtime status is an overlay:

```text
passed: check icon
running: progress ring and active border
pending: hollow indicator
warning: warning icon
failed: failure icon and border
blocked: stop/lock icon
invalidated: struck or broken-evidence treatment
```

Graph rules:

* one stable directed layout;
* no primary force-directed mode;
* node labels use one or two lines with full accessible text;
* edge direction remains visible at normal zoom;
* current execution path receives stronger contrast, not a field of continuous particles;
* only the selected/current node may use a restrained halo;
* queued, retried, and blocked states remain visually distinct;
* controls occupy one predictable corner;
* legend and semantic/status explanations are always reachable;
* minimap appears when the visible slice exceeds the viewport or measured node threshold;
* Fit, zoom, search, isolate, reset, and lock-layout controls use labeled tooltips and keyboard equivalents;
* pan/zoom never steals ordinary page scrolling when focus is outside the viewport;
* automatic focus never pulls the viewport away from deliberate inspection; show Return to current action.

### Assurance and Context Rail

The right rail is not a second dashboard competing with the task.

Default sections:

```text
Task details
Correctness gates
Measured metrics
Related review/evidence
```

When a graph node, claim, check, artifact, or timeline identity is selected, its inspector replaces or leads the rail and provides a clear Back to task summary action.

Rules:

* sections are collapsible and remember only ephemeral preference;
* Task details shows human labels first and IDs in copyable details;
* correctness gates show required, advisory, running, passed, failed, waived, skipped, unavailable, and stale accurately;
* metrics name unit, window, source, and whether values are forecast or measured;
* charts require enough points to convey a trend and provide an accessible table or summary;
* related items use their real status and open in the existing review/graph/timeline context;
* unknown values display `Unknown`, never `0`, `100%`, `Healthy`, or `Passed`.

### Honest Claims Versus Mockup Copy

The references contain persuasive but potentially unsafe labels. Production rules are:

* `Correctness: High` before validation becomes the selected correctness profile or target, not an achieved claim;
* `Evidence: Strong` appears only when an evidence policy defines and supports that level;
* `Confidence 92%` appears only from a versioned calibrated forecast and links to its basis;
* `Acceptance likelihood` is omitted until an independently evaluated calibrated model exists;
* `All systems operational` is replaced by individually attributable local health when partial failure is possible;
* `All gates passed` is bound to the exact diff, validation revision, policy, and time;
* green presentation never substitutes for the evidence report.

### Motion and Live-State Treatment

Motion communicates change rather than ambience.

Starting tokens:

```text
instant feedback: 80–120ms
control and disclosure: 140–180ms
pane transition: 180–240ms
graph patch interpolation: 160–240ms
```

Rules:

* no perpetual panel glow;
* no decorative edge particles in the normal graph;
* at most one subtle running pulse or progress ring per active region;
* do not animate large graph re-layout without preserving the selected node and viewport;
* stop animation when the page is hidden;
* reduced motion removes pulse, glow, streaming movement, and animated pan while preserving state changes;
* never use motion as the only indication that execution is active.

### Responsive Composition

Breakpoints are determined by minimum readable content widths:

```text
wide: >= 1440px
    expanded task rail + conversation/graph + assurance rail

standard: 1180–1439px
    collapsible task rail + conversation/graph + assurance drawer or narrow rail

compact: 800–1179px
    overlay task rail + conversation/graph tabs + context drawer

minimum supported: < 800px
    conversation-first single pane; graph/review/context are full-screen drawers
```

The prototype is desktop-first, but narrow mode must preserve the ability to read state, answer approval, Pause, Stop, inspect failure, and send a correction. No supported width may hide a blocking authority request behind an undiscoverable panel.

### Visual Acceptance Matrix

Before the shell is accepted, render deterministic fixtures for:

```text
empty repository/thread
forecast and plan awaiting approval
running with graph collapsed
running in Conversation Mode
running in Graph Focus Mode
running in Execution Focus Mode
approval required
budget warning and hard cap
validation running, failed, waived, and passed
disconnected and replaying
recovery required
completed and awaiting review
long names, large numbers, unknown price, and stale evidence
wide, standard, compact, and minimum widths
dark, light, high contrast, and reduced motion
```

Reference comparison evaluates hierarchy, density, alignment, legibility, stable layout, and status honesty. Pixel similarity to the concept images is not the goal; preserving the extracted design principles is.

## User-Facing Terminology

Use one term consistently:

| Internal concept | User-facing term |
|---|---|
| thread | Thread |
| task | Task |
| run | Attempt, only when multiple attempts matter |
| plan revision | Plan revision |
| worker | Agent process, only in diagnostics |
| graph revision | Graph revision |
| validation run | Check or Validation |
| permission grant | Approval |
| checkpoint | Checkpoint |
| retrieval candidate | Suggested prior work |
| influential artifact | Reused project knowledge |
| recovery-required | Needs recovery |

Do not call a thread a task or a task a run. Do not present internal IDs as primary labels; expose them in details and copy actions.

## Route Map

The prototype has these routes:

```text
/
    repository chooser or last valid workspace

/workspace/{repository_id}/thread/{thread_id}
    primary chat and graph workspace

/workspace/{repository_id}/memory
    project-memory inspection and correction

/settings
    providers, models, policies, budgets, appearance, data

/diagnostics
    health, versions, recovery-required tasks, logs, backup/export

/first-run
    local architecture, provider setup, repository selection
```

Review normally opens as a pane or drawer within the thread route so context is preserved. A dedicated review sub-route may be added if deep linking requires it, but it uses the same review state.

## Component Tree

```text
AppRoot
├─ SessionBootstrap
├─ AppRouter
├─ GlobalErrorBoundary
├─ GlobalShortcutManager
├─ AccessibilityAnnouncer
├─ DialogHost
├─ ToastHost
└─ AppShell
   ├─ ApplicationBar
   │  ├─ BrandMark
   │  ├─ RepositoryBreadcrumb
   │  ├─ WorktreeIndicator
   │  ├─ LocalHealthSummary
   │  ├─ GlobalSearchButton
   │  ├─ ShortcutHelpButton
   │  ├─ SettingsButton
   │  └─ LocalProfileMenu
   ├─ TaskWorkspaceHeader
   │  ├─ TaskStateBadge
   │  ├─ ConnectionBadge
   │  ├─ PauseResumeButton
   │  ├─ StopButton
   │  ├─ RequestReviewButton
   │  └─ TaskOverflowMenu
   ├─ TaskSummaryStrip
   │  ├─ CorrectnessEvidenceSummary
   │  ├─ ForecastConfidenceSummary
   │  ├─ ProgressPhaseSummary
   │  ├─ ModelEffortSummary
   │  ├─ TimeSummary
   │  ├─ CostBudgetSummary
   │  └─ GateSummary
   ├─ ThreadRail
   │  ├─ NewThreadButton
   │  ├─ PrimaryNavigation
   │  ├─ ThreadFilters
   │  ├─ VirtualThreadList
   │  │  └─ ThreadRow
   │  ├─ LocalDataHealth
   │  └─ RailPaginationSentinel
   ├─ WorkspaceLayout
   │  ├─ WorkspaceModeControls
   │  ├─ ConversationPane
   │  │  ├─ ThreadHeader
   │  │  ├─ ContextBanner
   │  │  ├─ VirtualTimeline
   │  │  │  └─ TimelineItemRenderer
   │  │  │     ├─ UserMessage
   │  │  │     ├─ AgentMessage
   │  │  │     ├─ RequirementCard
   │  │  │     ├─ ForecastCard
   │  │  │     ├─ PlanCard
   │  │  │     ├─ ContextSelectionCard
   │  │  │     ├─ ToolActivityCard
   │  │  │     ├─ ApprovalCard
   │  │  │     ├─ CheckpointCard
   │  │  │     ├─ ValidationCard
   │  │  │     ├─ DiffSummaryCard
   │  │  │     ├─ CostBudgetCard
   │  │  │     ├─ RecoveryCard
   │  │  │     ├─ ErrorCard
   │  │  │     └─ CompletionCard
   │  │  ├─ NewEventsButton
   │  │  └─ Composer
   │  │     ├─ AttachmentChips
   │  │     ├─ PolicySelector
   │  │     ├─ BudgetEditor
   │  │     ├─ ModelEffortOverride
   │  │     └─ ComposerActions
   │  └─ GraphPane
   │     ├─ GraphToolbar
   │     ├─ GraphModeTabs
   │     ├─ GraphSearch
   │     ├─ GraphViewport
   │     │  ├─ EdgeLayer
   │     │  ├─ NodeLayer
   │     │  ├─ GraphLegend
   │     │  ├─ GraphMinimap
   │     │  └─ GraphEmptyOverlay
   │     └─ ReturnToCurrentAction
   │  └─ ExecutionFocusRegion
   │     ├─ LiveExecutionTimeline
   │     ├─ FilteredExecutionLog
   │     └─ CurrentActionCard
   ├─ AssuranceRail
   │  ├─ TaskDetailsPanel
   │  ├─ CorrectnessGatesPanel
   │  ├─ MetricsPanel
   │  ├─ RelatedEvidencePanel
   │  └─ ContextInspector
   ├─ ReviewDrawer
   │  ├─ ReviewHeader
   │  ├─ ChangedFileList
   │  ├─ DiffViewer
   │  ├─ EvidenceSummary
   │  ├─ ValidationDetails
   │  ├─ CostSummary
   │  └─ ReviewActions
   └─ StatusRegion
```

Settings, memory, diagnostics, first-run, dialogs, and responsive drawers are route-level siblings of the primary workspace rather than children of the timeline.

## Frontend State Ownership

### Authoritative Remote State

The client derives this state from snapshots and ordered events:

```text
repositories and workspace state
threads and committed messages
tasks, plans, runs, and task state
approvals and grants
forecast, budget, reservation, usage, and cost
tool and model operation summaries
checkpoints and recovery assessments
diff, validation, evidence, and acceptance
graph identities, revisions, nodes, edges, and layout hints
memory artifacts, lineage, validity, and influence
settings revisions and provider/model catalog
```

### Ephemeral Client State

```text
selected route
selected graph node
graph viewport and temporary filters
pane sizes and collapsed state
expanded cards and active tabs
unsent drafts and local attachment selection
scroll position and auto-follow state
open dialog, drawer, or popover
temporary form validation
pending command indicator
```

The browser does not invent a durable task transition. Optimistic UI is limited to reversible presentation such as a pending message row, disabled button, draft, or local pane state. The server's committed result replaces it.

## Frontend Stores and Reducers

Conceptual stores are:

```go
type SessionStore struct {
    Connection ConnectionProjection
    LastAppliedSequence Sequence
}

type WorkspaceStore struct {
    Repository WorkspaceProjection
}

type ThreadStore struct {
    Threads ThreadCollection
    Pages map[ThreadID]TimelinePages
    Drafts map[ThreadID]Draft
}

type TaskStore struct {
    Tasks map[TaskID]TaskProjection
}

type GraphStore struct {
    Slices map[GraphViewKey]GraphProjection
    Selection GraphSelection
}

type ReviewStore struct {
    Bundles map[TaskID]ReviewProjection
}

type SettingsStore struct {
    Effective EffectiveSettingsProjection
}

type UIStore struct {
    Layout LayoutPreferences
    Dialogs DialogState
    Notifications NotificationState
}
```

Required reducer and query functions are:

```go
func ApplySessionSnapshot(state AppState, snapshot SessionSnapshot) (AppState, error)
func ApplySessionEvent(state AppState, event SessionEvent) (AppState, error)
func ApplyMessageDelta(state ThreadProjection, event MessageDelta) ThreadProjection
func FinalizeMessage(state ThreadProjection, event MessageFinal) ThreadProjection
func ApplyTaskTransition(state TaskProjection, event TaskStateChanged) (TaskProjection, error)
func ApplyBudgetUpdate(state BudgetProjection, event BudgetUpdated) BudgetProjection
func ApplyApprovalUpdate(state ApprovalProjection, event ApprovalEvent) ApprovalProjection
func ApplyValidationUpdate(state ReviewProjection, event ValidationUpdated) ReviewProjection
func ApplyGraphPatch(state GraphProjection, patch GraphPatch) (GraphProjection, error)
func MergeThreadPage(state ThreadProjection, page MessagePage) ThreadProjection
func ShouldAutoFollow(scroll ScrollState, event SessionEvent) bool
func AvailableTaskActions(task TaskProjection, connection ConnectionProjection) []TaskAction
```

Reducers are pure and deterministic. An impossible transition or graph patch revision mismatch triggers snapshot repair; it is not ignored.

## Shared Primitive Components

The design system includes:

```text
Button
IconButton
ToggleButton
Menu and MenuItem
Tabs and Tab
Dialog
Drawer
Popover
Tooltip
TextField and TextArea
Select
Number/CurrencyInput
Badge
ProgressIndicator
Skeleton
InlineAlert
Disclosure
VirtualList
ResizableSplit
CopyButton
ExternalLink
CodeBlock
EmptyState
ErrorState
```

Every primitive defines keyboard behavior, focus behavior, accessible name, disabled and busy states, high-contrast behavior, reduced motion, and pointer target size before feature components depend on it.

## Root and Shell Component Contracts

### `AppRoot`

Inputs: embedded build metadata and browser environment.

Owns: top-level service client, stores, route integration, error boundary, and cleanup.

States: booting, ready, incompatible, unauthorized, coordinator unavailable, database unavailable.

Actions: retry bootstrap, reload compatible client, open diagnostics.

### `SessionBootstrap`

Calls health/version, authenticates the per-launch session, restores a safe last route, fetches initial settings/workspace state, and starts the session stream. It never persists the launch secret in durable browser storage.

### `AppShell`

Owns only layout. It never fetches task data directly. Wide mode uses the task rail, elastic workspace, and assurance rail. Conversation Mode splits the elastic workspace between chat and graph. Graph Focus and Execution Focus reuse the same components without creating separate sources of state. Medium mode overlays the task rail and may convert assurance to a drawer. Narrow mode switches conversation, graph, review, and context through state-preserving tabs/drawers.

### `GlobalErrorBoundary`

Catches rendering failures, preserves current route and draft when possible, offers component retry, and links to a redacted diagnostic action. It does not convert protocol or domain errors into generic crashes.

### `AccessibilityAnnouncer`

Announces only meaningful state changes: connection restored/lost, approval required, task paused/completed/failed, validation failure, and recovery required. Token deltas and routine progress are not announced.

## Application and Task Header Contracts

`ApplicationBar`, `TaskWorkspaceHeader`, and `TaskSummaryStrip` consume compact immutable view models; children do not issue independent fetches.

`ApplicationBar` carries global identity, repository/worktree selection, attributable local health, search/help/settings, and the local profile. It does not carry task actions that would become ambiguous when no task is selected.

`RepositoryBreadcrumb` shows repository, branch, and worktree with copyable full values in details. Dirty/conflicted/detached state is text plus icon, not color alone.

`LocalHealthSummary` distinguishes coordinator, database, provider, stream, and update state. A partial failure cannot render as one green `All systems operational` claim.

`TaskWorkspaceHeader` carries task title, concise requirement, state, connection certainty, Pause/Resume, Stop, Request Review, and secondary task actions. It remains visible in Conversation, Graph Focus, Execution Focus, and review compositions.

`TaskStateBadge` shows task state and phase separately. Clicking it opens a concise timeline/status explanation.

`ConnectionBadge` distinguishes live, replaying, degraded, and disconnected. Disconnected never implies the backend task stopped.

`TaskSummaryStrip` renders at most six decision-relevant metric groups at once. It uses stable columns at wide sizes and a horizontally scrollable or wrapped summary at compact sizes without hiding Stop or blocking attention.

`ModelEffortSummary` shows actual selected provider/model/effort and marks manual overrides when this information is decision-relevant.

`CostBudgetSummary` shows forecast range, actual cost, estimated final cost, remaining cap, unknown-pricing state, and warning threshold. Estimated and actual figures use distinct labels.

`CorrectnessEvidenceSummary` shows the selected correctness profile before evidence exists and achieved evidence only after validation supports it. It does not turn a target into a result.

`PauseResumeButton` and `StopButton` derive availability from `AvailableTaskActions`; they use idempotency keys and busy states. Stop remains visible during every active phase.

`RequestReviewButton` appears only when a reviewable diff exists. Its busy, stale, required-check-running, and disconnected behavior is explicit.

## Thread Rail Contracts

`ThreadRail` owns filters, pagination, selection, and responsive overlay state.

`PrimaryNavigation` includes only implemented destinations. Experiments, global Evidence, or global Graphs do not appear as empty aspirational routes.

`ThreadRow` displays title, state, last activity, attention requirement, repository/task association, and unread marker. Attention states rank pending approval, recovery, validation failure, and user input above routine running state.

`LocalDataHealth` shows attributable coordinator and SQLite state, local-only posture, and a diagnostics action. It does not expose raw paths or imply provider health from database health.

The rail has explicit:

* loading skeleton;
* no threads;
* no matching filter results;
* pagination error with retry;
* repository unavailable;
* archived view.

Creating a thread adds a local pending row keyed by the command idempotency key. The committed thread replaces it without changing focus.

## Timeline Contracts

`VirtualTimeline` renders presentation items keyed by durable sequence or stable message identity. It loads newest content first, prepends older pages while preserving the scroll anchor, and auto-follows only when the user is near the bottom.

`TimelineItemRenderer` is an exhaustive registry. Unknown event kinds render an inspectable fallback with event type, time, and sequence; they never disappear silently.

### Message Components

`UserMessage` and `AgentMessage` render safe Markdown, code blocks, copy actions, completion/interruption status, stable identity chips, and optional details. Unsafe HTML and URL schemes are rejected.

Streamed text remains visibly in progress until `message_final`. Refresh/replay replaces deltas with durable final content.

### `RequirementCard`

Shows interpreted goal, constraints, assumptions, unresolved ambiguity, explicit files/symbols, risk cues, and the action to correct interpretation before approval.

### `ForecastCard`

Shows P50/P90 time, tokens, and cost; model/effort; validation profile; uncertainty reasons; missing-price state; and budget editor. It explains that a range is an estimate, not a promise.

### `PlanCard`

Shows scope, ordered steps, expected files, planned checks, risks, authority needs, and completion criteria. It supports approve, request change, and compare revision. Superseded plans are visibly historical and cannot be approved.

### `ContextSelectionCard`

Shows selected files/symbols grouped by reason, repository revision, budget use, exclusions, and an explanation action. It does not dump full source into the timeline.

### `ToolActivityCard`

Shows tool, purpose, scope, state, duration, exit status, and concise result. Output is redacted, paginated, and collapsed. Active cards update in place rather than adding one row per progress chunk.

### `ApprovalCard`

Shows exact action, arguments summarized safely, resolved target/scope, reason, consequences, expiry, and whether a previous grant matched. Actions are Allow once, Allow for this task, and Deny. Once resolved, buttons disappear and the attributable decision remains.

### `CheckpointCard`

Shows checkpoint time, plan step, diff summary, and reason. Restore appears only when policy and current state permit it.

### `ValidationCard`

Shows required/advisory checks, running progress, pass/fail/waived/skipped/unavailable states, revision binding, baseline comparison, and details. A waived check never uses success styling.

### `DiffSummaryCard`

Shows changed files, additions/deletions, source/test/config/generated categories, scope warnings, and Open review. It does not imply acceptance.

### `CostBudgetCard`

Appears for warning, cap, unknown price, material estimate change, or approved increase—not every usage tick.

### `RecoveryCard`

Shows last verified checkpoint, detected divergence or ambiguity, safe choices, and why automatic resume is unavailable. It distinguishes safe resume, reconcile, preserve patch, and abandon.

### `ErrorCard`

Shows a safe explanation, stable code, affected action, retryability, and next steps. Raw stack traces remain diagnostics-only.

### `CompletionCard`

Shows accepted/unaccepted completion state, files, validation, evidence, cost, assumptions, limitations, memory influence, and actions for review, repair, accept, rollback, or new related task.

## Composer Contract

The composer owns a per-thread local draft:

```go
type Draft struct {
    Text string
    Attachments []RepositoryAttachment
    PolicyOverride *PolicyPreset
    BudgetOverride *Money
    ModelOverride *ModelID
    EffortOverride *EffortLevel
}
```

It supports:

* multiline text;
* explicit submit and newline keyboard behavior;
* repository file/symbol attachment through server identities;
* policy and budget controls;
* optional model/effort override;
* send retry using the same idempotency key;
* pause/resume/stop variants according to task state.

Draft text is not sent for autocomplete or remote persistence. It clears only after committed message confirmation.

## Workspace Composition Contracts

`WorkspaceLayout` owns only the selected composition, split sizes, collapsed regions, and responsive presentation. It does not change task, graph, validation, or conversation state.

`WorkspaceModeControls` offers Conversation, Graph Focus, and Execution Focus only when the corresponding content exists. The selected mode is visible as text plus state, keyboard reachable, and preserved per thread.

Conversation Mode:

* renders `ConversationPane` and `GraphPane` through one adjustable split;
* gives blocking timeline actions priority over graph space;
* keeps the composer attached to the conversation;
* permits the graph to collapse without removing any task command.

Graph Focus Mode:

* expands `GraphPane` into the elastic workspace;
* moves conversation into a compact drawer or lower region;
* keeps task controls, approval attention, budget warnings, and composer access;
* routes selection details through `ContextInspector`.

Execution Focus Mode:

* renders the graph above or beside `ExecutionFocusRegion` according to available width;
* permits one of timeline, filtered log, current action, or compact conversation to be primary in the lower region;
* bounds log rows and output bytes;
* never becomes a general terminal or source editor;
* retains a clear action to return to Conversation Mode.

The first active execution may suggest Execution Focus. After the user changes mode, no event may switch it automatically. Reconnect and replay restore the last compatible mode without animation or viewport loss.

`LiveExecutionTimeline` shows material phase boundaries and their durable time. It is not a duplicate of every log line.

`FilteredExecutionLog` groups and filters safe redacted events by All, Info, Success, Warning, and Error. It preserves sequence, source, duration, and expandable bounded details.

`CurrentActionCard` shows the active tool or check, purpose, scope, progress when measurable, elapsed time, estimated remaining time only when supported, cancellation behavior, and a details link.

## Graph Component Contracts

`GraphPane` owns mode, query, selection, viewport, and inspector coordination. It does not own authoritative graph data.

`GraphModeTabs` exposes Program, Execution, and Evidence with a short explanation. Execution becomes default while running; Evidence becomes default after validation/completion unless the user has deliberately selected another mode.

`GraphToolbar` provides search, fit, reset, expand, isolate cone, revision compare, legend, and collapse.

`GraphViewport` renders one bounded slice using server layout hints. It preserves viewport and selection across compatible patches.

`EdgeLayer` renders relationship style and direction before `NodeLayer` so nodes remain interactive. `NodeLayer` renders full accessible labels even when visual text is truncated.

`GraphMinimap` appears only when the graph exceeds a measured viewport/node threshold. It is keyboard skippable, has a text alternative, and never replaces Fit or search.

`ReturnToCurrentAction` appears when live execution advances outside the user's deliberately inspected viewport. It never auto-pans the graph while the user is exploring.

`GraphEmptyOverlay` distinguishes no task graph, no nodes in filter, loading, unavailable revision, and graph-disabled-by-policy.

`ContextInspector` shows the selected node, claim, check, artifact, or timeline identity. For a graph node it shows the full descriptive atom/node name, stable ID, revision, class, status, contract summary, inputs/outputs/effects, evidence, cost/time, related messages, source, and actions. Unknown fields say `Unknown` or `Not modeled`; they are not blank.

## Assurance Rail Contracts

`AssuranceRail` owns disclosure and responsive drawer state only. Its data comes from task, review, graph, and metric projections.

`TaskDetailsPanel` shows milestone or task phase, plan revision, creation/update time, repository/worktree, owner when meaningful, and copyable internal IDs under details.

`CorrectnessGatesPanel` sorts blocking required checks first, followed by running, failed, warning, passed, waived, skipped, unavailable, and advisory checks. Its summary includes the exact numerator/denominator and revision binding.

`MetricsPanel` renders measured values before charts. A chart requires units, source, time window, enough samples, and an accessible summary/table. Unknown price, time, or confidence remains unknown.

`RelatedEvidencePanel` links review identity, diff, checks, evidence, accepted memory, and relevant external artifacts through stable identities. It does not invent a PR or design-note artifact merely to populate the panel.

Selecting an inspectable identity promotes `ContextInspector` to the rail. Closing or going back restores previous disclosure and scroll state.

## Review Drawer Contracts

`ReviewDrawer` binds to one task, diff identity, plan revision, validation revision, evidence revision, and graph revision.

`ChangedFileList` supports classification filters and warnings.

`DiffViewer` shows one file at a time, safe syntax presentation, whitespace control, hunk identity, and links to plan/actions/validation. It is a reviewer, not an editor.

`EvidenceSummary` presents claim-level guarantees and provenance. `ValidationDetails` presents exact commands, statuses, durations, revisions, and redacted output.

`ReviewActions` provides:

```text
Accept
Request repair
Reject while preserving patch
Roll back
Open in editor
```

Accept is disabled while required checks run or when the review is stale. If a required check failed or was waived, acceptance requires explicit acknowledgement and remains accurately recorded.

## Settings Components

`ProviderSettings` lists provider configurations by non-secret reference, status, endpoint, and last test. It supports add/update/test/delete without ever displaying an existing secret.

`ModelCatalogSettings` shows model capabilities, pricing snapshot status, and availability.

`PolicySettings` edits defaults for correctness/speed/cost, validation floors, concurrency, and task budgets.

`AppearanceSettings` controls theme, density, reduced motion, and persisted pane preferences.

`DataSettings` shows database location, size, backup, integrity check, retention, memory export, and deletion.

Repository-provided settings appear separately with source, requested capability, and approve/reject actions.

## Memory Components

`MemoryList` filters facts, commands, mappings, recipes, atoms, observations, and invalidated items.

`MemoryArtifactDetails` shows project scope, revision/bindings, maturity, evidence, lineage, aliases, retrieval history, influence history, vector configuration, and invalidation.

`MemoryActions` supports correction by revision, quarantine, invalidation, export, and deletion with descendant preview.

The UI never offers a button that promotes an advisory observation directly into validated authority.

## Diagnostics and First-Run Components

`FirstRunWizard` has short resumable steps:

```text
local architecture and data promise
-> provider configuration and test
-> repository selection and inspection
-> worktree/permission explanation
-> first thread
```

It has Skip where safe, Back without losing entered non-secret data, and a clear recovery path when provider setup fails.

`DiagnosticsPage` shows app/API/schema/frontend versions, health checks, provider status, worktree/disk/database state, active/recovery tasks, and redacted logs. Backup and export begin with scope/size preview.

## Command Functions

UI command methods wrap generated gRPC clients:

```go
func SendMessage(ctx context.Context, input SendMessageInput) CommandState
func ApprovePlan(ctx context.Context, input ApprovePlanInput) CommandState
func StartTask(ctx context.Context, input StartTaskInput) CommandState
func PauseTask(ctx context.Context, input PauseTaskInput) CommandState
func ResumeTask(ctx context.Context, input ResumeTaskInput) CommandState
func StopTask(ctx context.Context, input StopTaskInput) CommandState
func ResolveApproval(ctx context.Context, input ResolveApprovalInput) CommandState
func ChangeBudget(ctx context.Context, input ChangeBudgetInput) CommandState
func RequestRepair(ctx context.Context, input RepairInput) CommandState
func AcceptChange(ctx context.Context, input AcceptInput) CommandState
func RejectChange(ctx context.Context, input RejectInput) CommandState
func RollbackTask(ctx context.Context, input RollbackInput) CommandState
func OpenSource(ctx context.Context, input OpenSourceInput) CommandState
```

Each command owns one idempotency key until committed or explicitly abandoned. Buttons become busy, not optimistically successful. Stale-revision errors refresh authoritative state and explain what changed.

## Task State and Available Action Matrix

| Task state | Primary message | Available primary actions |
|---|---|---|
| Draft | Describe or refine the requirement | Send, change policy/budget |
| Forecasting | Estimating scope and cost | Stop |
| Awaiting plan approval | Review plan before work begins | Approve, request change, stop |
| Ready | Plan approved and prerequisites valid | Start, change budget, stop |
| Running | Agent is working | Pause, stop, inspect graph |
| Awaiting authority | A scoped action needs approval | Allow once, allow for task, deny, stop |
| Paused | Work is checkpointed | Resume, change budget, review, stop |
| Validating | Checks are running | Pause where safe, stop, inspect checks |
| Awaiting review | Work is ready for a decision | Review, accept, repair, reject, rollback |
| Needs recovery | Stored state and external state diverged | Safe resume, reconcile, preserve patch, abandon |
| Completed | Change was accepted | Inspect evidence, start related task |
| Failed | Attempt ended with unresolved failure | Inspect, repair/retry if eligible, preserve patch |
| Cancelled | Active attempt was stopped | Inspect, preserve patch, new attempt |
| Rolled back | Worktree restored to checkpoint | Resume from new plan or finish |

Unavailable actions are omitted or disabled with an explanation. They do not fail only after a click.

## Detailed Frontend Flows

### First Run

The user sees the local data promise before provider setup. Secret fields support paste and immediate test but never reveal stored values. Failure identifies authentication, network, model, or endpoint problems. Repository selection follows only after the coordinator is healthy.

### New Thread and Requirement

Selecting New thread focuses the composer. Sending creates one pending bubble. The committed message replaces it, then Forecast and Plan cards appear in chronological order. If planning takes longer than the defined latency threshold, show the current phase and Stop rather than an indeterminate spinner.

### Plan Review

The plan card highlights scope, validation, risk, budget, and assumptions before detailed steps. The default focus lands on the plan heading, not Approve. A changed plan resets approval, shows a revision comparison, and retains prior revisions as history.

### Live Work

Routine reads and progress update existing cards. Material edits, approvals, validation, budget warnings, and plan changes create distinct timeline items. The graph highlights the current path without auto-panning away from a user's deliberate inspection; a Return to current action button appears instead.

### Approval

The approval card receives focus announcement but does not steal keyboard focus while the user is typing. The exact action and scope appear before buttons. A denial records the decision and the agent either replans visibly or stops; it does not quietly choose another equivalent effect.

### Review

Open review preserves chat and graph position. The drawer begins with outcome, risks, required checks, and changed files. The user can follow a claim to validation, a validation to output, a hunk to plan step, and a graph node to source.

### Repair

The user may attach feedback to a file, hunk, validation failure, graph node, or general task. Submitting repair creates a new plan revision and checkpoint lineage. Previous accepted evidence remains historical and visibly stale for the new diff.

### Reconnect

The connection badge changes immediately. The timeline remains readable but mutation controls disable if sequence certainty is lost. After replay, controls re-enable and one subtle announcement says the session is live. Duplicated cards or repeated approvals are unacceptable.

### Recovery

Recovery begins with what is safely known, what changed, and what is ambiguous. Recommended safe action appears first, but destructive abandonment is not visually dominant. Preserve patch remains available whenever possible.

### Graph Exploration

Selecting a message identity chip opens the graph if collapsed, chooses the correct mode/revision when possible, focuses the node, and opens the inspector. Selecting a graph node highlights related timeline items without filtering unrelated history away.

### Budget

Budget warnings appear in the top bar and one timeline card. At the hard cap, the UI explains whether an in-flight request is settling, what state is checkpointed, and choices to raise the limit, finish with current work, or stop.

## Empty, Loading, Error, and Offline States

Every data-owning component implements:

```text
not requested
loading with stable layout
ready with data
ready but empty
partial/stale data
recoverable error
permission/policy denied
incompatible version
disconnected
```

Skeletons resemble final layout and stop after a bounded time. Spinners never replace actionable error text. A retry action states what will be retried and does not duplicate the original mutation.

## Focus, Keyboard, and Accessibility

* all functions are keyboard reachable;
* focus order follows rail, conversation, composer, graph, inspector, and review;
* new content does not steal focus;
* dialogs trap and restore focus;
* graph nodes support directional or ordered keyboard traversal;
* every status uses text/icon/shape in addition to color;
* full atom names remain available to assistive technology even when visually truncated;
* reduced motion disables non-essential graph and pane animation;
* screen-reader announcements are concise and rate-limited;
* shortcuts do not fire inside text entry unless explicitly modified;
* target sizes and contrast meet the chosen accessibility baseline.

## Frontend Telemetry

Local telemetry records product events in SQLite without keystroke or hidden-content capture:

```text
first-run step completion and failure class
time to first thread/message/plan/diff
plan approval or revision
pause, stop, and recovery use
approval response time
review opened and acceptance decision
graph opened, mode used, and node-link navigation
memory inspected or corrected
frontend error and reconnect duration
long-task and slow-render measurements
```

Metrics are locally inspectable and deletable. They support product evaluation, not behavioral surveillance.

## Frontend Flow Acceptance

The detailed frontend design is ready when:

* every server event has one explicit reducer and presentation or documented grouping rule;
* every user command has busy, success, stale, denied, disconnected, and retry behavior;
* every task state exposes the correct next actions;
* every major component defines loading, empty, partial, error, and accessibility behavior;
* the complete journey is keyboard usable;
* the graph can remain collapsed without blocking work;
* a user can explain current state, cost, authority, evidence, and next choice without reading raw logs.

---

# 27D. Prototype Developer Experience

The development environment should make the correct path the easiest path. A contributor must be able to locate the governing plan/TODO, start a deterministic local system, add one vertical capability, verify it at the right layers, replay failures, and inspect the resulting state without learning hidden commands.

## Developer-Experience Principles

* one repository contains server, worker, frontend, protobuf, migrations, tests, and documentation;
* one cross-platform Go development helper owns common commands;
* generated code is deterministic, clearly marked, and never hand-edited;
* local development works without a paid provider through deterministic fakes;
* every external integration has a fake or contract fixture;
* every durable workflow can be replayed from an isolated SQLite database;
* every test creates isolated database, repository, worktree, credential, port, and process resources;
* failures leave useful artifacts but no secrets;
* every repository-local disposable artifact is written beneath the ignored `.artifacts/` root;
* agents create no new Markdown file unless the user explicitly requests that specific file;
* the same command graph runs locally and in CI;
* agent instructions reference only commands and paths that actually exist.

## Planned Repository Layout

```text
/
├─ AGENTS.md
├─ CLAUDE.md
├─ TODOS.md
├─ README.md                 # only when explicitly requested by the user
├─ .artifacts/               # ignored; created on demand; sole disposable output root
├─ go.mod
├─ go.sum
├─ api/
│  └─ proto/
├─ cmd/
│  ├─ codeflux/
│  ├─ codeflux-worker/
│  └─ codeflux-dev/
├─ internal/
│  ├─ app/
│  ├─ domain/
│  ├─ coordinator/
│  ├─ worker/
│  ├─ storage/
│  ├─ events/
│  ├─ workspace/
│  ├─ gitwork/
│  ├─ providers/
│  ├─ executor/
│  ├─ agent/
│  ├─ review/
│  ├─ graph/
│  ├─ memory/
│  ├─ transport/
│  └─ testkit/
├─ migrations/
├─ web/
│  ├─ app/
│  ├─ components/
│  ├─ features/
│  ├─ state/
│  ├─ styles/
│  └─ testdata/
├─ testdata/
│  ├─ repositories/
│  ├─ events/
│  ├─ providers/
│  ├─ atoms/
│  └─ security/
└─ docs/
   └─ plan.md
```

This layout is a target, not evidence that packages already exist. Empty directories are not added merely to mirror the plan.

The `.artifacts/` directory is never tracked and has no placeholder. Build commands create only the children they need, such as `bin`, `wasm`, `coverage`, `package`, `bench`, `test-failures`, `db`, and `tmp`. Reviewed generated source remains in its declared source directory and is not treated as disposable output.

### Implemented Bootstrap Prerequisites

The current M01 repository commands require:

```text
Go:
    minimum language version 1.26.0
    latest security-patched Go 1.26 release required by bootstrap and CI

Git:
    required for source identity, tracked-file checks, hooks, and later worktrees

Staticcheck:
    exact supported version 2026.1
    required only by the lint command
```

No global `protoc`, Buf, Node, npm, Make, Bash, PowerShell module, SQLite CLI,
or C compiler is required for the current build and fast-test path. Protobuf
generation invokes Buf 1.72.0 through a version-qualified Go command and uses
the pinned `buf.build/protocolbuffers/go:v1.36.11` plugin. Its first run is an
explicitly network-capable bootstrap/generation action; ordinary builds and
tests compile the committed generated source without network access.

The implemented commands are:

```text
go run ./cmd/codeflux-dev build
go run ./cmd/codeflux-dev test-fast
go run ./cmd/codeflux-dev lint
go run ./cmd/codeflux-dev test-coverage
go run ./cmd/codeflux-dev test-race
go run ./cmd/codeflux-dev generate
go run ./cmd/codeflux-dev generate-check
go run ./cmd/codeflux-dev migration-check
go run ./cmd/codeflux-dev run --once
go run ./cmd/codeflux-dev benchmark atom-names
go run ./cmd/codeflux-dev benchmark generation
go run ./cmd/codeflux-dev artifact-check
```

To change protobuf output:

1. edit source beneath `api/proto/`;
2. run `go run ./cmd/codeflux-dev generate`;
3. inspect the generated `api/gen/` diff and its generated-file header;
4. run `go run ./cmd/codeflux-dev generate-check`;
5. run `go run ./cmd/codeflux-dev lint` and `test-fast`.

`generate-check` writes regeneration output beneath a validated
`.artifacts/tmp` child, compares exact file sets and bytes, and removes only
that child. The current frontend package embeds reviewed source assets directly.
No WASM generator exists yet, so these instructions do not claim a WASM
regeneration command; the v5 frontend spike must add and document that workflow
when it becomes real. (`M01-045`, `M01-046`)

## Development Helper

The implemented cross-platform entry point is:

```text
go run ./cmd/codeflux-dev <command>
```

It avoids requiring Make, Bash, PowerShell, Node package scripts, or a globally installed task runner for the basic loop.

Registry commands are listed below. `codeflux-dev help` is authoritative for
whether each command is implemented, gated, or an honest unavailable skeleton
at the current milestone:

```text
bootstrap
    verify Go/Git/tool versions and install pinned local generators under .artifacts

build
    compile packages and command binaries beneath .artifacts

generate
    generate protobuf, transport bindings, migrations catalog, and frontend assets

generate-check
    regenerate in a temporary area and fail on committed drift

migration-check
    validate migration ordering, embedding, and generated checksums

run
    start isolated development SQLite, fake provider, coordinator, and frontend

run-live
    start with an explicitly selected real provider and normal credential store

test-fast
    unit and pure component/reducer tests

test-integration
    real SQLite, Git, process, transport, and migration tests

test-browser
    browser component and end-to-end scenarios

test-security
    path, origin, permission, redaction, secret, and payload cases

test-race
    Go race detector for concurrency-owning packages

test-all
    the required local pre-submit suite

lint
    format, vet, selected linters, documentation, atom name/comment, and schema checks

seed
    create a named deterministic development scenario

replay
    replay a redacted event fixture or copied development session

inspect-db
    print a safe structured summary, never credentials or raw sensitive content

benchmark
    run current named microbenchmarks beneath .artifacts/bench; later milestones
    add startup, repository, event, frontend, graph, and vector benchmarks

artifact-check
    reject repository-local disposable output outside .artifacts

package
    build release packages when the M23 packaging subsystem exists

doctor
    run the same environment checks as the packaged product
```

Each command supports `--help`, uses non-zero exit codes, prints the exact failing subcommand, and accepts a temporary root. Machine-readable output is optional and versioned.

## Local Development Profiles

### Deterministic Profile

Default for development and tests:

```text
temporary SQLite database
temporary Git fixture repository
fake credential store
scripted provider
deterministic clock
deterministic ID generator
loopback ephemeral port
fixed pricing
no external network
```

### Interactive Fake Profile

Runs the real frontend and coordinator with scripted scenarios that can pause, request approval, stream slowly, fail, exceed budget, disconnect, and recover.

### Live Provider Profile

Requires explicit `run-live`, a configured credential reference, visible provider/model/cost banner, and a non-test database. It never runs in CI.

### Fault-Injection Profile

Allows named failures at durable boundaries:

```text
before/after event commit
before/after live publication
before/after edit
before/after checkpoint
during provider stream
during command process tree
during migration
during graph patch
during reconnect replay
```

## Deterministic Test Kit

`internal/testkit` owns:

```go
func NewTestClock(t testing.TB, start time.Time) *TestClock
func NewIDSequence(t testing.TB, seed uint64) *IDSequence
func NewTestDatabase(t testing.TB, opts DatabaseOptions) *TestDatabase
func NewGitRepository(t testing.TB, fixture RepositoryFixture) *TestRepository
func NewScriptedProvider(t testing.TB, script ProviderScript) *ScriptedProvider
func NewFakeCredentialStore(t testing.TB) *FakeCredentialStore
func NewEventRecorder(t testing.TB) *EventRecorder
func NewCoordinatorHarness(t testing.TB, opts HarnessOptions) *CoordinatorHarness
func NewBrowserScenario(t testing.TB, opts BrowserOptions) *BrowserScenario
```

Every helper registers cleanup through the test framework, validates target paths before deletion, and can preserve artifacts on failure behind an explicit flag.

Fixture builders prefer readable structured constructors over giant golden blobs. Golden files are used only when structural comparison would be less clear.

## Generated Code Workflow

Generated outputs include:

```text
protobuf Go server/client types
GoWebComponents-compatible client bindings
event-kind registry
timeline-card registry validation data
migration catalog
version metadata
embedded frontend asset manifest
```

Rules:

* generator versions are pinned;
* generated files include source and generator identity;
* generation writes only declared paths;
* `generate-check` must leave the repository unchanged;
* hand edits to generated files fail CI;
* protobuf compatibility and reserved fields are checked;
* event schema changes require reducer and presentation coverage;
* migration generation never rewrites an applied migration.

## Inner Development Loop

For one atomic TODO:

```text
read AGENTS.md and relevant plan section
-> confirm dependencies and gate
-> inspect current implementation and Git status
-> write or update the narrow failing test
-> implement the smallest behavior
-> run targeted test
-> run package/feature test
-> run generated/schema checks if applicable
-> exercise deterministic UI scenario when user-visible
-> inspect diff and database/event effects
-> update TODO completion evidence
```

The fast loop should finish in seconds for pure domain/reducer work and remain bounded for SQLite/package work. Slow suites are separated by purpose rather than hidden behind one opaque command.

## Adding a Backend Use Case

The golden path is:

1. identify the plan function and TODO ID;
2. add or reuse domain command/result/error types;
3. add the application-service method;
4. define authority, idempotency, expected revision, transaction, event, external effect, and cancellation behavior;
5. add repository operations only when the use case needs new persistence;
6. add the protobuf method or reuse an existing command;
7. implement validation and safe domain/transport conversion;
8. add a real-SQLite service test;
9. add duplicate-idempotency, stale-revision, denial, cancellation, and failure tests as applicable;
10. add a scripted end-to-end scenario;
11. expose UI only after the fake-client path works.

Transport handlers should remain visibly thin in review.

## Adding an Event and Timeline Card

1. define the domain event and whether it is durable/correctness-bearing;
2. add protobuf payload and reserve removed fields;
3. add SQLite serialization and replay test;
4. add server projection behavior;
5. add client reducer behavior;
6. add the presentation registry entry or documented grouping rule;
7. implement loading/live/final/replay states;
8. add accessibility announcement policy;
9. test event replay, duplicate delivery, unknown-version fallback, and card rendering;
10. verify the event is not silently dropped under backpressure.

CI fails when a new event lacks reducer or presentation/grouping coverage.

## Adding a Frontend Component

1. identify its user question and authoritative data source;
2. define immutable props/view model;
3. define local ephemeral state;
4. define commands and idempotency behavior;
5. define loading, empty, partial, stale, error, disconnected, and denied states;
6. define keyboard, focus, screen-reader, high-contrast, and reduced-motion behavior;
7. implement with shared primitives;
8. add pure component tests;
9. add a browser interaction scenario;
10. instrument render count if it participates in streaming;
11. verify it does not fetch data already owned by a parent view model.

## Adding a Graph Projection

1. define stable node/edge identity and class;
2. identify exact source events and revisions;
3. define deterministic projection and status rules;
4. define Program/Execution/Evidence visibility;
5. add SQLite/replay projection tests;
6. add bounded query behavior;
7. add layout fixture and stability assertion;
8. add graph rendering, keyboard, inspector, and chat-link behavior;
9. verify no token-only event creates a graph revision;
10. benchmark representative patch volume.

## Adding an Atom

1. define stable atom identity and compatibility boundary;
2. choose a standalone-descriptive canonical name;
3. write the complete schema-versioned atom documentation comment;
4. define signature, contract, effects, applicability, bindings, and obligations;
5. add implementation and independent evidence appropriate to atom tier;
6. run atom-name and atom-comment lint;
7. admit immutable name/documentation revisions into SQLite;
8. generate or verify the Go projection;
9. build normalized embedding input and record exact configuration;
10. test exact retrieval, vector candidacy, applicability rejection, invalidation, and rename lineage.

An atom is not reusable merely because it compiles or embeds successfully.

## Adding a Migration

1. define the domain invariant and rollback/recovery expectation;
2. allocate the next migration number;
3. write forward SQL without modifying prior migrations;
4. update repository queries;
5. add constraints and indexes;
6. test empty-database migration;
7. test upgrade from every supported prior schema;
8. test interrupted migration and backup restoration;
9. run integrity and representative query plans;
10. run `generate-check`.

## Adding a Provider

1. implement the normalized provider interface;
2. define capability mapping and model identity;
3. keep credentials behind the coordinator store;
4. implement streaming, cancellation, usage, stop reasons, and safe metadata;
5. classify retryable and terminal failures;
6. implement pricing or explicit unknown pricing;
7. add deterministic fake-server contract tests;
8. add redaction and partial-stream tests;
9. add opt-in live smoke test;
10. add provider settings and first-run presentation.

## Replay and Debugging

The event journal is the primary workflow debugger.

`replay` can:

```text
load a named fixture
load a redacted exported session
stop at a sequence
step one event
compare server and client projections
rebuild graph revisions
inject duplicate or missing delivery
simulate reconnect
show state transition and transaction identity
```

Debug views expose stable IDs, revisions, sequences, causation, correlation, policy, and binding metadata behind a development flag. They never expose credentials or unredacted tool/provider content.

Database inspection uses domain-aware read-only queries. Developers should not mutate application state manually with an SQLite shell as part of a normal workflow.

## Logging, Tracing, and Profiling

Development builds support:

```text
structured logs by task/run/request
event append and publish timing
SQLite transaction timing and busy waits
provider first-token and completion timing
tool process and cancellation timing
frontend reducer and render timing
graph layout and patch timing
memory retrieval channel and gate decisions
```

Go CPU, heap, goroutine, mutex, and block profiles are available through an authenticated loopback-only development endpoint. Browser performance markers correlate session sequence with reducer and render time.

Trace correlation never requires storing raw prompts or source.

## Test Layers and Ownership

| Layer | Purpose | Owner |
|---|---|---|
| Domain unit | state, money, policy, names, docs, applicability | owning domain package |
| Reducer/component | event projection and presentation | frontend feature |
| SQLite integration | constraints, transactions, migrations, replay | storage/use-case package |
| Process integration | worker, command, cancellation, crash | coordinator/worker |
| Contract | provider, gRPC, protobuf compatibility | adapter/transport |
| Browser | keyboard and end-to-end user flow | frontend feature |
| Security | authority, path, origin, redaction, secret, payload | boundary owner |
| Performance | startup, map, events, graph, rendering, vector | subsystem owner |
| Frozen benchmark | product correctness/speed/cost | independent evaluation |

Tests live near their owner. Shared fixtures live in `internal/testkit` or `testdata`; feature tests do not depend on execution order.

## CI and Local Parity

CI calls the same development-helper commands as contributors. It does not reproduce their logic in workflow YAML.

Required CI stages are:

```text
bootstrap/version check
generate-check
format/vet/lint
test-fast
test-integration
test-security
test-race
test-browser
package
```

Long benchmarks and live-provider smoke tests are scheduled or opt-in. CI artifacts are redacted and retained only when useful.

## Documentation and Agent Handoff

`AGENTS.md` contains real current commands once implemented. `CLAUDE.md` remains a thin pointer. `TODOS.md` records atomic work and gate evidence. `docs/plan.md` records architecture and product intent.

A handoff includes:

```text
TODO IDs
plan sections
source and schema changes
generated outputs
tests and commands run
event/transaction changes
UX states exercised
known limitations
next unblocked task
```

## DevX Acceptance

Developer experience is acceptable when:

* a fresh clone reaches deterministic local UI with one documented command after bootstrap;
* ordinary tests need no provider account or network;
* one failed browser scenario can be replayed against the same event fixture;
* adding an RPC, event/card, component, graph projection, atom, migration, or provider follows a documented golden path;
* local and CI command graphs match;
* generated drift and missing event presentation fail before review;
* debugging does not require manual database mutation or secret-bearing logs;
* an agent or human can identify the governing plan section, TODO, test, transaction, event, and UI presentation for a vertical change.

---

# 28. Initial Demonstrations

## Adaptive Coding Demonstration

Run a chronological sequence of accepted changes against one existing Go repository:

```text
Receive requirement
→ Map repository and construct task fingerprint
→ Retrieve compatible project evidence
→ Forecast effort, cost, time, and acceptance probability
→ Select model, reasoning effort, tools, and validation profile
→ Plan, edit, test, and inspect
→ Escalate when observed progress violates forecast
→ Pass required correctness gates
→ Produce reviewable diff and evidence summary
→ Store factual outcome and admitted reusable artifacts
→ Use that evidence to reduce later comparable-task effort
```

The demonstration should show:

* a routine task routed to a lower-cost policy without correctness loss;
* an unfamiliar or cross-cutting task escalated before budget exhaustion;
* a protected task receiving stronger mandatory validation;
* forecast-versus-actual reporting;
* an accepted prior artifact reducing context, tool calls, or repair rounds on a later task;
* explicit rejection of a stale or incompatible retrieved artifact;
* total time and cost compared with a fixed-policy baseline.

## Go Structural-Verification Demonstration

Generate a protected Go payment-orchestration service from a graph.

Example:

```text
Receive request
→ Validate request and authorization
→ Derive idempotency key
→ Acquire payment intent under a conditional durable claim
→ Match claim outcome
   ├─ Confirmed(Acquired)
   │    → Issue charge
   │    → Match charge outcome
   │       ├─ ConfirmedSuccess → Persist completed → Return success
   │       ├─ ConfirmedFailure → Persist terminal failure → Return declined
   │       └─ AmbiguousOutcome
   │            → Reconcile charge by the same key
   │            ├─ ConfirmedSuccess → Persist completed → Return success
   │            ├─ ConfirmedFailure → Persist terminal failure → Return declined
   │            └─ StillUnknown → Durable wait or escalation
   ├─ Confirmed(Completed) → Return existing result
   ├─ Confirmed(InProgress) → Reconcile or wait
   ├─ Confirmed(TerminalFailed) → Return existing failure
   └─ AmbiguousClaim → Reconcile the claim before issuance
```

The demonstration should establish:

* invalid requests issue no external effects;
* no two sequential issuance nodes share one logical effect identity;
* independent workflow instances derive the same key from durable business identity;
* claim, issuance, retry, and reconciliation share one singleton key provenance;
* effectful loops vary the key by stable element identity while retries preserve it;
* only a confirmed atomic claim can reach a new charge;
* `InProgress` never reaches a new charge;
* ambiguous charge outcomes cannot reach compensation without confirmed reconciliation;
* effects occur in required order;
* every claim and gateway result variant reaches a defined branch;
* handler capabilities are contained;
* generated Go matches graph-side traces for request construction.

The conditional claim's atomicity and the gateway's idempotency behavior remain external contracts. Codeflux proves that the generated structure uses them correctly; it does not predict their responses or guarantee that they honor their contracts.

## ReserveFlow Dogfood API Refinement Trial

After the clean-install prototype journey passes, use Codeflux itself to build a separate Go API from a frozen minimal repository. This is the final prototype dogfood trial and the primary source of evidence about what is confusing, missing, fragile, slow, expensive, or incorrectly specified.

The dogfood project is called `ReserveFlow` for the evaluation. It manages limited-capacity resources and time-bounded reservations. The domain is intentionally small enough to understand completely but rich enough to exercise persistence, concurrency, idempotency, state transitions, background work, external effects, recovery, security, review, atoms, graphs, and project memory.

### Isolation and Contamination Controls

The ReserveFlow repository is separate from the Codeflux repository and database.

The test setup uses:

```text
Codeflux repository and runtime database
    contains the tool implementation and its project memory

ReserveFlow working repository
    contains only the frozen API scaffold and accepted chronological changes

ReserveFlow application database
    belongs to the API under test and is not Codeflux runtime storage

Evaluator repository
    contains hidden acceptance, concurrency, security, and recovery tests
    and is inaccessible to Codeflux during implementation
```

Rules:

* freeze the scaffold revision before the first task;
* reveal requirements one chronological task at a time;
* do not expose future requirements or hidden tests to the agent;
* advance every comparison arm along the same accepted commit chain;
* allow Codeflux project memory only from earlier accepted ReserveFlow tasks;
* use the fixed-policy track first; adaptive routing is a separate later comparison;
* record every human clarification, approval, redirect, manual command, and source intervention;
* prohibit manual source edits during the evaluated Codeflux run;
* permit the operator to reject, repair, roll back, answer a blocking ambiguity, or grant scoped authority through normal Codeflux controls;
* do not let Codeflux inspect its own implementation repository while building ReserveFlow unless a specific tool-development task is explicitly opened;
* pin Go, dependency, provider, model, effort, tool, price, validation, and Codeflux versions for every run.

### API Domain

Core entities are:

```text
Resource
    id
    name
    total_capacity
    available_capacity
    version
    created_at
    updated_at

Reservation
    id
    resource_id
    customer_reference
    quantity
    state
    expires_at
    version
    created_at
    updated_at

IdempotencyRecord
    operation
    key
    canonical_request_hash
    response_status
    response_body
    resource_identity
    expires_at

OutboxEvent
    id
    aggregate_type
    aggregate_id
    event_type
    payload
    created_at
    published_at

WebhookEndpoint
    id
    callback_url
    secret_reference
    enabled

WebhookDelivery
    id
    outbox_event_id
    endpoint_id
    attempt
    state
    next_attempt_at
    last_status
    last_error
```

Reservation states are:

```text
Pending
Confirmed
Cancelled
Expired
```

Allowed transitions are:

```text
create -> Pending
Pending -> Confirmed
Pending -> Cancelled
Pending -> Expired
Confirmed -> Cancelled only when the later requirement explicitly introduces release
Cancelled -> terminal
Expired -> terminal
```

The initial state machine must not assume future transition changes.

### API Surface

Initial endpoints are:

```text
GET  /healthz
GET  /readyz

POST /v1/resources
GET  /v1/resources
GET  /v1/resources/{resource_id}

POST /v1/reservations
GET  /v1/reservations
GET  /v1/reservations/{reservation_id}
POST /v1/reservations/{reservation_id}/confirm
POST /v1/reservations/{reservation_id}/cancel

POST   /v1/webhook-endpoints
GET    /v1/webhook-endpoints
DELETE /v1/webhook-endpoints/{endpoint_id}
```

Mutating requests use a request ID and, where specified, an `Idempotency-Key`. State-transition requests use an expected version through `If-Match` or an equivalent explicit field. List endpoints use bounded cursor pagination and stable ordering.

The error envelope is consistent:

```json
{
  "error": {
    "code": "stable_machine_code",
    "message": "safe human explanation",
    "request_id": "traceable request identity",
    "details": {}
  }
}
```

The API never returns internal SQL, filesystem, stack, secret, or dependency details.

### Required Behavioral Properties

ReserveFlow must establish:

* creating a reservation atomically reduces available capacity;
* concurrent reservations cannot oversubscribe a resource;
* invalid quantity, unknown resource, expired request, or insufficient capacity creates no reservation and changes no capacity;
* one idempotency key plus the same canonical request returns the original result;
* reuse of one idempotency key with a materially different request returns a conflict;
* confirm, cancel, and expire obey the explicit state machine;
* repeated compatible transitions are either idempotent or return one documented conflict behavior;
* expected-version conflicts cannot overwrite newer state;
* expiration releases capacity exactly once;
* resource and reservation changes create outbox events in the same transaction;
* an outbox event is delivered at least once using one stable delivery identity;
* retries preserve delivery identity and do not create a new logical event;
* callback secrets are not stored in plaintext API rows, logs, error messages, fixtures, or events;
* webhook output is bounded and redacted;
* health, readiness, shutdown, migrations, and worker restart behave predictably;
* every accepted change has visible tests, hidden tests, diff review, and evidence.

The prototype does not claim exactly-once webhook delivery. It provides transactional event creation, stable delivery identity, bounded retry, and receiver-visible deduplication support.

### Frozen Scaffold

The initial repository contains only:

```text
Go module
minimal README with the first-task contract
empty command entry point
test helper skeleton
license and Git configuration
no domain implementation
no hidden tests
```

Do not pre-build abstractions for later tasks. The evaluation should reveal whether Codeflux introduces them prematurely.

### Chronological Requirement Sequence

Each item is a separate Codeflux task with a fresh forecast, plan, budget, review, episode, and acceptance decision.

#### Task 1: Server and Health

Build configuration loading, graceful startup/shutdown, `GET /healthz`, `GET /readyz`, request IDs, JSON content types, and safe error envelopes.

Tests examine port binding, cancellation, signal handling, malformed paths, and deterministic health behavior.

#### Task 2: Resource Persistence

Add SQLite migrations, resource creation/get/list, exact capacity validation, stable IDs, timestamps, and cursor pagination.

Tests examine migration from empty database, restart persistence, invalid capacity, stable ordering, cursor boundaries, and duplicate requests.

#### Task 3: Create a Reservation

Create a pending reservation and atomically decrement available capacity.

Tests examine invalid quantities, unknown resources, insufficient capacity, transaction rollback, and response/error shape.

#### Task 4: Idempotent Reservation Creation

Add `Idempotency-Key` behavior for reservation creation, including canonical request hashing and conflict on key reuse with different semantic input.

Tests examine identical retries, reordered JSON fields, transport retries, key expiry, concurrent same-key calls, and materially changed requests.

#### Task 5: Confirm and Cancel

Add explicit version-checked reservation transitions.

Tests examine valid transitions, stale versions, repeated requests, forbidden transitions, capacity release, and concurrent confirm/cancel races.

#### Task 6: Concurrent Capacity Safety

Harden reservation creation and cancellation under high concurrency.

Tests start many goroutines/processes against one limited resource and verify no oversubscription, negative capacity, lost update, duplicate reservation, or deadlock.

#### Task 7: Expiration Worker

Expire pending reservations after their deadline and release capacity exactly once. Add deterministic clock injection, worker lease/ownership, bounded scanning, and restart behavior.

Tests examine clock boundaries, multiple workers, crash after selection, crash after update, repeated scans, shutdown, and late confirmation.

#### Task 8: Transactional Outbox

Write resource/reservation domain events to an outbox in the same transaction as state changes. Add bounded polling and publish-state transitions.

Tests examine rollback, event ordering, duplicate polling, restart, poison events, and one event per logical state transition.

#### Task 9: Webhook Delivery

Add endpoint registration and signed webhook delivery through a mock receiver. Use stable delivery IDs, bounded retry/backoff, output limits, and secret references.

Tests examine success, timeout after receiver acceptance, connection failure, 4xx terminal behavior, 5xx retry behavior, duplicate receipt, signature validation, disabled endpoints, and secret redaction.

#### Task 10: API-Key Authorization

Protect administrative resource and webhook operations while keeping intended reservation operations under their declared policy.

Tests examine missing, malformed, invalid, revoked, and correctly scoped keys; timing-insensitive comparison where relevant; logs and errors; and capability leakage.

#### Task 11: Observability and Diagnostics

Add structured request logs, stable error codes, basic local metrics, readiness dependencies, and redacted diagnostics.

Tests examine correlation across request, database, worker, and webhook events without exposing bodies or secrets.

#### Task 12: OpenAPI and Client Contract

Create and verify an OpenAPI contract from implemented behavior. Add examples, error definitions, pagination, idempotency, and concurrency headers.

Tests compare contract paths/statuses/schemas with handlers and ensure documentation does not promise unsupported guarantees.

#### Task 13: Injected Production Defect

Introduce one frozen realistic defect in a separate accepted starting revision, such as capacity released twice after an expiration/cancellation race or idempotency scope missing the operation identity.

Ask Codeflux to diagnose and fix it without revealing the root cause. Require a reproducing test before acceptance.

#### Task 14: Requirement Change

Change one domain rule after memory and atoms exist—for example, allow cancellation of confirmed reservations within a bounded release window.

Measure whether Codeflux identifies affected state transitions, capacity rules, API contract, outbox events, webhook behavior, tests, documentation, graph nodes, evidence, and invalidated memory.

#### Task 15: Dependency and Migration Change

Upgrade one pinned dependency and add one backwards-compatible schema field, then require migration, data preservation, API compatibility, and evidence invalidation where relevant.

Measure whether bindings and cached/reused artifacts are correctly rechecked.

### Visible and Hidden Test Split

Visible tests teach ordinary repository conventions and give the agent a legitimate local feedback loop.

Hidden tests independently cover:

```text
concurrency interleavings
idempotency equivalence and conflict
transaction rollback
crash and restart boundaries
worker ownership
pagination stability
authorization scope
secret leakage
webhook ambiguity and retry identity
migration preservation
OpenAPI/implementation disagreement
state-machine invalid transitions
memory and atom invalidation after requirement/dependency changes
```

Hidden tests must assert behavior rather than internal implementation shape unless a structural requirement is explicitly part of the task.

### Codeflux Features Exercised

The trial must exercise:

```text
repository mapping and context explanation
task fingerprints
fixed forecast and budget
provider/model accounting
plan review and revision
worktree isolation
safe edits and concurrent user-change detection
commands and approvals
checkpoint, pause, reconnect, resume, and crash recovery
validation and evidence
chat, review, and graph navigation
descriptive atom naming and structured comments
exact atom and fact retrieval
optional vector candidate discovery
lineage, invalidation, and historical claims
cost/speed/correctness scorecard
```

At least one task must intentionally run with the graph collapsed to ensure the code-first path remains complete. At least one later task must use Program, Execution, and Evidence modes to evaluate whether the graph materially improves understanding.

### Failure and Friction Taxonomy

Every observed problem receives one primary category:

```text
product-scope misunderstanding
requirement interpretation
forecast or routing
context selection
planning
model/provider
tool schema
permission or authority
filesystem or Git isolation
edit conflict
command execution
validation selection
evidence or review
event/replay
checkpoint or recovery
budget/accounting
frontend state or interaction
graph projection or usability
atom naming/documentation/retrieval
memory compatibility or invalidation
SQLite schema/transaction
performance
developer experience
benchmark or hidden-test defect
API-specific design defect
```

Also record:

```text
severity
frequency
reproducibility
first responsible layer from Section 0
user-visible symptom
root cause
workaround used
whether the workaround contaminated evaluation
proposed smallest fix
regression test
rerun outcome
```

### Controlled Refinement Loop

When Codeflux fails or creates serious friction:

```text
1. Freeze the ReserveFlow task, base revision, Codeflux version, event sequence,
   worktree diff, provider/model, policy, budget, and observed failure.

2. Classify whether the defect belongs to Codeflux, the model/provider,
   ReserveFlow requirements, the visible tests, hidden tests, environment,
   or evaluation protocol.

3. Reproduce the failure outside the contaminated partial run using the smallest
   deterministic Codeflux fixture that still fails.

4. Add a Codeflux regression test at the lowest responsible layer.

5. Implement the smallest general fix in the Codeflux repository.

6. Run targeted, subsystem, security, replay, and relevant end-to-end tests.

7. Rebuild Codeflux and rerun the failed ReserveFlow task from its original clean
   base and original revealed requirement, not from the repaired partial worktree.

8. First rerun with project memory disabled to prove the tool fix; then rerun the
   chronological track with memory enabled to measure compounding behavior.

9. Check for regression on earlier ReserveFlow tasks and at least one unrelated
   fixture so the fix does not overfit the dogfood API.

10. Record whether the refinement improved correctness, speed, cost, UX, or DevX,
    and whether it introduced a new tradeoff.
```

Do not patch prompts, permissions, validators, or routing merely to make one hidden test pass. A refinement must state the general failure class it fixes.

### Dogfood Metrics

Per task, record:

* visible and hidden acceptance;
* independent diff review;
* regressions and delayed defects;
* time to forecast, plan, first action, first diff, validation, review, and acceptance;
* input, cached, and output tokens;
* provider, model, tool, and estimated human cost;
* forecast P50/P90 coverage and error;
* plan revisions, repair rounds, repeated actions, and escalations;
* files selected versus files actually changed or needed;
* approvals requested, granted, denied, and later found unnecessary;
* checkpoint, reconnect, recovery, and resume results;
* graph opens, mode use, node/message navigation, and user-rated usefulness;
* exact and vector retrieval candidates, eligibility decisions, influence, and outcome;
* atoms reused, adapted, rejected, invalidated, or newly admitted;
* manual interventions and workarounds;
* UI confusion and developer-harness failures.

Across the sequence, report whether marginal cost, time, context, and repair rounds decline without correctness loss.

### Comparison Tracks

Use the same chronological requirements and accepted commit chain for:

```text
Track A: strong fixed coding-agent baseline without Codeflux project memory
Track B: Codeflux fixed model/effort policy without vector discovery
Track C: Codeflux fixed policy with admitted deterministic project memory
Track D: later shadow/adaptive policy only after its independent gate
```

The initial dogfood exit depends on Track B working correctly. Track C tests compounding value. Track D is not required to call the prototype usable.

### Dogfood Exit Criteria

The dogfood trial passes when:

* the frozen sequence completes from a clean scaffold without manual source edits;
* every task passes visible and hidden acceptance before advancing;
* no resource is oversubscribed and no capacity is released twice;
* no logical reservation, outbox event, or webhook delivery is duplicated outside its declared semantics;
* no unauthorized action, secret leak, silent provider switch, skipped required check, or false evidence claim occurs;
* pause, reconnect, worker crash, coordinator crash, and restart preserve safe state;
* final API behavior and OpenAPI documentation agree;
* the user can understand state, cost, authority, diff, validation, graph, and next action without raw database manipulation;
* every Codeflux defect found has a reproduction, owning layer, regression test, fix decision, and rerun result;
* fixes demonstrate generality on an unrelated fixture;
* the final full rerun has no regression against the original clean-run scorecard;
* the plan records which capabilities should continue, narrow, redesign, defer, or be killed.

The purpose is not to prove Codeflux is generally superior from one API. The purpose is to make the first integrated system fail concretely enough that refinement is driven by evidence instead of another speculative design pass.

---

# 29. Revised Development Sequence

The dependency-ordered, atomic prototype build queue is maintained in [`../TODOS.md`](../TODOS.md). Its traceability index maps every implementation milestone back to the governing sections of this plan; this section remains the authoritative phase and gate sequence.

Phase-to-layer mapping:

```text
Phase 0  -> Layer 0
Phase 1  -> Layers 1-15, built strictly in the order defined in Section 0
Phase 2  -> Layer 16 plus the review/evidence hardening of Layer 15
Exit gate -> Layer 17 packaging, clean-machine evaluation, and ReserveFlow dogfood
Phase 3  -> Layer 18 shadow measurement
Phase 4  -> Layer 18 limited adaptive routing
Phase 5  -> Layer 18 broader memory only after measured value
Phase 6  -> Layer 19 optional Deep Go Verification
Phase 7  -> Layer 20 mechanical rules and advisory experiments
```

## Phase 0: Frozen Evidence and Decisions

Before implementation:

* create a root `AGENTS.md` as the authoritative coding-agent contract and a thin root `CLAUDE.md` that points to it;
* require agents to trace non-trivial implementation work to this plan and the atomic IDs in `TODOS.md`;
* adapt the Karpathy-inspired disciplines of visible assumptions, simplicity, surgical changes, and goal-driven verification while keeping Codeflux-specific authority, security, storage, and evidence rules primary;
* freeze the chronological adaptive-platform benchmark;
* select credible current coding-agent baselines on a separate tuning set;
* freeze model versions, prices, tools, static risk policy, and fixed baseline policy;
* define task, risk, correctness, speed, and cost metrics;
* define acceptance authority and hidden-test handling;
* define minimal task, run, event, policy, cost, validation, outcome, artifact, and binding schemas;
* define project data boundaries and retention;
* freeze usability, correctness, routing, forecast, and compounding kill thresholds.

The graph protocol remains an independent backlog track. Do not delay the code-first runtime for incident archaeology or the graph experiment, and do not begin graph production engineering before both the code-first baseline and independent graph gates pass.

## Phase 1: Code-First Agent Runtime

Build the smallest useful competitor:

* one cross-platform `codeflux-dev` helper for deterministic bootstrap, generation, local run, tests, lint, fixtures, and replay;
* explicit application-service functions with typed commands/results, authorization, idempotency, expected revisions, transaction boundaries, emitted events, external effects, cancellation, and typed errors;
* deterministic fake-provider, credential, clock, ID, SQLite, Git, worker, event, and browser harnesses;
* single-command local installation and update path;
* terminal task interface;
* local GoWebComponents v5 chat client served by the local coordinator;
* typed gRPC service contracts and one ordered resumable session stream;
* bounded framework/transport spike that verifies the v5 bridge, cancellation, reconnect, and streaming performance;
* task-scoped read-only execution graph with chat-to-graph identity links;
* explicit frontend routes, component tree, stores, pure event reducers, task-state/action matrix, progressive-disclosure rules, and loading/empty/error/disconnected/accessibility behavior;
* bring-your-own-provider credentials and local secret storage;
* repository mapping and targeted context assembly;
* source editing and diff management;
* shell, build, test, formatter, and static-analysis tools;
* git branch or worktree isolation and conflict handling;
* checkpoint, cancellation, rollback, and recovery;
* resumable sessions and live progress;
* plan and diff approval;
* user approvals and authority boundaries;
* safe filesystem and external-action permissions;
* visible task budget and hard cost cap;
* concurrent-user-edit detection;
* single-agent planning, execution, and repair;
* factual event, cost, latency, and outcome capture;
* reviewable final evidence summary.

Use a fixed model and effort policy initially so later routing improvements have a credible baseline.

The MVP acceptance journey is:

```text
Open repository
→ inspect policy and environment
→ submit task
→ receive plan, scope, and fixed-policy budget
→ approve or redirect
→ observe interruptible progress and checkpoints
→ inspect diff and validation evidence
→ accept, request repair, or roll back
→ resume later with factual state preserved
```

## Phase 2: Correctness, Review, and Deterministic Memory

Add:

* task-risk classification;
* routine, elevated, and protected validation profiles;
* acceptance-test and regression selection;
* independent review triggers;
* security and high-risk change gates;
* CI integration;
* PR-ready diff and evidence presentation;
* failure classification and rollback policy;
* versioned repository facts;
* known build and test commands;
* file-to-test mappings;
* repository conventions;
* accepted regression cases;
* schema-versioned, thorough Go atom comments with explicit selection, exclusion, semantic, effect, failure, retry, security, binding, example, and verification fields;
* descriptive atom naming with canonical/display/normalized forms, collision control, alias lineage, and semantic rename classification;
* atom-documentation extraction, validation, immutable SQLite revisions, normalized embedding inputs, and comment/contract drift invalidation;
* artifact maturity and invalidation;
* local memory inspection, correction, export, and deletion.

No adaptive optimization proceeds until correctness outcomes are trustworthy.

## Phase 3: Shadow Forecasting and Counterfactual Data

Before Phase 3 begins, complete the §28 ReserveFlow Dogfood API Refinement Trial under Milestone 24. The fixed-policy Codeflux track must build the chronological API from a frozen scaffold without manual source edits, pass independent hidden acceptance one task at a time, and process every Codeflux-owned failure through the controlled reproduce/fix/clean-rerun protocol. The dogfood trial validates the integrated Layers 1–17 product; it does not authorize adaptive routing or prove broad superiority from one API.

Build:

* versioned task fingerprints;
* model and tool performance catalog;
* cold-start heuristic forecaster;
* shadow acceptance, time, and cost distributions;
* conservative abstention and out-of-distribution detection;
* calibration-subset policy matrix;
* version-specific forecast invalidation;
* shadow routing recommendations;
* forecast and recommendation evaluation.

Forecasts have no execution authority in this phase.

## Phase 4: Limited Adaptive Routing

Enable only:

* model selection;
* reasoning-effort selection;
* live progress monitoring;
* re-planning and escalation;
* routing decision and regret reporting.

Validation floors remain immutable, and multi-agent topology remains disabled. Run Platform Arm B against both static baselines. Continue only if the complete routing policy, including failed cheap attempts and escalation, reduces cost or latency without correctness regression.

## Phase 5: Broader Compounding Project Memory

Extend project-local memory with:

* validated and matured debugging knowledge;
* validated plans and change strategies;
* exact reusable code, tests, and atom artifacts;
* governed mechanical rules;
* structured retrieval;
* compatibility, version, and lineage checks;
* reuse and rejection logging;
* invalidation and maintenance policies.

Keep vector retrieval, cross-project promotion, and advisory patterns disabled until deterministic memory demonstrates value.

Run Platform Arm C against isolated Arm B history. The compounding claim must survive maintenance cost, contamination controls, and chronological evaluation.

## Phase 6: Optional Deep Go Verification

First complete incident archaeology and the disposable graph experiment. Proceed only if the code-first product baseline works, the incident surface is material, and the graph passes.

Build in gated increments:

1. semantic kernel and independent conformance;
2. restricted type system and graph IR;
3. provenance, regions, effects, and the four structural rules;
4. obligation evidence and dependency cones;
5. deterministic Go lowering and source maps;
6. durable-runtime profile and adapters;
7. semantic review, claim invalidation, and assurance advisories.

If the graph loses, implement valuable rules over ordinary Go where feasible.

## Phase 7: Mechanical and Advisory Improvement

Convert repeated supported findings into:

* reproducible regression cases;
* repository checks and lint rules;
* task-planning heuristics;
* compiler or generator diagnostics;
* validation-profile updates;
* routing features with measured predictive value.

Mechanical rules follow replay, warning, override, false-positive, expiry, and demotion governance.

Only after routing, deterministic memory, and validation demonstrate value:

* extract offline hypotheses from chronological episodes;
* run candidate routing and advisory policies in shadow mode;
* preserve permanent clean-room tasks;
* conduct lineage-aware controlled trials;
* expose only experimentally useful policies or patterns;
* retire policies when calibration, bindings, or outcomes degrade.

Multi-agent topology requires a separate controlled trial showing incremental value after coordination cost.

Do not fine-tune models on Codeflux episodes under the initial governance policy.

---

# 30. Kill and Pivot Criteria

## Code-First Agent Failure

Compare against at least two named, version-bound credible coding-agent baselines. Initial thresholds, frozen before evaluation, are:

* no more than a five-percentage-point hidden-acceptance deficit;
* no severe correctness or authority regression;
* median repository setup and first-task readiness under fifteen minutes;
* at least seventy percent of eligible tasks completed without human rescue;
* successful checkpoint resume and rollback on at least ninety-five percent of conformance cases.

If the fixed-policy Codeflux runtime fails these thresholds:

* stop work on routing and compounding claims;
* repair the basic repository, editing, tool, and validation loop first.

## Adaptive Routing Failure

After at least one hundred eligible paired tasks, retain adaptive routing only when the complete Arm B policy:

* remains within the frozen correctness non-inferiority margin;
* reduces accepted-change latency or total economic cost by at least fifteen percent against the best static safe policy;
* includes all failed cheap attempts and escalation cost;
* escalates on no more than thirty percent of routine eligible tasks unless the escalations remain net-positive.

Retain useful forecasting telemetry even if automated routing fails.

## Compounding-Memory Failure

After at least one hundred eligible paired tasks, stop claiming that past effort compounds when Arm C:

* fails to maintain Arm B correctness;
* fails to improve late-block total cost or accepted-change latency by at least fifteen percent;
* shows no increasing paired advantage over chronological task index;
* produces savings smaller than memory capture, review, invalidation, and retrieval cost;
* causes unacceptable stale-evidence or cross-project contamination risk.

Retain factual project memory that improves review or reproducibility even if automatic reuse fails.

## Forecast Calibration Failure

Do not use forecasts for automatic routing when P90 time or cost intervals cover fewer than eighty percent of held-out outcomes, acceptance calibration exceeds the frozen error tolerance, or out-of-distribution abstention fails its safety tests. Fall back to conservative fixed policies until the forecast is revalidated.

## Multi-Agent Topology Failure

Do not enable adaptive multi-agent topology unless a controlled eligible-task subset shows at least ten percent improvement in accepted-change latency or cost at non-inferior correctness after coordination overhead.

## Graph Medium Failure

Stop graph-first editing when Arm C fails the pre-registered medium-experiment criteria:

* stop graph-first editing;
* retain effect and contract analysis over Go source.

If Arm B materially improves on Arm A while Arm C does not improve on Arm B, build the structural-effect verifier over ordinary Go.

## Addressable Surface Failure

Use the frozen incident count:

* twenty or more qualifying incidents: proceed with the request-side product;
* ten through nineteen: run a narrower domain study and classify another one hundred incidents before compiler construction;
* fewer than ten: stop the graph platform.

## Exact Reuse Failure

After one hundred eligible tasks, conduct an interim review. After five hundred, stop investment in atom discovery and recommendation when either:

* meaningful reuse occurs in fewer than twenty percent of eligible tasks; or
* reuse provides no measurable improvement in defects, review time, or total economic cost.

Atom identity may remain for provenance, lowering, and source mapping.

## Kernel Explosion

If the useful kernel cannot remain small and stable:

* simplify the graph language;
* reduce evaluability claims;
* avoid building a general runtime.

## Proof Coverage Failure

If most high-value obligations remain runtime-only:

* stop broad compiler development;
* retain only request-side effect analysis that remains mechanically useful.

## Review Failure

If graph revisions cannot be reviewed, blamed, and merged more effectively than source changes:

* abandon the graph as the authoritative collaborative artifact.

## Assurance Drift

If deadlines systematically push critical workflows toward external-only guarantees despite CI policy:

* treat the assurance model as operationally unsuccessful.

## Learning-System Failure

Stop advisory retrieval when controlled, lineage-independent trials show no improvement or show increased defect, review, security, or cost risk. Preserve verified atoms, mechanical rules, and regression cases independently of the advisory result.

---

# 31. Evidence-Driven Reuse and Learning

The learning system is a governed project-memory and policy-improvement layer around the code-first agent. It is not a prerequisite for a useful editing and validation loop.

Its priority is:

```text
Versioned repository and tool facts
    ↓
Validated execution recipes and acceptance artifacts
    ↓
Exact compatible code, plans, and atoms
    ↓
Mechanical verification rules
    ↓
Reproducible regression cases
    ↓
Offline observations and hypotheses
    ↓
Experimentally validated advisory patterns
```

This ordering describes evidence strength. Every channel still requires evidence of economic value and remains subject to a kill criterion.

## Learning Artifact Types

### Workspace Facts

Versioned facts include repository topology, ownership, build and test commands, dependency relationships, generated-file policy, environment requirements, and known failure signatures.

Facts bind to repository revisions or explicit validity ranges and are invalidated when their supporting evidence changes.

### Execution Recipes

A validated or matured task may produce a reusable recipe containing:

* applicability predicates;
* scoped plan structure;
* required tools and commands;
* expected failure signals;
* validation profile;
* observed cost and latency;
* supporting accepted episodes.

Recipes guide planning but do not authorize edits or bypass current validation.

### Routing Evidence

Routing evidence records how a model, reasoning effort, topology, and validation policy performed for a task fingerprint. It is used for calibrated forecasting and policy evaluation, not as proof of task correctness.

### Executable Atoms

Atoms are typed, reusable graph components with:

* input and output types;
* effects and capabilities;
* contracts and proof obligations;
* dependency bindings;
* verification evidence;
* stable identity and lineage.

Typed compatibility and obligation preservation outrank semantic similarity.

### Mechanical Rules

A supported refinement should become a validator check, lint rule, compiler diagnostic, code-generation fix, contract, or proof obligation whenever it can be expressed mechanically.

A mechanical rule is a prohibition and can block correct programs. It therefore requires stronger governance than advisory prose.

### Regression Cases

A failure becomes a regression case only when it has:

* a reproducible input;
* a stable or versioned oracle;
* a demonstrated failure;
* an expected outcome.

An alleged anti-pattern without a reproducible failing case is stored only as an observation.

### Observations and Hypotheses

Unproven findings may be retained for offline research. They are not shown to working agents and do not affect generation.

### Advisory Patterns

Advisory patterns are experimental, scoped recommendations. They never silently override agent reasoning, verifier results, or compatibility rules.

## Chronological Episodes

Capture append-only factual events such as:

```text
Task received
Starting revision selected
Workspace facts and prior evidence retrieved
Effort forecast and routing policy selected
Source or graph edit proposed
Validator result observed
Compiler diagnostic emitted
Refinement applied
Proof obligation evaluated
Generated Go executed
Human decision recorded
```

An episode records:

* the versioned task fingerprint;
* starting and ending revisions;
* proposed and accepted changes;
* compiler, evaluator, verifier, test, and runtime results;
* retrieved artifacts;
* reuse and rejection decisions;
* human decisions;
* cost and timing evidence.

Agent explanations are stored only as `agent_self_report` with `evidence_strength: none`. They are not treated as causal accounts. Pattern extraction relies on observable transitions, controlled replay, and failing-versus-passing comparisons.

## Influence Lineage

Every derived artifact records:

```text
artifact_id
origin_artifact_id
derived_from[]
influenced_by[]
supporting_episode_ids[]
lineage_root_ids[]
```

Artifacts and episodes from the same influence lineage count as one evidence family. If a pattern enters an agent's context, the resulting artifact joins that lineage even when the agent rewrites the idea completely.

Descendants of a pattern do not independently confirm their ancestor. Independent confirmation must come from episodes never exposed to that lineage.

## Artifact Failure Protocol

This protocol activates for project-memory artifacts after the compounding-memory phase and for graph assurance artifacts only after the graph subsystem is authorized. Consequences attach to artifacts and their evidence, not to agent narratives.

### Advisory Lesson Failure

A detected failure correlated with an advisory lesson immediately quarantines the lesson and stops new retrieval. Causation is investigated from the same starting revision using:

```text
Arm A: clean context without the lesson or its descendants
Arm B: context containing the lesson
```

Run at least five and at most ten executions per arm under the same model, tools, limits, and acceptance criteria. Use a fixed investigation budget. Exhausting the budget without demonstrating benefit retires the lesson rather than leaving it pending indefinitely.

Apply these consequences:

* when the lesson arm performs worse, invalidate the lesson;
* when both arms perform similarly badly, retire it because benefit is absent;
* when adaptation or applicability failed, invalidate the exposed version and require any revision to restart as a candidate;
* when bindings or specifications changed, expire or retire the old version and require a new candidate;
* when evidence is ambiguous, retire the lesson;
* when replay demonstrates benefit, retain that result only as evidence for an independently identified successor candidate.

Quarantine is terminal for the exposed lesson version. A failed lesson is not repaired in place or restored with inherited authority. An independently derived successor receives a new identity, no inherited confidence, the original counterexample, and fresh admission requirements.

Safety-critical advisory lessons are prohibited. Safety properties must be expressed as obligations, contracts, mechanical rules, capability restrictions, or regression cases. Prose advice cannot carry a safety guarantee.

### Descendant Contamination

Lineage distinguishes:

```text
depends_on(artifact)    = semantic dependency on the failed claim
influenced_by(artifact) = contextual exposure without a mechanical dependency
```

Direct semantic dependents are quarantined automatically. Contextually influenced descendants are flagged, excluded from independent confirmation, and required to verify their own claims, but are not automatically invalidated.

If a failure reaches any deployed workflow or more than five percent of active artifacts, automatic quarantine and assurance downgrade still occur, while permanent bulk invalidation requires assurance-owner review.

## Assurance Evidence Failure

An atom implementation and the evidence supporting each obligation are separate versioned artifacts.

Failure may be discovered through:

* dependency-version watchers;
* scheduled architecture and toolchain conformance runs;
* evaluator-versus-Go disagreement;
* independently authored kernel and modeled-atom test vectors;
* property and metamorphic tests;
* runtime contradictions and production counterexamples;
* contract challenge suites based on independently sourced examples and counterexamples;
* periodic specification review;
* explicit specification, dependency, toolchain, or architecture changes.

A contract that faithfully encodes the wrong business property cannot be eliminated by mechanical conformance alone. Guarantee reports must expose the contract's source, approval, assumptions, review date, and runtime-observation status. Periodic review and contradiction ingestion reduce detection time but do not convert approved intent into mechanically proven truth.

When evidence fails:

1. suspend the affected evidence version;
2. mark the supported obligation unresolved;
3. locate every obligation whose dependency cone includes that evidence;
4. deterministically re-derive all downstream guarantee levels;
5. create an assurance advisory with affected artifact and version ranges;
6. notify affected projects and deployed workflows;
7. apply each project's minimum-assurance deployment policy.

Downgrade to `Contract checked` only when valid independent contract evidence exists. Otherwise downgrade to `Unestablished`. Silent fallback is prohibited.

Protected workflows block deployment when they fall below policy. Already deployed workflows receive a high-priority advisory.

Recovery requires a reproducible counterexample, corrected implementation or evidence, a new evidence version, complete conformance across the supported matrix, re-evaluation of affected obligations, downstream project verification, and human approval for critical-path guarantees.

## Kernel Integrity Incident

Kernel failure is a trusted-computing-base incident, not an ordinary atom failure.

The kernel requires a normative semantic specification, independently authored test vectors, metamorphic properties, cross-implementation comparison, and scheduled execution across the supported matrix. Agreement between the evaluator and Go implementation is necessary but insufficient because both may share the same misunderstanding.

A credible kernel contradiction receives the highest assurance severity:

1. freeze new fully evaluated claims that depend on the affected kernel semantics;
2. suspend the affected kernel evidence;
3. re-derive every reachable guarantee;
4. block protected deployments below policy;
5. issue a project-wide assurance advisory;
6. prioritize active and deployed workflows for re-verification;
7. create historical invalidation overlays for every affected claim.

Recovery requires an independently reviewed semantic correction, a new kernel and evidence version, complete conformance, and re-verification of every supported active project. Previous evidence is retained as invalidated history.

## Historical Claim Invalidation

Immutable revisions retain what was asserted at the time, but a mutable claim-status overlay records what is currently known:

```text
claim_id
original_evidence_version
original_assurance_level
current_assurance_level
invalidated_at
assurance_advisory_id
affected_version_range
reason
remediation_status
replacement_evidence_version
```

Every current view of a historical guarantee resolves this overlay and can display:

```text
Originally asserted: Fully evaluated
Current status: Invalidated
Advisory: CFSA-YYYY-NNNN
Affected versions: ...
```

Previously exported reports cannot be rewritten. A public or customer-visible advisory registry and targeted notification path must therefore carry advisory identifiers, affected version ranges, downgraded claims, deployed-workflow impact, and fixed versions.

## Versioned Task Fingerprints

The task fingerprint includes:

```text
fingerprint_schema_version
domain
input_types
output_types
effects
capabilities
obligations
dependencies
runtime_constraints
security_classification
```

Applicability conditions must be deterministic predicates rather than prose. Every predicate declares its expected fingerprint-schema version, required fields, supported migrations, and behavior when fields are absent. Incompatible schema versions never match silently.

## Retrieval and Pre-Work Gate

Before generating a solution, the agent:

1. constructs the task fingerprint;
2. loads version-compatible workspace facts and execution recipes;
3. retrieves similar accepted and failed task outcomes for effort forecasting;
4. searches for exact compatible code, plan, test, and atom artifacts;
5. checks applicable mechanical rules and regression cases;
6. validates repository revision, dependencies, capabilities, contracts, and obligations;
7. forecasts effort and selects the initial execution policy;
8. reuses, adapts, composes, or generates;
9. records every retrieval, routing, reuse, rejection, escalation, and outcome.

Structured database fields are authoritative. Embeddings provide candidate recall only.

The retrieval order is:

1. exact repository, revision, tool, and artifact identity;
2. deterministic applicability and compatibility filters;
3. accepted execution recipes, rules, and regression cases;
4. typed compatibility for code and atoms;
5. vector similarity for candidate recall;
6. deterministic compatibility validation before influence;
7. routing and planning only from the validated candidate set.

## Mechanical-Rule Governance

Every candidate rule requires:

* a reproducible failing case;
* an explicit safety or policy property;
* deterministic applicability predicates;
* documented scope;
* toolchain, generator, and dependency bindings;
* replay against accepted revisions;
* a frozen-benchmark false-positive measurement;
* an override path.

New rules default to `warn`.

A rule cannot become an error until:

* at least two hundred accepted historical revisions are available for replay;
* it has at least fifty applicable benchmark or real exposures;
* historical replay rejects zero accepted revisions;
* it has zero confirmed false positives;
* it has no accepted overrides;
* its bindings remain valid.

For every warning, record whether it was fixed or ignored and whether the final revision was accepted and verified. An ignored warning followed by an accepted, verified result becomes a candidate counterexample.

For every override, record the rejected program, rule identity, justification, acceptance outcome, and verification outcome. Repeated accepted overrides automatically suspend or demote the rule.

Rules expire when relevant bindings change and must be revalidated.

## Regression Oracle and Execution Policy

Classify regression cases as:

* semantic regressions;
* safety regressions;
* versioned specification regressions;
* versioned external-contract regressions.

A changed business decision may retire a specification regression without implying a new defect.

Execution tiers are:

* pre-commit: small, deterministic, high-value cases;
* CI: cases selected by the changed dependency cone;
* nightly: the complete active corpus;
* scheduled audit: stale, redundant, or non-discriminating cases.

Subsumed cases may leave active execution, but their provenance remains.

## Permanent Clean Room

At least fifteen percent of eligible tasks, or one entire project, remains permanently unexposed to advisory retrieval.

The clean-room cohort:

* may use mechanically verified atoms, rules, and regressions;
* never receives advisory patterns or their descendants;
* records `advisory_exposure: false`;
* cannot later be relabeled as unexposed;
* supplies independent confirmation and counterexamples.

## Fine-Tuning Boundary

Models must not be fine-tuned on Codeflux episodes under the initial policy because weight-level contamination cannot be represented by artifact lineage.

Lifting this prohibition requires unanimous approval from:

* the project owner;
* the security and data-governance owner;
* the independent evaluation owner.

Approval requires a new contamination model plus a separate clean-room model and corpus. Existing advisory evidence does not automatically transfer to a fine-tuned system.

## Advisory Pattern Evaluation

Advisory patterns begin offline and then run in shadow mode. Agent-facing exposure requires controlled comparison with the permanent clean-room cohort and evidence from independent lineages.

No pattern receives a self-sealing `Preferred` status. Patterns are experimentally available, retired, or invalidated. Advisory retrieval remains optional, and the agent's acceptance or rejection is recorded.

---

# 32. Central Design Principles

```text
The product delivers correct, reviewable repository changes.

Correctness is a constraint; speed and cost are optimized inside it.

Every backend mutation declares authority, idempotency, revision, transaction, event, effect, cancellation, and failure behavior.

Every frontend event has a deterministic reducer and an explicit presentation or grouping rule.

Every task state exposes an honest next-action set.

The correct development path must also be the easiest local path.

Effort forecasts are calibrated distributions, not promises.

Routing decisions remain observable, reversible, and measurable.

Accepted work should create reusable project capital.

Past work must earn influence through compatibility and evidence.

Ordinary source code is the default working medium.

Go is the first deep semantic-verification target.

The graph must earn its role.

The kernel is the trusted base.

Guarantees attach to claims.

Request-side effects are the primary verification surface.

External responses remain external.

Generated Go is a projection.

Stable identity enables review.

Atoms provide local semantics; regions close workflow obligations.

Atom comments provide rich retrieval material; typed contracts and evidence remain authoritative.

Atom names preserve domain context when displayed alone; precision is preferred over artificial brevity.

Every atom vector binds to an exact documentation revision, contract hash, repository revision, and embedding configuration.

Chat controls work; the graph explains structure, execution, and correctness.

The graph remains task-scoped, optional, and read-only until interaction evidence justifies more.

Logical effect identity and key provenance are explicit.

Ambiguous outcomes reconcile before compensation.

Benchmarks precede implementation.

Assurance levels must gate critical paths.

Learning must prove its value later.

Mechanical prohibitions require measured false-positive control.

Lineage determines evidence independence.

Vector similarity discovers candidates; it does not establish validity.

Evidence failure re-derives guarantees transitively.

Historical claims remain immutable but can be retroactively invalidated.

Kernel contradictions are system-wide assurance incidents.
```

The immediate design priority is the code-first agent runtime, frozen adaptive-platform benchmark, and trustworthy correctness telemetry. Model routing and compounding memory are built only after that baseline works. The kernel atom list becomes load-bearing only if the graph experiment authorizes the deep Go verifier.
