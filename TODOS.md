# Codeflux Prototype TODOs

This is the dependency-ordered execution checklist for the first usable Codeflux prototype.

The prototype is a local-first coding agent for repositories the user controls. It optimizes correctness first, then speed and cost. Its primary interface is a GoWebComponents v5 chat thread with a task-scoped graph showing program structure, live execution, and correctness evidence. The local Go coordinator, task workers, model adapters, Git integration, and SQLite database remain authoritative.

The detailed product and research rationale lives in [`docs/plan.md`](docs/plan.md). This file is the build queue.

## Required Task Shape

Every implementation task is intended to be atomic: one developer should be able to finish it in one focused work session without making an unrecorded architecture decision. A task is not complete until its output exists and its verification succeeds.

Use this interpretation for every checkbox:

```text
ID and labels
    The stable work identifier and the kind of review it requires.

Action
    One observable change to source, schema, tests, UI, or documentation.

Plan reference
    Supplied by the milestone's "Plan references" block and, where narrower,
    by an inline `Plan:` note.

Depends on
    The preceding task IDs or milestone gate that must already be complete.

Output
    The file, generated contract, database object, UI state, test fixture,
    benchmark result, or recorded decision produced by the task.

Verify
    The exact automated test, manual scenario, schema inspection, or benchmark
    that must succeed before checking the box.
```

When an item still requires more than one independently verifiable output, split it before implementation. Gate items aggregate evidence; they are intentionally broader and do not replace the atomic tasks below them.

## Plan Traceability Index

| TODO milestone | Primary plan references |
|---|---|
| M00 | [§0 Linear Concept and Build Order](docs/plan.md#0-linear-concept-and-build-order), [§1 Product Constraints](docs/plan.md#1-product-constraints), [§2 Revised Product Thesis](docs/plan.md#2-revised-product-thesis), [§3 Load-Bearing Experiments](docs/plan.md#3-load-bearing-experiments), [§27 Initial Product Scope](docs/plan.md#27-initial-product-scope), [§30 Kill and Pivot Criteria](docs/plan.md#30-kill-and-pivot-criteria) |
| M01 | [§27 Hobbyist MVP Decisions](docs/plan.md#hobbyist-mvp-decisions), [§27D Prototype Developer Experience](docs/plan.md#27d-prototype-developer-experience), [§29 Phase 1](docs/plan.md#phase-1-code-first-agent-runtime) |
| M02 | [§5 Program Architecture](docs/plan.md#5-program-architecture), [§18 Stable Graph Identity](docs/plan.md#18-stable-graph-identity), [§22 Correctness and Assurance Gates](docs/plan.md#22-correctness-and-assurance-gates), [§23 Storage](docs/plan.md#23-storage) |
| M03 | [§23 Storage](docs/plan.md#23-storage), [Database Authority](docs/plan.md#database-authority), [Core Operational Entities](docs/plan.md#core-operational-entities), [Transactions, Migrations, and Recovery](docs/plan.md#transactions-migrations-and-recovery) |
| M04 | [§27 Provider Credentials](docs/plan.md#provider-credentials), [Commands, Secrets, and Malicious Repository Content](docs/plan.md#commands-secrets-and-malicious-repository-content) |
| M05 | [§23 Core Operational Entities](docs/plan.md#core-operational-entities), [§27A Unified Session Stream](docs/plan.md#unified-session-stream), [§27B Transaction and Event Functions](docs/plan.md#transaction-and-event-functions), [Persistence, Recovery, Diagnostics, and Updates](docs/plan.md#persistence-recovery-diagnostics-and-updates) |
| M06 | [§27A Framework and Transport Spike](docs/plan.md#framework-and-transport-spike), [Rendering and Performance](docs/plan.md#rendering-and-performance), [Frontend Acceptance Criteria](docs/plan.md#frontend-acceptance-criteria) |
| M07 | [§27A Service Contracts](docs/plan.md#service-contracts), [Client, Server, and Storage Boundary](docs/plan.md#client-server-and-storage-boundary), [§27B Prototype Backend Function and Flow Specification](docs/plan.md#27b-prototype-backend-function-and-flow-specification) |
| M08 | [§5 Workspace Intelligence](docs/plan.md#workspace-intelligence), [§27 Repository Indexing and Context Selection](docs/plan.md#repository-indexing-and-context-selection) |
| M09 | [§19 Review and Source Mapping](docs/plan.md#19-review-and-source-mapping), [§27 Local Runtime and Repository Isolation](docs/plan.md#local-runtime-and-repository-isolation), [§29 Phase 1](docs/plan.md#phase-1-code-first-agent-runtime) |
| M10 | [§27 Commands, Secrets, and Malicious Repository Content](docs/plan.md#commands-secrets-and-malicious-repository-content), [Plugins and Custom Commands](docs/plan.md#plugins-and-custom-commands), [§21 Agent Architecture](docs/plan.md#21-agent-architecture) |
| M11 | [§21 Coordinator](docs/plan.md#coordinator), [Coding Agent](docs/plan.md#coding-agent), [§27 Local Runtime and Repository Isolation](docs/plan.md#local-runtime-and-repository-isolation) |
| M12 | [§27 Initial Model Providers](docs/plan.md#initial-model-providers), [Provider Credentials](docs/plan.md#provider-credentials), [§21 Routing Safety](docs/plan.md#routing-safety) |
| M13 | [§5 Adaptive Execution Policy](docs/plan.md#adaptive-execution-policy), [§21 Effort Forecaster](docs/plan.md#effort-forecaster), [Model and Effort Router](docs/plan.md#model-and-effort-router), [§25 Cost](docs/plan.md#cost), [Forecast and Routing Quality](docs/plan.md#forecast-and-routing-quality) |
| M14 | [§5 Execution and Review](docs/plan.md#execution-and-review), [§21 Agent Architecture](docs/plan.md#21-agent-architecture), [§22 Correctness and Assurance Gates](docs/plan.md#22-correctness-and-assurance-gates) |
| M15 | [§27 Persistence, Recovery, Diagnostics, and Updates](docs/plan.md#persistence-recovery-diagnostics-and-updates), [§29 Phase 1](docs/plan.md#phase-1-code-first-agent-runtime) |
| M16-M18 | [§27A Local Frontend and Tooling](docs/plan.md#27a-local-frontend-and-tooling), [§27C Prototype Frontend Component and UX Specification](docs/plan.md#27c-prototype-frontend-component-and-ux-specification), [§25 MVP Usability](docs/plan.md#mvp-usability) |
| M19 | [§18 Stable Graph Identity](docs/plan.md#18-stable-graph-identity), [§27A Graph Modes](docs/plan.md#graph-modes), [Graph Rendering Rules](docs/plan.md#graph-rendering-rules), [Node Inspector](docs/plan.md#node-inspector) |
| M20 | [§22 Correctness and Assurance Gates](docs/plan.md#22-correctness-and-assurance-gates), [§19 Review and Source Mapping](docs/plan.md#19-review-and-source-mapping), [§27A Evidence](docs/plan.md#evidence) |
| M21 | [§7 Atom Naming and Retrieval Identity](docs/plan.md#atom-naming-and-retrieval-identity), [Atom Documentation as Retrieval Material](docs/plan.md#atom-documentation-as-retrieval-material), [§31 Evidence-Driven Reuse and Learning](docs/plan.md#31-evidence-driven-reuse-and-learning), [§23 Atom Storage](docs/plan.md#atom-storage), [Vector Storage](docs/plan.md#vector-storage), [§29 Phase 2](docs/plan.md#phase-2-correctness-review-and-deterministic-memory) |
| M22 | [§24 Specification Review](docs/plan.md#24-specification-review), [§25 Metrics](docs/plan.md#25-metrics), [§26 Benchmark Timing](docs/plan.md#26-benchmark-timing), [§27D Prototype Developer Experience](docs/plan.md#27d-prototype-developer-experience) |
| M23 | [§27 Persistence, Recovery, Diagnostics, and Updates](docs/plan.md#persistence-recovery-diagnostics-and-updates), [§27A Local Security](docs/plan.md#local-security), [§30 Kill and Pivot Criteria](docs/plan.md#30-kill-and-pivot-criteria) |
| M24 | [§28 Initial Demonstrations](docs/plan.md#28-initial-demonstrations), [§28 ReserveFlow Dogfood API Refinement Trial](docs/plan.md#reserveflow-dogfood-api-refinement-trial), [§29 Revised Development Sequence](docs/plan.md#29-revised-development-sequence), [§30 Kill and Pivot Criteria](docs/plan.md#30-kill-and-pivot-criteria) |

## Dependency Spine

```text
M00 scope freeze
 -> M01 repository bootstrap
 -> M02 domain states
 -> M03 SQLite authority
 -> M04 credentials/redaction
 -> M05 event journal
 -> M06 v5 transport spike
 -> M07 gRPC contracts
 -> M08 repository intelligence
 -> M09 worktree and diff
 -> M10 mediated tools
 -> M11 coordinator/worker
 -> M12 providers
 -> M13 fixed routing and budgets
 -> M14 agent loop
 -> M15 recovery
 -> M16 frontend shell
 -> M17 conversation UI
 -> M18 live task controls
 -> M19 task graph
 -> M20 review/evidence
 -> M21 deterministic memory
 -> M22 test and benchmark harness
 -> M23 packaging and hardening
 -> M24 vertical-slice exit and ReserveFlow dogfood refinement trial
```

## How to Use This Checklist

- Complete milestones in order unless a task explicitly says it can run in parallel.
- Treat every `GATE` item as blocking for the next milestone.
- Keep pull requests and commits small enough that one checklist item or one tightly related cluster can be reviewed independently.
- Add the implementation commit, test name, or benchmark result beneath a completed item when the result is not obvious from the source.
- Do not mark a task complete because code exists; mark it complete only when its stated test or acceptance condition passes.
- Do not silently weaken an acceptance condition. Record the decision in `docs/plan.md` and update this file.
- Store Codeflux-managed threads, events, tasks, graphs, atoms, vectors, evidence, budgets, and learned artifacts in SQLite.
- Keep source code, protobuf definitions, SQL migrations, tests, build configuration, and documentation as ordinary Git-tracked repository files.
- Store provider credentials in the operating-system credential store, never in SQLite.
- Keep the initial routing policy fixed until baseline telemetry is credible.
- Keep the graph optional, task-scoped, and read-only in the prototype.
- Do not implement the deep Go semantic verifier until its experiment gate authorizes it.

## Task Labels

| Label | Meaning |
|---|---|
| `BLOCKER` | Required before dependent work can start |
| `GATE` | Evidence-based milestone exit criterion |
| `SECURITY` | Requires explicit abuse-case or boundary testing |
| `DATA` | Changes SQLite schema, persistence, or migration behavior |
| `UX` | User-visible workflow or accessibility requirement |
| `SPIKE` | Time-boxed investigation that must end in a recorded decision |
| `TEST` | Primarily verification work |
| `DOC` | Documentation or operator-facing guidance |
| `DEFER` | Explicitly outside the first prototype |

## Prototype Definition of Done

- [ ] `DONE-001` A new user can install or build Codeflux, open a local Go repository, configure one provider, and begin a task without manually editing the database.
- [ ] `DONE-002` A user can describe a change in chat, inspect the proposed scope and budget, approve execution, observe progress, review the diff and evidence, and accept or roll back the work.
- [ ] `DONE-003` Every task runs in an isolated Git worktree or equivalent isolated branch workspace.
- [ ] `DONE-004` The coordinator can pause, cancel, checkpoint, recover, and resume a task without duplicating correctness-bearing actions.
- [ ] `DONE-005` The interface shows the selected model, effort level, forecast range, actual usage, actual cost, and hard budget.
- [ ] `DONE-006` The fixed baseline routing policy is deterministic and recorded with every run.
- [ ] `DONE-007` At least OpenAI, Anthropic, and one OpenAI-compatible local endpoint can be configured through the provider interface.
- [ ] `DONE-008` Provider credentials remain in the OS credential store and are absent from SQLite, logs, prompts, diagnostics, and UI event payloads.
- [ ] `DONE-009` The task-scoped graph can show Program, Execution, and Evidence views linked to stable identities in the chat thread.
- [ ] `DONE-010` SQLite is the sole authoritative store for Codeflux-managed runtime and learning state.
- [ ] `DONE-011` A killed and restarted coordinator can replay the journal, validate the worktree binding, and present a safe recovery choice.
- [ ] `DONE-012` Risky commands and external effects require a precise inline approval with allow-once, allow-for-task, and deny choices.
- [ ] `DONE-013` The prototype passes its unit, integration, migration, reconnect, security-boundary, and end-to-end smoke suites.
- [ ] `DONE-014` The prototype completes the frozen demonstration task with an inspectable timeline, diff, evidence report, and cost summary.
- [ ] `DONE-015` Known limitations, unsupported guarantees, and deferred enterprise features are visible and documented.
- [ ] `DONE-016` From a frozen clean scaffold, Codeflux builds the chronological ReserveFlow API through independent hidden acceptance without manual source edits, and every Codeflux defect found is reproduced, fixed or explicitly deferred, and rerun from the original clean task boundary.

---

# Milestone 00: Freeze the Prototype Contract

Goal: turn the broad plan into a bounded prototype contract before implementation choices silently expand scope.

Plan references: §0 Linear Concept and Build Order; §1 Product Constraints; §2 Revised Product Thesis; §3 Load-Bearing Experiments; §27 Initial Product Scope; §29 Phase 0; §30 Kill and Pivot Criteria; §32 Central Design Principles.

Depends on: no implementation dependency.

Milestone output: a frozen prototype contract, benchmark task, acceptance authority, fixed-policy baseline, explicit non-goals, and one unambiguous small-to-large concept/build order recorded in the plan.

## Product Boundary

- [x] `M00-001 BLOCKER` Write a one-paragraph prototype promise using correctness, speed, and cost in that order.
- [x] `M00-002 BLOCKER` Define the initial user as a single technically capable hobbyist working on a repository they control.
- [x] `M00-003 BLOCKER` Define the supported host operating systems for the prototype.
- [x] `M00-004 BLOCKER` Decide whether the first internal build may support only the current development OS while preserving portable interfaces.
- [x] `M00-005 BLOCKER` Define the supported repository state: local Git repository with a clean or explicitly acknowledged dirty worktree.
- [x] `M00-006 BLOCKER` Define the supported language scope for repository intelligence as Go-first.
- [x] `M00-007 BLOCKER` Define which task classes must work: scoped bug fix, feature, test, refactor, dependency/configuration change, and behavior-linked documentation.
- [x] `M00-008 BLOCKER` Define the maximum number of simultaneously active tasks in the prototype.
- [x] `M00-009 BLOCKER` Define whether multiple inactive threads may exist for one repository.
- [x] `M00-010 BLOCKER` Confirm that the graphical interface is primary but the coordinator remains usable through a minimal CLI for diagnostics and automation.
- [x] `M00-011 BLOCKER` Confirm that ordinary source code remains the editable medium.
- [x] `M00-012 BLOCKER` Confirm that the graph is initially derived, task-scoped, optional, and read-only.
- [x] `M00-013 BLOCKER` Confirm that direct visual programming is excluded.
- [x] `M00-014 BLOCKER` Confirm that an embedded code editor, full file explorer, and terminal are excluded.
- [x] `M00-015 BLOCKER` Confirm that multi-user collaboration, hosted accounts, and repository upload are excluded.
- [x] `M00-016 BLOCKER` Confirm that adaptive routing is shadow-only until the fixed-policy baseline is measured.
- [x] `M00-017 BLOCKER` Confirm that semantic Go graph generation and deep verification remain gated research tracks.
- [x] `M00-018 DOC` Add a concise prototype/non-goals summary near the top of `docs/plan.md` if it is not already discoverable.

## Acceptance and Measurement Contract

- [x] `M00-019` Define what counts as a successfully completed coding task.
- [x] `M00-020` Define what counts as a user-accepted result.
- [x] `M00-021` Define what counts as an agent-caused regression.
- [x] `M00-022` Define what counts as an unauthorized action.
- [x] `M00-023` Define correctness metrics for the prototype.
- [x] `M00-024` Define latency metrics for time-to-plan, time-to-first-action, time-to-first-diff, and time-to-completion.
- [x] `M00-025` Define token and monetary cost metrics.
- [x] `M00-026` Define usability metrics for installation, first task, interruption, review, and recovery.
- [x] `M00-027` Define persistence metrics for replay and crash recovery.
- [x] `M00-028` Define UI responsiveness thresholds for long threads and graph updates.
- [x] `M00-029` Define a hard maximum acceptable reconnect data-loss count of zero for correctness-bearing events.
- [x] `M00-030` Define a hard maximum acceptable duplicate-action count of zero after retries or reconnects.
- [x] `M00-031` Define the prototype's unsupported assurance claims in plain language.
- [x] `M00-032` Choose one frozen end-to-end demonstration repository and task.
- [x] `M00-033` Record the demonstration repository revision.
- [x] `M00-034` Write hidden or separately stored acceptance tests for the demonstration.
- [x] `M00-035` Define the direct manual or current-agent baseline for comparison.
- [x] `M00-036` Freeze the model, reasoning effort, prices, and tools used by the initial baseline.
- [x] `M00-037` Define the evidence that authorizes moving from fixed routing to shadow forecasting.
- [x] `M00-038` Define the evidence that authorizes expanding the graph beyond execution/evidence visualization.

## Plan Ordering and Dependency Clarity

- [x] `M00-039 DOC` Add a canonical small-to-large concept stack to `docs/plan.md` from typed values through stable identities, revisions, transitions, events, application functions, flows, UI, evidence, memory, policies, and optional graph verification.
- [x] `M00-040 DOC` Define the core vocabulary so identity, revision, command, query, event, projection, flow, evidence, episode, knowledge, atom, graph, and policy are introduced before their compositions.
- [x] `M00-041 DOC` Map the full prototype into linear Layers 0 through 17 and later ambitions into Layers 18 through 20.
- [x] `M00-042 DOC` Add prohibited dependency directions so later graph, vector, adaptive, multi-agent, and enterprise concepts cannot leak into earlier foundations.
- [x] `M00-043 DOC` Add explicit branch and stop gates for vectors, adaptive routing, multi-agent execution, and deep verification.
- [x] `M00-044 DOC` Add one canonical reading sequence and direct implementation work to dependency-ordered M00-M24 rather than numeric plan headings.
- [x] `M00-045 TEST` Add a documentation check that every implementation milestone maps to one canonical layer and no canonical layer references a missing plan section.
- [x] `M00-046 DOC` Require every new major subsystem to identify its smallest prerequisite concepts, owning layer, inputs, outputs, and forbidden forward dependencies.

## Gate

- [x] `M00-G01 GATE` Every prototype feature maps to a user journey step or a measurement requirement.
- [x] `M00-G02 GATE` Every explicitly deferred feature is absent from the critical path.
- [x] `M00-G03 GATE` The frozen demonstration can be evaluated without relying on author judgment.
- [x] `M00-G04 GATE` The prototype promise does not claim proof of external system behavior.
- [x] `M00-G05 GATE` Product concepts, implementation layers, branch points, and reference chapters form one unambiguous small-to-large path with no hidden circular dependency.

Completion evidence:

- Frozen contract: `docs/plan.md`, "Frozen Prototype Contract".
- Demonstration scaffold: `reserveflow` revision `1d14bb1b01ceb8ecb691e58b10998f37e66ee8d9`.
- Independent evaluator: `reserveflow-evaluator` revision `c7179fd32d9b010877db2c3102bce327cf980bbf`.
- Evaluator self-containment test: passed; Task 1 acceptance test is intentionally red against the empty frozen scaffold.
- Traceability: `docs/check-plan-trace.ps1` passed for M00-M24, Layers 0-20, canonical section references, and the major-subsystem declaration rule.

---

# Milestone 01: Repository and Toolchain Bootstrap

Goal: create a repeatable Go workspace with strict quality checks and an intentionally boring build path.

Plan references: §27 Hobbyist MVP Decisions; §29 Phase 1: Code-First Agent Runtime.

Depends on: `M00-G01` through `M00-G05`.

Milestone output: a clean Go workspace that builds, generates, tests, reports its version from a fresh clone, and enforces repository-wide agent and atom-documentation rules.

## Repository Structure

- [x] `M01-001 BLOCKER` Initialize the Go module with the chosen module path.
- [x] `M01-002 BLOCKER` Pin the minimum supported Go version.
- [x] `M01-003` Check whether the user has explicitly requested a root `README.md`; create only that file if explicitly authorized, otherwise record that its intentional absence does not block the milestone.
- [x] `M01-004` Add a root `LICENSE` after choosing the project license.
- [x] `M01-005` Add a root `.gitignore` rule for `.artifacts/`, the sole repository-local root for binaries, WASM output, packages, coverage, profiles, benchmarks, test-failure captures, temporary development databases, disposable logs, generated comparison trees, and staging files.
- [x] `M01-006` Add `.gitattributes` rules for consistent text normalization.

Repository bootstrap evidence:

- Module path: `codeflux.dev/codeflux`.
- Minimum language version: Go 1.26.0; CI/tool bootstrap must use the current patched Go 1.26 release rather than silently accepting an older security patch.
- Root `README.md`: intentionally absent because the user did not explicitly request that file; this does not block M01.
- License: Apache License 2.0.
- Text normalization: LF by default, CRLF only for Windows batch command files, and common binary assets marked binary.
- [x] `M01-007` Create `cmd/codeflux` for the user-facing executable.
- [x] `M01-008` Create `internal/coordinator` for coordinator application logic.
- [x] `M01-009` Create `internal/worker` for task-worker logic.
- [x] `M01-010` Create `internal/domain` for stable domain types.
- [x] `M01-011` Create `internal/storage` for SQLite repositories and transactions.
- [x] `M01-012` Create `internal/events` for journal and stream contracts.
- [x] `M01-013` Create `internal/providers` for model-provider abstractions.
- [x] `M01-014` Create `internal/workspace` for repository discovery and context assembly.
- [x] `M01-015` Create `internal/executor` for mediated tool and command execution.
- [x] `M01-016` Create `internal/gitwork` for Git/worktree operations.
- [x] `M01-017` Create `internal/agent` for planning and execution orchestration.
- [x] `M01-018` Create `internal/graph` for graph projection and queries.
- [x] `M01-019` Create `internal/review` for diffs, validation, and evidence.
- [x] `M01-020` Create `internal/transport` for gRPC and browser bridging.
- [x] `M01-021` Create `web` or the v5-prescribed directory for GoWebComponents client code.
- [x] `M01-022` Create `api/proto` for protobuf definitions.
- [x] `M01-023` Create `migrations` for embedded SQL migrations.
- [x] `M01-024` Create `testdata` with clear rules preventing secrets or private repositories.
- [x] `M01-025` Add package documentation stating the dependency direction between internal packages.

Package-skeleton evidence: `go list ./...` found 19 documented packages;
`go build -o .artifacts/bin/codeflux.exe ./cmd/codeflux`, `go test ./...`,
and `go vet ./...` passed; the testdata policy permits only synthetic or
explicitly redistributable, secret-free fixtures.

## Build and Quality Tooling

- [x] `M01-026 BLOCKER` Add a single local command that builds server, worker, generated protobuf code, and frontend assets.
- [x] `M01-027 BLOCKER` Add a single local command that runs the complete fast test suite.

Initial command evidence:

- `go run ./cmd/codeflux-dev build` compiles every tracked package and writes `codeflux` and `codeflux-worker` only beneath `.artifacts/bin`.
- `go run ./cmd/codeflux-dev test-fast` runs the complete current module test suite.
- Generated protobuf and embedded frontend packages join the all-package build automatically when their later source tasks add them; no generator capability is claimed yet.
- [x] `M01-028` Add `go fmt` verification.
- [x] `M01-029` Add `go vet` verification.
- [x] `M01-030` Choose and configure a Go linter set with low false-positive noise.
- [x] `M01-031` Add race-detector coverage for packages that own concurrency.
- [x] `M01-032` Add unit-test coverage reporting without making raw percentage the quality target.

Quality-command evidence:

- `lint` verifies formatting in-process, runs `go vet ./...`, and requires/runs Staticcheck 2026.1.
- `test-race` runs `go test -race ./...` on supported hosts and fails explicitly on the current unsupported Windows ARM64 host; the supported CI execution is tracked by M01-051.
- `test-coverage` writes `.artifacts/coverage/unit.out`; coverage is diagnostic and has no raw-percentage gate.
- [x] `M01-033` Add protobuf generation through a pinned tool version.

Protobuf generation evidence: Buf 1.72.0 compiles the `api/proto` module and
invokes `google.golang.org/protobuf/cmd/protoc-gen-go` from the module-pinned
v1.36.11 dependency. Generation and isolated generation-check pass without a
remote-plugin registry request.
- [x] `M01-034` Add migration embedding through `go:embed`.
- [x] `M01-035` Add frontend WASM asset embedding or deterministic packaging.

Embedding and packaging evidence: package tests read the exact embedded
`000000_bootstrap.sql` path; deterministic frontend packaging accepts the empty
pre-spike asset set and its generator test packages a Go/WASM fixture. The
bootstrap migration is an intentional no-op and does not pre-implement M03
schema.
- [x] `M01-036` Ensure generated files carry a generated-file header.
- [x] `M01-037` Ensure generation is reproducible and leaves the worktree clean when rerun.
- [x] `M01-038` Add a check that committed generated code matches protobuf sources.
- [x] `M01-039` Add a check that migration numbers are unique and ordered.
- [x] `M01-040` Add a check that no local SQLite database is committed.
- [x] `M01-041 SECURITY` Add a secret-scanning hook or CI step for common provider-key formats.

Repository-integrity evidence:

- `generate-check` regenerates beneath a validated `.artifacts/tmp` child, compares the exact `api/gen` tree, and removes only that child.
- `lint` requires generated headers, contiguous unique migration numbers, no tracked SQLite extension or magic, and no common OpenAI, Anthropic, or GitHub token shape in tracked text.
- `.githooks/pre-commit` invokes the same lint command; CI enforcement is added separately by M01-050.
- [x] `M01-042` Add structured version metadata: semantic version, commit, build date, Go version, schema version, and frontend version.
- [x] `M01-043` Add `codeflux version`.
- [x] `M01-044` Add `codeflux doctor` skeleton.

Version/doctor evidence: the development build links deterministic Git commit
and commit-date metadata, reports all six required fields, and returns exit 3
from `doctor` while explicitly naming storage, credential-store, and browser
transport as unavailable rather than healthy.
- [x] `M01-045 DOC` Document required native dependencies, if any.
- [x] `M01-046 DOC` Document how to regenerate protobuf and WASM assets.

Documentation evidence: `docs/plan.md` lists the real Go, Git, and Staticcheck
requirements; identifies pinned network-fetched protobuf tooling; gives the
copy/paste generate/check/lint/test sequence; and explicitly states that no
WASM generator exists yet.

## Continuous Integration

- [x] `M01-047` Add CI for the primary development operating system.
- [x] `M01-048` Add CI for all declared prototype operating systems.
- [x] `M01-049` Cache Go modules without caching generated correctness results.
- [x] `M01-050` Run formatting, vet, lint, unit tests, migration checks, generation checks, and secret scanning in CI.
- [x] `M01-051` Run race tests on at least one CI job.
- [x] `M01-052` Upload redacted test artifacts only when a job fails.
- [x] `M01-053` Prevent CI logs from printing provider secrets.
- [x] `M01-054` Add a dependency-update policy and review cadence.

Evidence:

- `.github/workflows/ci.yml` assigns the primary quality job to GitHub-hosted Windows 11 ARM64 and build/test matrix jobs to every frozen prototype target.
- `actions/setup-go` keys dependency caching from `go.sum`; no generated tree, test result, or `.artifacts` path is cached.
- Quality invokes the repository lint aggregate, deterministic regeneration, coverage tests, and build; Ubuntu AMD64 invokes the race helper.
- Failure uploads select only the helper-generated allow-listed `context.json`; unit coverage, raw logs, environment variables, and provider credentials are never upload inputs.
- Provider credential variables are explicitly empty in every job, checkout credentials are not persisted, and Dependabot groups Go and workflow updates on a weekly review cadence.
- The failure-artifact unit test, fast suite, lint aggregate, `actionlint` v1.7.12, and whitespace validation passed locally.

## Agent Instruction and Harness Files

Plan: §21 Agent Architecture; §24 Specification Review; §27 Hobbyist MVP Decisions; §29 Revised Development Sequence; §32 Central Design Principles.

- [x] `M01-055 DOC` Create root `AGENTS.md` as the authoritative repository-wide instruction file. Output: `AGENTS.md`. Verify: it identifies scope, reading order, product boundaries, workflow, storage, security, frontend, testing, and handoff rules.
- [x] `M01-056 DOC` Create root `CLAUDE.md` as a thin Claude Code compatibility entry point. Output: `CLAUDE.md`. Verify: it directs Claude to `AGENTS.md`, `docs/plan.md`, and `TODOS.md` without duplicating the full rule set.
- [x] `M01-057 DOC` Add the community-maintained Karpathy-inspired agent-coding guidelines as an accurately labeled reference. Output: links and attribution in `AGENTS.md` and `CLAUDE.md`. Verify: both files state that the source is community-maintained and not an official Andrej Karpathy repository.
- [x] `M01-058 DOC` Adapt the four relevant disciplines into Codeflux-specific rules: visible assumptions, smallest sufficient design, surgical edits, and verifiable outcomes. Output: `AGENTS.md` Agentic Coding Discipline. Verify: each discipline contains executable repository-specific behavior rather than a slogan.
- [x] `M01-059 DOC` Establish an instruction authority model that prevents drift. Output: `AGENTS.md` is authoritative and `CLAUDE.md` is a pointer. Verify: no project rule is independently restated in full in `CLAUDE.md`.

Evidence:

- Root `AGENTS.md` defines repository scope, mandatory reading order, product boundaries, artifact and ledger policy, four executable coding disciplines, storage, security, frontend, testing, and handoff rules.
- Root `CLAUDE.md` remains a thin compatibility pointer to `AGENTS.md`, `docs/plan.md`, and `TODOS.md`; it does not restate the authoritative rule set.
- Both files identify the pinned community-maintained reference and explicitly state that it is not an official Andrej Karpathy repository.
- `AGENTS.md` contains the versioned atom-comment field template and standalone-descriptive atom naming grammar required by M01-066 and M01-072.

- [x] `M01-060 TEST` Add a documentation check that requires root `AGENTS.md` and `CLAUDE.md`.
- [x] `M01-061 TEST` Add a documentation check that `CLAUDE.md` still points to `AGENTS.md`, `docs/plan.md`, and `TODOS.md`.
- [x] `M01-062 TEST` Add a link check for repository-relative instruction links and the pinned external Karpathy-inspired reference.
- [x] `M01-063 DOC` Add real build, test, generation, migration, and benchmark commands to `AGENTS.md` only after those commands exist.
- [x] `M01-064 DOC` Review agent instructions at each prototype milestone gate and remove rules that have become false, redundant, or unhelpful.
- [x] `M01-065 TEST` Add an instruction smoke scenario in which a coding agent must identify the relevant plan section and TODO ID before changing a fixture.
- [x] `M01-066 DOC` Define the required structured Go atom-comment style in `AGENTS.md`. Output: versioned field template covering selection, semantics, contracts, effects, failures, retry, reconciliation, security, dependencies, examples, verification, and retrieval concepts. Verify: the template starts with the Go identifier and distinguishes descriptive comments from correctness authority.
- [x] `M01-067 TEST` Add a repository check that every Go atom declaration has a schema-versioned Codeflux atom doc comment.
- [x] `M01-068 TEST` Add a repository check that every required atom-comment field is present and is either substantive or `None` with a reason.
- [x] `M01-069 TEST` Add a repository check that atom comments begin with their Go identifier and pass Go doc-comment linting.
- [x] `M01-070 TEST` Add fixtures for complete, missing-field, empty-field, malformed-version, keyword-stuffed, and identifier-mismatched atom comments.
- [ ] `M01-071 DOC` Add a reviewed real atom-comment example after the first executable atom exists; do not invent a fake production contract before then.
- [x] `M01-072 DOC` Define the atom naming grammar in `AGENTS.md`. Output: `<Verb><DomainObject><ImportantQualifier><ObservableOutcome>` guidance with good and bad examples. Verify: it explicitly prefers a longer contextual name over a generic short name.
- [x] `M01-073 TEST` Add a naming check that rejects empty, single-generic-word, filler-suffixed, version-encoded, and hash-encoded atom names.
- [x] `M01-074 TEST` Add a naming check that requires executable atom names to begin with a recognized concrete action verb or receive an explicit reviewed exception.
- [x] `M01-075 TEST` Add a naming check that detects unexplained abbreviations and requires an allowlisted established domain abbreviation.
- [x] `M01-076 TEST` Add fixtures for descriptive names, ambiguous names, misleading guarantee names, provider-specific names, semantic-preserving renames, and semantic-breaking renames.
- [ ] `M01-077 DOC` Add a naming review checklist to the first real atom pull-request template after repository contribution templates exist.
- [x] `M01-078 TEST` Verify canonical Go name, display name, and normalized semantic phrase are deterministically derived and remain traceable to one atom identity.

Instruction-check evidence:

- The lint aggregate now requires tracked root instruction files, a thin `CLAUDE.md`, all three authority pointers, the exact pinned community reference, accurate attribution, resolvable repository-relative links, and the strong Markdown-creation prohibition.
- The synthetic smoke transcript must place an anchored `docs/plan.md` section and declared atomic TODO before its fixture edit; the negative unit case reverses that order and is rejected.
- Instruction, missing-file, missing-pointer, weakened-rule, missing-link, escaping-link, and smoke-order tests pass.

Atom-admission evidence:

- `//codeflux:atom` establishes the explicit source admission boundary; an orphan marker or admitted declaration without the exact schema v1 header fails lint.
- The parser requires every v1 field exactly once, substantive text or `None: <reason>`, identifier-first prose, and non-stuffed retrieval material.
- Named Go fixtures exercise complete, missing, empty, malformed-version, keyword-stuffed, and mismatched comments.
- Table-driven name fixtures cover descriptive, ambiguous, filler, version, hash, action-exception, abbreviation, misleading-guarantee, provider-specific, and rename classification cases.
- Deterministic derivation binds canonical name, display name, and normalized phrase to the same explicit atom identity.

## Developer Command and Package Experience

Plan: §27B Process and Package Ownership; §27D Planned Repository Layout, Development Helper, Local Development Profiles, Generated Code Workflow, and Inner Development Loop.

- [x] `M01-079 BLOCKER` Create `cmd/codeflux-dev` as the cross-platform development entry point without requiring Make, Bash, PowerShell, Node scripts, or a global task runner.
- [x] `M01-080` Define the development command registry with name, purpose, prerequisites, arguments, exit codes, and machine-readable-output capability.
- [x] `M01-081` Implement `codeflux-dev bootstrap` to verify pinned Go, Git, protobuf, GoWebComponents, and generator requirements.
- [x] `M01-082` Implement `codeflux-dev generate` as the only normal entry point for protobuf, event registry, migration catalog, version metadata, and frontend generation.
- [x] `M01-083` Implement `codeflux-dev generate-check` using an isolated generation target and committed-output comparison.
- [x] `M01-084` Implement `codeflux-dev run` with temporary SQLite, fake credentials, scripted provider, ephemeral port, and no external network.
- [x] `M01-085` Implement `codeflux-dev run-live` with explicit provider, credential reference, database, and visible cost warning.
- [x] `M01-086` Implement named `test-fast`, `test-integration`, `test-browser`, `test-security`, `test-race`, and `test-all` commands.
- [x] `M01-087` Implement `lint` orchestration for format, vet, selected linters, documentation links, protobuf compatibility, migrations, atom names, and atom comments.
- [x] `M01-088` Implement `seed`, `replay`, `inspect-db`, `benchmark`, and `doctor` command skeletons with honest unavailable messages until their subsystems exist.
- [x] `M01-089` Define deterministic, interactive-fake, live-provider, and fault-injection development profiles.
- [x] `M01-090` Ensure each development command accepts an explicit temporary/application root and never defaults destructive work to the repository root.
- [x] `M01-091` Ensure each command supports `--help`, stable non-zero failure codes, and the exact failing sub-step.
- [x] `M01-092` Add a package-dependency check that enforces inward domain dependencies and prevents storage/provider/transport code from owning frontend or domain policy.
- [x] `M01-093` Add a generated-file check that rejects hand edits and generation outside declared paths.
- [x] `M01-094 DOC` Document the atomic inner loop from plan/TODO selection through targeted test, implementation, broader verification, diff inspection, and completion evidence.
- [x] `M01-095 TEST` Run bootstrap, generate-check, lint, and fast tests from a clean clone using only documented commands.
- [x] `M01-096 DOC` Define in `AGENTS.md` that agents may not create any new Markdown file without an explicit user request naming that file.
- [x] `M01-097 TEST` Add an instruction check that fails if the Markdown-creation rule is removed or weakened.
- [x] `M01-098 BLOCKER` Configure every development command to place repository-local disposable output beneath `.artifacts/`.
- [x] `M01-099 TEST` Verify build, test, race, coverage, browser, benchmark, package, diagnostic, replay, and failure-preservation commands create no repository-local artifact outside `.artifacts/`.
- [x] `M01-100 TEST` Verify a command rejects an explicit repository-local artifact destination outside `.artifacts/` before writing.
- [x] `M01-101 TEST` Verify cleanup resolves and validates a child of `.artifacts/` and cannot delete the repository root, source directories, or an external path.
- [x] `M01-102 TEST` Verify `.artifacts/` and every descendant remain ignored, cannot enter a normal commit, and need no tracked placeholder.
- [x] `M01-103` Configure CI artifact uploads to select redacted files from `.artifacts/` without creating another staging root.
- [x] `M01-104 TEST` Scan the worktree after every CI command and fail on known disposable build/test artifact types outside `.artifacts/`.

Developer-command registry evidence:

- `go run ./cmd/codeflux-dev help` discovers the sorted command surface without Make, shell scripts, Node, or a global task runner.
- Registry schema v1 records each command's purpose, prerequisites, arguments, availability, stable exit meanings, and current machine-output capability; JSON discovery is deterministic.
- Every declared command supports text and JSON help. Unimplemented subsystem commands return exit 3 and a versioned unavailable result instead of claiming success.
- Shared root resolution defaults to a command child beneath `.artifacts`, rejects the repository root, and rejects any repository-local destination outside `.artifacts`.

Bootstrap evidence:

- `go run ./cmd/codeflux-dev bootstrap --json` selected and verified Go 1.26.5, Git 2.54.0, Buf 1.72.0, protoc-gen-go 1.36.11, and Staticcheck 2026.1.
- Pinned tools install beneath `.artifacts/tools/bin`; provider-token, credential, password, and secret-shaped environment variables are removed before tool subprocesses.
- Bootstrap validates `go.mod`, Buf configuration, and generator source pins before installation.
- The GoWebComponents check proves the dependency is absent and M06-001 remains the authority for selecting the exact v5 release; this intentional deferred result does not masquerade as an installed framework.

Generation evidence:

- The single `generate` command now produces protobuf Go types, a directive-derived event registry, checksummed migration catalog, schema/frontend version constants, and a checksummed frontend asset manifest.
- Every generated Go file has a declared path and first-line generator warning; lint rejects generated suffixes or headers outside those paths.
- `generate-check` regenerated all five output families beneath a validated `.artifacts/tmp` child, compared exact file sets and bytes, removed the child, and left the worktree unchanged.
- Generator fixture tests proved byte-identical repeated output, source-family coverage, duplicate event rejection, and embedded migration/asset checksum agreement.

Development-profile evidence:

- `go run ./cmd/codeflux-dev run --once --json` created a unique temporary `.sqlite` target, held fake credentials only in memory, exposed a fixed scripted-provider profile, bound `127.0.0.1` on an ephemeral port, passed its bounded health self-check, shut down, and removed the run child.
- The HTTP client disables proxies and the profile declares external provider access false; no provider credential is read or persisted.
- Registry data defines deterministic, interactive-fake, live-provider, and 13-boundary fault-injection profiles without claiming their later coordinator/storage/provider subsystems already exist.
- Unit tests verify loopback-only binding, zero-length new SQLite target, fake state, profile inventory, fault boundaries, and safe artifact-child classification.

Command-orchestration evidence:

- `run-live` requires an explicit supported provider, non-secret `os://` credential reference, and absolute database outside the repository; it shows the real-cost warning and returns stable unavailable exit 3 until M04/M12 adapters exist.
- Current-scope integration and security commands run real Go suites; browser returns a versioned unavailable result until M16; race retains the platform-specific failure and Ubuntu CI route.
- `test-all` ran lint, isolated generation, fast, integration, and security sub-steps in order and named the precise sub-step on failure.
- Lint now covers gofmt, repository instructions/links, migration ordering, atom comments/names, generated paths, provider-token scanning, Buf lint, Buf breaking against committed `HEAD`, Go vet, and Staticcheck 2026.1.
- Seed, replay, inspect-db, and doctor skeletons return stable exit 3 with text or versioned JSON rather than fabricated subsystem results; benchmark is now a real M01 microbenchmark runner.

Developer-safety evidence:

- Every registry command supports `--help` and `--root`; a table test sends an unsafe repository-local root to every command and proves exit 2 occurs before the path is created.
- Output-producing build, coverage, generation-check, benchmark, failure-capture, and deterministic-run commands honored explicit safe roots; cleanup removed only resolved `.artifacts` children.
- The package check parses production Go imports and rejects outward domain imports, frontend imports from adapters, and concrete sibling-adapter coupling.
- Generated headers and suffixes are limited to declared paths, while isolated regeneration rejects byte-level hand edits.
- The Markdown rule negative test, ignored-artifact and forced-add tests, disposable-extension escape test, and cleanup boundary tests pass.
- CI invokes `artifact-check` with `always()` after every build/test/generation/race command and uploads only `.artifacts/test-failures/context.json` on failure.
- Real migration validation and atom-name/generation benchmarks now back the commands documented in `AGENTS.md`; the milestone instruction review updated stale “planned helper” wording.

## Gate

- [x] `M01-G01 GATE` A fresh clone builds with one documented command.
- [x] `M01-G02 GATE` A fresh clone runs the fast tests with one documented command.
- [x] `M01-G03 GATE` Regeneration is deterministic and produces no unexplained diff.
- [x] `M01-G04 GATE` CI passes on every declared prototype platform.
- [x] `M01-G05 GATE` Root `AGENTS.md` and `CLAUDE.md` pass instruction-presence, reference, and link checks without duplicating their authoritative rules.
- [x] `M01-G06 GATE` Atom-comment schema, lint rules, and fixtures prevent undocumented or structurally incomplete atoms from entering the reusable catalog.
- [x] `M01-G07 GATE` Atom names remain standalone-descriptive, semantically scoped, non-misleading, and deterministically represented for graph and retrieval use.
- [x] `M01-G08 GATE` A fresh contributor can discover, generate, lint, test, and start the deterministic development profile through one cross-platform helper and documented commands.
- [x] `M01-G09 GATE` Agents create no new Markdown files without an explicit user request naming the file, and all repository-local disposable build/test artifacts remain under the ignored `.artifacts/` root.

Gate evidence:

- A local no-hardlink clone under `.artifacts/tmp` ran the documented bootstrap, build, fast tests, generate-check, lint, migration-check, deterministic run self-check, atom-name benchmark, and artifact-check commands successfully; `git diff --exit-code` and `git status --short` were empty.
- Bootstrap selected Go 1.26.5 and installed all pinned tools inside the clone's ignored `.artifacts/tools`.
- Instruction, atom documentation, atom naming, root safety, package direction, generated path, cleanup, and artifact policy checks all ran through clean-clone lint.
- Cross-builds succeeded for Windows ARM64, Windows AMD64, macOS ARM64, and Linux AMD64 without creating repository output.
- Public hosted CI run `30519967041` passed at commit `c8b3f2f2e3cb00a402bb9fb9205c48cda673c246`: Windows 11 ARM64 quality and build, Windows Server 2025 AMD64 build, macOS 15 ARM64 build, Ubuntu 24.04 AMD64 build, and Ubuntu 24.04 AMD64 race jobs all completed successfully.

---

# Milestone 02: Domain Model and State Machines

Goal: define explicit identities and transitions before transport or UI code depends on accidental structs.

Plan references: §5 Program Architecture; §18 Stable Graph Identity; §22 Correctness and Assurance Gates; §23 Core Operational Entities.

Depends on: `M01-G01` through `M01-G08`.

Milestone output: transport-independent identity, lifecycle, policy, budget, risk, and assurance types with exhaustive transition tests.

## Stable Identity Types

- [x] `M02-001 BLOCKER` Define distinct typed IDs for project, repository, workspace, thread, message, task, run, event, checkpoint, approval, graph, graph revision, node, edge, validation, evidence, artifact, atom, model request, provider, and budget.
- [x] `M02-002` Choose a sortable identifier format suitable for local generation.
- [x] `M02-003` Implement ID parsing and validation.
- [x] `M02-004` Reject empty or malformed IDs at domain boundaries.
- [x] `M02-005` Add JSON, SQL, and protobuf conversion tests for every ID type.
- [x] `M02-006` Ensure different ID types cannot be accidentally interchanged in Go.

Identity evidence:

- Every identity is a distinct non-assignable and non-convertible Go struct
  backed by a private typed core and a unique short kind prefix.
- The canonical payload is lowercase RFC 9562 UUIDv7. Its 48-bit Unix
  millisecond prefix sorts chronologically and its cryptographic entropy
  supports dependency-free local generation.
- Parsers reject empty values, wrong kind prefixes, non-canonical hexadecimal,
  misplaced separators, non-v7 UUIDs, and invalid variants with typed
  `IDParseError` values that unwrap to `ErrInvalidID`.
- JSON text and SQL scan/value boundaries reject null, empty, malformed, or
  wrong-representation input instead of admitting an invalid zero identity.
- Every identity kind round-trips through validating JSON text, SQL
  `driver.Valuer`/`sql.Scanner`, and deterministic generated-protobuf wire
  encoding. The transport envelope carries an explicit kind enum and rejects
  nil, unspecified, wrong-kind, empty, and malformed values without importing
  generated protobuf types into the domain package.

## Task and Run States

- [x] `M02-007 BLOCKER` Define task states: draft, forecasting, awaiting-plan-approval, ready, running, paused, awaiting-authority, validating, awaiting-review, completed, failed, cancelled, rolled-back, and recovery-required.
- [x] `M02-008 BLOCKER` Define permitted task-state transitions.
- [x] `M02-009` Define terminal and recoverable task states.
- [x] `M02-010` Define run states separately from task states.
- [x] `M02-011` Define command-execution states separately from run states.
- [x] `M02-012` Define approval-request states.
- [x] `M02-013` Define checkpoint states.
- [x] `M02-014` Define validation states.
- [x] `M02-015` Define graph-revision states.
- [x] `M02-016` Define change-acceptance states.
- [x] `M02-017` Add pure transition validators for each state machine.
- [x] `M02-018 TEST` Test every allowed state transition.
- [x] `M02-019 TEST` Test representative forbidden transitions.
- [x] `M02-020 TEST` Property-test that terminal states cannot silently return to running.
- [x] `M02-021 TEST` Property-test that approval is required before an awaiting-authority action begins.

State-machine evidence:

- Task, run, command execution, approval request, checkpoint, validation, graph
  revision, and change acceptance use distinct string types and transition
  tables in the pure domain package.
- `TransitionError` provides stable `ErrUnknownState`,
  `ErrInvalidTransition`, and `ErrApprovalRequired` classifications plus a
  bounded user-presentable message.
- Exhaustive table tests execute every declared allowed edge; independent
  negative cases cover each machine and require typed errors.
- Terminal task states cannot transition directly to running. Both task resume
  from `awaiting-authority` and command authorization require an explicitly
  granted approval state; pending, denied, expired, cancelled, and absent
  approvals fail.

## Policy, Budget, and Assurance Types

- [x] `M02-022` Define correctness/speed/cost policy presets.
- [x] `M02-023` Define reasoning-effort levels without coupling them to one provider's names.
- [x] `M02-024` Define model capability metadata.
- [x] `M02-025` Define monetary values using exact integer minor units or decimal arithmetic.
- [x] `M02-026` Define token usage by input, cached input, output, and provider-specific categories.
- [x] `M02-027` Define a task budget with warning and hard-stop thresholds.
- [x] `M02-028` Define P50/P90 forecast ranges for latency, tokens, and cost.
- [x] `M02-029` Define risk levels: routine, elevated, and protected.
- [x] `M02-030` Define assurance levels: fully evaluated, model verified, contract checked, runtime only, and invalidated.
- [x] `M02-031` Define validation result severity independent of log severity.
- [x] `M02-032` Define typed reasons for pause, failure, cancellation, rollback, and invalidation.
- [x] `M02-033` Add deterministic serialization tests for all policy-bearing types.

Policy and assurance evidence:

- User-facing correctness, balanced, fast, and economical presets preserve
  correctness as a policy floor while provider-neutral effort values remain
  independent of vendor terminology.
- Money and forecasts use integer minor units and exact integer counts. Unknown
  usage and forecast dimensions are explicit and cannot be serialized as
  known-zero values.
- Budgets carry warning and hard-stop thresholds for cost, tokens, wall clock,
  provider calls, repair rounds, and tool executions.
- Deterministic JSON coverage enumerates every policy-bearing type and every
  declared enum/reason value. An AST guard rejects binary floating-point types
  from the policy source.

## Gate

- [x] `M02-G01 GATE` No transport, database, or UI package owns an alternative task-state definition.
- [x] `M02-G02 GATE` Invalid transitions fail with typed, user-presentable errors.
- [x] `M02-G03 GATE` Cost arithmetic has no binary floating-point dependency.

---

# Milestone 03: SQLite Foundation

Goal: establish SQLite as the durable authority for all Codeflux-managed state.

Plan references: §23 Database Authority; Atom Storage; Graph Storage; Vector Storage; Core Operational Entities; Transactions, Migrations, and Recovery.

Depends on: `M02-G01` through `M02-G03`.

Milestone output: a migrated, backed-up, integrity-checked SQLite database plus domain repositories and real-database tests.

## Database Lifecycle

- [x] `M03-001 BLOCKER DATA` Select the Go SQLite driver and document CGO or pure-Go implications.
- [x] `M03-002 BLOCKER DATA` Define the default database location per operating system.
- [x] `M03-003 DATA` Create the application-data directory with restrictive permissions.
- [x] `M03-004 DATA` Open SQLite with foreign keys enabled.
- [x] `M03-005 DATA` Enable WAL mode.
- [x] `M03-006 DATA` Set and test a busy timeout.
- [x] `M03-007 DATA` Choose and document synchronous durability settings.
- [x] `M03-008 DATA` Configure a bounded connection pool appropriate for SQLite.
- [x] `M03-009 DATA` Add a health check that verifies reads, writes, foreign keys, and journal mode.
- [x] `M03-010 DATA` Add graceful close behavior.
- [x] `M03-011 DATA` Add corruption and unreadable-file error classification.
- [x] `M03-012 DATA` Prevent two incompatible Codeflux versions from concurrently migrating the same database.

Database-lifecycle evidence:

- `modernc.org/sqlite` v1.55.0 provides the pinned CGO-free driver across all
  declared CI platforms, including Windows ARM64.
- Default Windows, macOS, and Linux application-data locations and restrictive
  directory/file permissions are explicit and tested.
- Every pooled connection requests foreign keys, WAL, a bounded busy timeout,
  `synchronous=FULL`, disabled double-quoted string fallback, and immediate
  write intent. The pool is capped at four connections by default.
- Real-SQLite tests verify read/write health, foreign-key rejection, policy
  pragmas, actual lock-wait timeout bounds, corrupt content classification, and
  idempotent WAL-checkpointed close.

## Migration System

- [x] `M03-013 BLOCKER DATA` Define monotonically ordered forward migrations.
- [x] `M03-014 DATA` Create a schema-version table.
- [x] `M03-015 DATA` Record migration checksum, application version, start time, completion time, and result.
- [x] `M03-016 DATA` Embed migrations in the binary.
- [x] `M03-017 DATA` Apply migrations transactionally when SQLite permits.
- [x] `M03-018 DATA` Back up the database before a schema-changing migration.
- [x] `M03-019 DATA` Validate available disk space before backup and migration.
- [x] `M03-020 DATA` Refuse startup when the database schema is newer than the binary supports.
- [x] `M03-021 DATA` Preserve a failed migration error without repeatedly mutating the database.
- [x] `M03-022 TEST` Test migration from an empty database.
- [x] `M03-023 TEST` Test migration across every committed schema version.
- [x] `M03-024 TEST` Test restart after an interrupted migration.
- [x] `M03-025 TEST` Test backup restoration.

Migration-system evidence:

- An OS-backed per-database lock provides cross-process migration authority and
  releases automatically on process termination.
- Embedded catalog entries are contiguous and their generated SHA-256 values
  are rechecked against embedded SQL before use and against applied history.
- Migration-control tables retain schema version and complete attempt history.
  Each migration, version advance, and success record commits atomically.
- Disk-space validation precedes a restrictive SQLite Online Backup snapshot.
  Newer schemas, checksum drift, and stable prior failures refuse mutation.
- Real-SQLite tests cover empty migration, upgrade from every committed schema
  version, lock contention, insufficient space, downgrade refusal, checksum
  mismatch, transactional failure restoration, interrupted-start restoration,
  stable retry refusal, and direct backup restoration.

## Initial Operational Schema

- [x] `M03-026 BLOCKER DATA` Create project and repository tables.
- [x] `M03-027 BLOCKER DATA` Create workspace and worktree-binding tables.
- [x] `M03-028 BLOCKER DATA` Create thread and message tables.
- [x] `M03-029 BLOCKER DATA` Create task, run, and task-event tables.
- [x] `M03-030 BLOCKER DATA` Create approval and permission-decision tables.
- [x] `M03-031 BLOCKER DATA` Create provider, model-catalog, pricing-snapshot, and model-request tables without credential columns.
- [x] `M03-032 BLOCKER DATA` Create forecast, budget, and usage tables.
- [x] `M03-033 BLOCKER DATA` Create command-execution and redacted-output tables.
- [x] `M03-034 BLOCKER DATA` Create checkpoint and recovery-attempt tables.
- [x] `M03-035 BLOCKER DATA` Create artifact, diff-summary, validation, and evidence tables.
- [x] `M03-036 DATA` Add created, updated, and immutable-revision timestamps where semantically valid.
- [x] `M03-037 DATA` Add foreign keys for ownership and lineage.
- [x] `M03-038 DATA` Add uniqueness constraints for idempotency keys.
- [x] `M03-039 DATA` Add uniqueness constraints for monotonic per-session event sequences.
- [x] `M03-040 DATA` Add indexes for active tasks, thread pagination, event replay, approvals, worktree binding, and cost aggregation.
- [x] `M03-041 DATA` Add check constraints for non-negative token, time, and cost values.
- [x] `M03-042 DATA` Add check constraints for enumerated state values or validate them through strict repository code.
- [x] `M03-043 DATA` Decide where immutable rows are enforced by triggers versus application code.

Initial-schema evidence:

- Migration 1 creates all 26 Phase 1 operational tables with ownership and
  lineage foreign keys and no provider-credential columns.
- Mutable aggregates carry lifecycle timestamps and optimistic revisions.
  Append-only facts reject in-place updates through triggers while explicit
  deletion remains available to the later user-erasure lifecycle.
- Idempotency keys, message/event/output sequences, active-task, pagination,
  replay, approval, worktree, and cost queries have database constraints or
  indexes at their authority boundary.
- Real-SQLite schema tests enumerate every required table and index, reject
  invalid lineage, invented states, duplicate sequences, negative exact values,
  and immutable rewrites, scan every column for credential shapes, and pass
  foreign-key plus full integrity checks.

## Repository Layer

- [x] `M03-044 BLOCKER` Define a transaction runner with context cancellation.
- [x] `M03-045` Define repository interfaces around domain operations, not generic CRUD.
- [x] `M03-046` Implement project and repository persistence.
- [x] `M03-047` Implement thread creation and cursor pagination.
- [x] `M03-048` Implement message append with idempotency.
- [x] `M03-049` Implement task creation and transition persistence.
- [x] `M03-050` Implement atomic event append and sequence allocation.
- [x] `M03-051` Implement approval creation and resolution.
- [x] `M03-052` Implement budget reservation and actual-cost posting.
- [x] `M03-053` Implement checkpoint persistence.
- [x] `M03-054` Implement validation and evidence persistence.
- [x] `M03-055` Return typed not-found, conflict, stale-revision, busy, corruption, and constraint errors.
- [x] `M03-056 TEST` Test every repository against a temporary real SQLite database.
- [x] `M03-057 TEST` Test concurrent event append ordering.
- [x] `M03-058 TEST` Test idempotent retries of message, approval, and state-transition writes.
- [x] `M03-059 TEST` Test rollback after a failure halfway through a multi-table operation.
- [x] `M03-060 TEST` Test cancellation of a blocked database call.

Transaction-runner evidence:

- One immediate-write runner owns commit/rollback behavior and exposes no
  generic SQL surface to application packages.
- Typed not-found, conflict, stale-revision, busy, corruption, and constraint
  classifications remain matchable without exposing raw database details.
- A real-SQLite multi-table write injected with a halfway failure retains
  neither row.
- A contending writer with a 100 ms context deadline returns within the
  deadline rather than waiting for the five-second SQLite busy timeout, and the
  connection's configured timeout is restored afterward.
- Task state and its correctness-bearing event commit atomically under one
  expected revision. Twenty concurrent event appends receive each integer
  sequence exactly once.
- Identical task, message, approval, checkpoint, and transition retries return
  the original fact; changed claims conflict.
- Exact budget reservation and actual posting reject stale revisions, currency
  mismatch, overflow, excess release, and hard-cap excess. Checkpoints,
  validations, and evidence verify typed lineage in real SQLite.

## Diagnostics and Maintenance

- [x] `M03-061` Add `codeflux doctor` database checks.
- [x] `M03-062` Add an explicit user-triggered database backup command.
- [x] `M03-063` Add an explicit user-triggered integrity check.
- [x] `M03-064` Add safe WAL checkpointing on shutdown.
- [x] `M03-065` Define retention rules for verbose tool output.
- [x] `M03-066` Define deletion semantics for projects, threads, and learned artifacts.
- [x] `M03-067` Ensure deletion does not leave unreferenced vector or graph rows.
- [x] `M03-068` Add database size reporting to diagnostics.
- [x] `M03-069` Add schema and migration versions to diagnostics.

Diagnostics-and-maintenance evidence:

- `codeflux doctor` performs real path-free database health, byte-size, schema,
  supported-version, and migration-history checks without emitting selected
  database or executable paths.
- Explicit backup and integrity commands operate on a user-selected database,
  return stable non-zero failures, and expose no raw SQLite or filesystem
  details.
- Graceful close performs a truncating WAL checkpoint and reports busy or
  failed checkpoints before closing the pool.
- Real-SQLite tests enforce the 30-day and newest-8-MiB verbose-output policy,
  explicit tombstone/purge deletion modes, and a foreign-key orphan check that
  future graph and vector ownership relations must join.

## Gate

- [x] `M03-G01 GATE` SQLite survives a forced process termination during representative writes.
- [x] `M03-G02 GATE` Replaying persisted task events reconstructs the same task state.
- [x] `M03-G03 GATE` No schema column or diagnostic output contains provider credentials.
- [x] `M03-G04 GATE` Migration, backup, restoration, and downgrade-refusal tests pass.

Milestone-03 gate evidence:

- A child process commits one atomic task transition, opens a second
  task-and-event transaction, signals after both representative writes, and is
  forcibly terminated before commit. Reopen preserves the committed fact,
  removes both uncommitted changes, and passes integrity and foreign-key checks.
- Ordered replay starts from draft, requires contiguous event sequences,
  validates every transition through the domain state machine, ignores
  non-state events for state projection, and matches the persisted state and
  revision. A deleted sequence is classified as corruption.
- Schema introspection rejects credential-, secret-, key-, token-, password-,
  and authorization-shaped columns. Doctor tests use a secret-shaped selected
  path and prove neither it, executable paths, nor raw database failures are
  emitted.
- Empty and every-version migrations, unsupported-newer-schema refusal,
  disk-space refusal, checksum mismatch, failed/interrupted restoration,
  stable retry refusal, cross-process migration authority, and direct online
  backup/restore all pass against real SQLite.

---

# Milestone 04: Configuration, Credentials, and Redaction

Goal: configure providers and policies without allowing secrets to leak across boundaries.

Plan references: §27 Provider Credentials; Commands, Secrets, and Malicious Repository Content; Honest Cost Display.

Depends on: `M03-G01` through `M03-G04`.

Milestone output: validated non-secret settings, OS-backed provider credentials, and one redaction pipeline used before every persistence or display boundary.

## Configuration

- [x] `M04-001 BLOCKER` Define configuration precedence: defaults, user settings, approved repository settings, task overrides.
- [x] `M04-002` Define which settings live in SQLite.
- [x] `M04-003` Define which settings may be supplied through environment variables.
- [x] `M04-004` Define which repository-provided settings require first-use approval.
- [x] `M04-005` Add typed validation for provider endpoints, budgets, timeouts, worktree locations, and policy presets.
- [x] `M04-006` Reject unknown security-sensitive settings.
- [x] `M04-007` Record effective non-secret configuration with every run.
- [x] `M04-008` Add a settings revision so tasks can bind to the configuration they used.
- [x] `M04-009` Add configuration import/export that excludes credentials and private task data.

Configuration-contract evidence:

- Resolution is fixed as defaults, user settings, approved repository settings,
  then task overrides, with the winning source attributable per field.
- Any non-empty repository layer requires an explicit approval reference;
  unapproved settings fail instead of being silently ignored or applied.
- The only non-secret `CODEFLUX_*` environment inputs are provider endpoint,
  hard-budget minor units and currency, request timeout, and policy preset.
  Every other Codeflux environment setting is rejected.
- Validation requires HTTPS except for loopback HTTP, exact non-negative money,
  a 1-second-to-10-minute timeout, a clean absolute non-root worktree path, and
  a declared policy preset.
- Bounded strict JSON import rejects unknown or trailing fields. Its complete
  export schema contains no credential, secret, provider-key, transcript,
  prompt, or private-task-data field.
- Migration 2 stores immutable user or approved-repository revisions, binds
  repository-created tasks to one revision, and permits a run snapshot only
  when its revision matches the owning task binding.
- The storage boundary recursively rejects secret-shaped JSON field names,
  content-hashes accepted snapshots, and returns the original immutable row for
  identical retries while rejecting changed retries.

## Credential Store

- [x] `M04-010 BLOCKER SECURITY` Define a credential-store interface.
- [x] `M04-011 SECURITY` Implement Windows Credential Manager support.
- [x] `M04-012 SECURITY` Implement macOS Keychain support or mark it unavailable for the current platform build.
- [x] `M04-013 SECURITY` Implement Linux Secret Service support or mark it unavailable for the current platform build.
- [x] `M04-014 SECURITY` Implement explicit environment-variable fallback.
- [x] `M04-015 SECURITY` Store only a credential reference in SQLite.
- [x] `M04-016 SECURITY` Add credential create, update, test, and delete operations.
- [x] `M04-017 SECURITY` Ensure task workers never receive raw provider keys.
- [x] `M04-018 SECURITY` Prevent credential values from implementing accidental string formatting.
- [x] `M04-019 TEST` Verify credentials are absent from database pages after provider use.
- [x] `M04-020 TEST` Verify credentials are absent from child-process environments.

Credential-boundary evidence:

- The store contract exposes create, update, retrieve, test, and delete by
  validated opaque reference. Windows uses Credential Manager; macOS and Linux
  builds return an explicit unavailable result rather than a silent fallback.
- Environment fallback is explicitly mapped and read-only. Secret values reject
  JSON/text serialization and every formatting verb, expose only a temporary
  cleared callback copy, and support explicit destruction.
- Schema 3 stores only an `os://service/account` reference. A real-SQLite test
  performs in-memory provider use, closes/checkpoints the database, and proves
  the known material is absent from database, WAL, and shared-memory pages.
- Worker environment construction strips known and suffix-shaped provider
  secret names. A real child test proves raw API-key and access-token fixtures
  are absent while ordinary environment values remain.

## Redaction

- [x] `M04-021 BLOCKER SECURITY` Define a redaction pipeline used before prompt persistence, log persistence, UI delivery, and diagnostic export.
- [x] `M04-022 SECURITY` Add exact-value redaction for credentials currently loaded by the process.
- [x] `M04-023 SECURITY` Add pattern redaction for supported provider key formats.
- [x] `M04-024 SECURITY` Add common private-key and bearer-token redaction.
- [x] `M04-025 SECURITY` Add entropy-based detection only if false-positive behavior is acceptable and inspectable.
- [x] `M04-026 SECURITY` Mark redacted spans without preserving the original length when length could leak information.
- [x] `M04-027 SECURITY` Ensure structured fields are redacted before serialization.
- [x] `M04-028 SECURITY` Apply output-size limits before and after redaction.
- [x] `M04-029 TEST` Build a corpus of secret-bearing command, provider, HTTP, and exception outputs.
- [x] `M04-030 TEST` Test redaction across chunk boundaries in streamed output.
- [x] `M04-031 TEST` Test that redaction cannot be bypassed with common whitespace or quoting variations.
- [x] `M04-032 TEST` Test diagnostic exports for secret absence.

Redaction evidence:

- The same boundary-typed pipeline covers prompt/log persistence, UI delivery,
  and diagnostic export with exact loaded-value, supported provider-key,
  API/header, bearer-token, and PEM private-key matching.
- A constant `[REDACTED]` marker leaks no source span length. Structured
  sensitive fields are replaced before valid JSON serialization.
- Input is bounded before matching with a maximum-secret-length truncation
  guard; output is bounded afterward. Oversize structured output becomes a
  small valid JSON truncation record.
- The command/provider/HTTP/exception corpus passes at all four boundaries.
  Every split position for exact, provider-pattern, and bearer fixtures is
  redacted, as are common quote, whitespace, header, and assignment variants.
- Entropy detection is deliberately disabled and reported as such because no
  inspectable false-positive study authorizes it.

## Gate

- [x] `M04-G01 GATE` A provider can be configured and tested without writing its secret to SQLite.
- [x] `M04-G02 GATE` A full mock task produces no known secret in logs, events, prompts, UI payloads, or diagnostics.
- [x] `M04-G03 GATE` Repository configuration cannot grant itself new permissions.

Milestone-04 gate evidence:

- Credential-store test and provider-reference persistence use the secret only
  in memory, then scan closed SQLite pages for its absence.
- A real-SQLite mock task retrieves a mapped provider credential, redacts prompt,
  event/log, UI, and structured diagnostic values through their named
  boundaries, persists the prompt and event, and proves the known value is
  absent from every output and SQLite artifact.
- Repository overlays cannot resolve without an approval reference, and strict
  configuration import rejects unknown `permissions` and `commands` fields.

---

# Milestone 05: Event Journal and Unified Session Stream

Goal: create one durable, ordered event model that drives replay, recovery, UI updates, and graph projection.

Plan references: §23 Core Operational Entities; Transactions, Migrations, and Recovery; §27A Unified Session Stream; §27 Persistence, Recovery, Diagnostics, and Updates.

Depends on: `M03-G01` through `M03-G04`; secret-bearing test scenarios also depend on `M04-G01` through `M04-G03`.

Milestone output: one typed, durable, monotonic event stream that joins replay to live delivery without gaps or duplicates.

## Event Envelope

- [x] `M05-001 BLOCKER` Define the `SessionEvent` protobuf and domain type.
- [x] `M05-002 BLOCKER` Include sequence, session/thread/task IDs, timestamp, kind, revision, causation ID, correlation ID, and typed payload.
- [x] `M05-003` Define message-delta and message-final payloads.
- [x] `M05-004` Define plan-created and plan-changed payloads.
- [x] `M05-005` Define tool-started, tool-progress, and tool-completed payloads.
- [x] `M05-006` Define approval-requested and approval-resolved payloads.
- [x] `M05-007` Define task-state-changed payloads.
- [x] `M05-008` Define forecast, usage, cost, and budget payloads.
- [x] `M05-009` Define validation-updated payloads.
- [x] `M05-010` Define graph-snapshot and graph-patch payloads.
- [x] `M05-011` Define checkpoint-created and recovery-required payloads.
- [x] `M05-012` Define user-presentable error payloads with stable error codes.
- [x] `M05-013` Version event payloads for forward-compatible replay.
- [x] `M05-014` Define which event fields are safe for UI delivery.
- [x] `M05-015` Define which event types are immutable and correctness-bearing.

Session-event-contract evidence:

- `session.proto` defines one versioned oneof envelope with monotonic sequence,
  session/thread/task identity, UTC microsecond timestamp, entity revision,
  causation and correlation identities, kind, and all initial typed payloads.
- Domain validation requires canonical identities, exactly one kind-matched
  payload, payload version 1, exact money/token values, valid state machines,
  and only redacted user-presentable text fields.
- Message delta and tool progress are explicitly ephemeral/coalescible. Task
  transitions, approvals, budgets, validations, graph revisions, checkpoints,
  recovery, and errors are immutable correctness-bearing material events.
- The new stable session identity round-trips through domain, SQL/text/JSON
  identity machinery, and the protobuf identity envelope.

## Append and Publish

- [ ] `M05-016 BLOCKER DATA` Implement atomic event persistence and sequence allocation.
- [ ] `M05-017 BLOCKER` Publish an event only after its transaction commits.
- [ ] `M05-018` Implement in-process subscriptions by thread or task.
- [ ] `M05-019` Implement bounded subscriber buffers.
- [ ] `M05-020` Define backpressure behavior for token deltas.
- [ ] `M05-021` Define backpressure behavior for verbose tool progress.
- [ ] `M05-022` Prohibit dropping task transitions, approvals, budgets, validations, checkpoints, graph revisions, or errors.
- [ ] `M05-023` Coalesce superseded ephemeral progress events without changing durable history.
- [ ] `M05-024` Close subscriptions cleanly on cancellation.
- [ ] `M05-025` Remove disconnected subscribers without leaking goroutines.
- [ ] `M05-026` Add per-session and global subscriber metrics.

## Replay

- [ ] `M05-027 BLOCKER` Implement replay from `after_sequence`.
- [ ] `M05-028` Return a snapshot plus subsequent events when replaying an old or compacted range.
- [ ] `M05-029` Detect a client's stale entity revision.
- [ ] `M05-030` Ensure replay and live delivery join without a gap or duplicate.
- [ ] `M05-031` Make all UI command requests idempotent.
- [ ] `M05-032` Persist command idempotency keys and final results.
- [ ] `M05-033 TEST` Test reconnect at every boundary around a committed event.
- [ ] `M05-034 TEST` Test duplicate command delivery.
- [ ] `M05-035 TEST` Test slow subscribers and forced disconnects.
- [ ] `M05-036 TEST` Test a stream with interleaved chat, graph, approval, and cost events.
- [ ] `M05-037 TEST` Test replay after coordinator restart.
- [ ] `M05-038 TEST` Property-test strictly increasing per-session sequence numbers.

## Gate

- [ ] `M05-G01 GATE` A subscriber can disconnect, miss events, reconnect, and reconstruct the exact current task state.
- [ ] `M05-G02 GATE` Duplicate commands do not duplicate messages, actions, approvals, or costs.
- [ ] `M05-G03 GATE` Event persistence and publication ordering is documented and covered by concurrency tests.

---

# Milestone 06: GoWebComponents v5 and gRPC Transport Spike

Goal: remove framework and browser-transport uncertainty before building the product UI.

Plan references: §27A Framework and Transport Spike; Application Layout; Unified Session Stream; Rendering and Performance; Local Security; Frontend Acceptance Criteria.

Depends on: `M05-G01` through `M05-G03`.

Milestone output: a recorded v5 version and transport decision backed by reconnect, cancellation, security, and 300-node rendering measurements.

## Framework Pin

- [ ] `M06-001 BLOCKER SPIKE` Locate and pin the exact GoWebComponents v5 module and release.
- [ ] `M06-002 SPIKE` Record its Go and browser compatibility requirements.
- [ ] `M06-003 SPIKE` Build the smallest v5 component into WebAssembly.
- [ ] `M06-004 SPIKE` Serve the WASM client from the local Go server.
- [ ] `M06-005 SPIKE` Verify v5 routing and browser-history behavior.
- [ ] `M06-006 SPIKE` Verify v5 local component state and shared state primitives.
- [ ] `M06-007 SPIKE` Verify list virtualization with 10,000 synthetic thread entries.
- [ ] `M06-008 SPIKE` Verify cancellation and cleanup when a component unmounts.
- [ ] `M06-009 SPIKE` Verify clipboard behavior.
- [ ] `M06-010 SPIKE` Verify safe external-editor handoff capabilities.
- [ ] `M06-011 SPIKE` Verify keyboard, focus, and screen-reader primitives.
- [ ] `M06-012 SPIKE` Record unsupported or unstable framework APIs.

## Transport

- [ ] `M06-013 BLOCKER SPIKE` Inspect and test the v5 typed gRPC bridge.
- [ ] `M06-014 SPIKE` Confirm whether the bridge uses WebSocket, gRPC-Web, or another transport.
- [ ] `M06-015 SPIKE` Generate a v5 client for a unary health method.
- [ ] `M06-016 SPIKE` Generate a v5 client for `SubscribeSession`.
- [ ] `M06-017 SPIKE` Stream at least 10,000 ordered synthetic events.
- [ ] `M06-018 SPIKE` Cancel a live subscription from the browser.
- [ ] `M06-019 SPIKE` Reconnect using `after_sequence`.
- [ ] `M06-020 SPIKE` Test browser refresh during active streaming.
- [ ] `M06-021 SPIKE` Test coordinator restart during active streaming.
- [ ] `M06-022 SPIKE` Measure framing and serialization overhead.
- [ ] `M06-023 SPIKE` Verify maximum message behavior for graph snapshots and tool summaries.
- [ ] `M06-024 SPIKE` Verify same-origin enforcement.
- [ ] `M06-025 SPIKE` Verify loopback binding.
- [ ] `M06-026 SPIKE` Verify per-launch session-secret authentication.
- [ ] `M06-027 SPIKE` Determine whether an embedded Go bridge avoids a separate proxy.
- [ ] `M06-028 SPIKE` If the v5 bridge fails, compare unary plus server-streaming gRPC-Web against a small embedded WebSocket bridge.
- [ ] `M06-029 SPIKE` Choose one transport and record the rejected alternatives.

## Rendering Load

- [ ] `M06-030 SPIKE` Stream token deltas while updating cost and task state.
- [ ] `M06-031 SPIKE` Render a synthetic 300-node directed graph.
- [ ] `M06-032 SPIKE` Apply graph patches while token streaming continues.
- [ ] `M06-033 SPIKE` Measure frame time, memory, and DOM node count.
- [ ] `M06-034 SPIKE` Test 30-50 ms token batching.
- [ ] `M06-035 SPIKE` Test 50-100 ms graph-patch batching.
- [ ] `M06-036 SPIKE` Determine the SVG node threshold on the target hobbyist laptop.
- [ ] `M06-037 SPIKE` Verify that chat updates do not rerender the graph subtree.
- [ ] `M06-038 SPIKE` Verify that graph interaction does not rerender the full thread.

## Gate

- [ ] `M06-G01 GATE` Record the exact v5 version and transport architecture.
- [ ] `M06-G02 GATE` Demonstrate ordered reconnectable streaming with no event loss or duplication.
- [ ] `M06-G03 GATE` Demonstrate responsive simultaneous thread and 300-node graph updates.
- [ ] `M06-G04 GATE` Confirm the prototype requires no separately installed proxy.
- [ ] `M06-G05 GATE` Replace all spike-only code with a retained minimal transport test or delete it after recording the decision.

---

# Milestone 07: gRPC API Surface

Goal: define small domain-oriented contracts that support the complete user journey without leaking database tables.

Plan references: §27A Client, Server, and Storage Boundary; Service Contracts; Unified Session Stream; Local Security.

Depends on: `M06-G01` through `M06-G05`.

Milestone output: generated, versioned server/client contracts plus a complete application-function catalog with validation, authority, idempotency, revisions, transaction/event ownership, external-effect behavior, and chronological flow tests.

## API Conventions

- [ ] `M07-001 BLOCKER` Define protobuf package, Go package, and versioning conventions.
- [ ] `M07-002` Define a standard error detail with stable code, safe message, retryability, and relevant entity ID.
- [ ] `M07-003` Define cursor pagination.
- [ ] `M07-004` Define idempotency-key fields for mutating requests.
- [ ] `M07-005` Define expected-revision fields for optimistic concurrency.
- [ ] `M07-006` Define timestamp and duration conventions.
- [ ] `M07-007` Define exact monetary and token value messages.
- [ ] `M07-008` Define redacted-output conventions.
- [ ] `M07-009` Reserve protobuf fields instead of reusing removed numbers.
- [ ] `M07-010` Add API compatibility checks.

## Workspace Service

- [ ] `M07-011` Define `OpenWorkspace`.
- [ ] `M07-012` Define `GetWorkspaceState`.
- [ ] `M07-013` Define `ListRepositories`.
- [ ] `M07-014` Define `InspectRepository`.
- [ ] `M07-015` Define safe path and Git-state responses.

## Thread Service

- [ ] `M07-016` Define `CreateThread`.
- [ ] `M07-017` Define `ListThreads`.
- [ ] `M07-018` Define `GetThreadPage`.
- [ ] `M07-019` Define `SendMessage`.
- [ ] `M07-020` Define `RenameThread`.
- [ ] `M07-021` Define `ArchiveThread`.

## Task Service

- [ ] `M07-022` Define `CreateTask` or task creation semantics through `SendMessage`.
- [ ] `M07-023` Define `GetTask`.
- [ ] `M07-024` Define `StartTask`.
- [ ] `M07-025` Define `PauseTask`.
- [ ] `M07-026` Define `ResumeTask`.
- [ ] `M07-027` Define `CancelTask`.
- [ ] `M07-028` Define `ApproveAction`.
- [ ] `M07-029` Define `SetBudget`.
- [ ] `M07-030` Define `RequestRepair`.
- [ ] `M07-031` Define `RollbackTask`.

## Graph Service

- [ ] `M07-032` Define `GetGraphSlice`.
- [ ] `M07-033` Define `ExpandGraph`.
- [ ] `M07-034` Define `GetNode`.
- [ ] `M07-035` Define `ExplainNode`.
- [ ] `M07-036` Define `CompareGraphRevisions`.
- [ ] `M07-037` Define bounded node/edge counts and continuation behavior.

## Review and Settings Services

- [ ] `M07-038` Define `GetDiffSummary`.
- [ ] `M07-039` Define `GetValidationReport`.
- [ ] `M07-040` Define `AcceptChange`.
- [ ] `M07-041` Define `RejectChange`.
- [ ] `M07-042` Define `OpenInEditor`.
- [ ] `M07-043` Define `GetModels`.
- [ ] `M07-044` Define `GetPolicy`.
- [ ] `M07-045` Define `SetPolicy`.
- [ ] `M07-046` Define `SetBudgetDefaults`.
- [ ] `M07-047` Define `ConfigureProvider`.
- [ ] `M07-048` Define `TestProvider`.
- [ ] `M07-049` Define `SubscribeSession`.

## Implementation

- [ ] `M07-050 BLOCKER` Generate Go server and v5 client bindings.
- [ ] `M07-051` Implement request validation interceptors.
- [ ] `M07-052` Implement session authentication interceptors.
- [ ] `M07-053` Implement safe error mapping.
- [ ] `M07-054` Implement request correlation and structured logging.
- [ ] `M07-055` Implement deadline propagation.
- [ ] `M07-056` Implement graceful server shutdown.
- [ ] `M07-057 TEST` Add in-process API tests for every method.
- [ ] `M07-058 TEST` Add malformed-request tests.
- [ ] `M07-059 TEST` Add stale-revision and duplicate-idempotency tests.
- [ ] `M07-060 TEST` Add unauthorized-session tests.

## Backend Function and Flow Coverage

Plan: §27B Backend Design Rules through Backend Flow Acceptance.

- [ ] `M07-061 BLOCKER` Create a machine-reviewable catalog mapping every prototype gRPC method to one application-service function, command/query type, result type, authorization rule, and domain-error mapping.
- [ ] `M07-062` For every mutating application function, record its idempotency key scope and duplicate-result behavior.
- [ ] `M07-063` For every concurrently mutable entity, record the required expected revision and stale-conflict response.
- [ ] `M07-064` For every mutating application function, record its SQLite transaction boundary and repositories touched.
- [ ] `M07-065` For every mutating application function, record durable events appended in the committing transaction.
- [ ] `M07-066` For every external effect, record durable intent, effect identity, outcome, ambiguity, retry, and cancellation behavior.
- [ ] `M07-067` Define the safe transport mapping for not-found, invalid transition, stale revision, duplicate, denied, budget exhausted, cancelled, retryable provider, corruption, and recovery-required errors.
- [ ] `M07-068` Verify gRPC handlers contain input validation, conversion, delegation, and error mapping only.
- [ ] `M07-069` Define application lifecycle functions and startup/shutdown ordering.
- [ ] `M07-070` Define workspace, repository-map, context-selection, and explanation functions.
- [ ] `M07-071` Define thread, message, pagination, rename, and archive functions.
- [ ] `M07-072` Define requirement, fingerprint, retrieval, forecast, plan, revision, approval, and task-lifecycle functions.
- [ ] `M07-073` Define worktree, safe-path, edit-batch, diff, checkpoint, restore, acceptance, abandonment, and cleanup functions.
- [ ] `M07-074` Define provider, model-request, stream, cancellation, retry, usage-reconciliation, and price functions.
- [ ] `M07-075` Define exact budget creation, reservation, usage commit, release, limit raise, and snapshot functions.
- [ ] `M07-076` Define tool discovery, authority classification, approval, execution, cancellation, output bounding, and redaction functions.
- [ ] `M07-077` Define worker spawn, lease, heartbeat, pause, resume, cancel, checkpoint, status, and lost-worker classification functions.
- [ ] `M07-078` Define risk, validation selection, execution, invalidation, baseline, evidence, review, acceptance, repair, rejection, and editor-open functions.
- [ ] `M07-079` Define graph projection, revision, patch, layout, slice, expansion, cone, comparison, node, and explanation functions.
- [ ] `M07-080` Define episode, fact extraction, fingerprint, atom name/doc admission, embedding, exact retrieval, vector candidate, applicability, assurance, influence, and invalidation functions.
- [ ] `M07-081` Define credential, settings, doctor, backup, integrity, and diagnostic-export functions.
- [ ] `M07-082 TEST` Execute the complete startup flow against deterministic ports, database, and recovery fixtures.
- [ ] `M07-083 TEST` Execute open-repository and context-selection flows against clean, dirty, detached, conflicted, and malicious fixtures.
- [ ] `M07-084 TEST` Execute submit-requirement, forecast, plan, revise, approve, and start flows through generated clients.
- [ ] `M07-085 TEST` Execute an agent tool step through automatic, approval-required, denied, failed, cancelled, and retryable paths.
- [ ] `M07-086 TEST` Execute pause, resume, cancellation, provider failure, and hard-budget flows at each durable boundary.
- [ ] `M07-087 TEST` Execute validation, review, accept, repair, reject, rollback, and stale-review flows.
- [ ] `M07-088 TEST` Execute reconnect/replay with event commit before delivery, duplicate delivery, and stale projection.
- [ ] `M07-089 TEST` Execute coordinator/worker crash classification without repeating an ambiguous action.
- [ ] `M07-090 TEST` Execute pre-work retrieval and atom admission while proving vector similarity cannot bypass eligibility.

## Gate

- [ ] `M07-G01 GATE` The generated client can perform the complete synthetic user journey.
- [ ] `M07-G02 GATE` API messages expose no SQLite implementation details or secrets.
- [ ] `M07-G03 GATE` Every mutating method is idempotent or explicitly documents why it cannot be retried.
- [ ] `M07-G04 GATE` Every backend application function has explicit authority, revision, transaction, event, side-effect, cancellation, and typed-error behavior, and every chronological flow passes against deterministic fakes.

---

# Milestone 08: Repository Discovery and Workspace Intelligence

Goal: understand enough of a Go repository to assemble targeted, revision-bound context without embedding or uploading the entire codebase.

Plan references: §5 Workspace Intelligence; Human Intent; Task Fingerprint and Retrieval; §27 Repository Indexing and Context Selection; Commands, Secrets, and Malicious Repository Content.

Depends on: `M07-G01` through `M07-G04`.

Milestone output: a deterministic, revision-bound Go repository map and explainable bounded context manifest.

## Repository Opening

- [ ] `M08-001 BLOCKER` Resolve and canonicalize the user-selected repository path.
- [ ] `M08-002 SECURITY` Reject paths that do not exist or are not directories.
- [ ] `M08-003` Detect whether the directory belongs to a Git repository.
- [ ] `M08-004` Resolve the repository root without following unsafe user-controlled indirection.
- [ ] `M08-005` Read the current branch, HEAD revision, remotes, and worktree status.
- [ ] `M08-006` Detect detached HEAD.
- [ ] `M08-007` Detect merge, rebase, cherry-pick, or bisect state.
- [ ] `M08-008` Detect submodules and record whether the prototype supports them.
- [ ] `M08-009` Detect nested repositories.
- [ ] `M08-010` Detect Git LFS pointers without automatically fetching content.
- [ ] `M08-011` Detect untracked and ignored files.
- [ ] `M08-012` Present dirty-state risks before creating a task worktree.
- [ ] `M08-013` Persist repository identity separately from mutable paths.
- [ ] `M08-014` Bind every workspace snapshot to a Git revision.

## Go Repository Map

- [ ] `M08-015 BLOCKER` Locate `go.mod`, `go.work`, and relevant nested modules.
- [ ] `M08-016` Parse module paths and declared Go versions.
- [ ] `M08-017` Run bounded `go list` commands through the mediated executor.
- [ ] `M08-018` Collect package names, directories, imports, test files, and build targets.
- [ ] `M08-019` Collect exported and unexported symbols through Go syntax/type tooling.
- [ ] `M08-020` Collect symbol definitions and references.
- [ ] `M08-021` Collect function callers and callees where Go tooling can resolve them cheaply.
- [ ] `M08-022` Collect interface implementations where feasible.
- [ ] `M08-023` Collect build tags and platform-specific file constraints.
- [ ] `M08-024` Identify generated files.
- [ ] `M08-025` Identify likely formatter, test, lint, and build commands from project files.
- [ ] `M08-026` Identify nearby tests for each source package.
- [ ] `M08-027` Identify repository instruction files and classify them as untrusted input.
- [ ] `M08-028` Record map warnings instead of failing the whole repository for one unparsable package.

## Deterministic Context Selection

- [ ] `M08-029 BLOCKER` Tokenize requirement terms and explicit paths/symbols.
- [ ] `M08-030` Resolve explicit file references first.
- [ ] `M08-031` Resolve explicit symbol references second.
- [ ] `M08-032` Rank exact term matches in paths, symbol names, documentation, and tests.
- [ ] `M08-033` Expand direct dependency and caller/callee neighbors.
- [ ] `M08-034` Expand nearby tests and configuration.
- [ ] `M08-035` Add Git history only for already relevant paths.
- [ ] `M08-036` Add further context only when a tool result or failure justifies it.
- [ ] `M08-037` Enforce separate file-count, byte, and estimated-token budgets.
- [ ] `M08-038` Deduplicate identical or overlapping excerpts.
- [ ] `M08-039` Preserve line numbers and repository-relative paths.
- [ ] `M08-040` Mark generated, binary, minified, vendor, and dependency content.
- [ ] `M08-041` Exclude likely secrets before provider context assembly.
- [ ] `M08-042` Record why each selected context item was included.
- [ ] `M08-043` Persist the context manifest and revision binding in SQLite.
- [ ] `M08-044` Invalidate cached mappings when supporting files change.
- [ ] `M08-045` Expose selected context to the user through an expandable card.

## Prompt-Injection Boundary

- [ ] `M08-046 SECURITY` Label repository content as untrusted data in agent prompts.
- [ ] `M08-047 SECURITY` Prevent repository text from modifying permission policy.
- [ ] `M08-048 SECURITY` Prevent repository text from granting network or credential access.
- [ ] `M08-049 SECURITY` Require first-use approval for repository-suggested custom commands.
- [ ] `M08-050 SECURITY` Show the source and scope of repository-provided instructions.
- [ ] `M08-051 TEST` Build a malicious-repository fixture containing fake system instructions.
- [ ] `M08-052 TEST` Verify the fixture cannot bypass command approval.
- [ ] `M08-053 TEST` Verify the fixture cannot cause secret disclosure.

## Gate

- [ ] `M08-G01 GATE` A representative Go repository opens and produces a revision-bound deterministic map.
- [ ] `M08-G02 GATE` The same requirement and revision produce the same ordered context manifest.
- [ ] `M08-G03 GATE` The context card explains every selected file or excerpt.
- [ ] `M08-G04 GATE` Malicious repository text cannot alter system authority.

---

# Milestone 09: Git Isolation, Editing, and Diff Management

Goal: ensure agent edits are inspectable, isolated, reversible, and bound to the repository revision they were based on.

Plan references: §19 Review and Source Mapping; §27 Local Runtime and Repository Isolation; §29 Phase 1.

Depends on: `M08-G01` through `M08-G04`.

Milestone output: isolated task worktrees, conflict-aware file mutations, traceable diffs, acceptance, repair, rollback, and explicit cleanup.

## Worktree Lifecycle

- [ ] `M09-001 BLOCKER` Define the worktree naming and storage convention.
- [ ] `M09-002` Generate collision-resistant task branch names.
- [ ] `M09-003` Record base repository, base revision, task branch, and worktree path atomically.
- [ ] `M09-004` Create a dedicated Git worktree for a task.
- [ ] `M09-005` Refuse to reuse an active worktree owned by another task.
- [ ] `M09-006` Verify the new worktree starts at the expected base revision.
- [ ] `M09-007` Handle repositories with dirty primary worktrees without modifying those changes.
- [ ] `M09-008` Handle branch-name collisions.
- [ ] `M09-009` Handle worktree creation failure with cleanup.
- [ ] `M09-010` Detect manual deletion or movement of a task worktree.
- [ ] `M09-011` Detect external commits in the task worktree.
- [ ] `M09-012` Detect concurrent user edits during agent execution.
- [ ] `M09-013` Pause before overwriting a file changed since it was read.
- [ ] `M09-014` Preserve task worktrees after failure until the user chooses cleanup.

## Safe File Operations

- [ ] `M09-015 BLOCKER SECURITY` Resolve every edit path relative to the task worktree.
- [ ] `M09-016 SECURITY` Reject path traversal outside the task worktree.
- [ ] `M09-017 SECURITY` Decide and test symlink handling.
- [ ] `M09-018 SECURITY` Prevent writes through symlinks to external locations.
- [ ] `M09-019` Preserve file permissions where practical.
- [ ] `M09-020` Preserve newline style unless the formatter intentionally changes it.
- [ ] `M09-021` Preserve UTF-8 and reject unsupported binary edits.
- [ ] `M09-022` Apply edits with expected-content or expected-hash preconditions.
- [ ] `M09-023` Return a typed conflict when expected content changed.
- [ ] `M09-024` Support create, update, rename, and delete as explicit operations.
- [ ] `M09-025` Require higher-risk approval for large or broad deletes.
- [ ] `M09-026` Record a redacted edit summary as an event.

## Diff and Acceptance

- [ ] `M09-027 BLOCKER` Produce repository-relative unified diffs.
- [ ] `M09-028` Produce per-file added/deleted line counts.
- [ ] `M09-029` Classify generated, dependency, test, configuration, and source changes.
- [ ] `M09-030` Detect binary changes.
- [ ] `M09-031` Detect suspiciously broad formatting churn.
- [ ] `M09-032` Detect changes outside the approved plan scope.
- [ ] `M09-033` Summarize diff intent without substituting summary for source review.
- [ ] `M09-034` Link changed lines to related task events and validation.
- [ ] `M09-035` Implement user acceptance of the worktree result.
- [ ] `M09-036` Decide whether acceptance creates a commit, applies a patch, or offers both.
- [ ] `M09-037` Preserve original author attribution rules.
- [ ] `M09-038` Implement user-requested repair without losing the previous checkpoint.
- [ ] `M09-039` Implement rollback to the last valid checkpoint.
- [ ] `M09-040` Implement task abandonment without deleting the branch by default.
- [ ] `M09-041` Implement explicit cleanup of abandoned worktrees.

## Tests

- [ ] `M09-042 TEST` Test worktree creation and cleanup in a temporary repository.
- [ ] `M09-043 TEST` Test dirty primary worktree preservation.
- [ ] `M09-044 TEST` Test concurrent user edit detection.
- [ ] `M09-045 TEST` Test symlink escape attempts.
- [ ] `M09-046 TEST` Test path traversal attempts.
- [ ] `M09-047 TEST` Test expected-hash conflicts.
- [ ] `M09-048 TEST` Test rename and delete diffs.
- [ ] `M09-049 TEST` Test rollback after several edit batches.
- [ ] `M09-050 TEST` Test coordinator restart with an intact task worktree.

## Gate

- [ ] `M09-G01 GATE` No agent edit can reach outside the task worktree through supported file operations.
- [ ] `M09-G02 GATE` User changes made after agent read are not silently overwritten.
- [ ] `M09-G03 GATE` Every accepted patch can be traced to a base revision and task.

---

# Milestone 10: Mediated Commands, Tools, and Permissions

Goal: let the agent inspect and validate a repository while keeping authority explicit and auditable.

Plan references: §21 Coordinator and Coding Agent; §27 Commands, Secrets, and Malicious Repository Content; Plugins and Custom Commands; §22 Correctness and Assurance Gates.

Depends on: `M09-G01` through `M09-G03`; credential and output paths depend on `M04-G01` through `M04-G03`.

Milestone output: a typed tool protocol, executable permission policy, controlled subprocess runner, approval ledger, and adversarial boundary tests.

## Tool Protocol

- [ ] `M10-001 BLOCKER` Define a typed internal tool request and result envelope.
- [ ] `M10-002` Include tool name, arguments, working directory, timeout, authority class, idempotency, and expected side effects.
- [ ] `M10-003` Define read-file and list-directory tools.
- [ ] `M10-004` Define symbol/search tools.
- [ ] `M10-005` Define structured edit tools.
- [ ] `M10-006` Define diff-inspection tools.
- [ ] `M10-007` Define command-execution tools.
- [ ] `M10-008` Define Git-status and history tools.
- [ ] `M10-009` Define test, build, format, and static-analysis wrappers.
- [ ] `M10-010` Define user-facing tool summaries.
- [ ] `M10-011` Version tool schemas and record the version per run.

## Permission Policy

- [ ] `M10-012 BLOCKER SECURITY` Define automatic read-only actions.
- [ ] `M10-013 BLOCKER SECURITY` Define task-scoped file-write actions.
- [ ] `M10-014 BLOCKER SECURITY` Define approval-required actions.
- [ ] `M10-015 SECURITY` Classify network access.
- [ ] `M10-016 SECURITY` Classify dependency installation.
- [ ] `M10-017 SECURITY` Classify writes outside the task worktree.
- [ ] `M10-018 SECURITY` Classify credential access.
- [ ] `M10-019 SECURITY` Classify destructive filesystem and Git actions.
- [ ] `M10-020 SECURITY` Classify privileged commands and process management.
- [ ] `M10-021 SECURITY` Classify external messaging, deployment, and publication.
- [ ] `M10-022 SECURITY` Refuse actions with unknown authority classes.
- [ ] `M10-023 SECURITY` Bind allow-for-task decisions to exact action patterns and scope.
- [ ] `M10-024 SECURITY` Expire task-scoped permissions when the task ends.
- [ ] `M10-025 SECURITY` Never infer permission from prior unrelated tasks.
- [ ] `M10-026 SECURITY` Record requester, reason, exact command/action, scope, decision, and time.

## Command Execution

- [ ] `M10-027 BLOCKER` Execute commands in the task worker, not the browser.
- [ ] `M10-028` Pass argument arrays instead of concatenated shell strings where possible.
- [ ] `M10-029` Set the task worktree as the default working directory.
- [ ] `M10-030` Validate working directories against task scope.
- [ ] `M10-031` Apply bounded timeouts.
- [ ] `M10-032` Support cooperative cancellation.
- [ ] `M10-033` Kill descendant processes on cancellation where the platform permits.
- [ ] `M10-034` Bound stdout and stderr capture.
- [ ] `M10-035` Stream redacted progress without persisting unbounded output.
- [ ] `M10-036` Preserve exit code, duration, timeout, cancellation, and truncation metadata.
- [ ] `M10-037` Separate environment allowlists from the coordinator environment.
- [ ] `M10-038` Remove provider credentials from worker environments.
- [ ] `M10-039` Record executable identity and resolved path.
- [ ] `M10-040` Detect commands that exceed approved scope.
- [ ] `M10-041` Provide a user-readable approval description.
- [ ] `M10-042` Provide allow-once, allow-for-task, and deny.
- [ ] `M10-043` Do not silently fall back after denial.

## Custom Commands and Plugins

- [ ] `M10-044` Store approved custom command definitions in SQLite.
- [ ] `M10-045` Represent custom command arguments as arrays with typed placeholders.
- [ ] `M10-046` Require first-use approval for repository-suggested commands.
- [ ] `M10-047` Record command version and source.
- [ ] `M10-048` Define the subprocess boundary for future MCP or JSON-RPC plugins.
- [ ] `M10-049 DEFER` Do not load arbitrary plugin code into the coordinator.
- [ ] `M10-050 DEFER` Do not implement a plugin marketplace in the prototype.

## Tests

- [ ] `M10-051 TEST` Test automatic read-only actions.
- [ ] `M10-052 TEST` Test task-scoped edits.
- [ ] `M10-053 TEST` Test network-command approval.
- [ ] `M10-054 TEST` Test dependency-install approval.
- [ ] `M10-055 TEST` Test destructive-command denial.
- [ ] `M10-056 TEST` Test allow-for-task scope expiration.
- [ ] `M10-057 TEST` Test timeout and process-tree cancellation.
- [ ] `M10-058 TEST` Test output truncation and redaction.
- [ ] `M10-059 TEST` Test a malicious command description cannot change the executed argument array.

## Gate

- [ ] `M10-G01 GATE` Every non-automatic action has an attributable policy decision.
- [ ] `M10-G02 GATE` Denied authority cannot be regained through tool substitution.
- [ ] `M10-G03 GATE` Cancellation terminates representative child-process trees on every supported platform.

---

# Milestone 11: Coordinator and Worker Lifecycle

Goal: isolate task execution from the long-lived process while preserving durable control and recovery.

Plan references: §21 Agent Architecture; Coordinator; Coding Agent; Progress Monitor and Dynamic Escalation; §27 Local Runtime and Repository Isolation.

Depends on: `M10-G01` through `M10-G03`.

Milestone output: authenticated one-worker-per-task subprocess execution with ownership, heartbeats, cancellation, concurrency control, and graceful shutdown.

## Coordinator

- [ ] `M11-001 BLOCKER` Implement coordinator startup and dependency wiring.
- [ ] `M11-002` Acquire a local single-instance lock or define multi-instance behavior.
- [ ] `M11-003` Open and migrate SQLite before accepting tasks.
- [ ] `M11-004` Initialize credential, provider, workspace, event, and transport services.
- [ ] `M11-005` Bind the frontend server to loopback.
- [ ] `M11-006` Generate a per-launch browser session secret.
- [ ] `M11-007` Restore incomplete task metadata at startup.
- [ ] `M11-008` Detect orphaned worker processes.
- [ ] `M11-009` Detect missing or divergent worktrees.
- [ ] `M11-010` Present recovery-required state instead of auto-resuming uncertain work.
- [ ] `M11-011` Implement graceful shutdown ordering.
- [ ] `M11-012` Stop accepting new mutations before draining streams.
- [ ] `M11-013` Ask active workers to checkpoint and stop.
- [ ] `M11-014` Flush committed events and checkpoint WAL.

## Worker Protocol

- [ ] `M11-015 BLOCKER` Define a versioned coordinator/worker protocol.
- [ ] `M11-016` Define worker startup parameters without raw credentials.
- [ ] `M11-017` Pass task ID, run ID, worktree path, policy revision, tool schema, and coordinator endpoint.
- [ ] `M11-018` Authenticate the worker to the coordinator.
- [ ] `M11-019` Reject protocol-version mismatch.
- [ ] `M11-020` Implement worker heartbeat.
- [ ] `M11-021` Implement coordinator-issued pause, resume, cancel, and checkpoint requests.
- [ ] `M11-022` Implement worker status and tool-event reporting.
- [ ] `M11-023` Bound reconnect attempts.
- [ ] `M11-024` Mark the task recovery-required after heartbeat expiry.
- [ ] `M11-025` Prevent two workers from owning the same active run.
- [ ] `M11-026` Persist worker process metadata for diagnostics.

## Process Isolation

- [ ] `M11-027 SECURITY` Launch one subprocess worker per active task.
- [ ] `M11-028 SECURITY` Set the worker working directory to the task worktree.
- [ ] `M11-029 SECURITY` Provide the minimum required environment.
- [ ] `M11-030 SECURITY` Keep credential-store handles in the coordinator.
- [ ] `M11-031 SECURITY` Keep SQLite writes behind coordinator repositories unless a carefully reviewed writer boundary is required.
- [ ] `M11-032 SECURITY` Apply platform-appropriate process-group management.
- [ ] `M11-033 SECURITY` Support an optional user-provided container command for stronger isolation.
- [ ] `M11-034` Clearly label the default isolation as mediated workspace confinement, not a perfect sandbox.

## Concurrency

- [ ] `M11-035` Define the active-task concurrency limit.
- [ ] `M11-036` Queue excess tasks with visible position and reason.
- [ ] `M11-037` Prevent starvation of paused or approval-blocked tasks.
- [ ] `M11-038` Define provider concurrency limits separately from worker limits.
- [ ] `M11-039` Define database write-contention handling.
- [ ] `M11-040` Define shutdown behavior for queued tasks.

## Tests

- [ ] `M11-041 TEST` Test normal worker startup and exit.
- [ ] `M11-042 TEST` Test worker crash.
- [ ] `M11-043 TEST` Test coordinator crash.
- [ ] `M11-044 TEST` Test heartbeat loss.
- [ ] `M11-045 TEST` Test duplicate worker ownership.
- [ ] `M11-046 TEST` Test protocol-version mismatch.
- [ ] `M11-047 TEST` Test graceful shutdown with running, paused, and queued tasks.
- [ ] `M11-048 TEST` Test worker environment for credential absence.

## Gate

- [ ] `M11-G01 GATE` Killing a worker cannot corrupt coordinator state or another task worktree.
- [ ] `M11-G02 GATE` Killing the coordinator leaves enough durable state for a safe recovery choice.
- [ ] `M11-G03 GATE` No task runs with two active worker owners.

---

# Milestone 12: Model Provider Abstraction

Goal: support a small set of providers consistently while preserving exact usage, version, failure, and privacy evidence.

Plan references: §21 Model and Effort Router; Routing Safety; §27 Provider Credentials; Initial Model Providers; Honest Cost Display.

Depends on: `M11-G01` through `M11-G03`; credential access depends on `M04-G01` through `M04-G03`.

Milestone output: normalized OpenAI, Anthropic, and OpenAI-compatible adapters with exact accounting, cancellation, bounded retry, and no silent switching.

## Provider Interface

- [ ] `M12-001 BLOCKER` Define provider discovery and capability methods.
- [ ] `M12-002 BLOCKER` Define streaming response and cancellation.
- [ ] `M12-003` Define request messages, tool declarations, tool results, and structured-output requirements.
- [ ] `M12-004` Define normalized stop reasons.
- [ ] `M12-005` Define normalized usage accounting.
- [ ] `M12-006` Define provider-specific raw metadata retention after redaction.
- [ ] `M12-007` Define timeout, retryable, rate-limit, authentication, invalid-request, safety, and unavailable errors.
- [ ] `M12-008` Define model capabilities: tools, structured output, context length, image input, and reasoning controls.
- [ ] `M12-009` Define exact provider/model/version identity recorded per request.
- [ ] `M12-010` Define pricing snapshots separately from mutable current pricing.
- [ ] `M12-011` Define cancellation semantics and late-response handling.
- [ ] `M12-012` Define request idempotency where providers support it.

## OpenAI Adapter

- [ ] `M12-013` Implement model configuration.
- [ ] `M12-014` Implement credential lookup.
- [ ] `M12-015` Implement streaming text.
- [ ] `M12-016` Implement tool calls and tool results.
- [ ] `M12-017` Implement structured output needed by the planner.
- [ ] `M12-018` Implement cancellation.
- [ ] `M12-019` Normalize usage and stop reasons.
- [ ] `M12-020` Capture request IDs and safe provider metadata.
- [ ] `M12-021` Classify errors and retry hints.
- [ ] `M12-022 TEST` Test against a deterministic mock server.
- [ ] `M12-023 TEST` Add an opt-in live smoke test.

## Anthropic Adapter

- [ ] `M12-024` Implement model configuration.
- [ ] `M12-025` Implement credential lookup.
- [ ] `M12-026` Implement streaming text.
- [ ] `M12-027` Implement tool calls and tool results.
- [ ] `M12-028` Implement structured output needed by the planner.
- [ ] `M12-029` Implement cancellation.
- [ ] `M12-030` Normalize usage and stop reasons.
- [ ] `M12-031` Capture request IDs and safe provider metadata.
- [ ] `M12-032` Classify errors and retry hints.
- [ ] `M12-033 TEST` Test against a deterministic mock server.
- [ ] `M12-034 TEST` Add an opt-in live smoke test.

## OpenAI-Compatible Local Adapter

- [ ] `M12-035` Implement configurable loopback or user-approved endpoint.
- [ ] `M12-036` Support optional credentials.
- [ ] `M12-037` Implement model listing where the endpoint provides it.
- [ ] `M12-038` Implement streaming text.
- [ ] `M12-039` Detect and advertise tool-call support.
- [ ] `M12-040` Handle endpoints without usage reporting.
- [ ] `M12-041` Handle nonstandard error bodies safely.
- [ ] `M12-042` Require approval before connecting to a non-loopback endpoint.
- [ ] `M12-043 TEST` Test against a local deterministic fake endpoint.

## Retry, Fallback, and Accounting

- [ ] `M12-044 BLOCKER` Implement bounded retry for transient transport failures.
- [ ] `M12-045` Respect provider retry-after guidance within the task deadline and budget.
- [ ] `M12-046` Do not retry invalid, authentication, or policy errors.
- [ ] `M12-047` Record each physical request attempt.
- [ ] `M12-048` Attribute retry usage and cost to the task.
- [ ] `M12-049` Preserve partial streamed output as non-final evidence.
- [ ] `M12-050` Pause after retry budget exhaustion.
- [ ] `M12-051` Offer retry, resume, or explicitly approved provider switch.
- [ ] `M12-052` Never silently switch providers or models.
- [ ] `M12-053` Handle missing pricing as unknown rather than zero.
- [ ] `M12-054` Reconcile estimated and provider-reported usage.
- [ ] `M12-055` Surface accounting discrepancies.

## Gate

- [ ] `M12-G01 GATE` The same normalized mock conversation passes through all three adapters.
- [ ] `M12-G02 GATE` Cancellation stops UI streaming and prevents additional tool execution.
- [ ] `M12-G03 GATE` Every request has attributable provider, model, version, usage, price snapshot, latency, and final status.
- [ ] `M12-G04 GATE` Provider switching never occurs without explicit user authority.

---

# Milestone 13: Fixed Routing, Forecasts, and Budget Enforcement

Goal: make cost and effort visible and enforceable before attempting learned optimization.

Plan references: §5 Adaptive Execution Policy; §21 Effort Forecaster; Model and Effort Router; Routing Safety; §25 Cost; Forecast and Routing Quality; §29 Phases 1, 3, and 4.

Depends on: `M12-G01` through `M12-G04`.

Milestone output: a versioned fixed baseline, transparent P50/P90 heuristic, exact cost ledger, reservations, warnings, and hard-stop enforcement.

## Fixed Baseline Policy

- [ ] `M13-001 BLOCKER` Choose the fixed baseline provider, model, and effort level.
- [ ] `M13-002` Define fixed policy behavior for planning, execution, repair, and review.
- [ ] `M13-003` Define maximum planning and repair rounds.
- [ ] `M13-004` Define maximum tool calls per round.
- [ ] `M13-005` Define maximum context budget.
- [ ] `M13-006` Define default task monetary and token budgets.
- [ ] `M13-007` Version the fixed policy.
- [ ] `M13-008` Persist the exact policy with every task and run.
- [ ] `M13-009` Make policy selection deterministic for identical inputs.
- [ ] `M13-010` Expose manual model/effort override as an explicit recorded choice.

## Initial Forecast

- [ ] `M13-011 BLOCKER` Define a transparent heuristic forecast based on task class, repository size, likely files, validation commands, and fixed model.
- [ ] `M13-012` Produce P50/P90 latency estimates.
- [ ] `M13-013` Produce P50/P90 token estimates.
- [ ] `M13-014` Produce P50/P90 cost estimates.
- [ ] `M13-015` Produce uncertainty reasons.
- [ ] `M13-016` Distinguish unknown price from zero cost.
- [ ] `M13-017` Record forecast features and algorithm version.
- [ ] `M13-018` Present the forecast before execution begins.
- [ ] `M13-019` Allow the user to adjust budget before approval.
- [ ] `M13-020` Compare forecast with actual results after completion.
- [ ] `M13-021` Do not present forecasts as promises.

## Budget Ledger

- [ ] `M13-022 BLOCKER DATA` Implement atomic budget creation.
- [ ] `M13-023 DATA` Implement pre-request estimated-cost reservation.
- [ ] `M13-024 DATA` Reconcile reservation with actual usage after the request.
- [ ] `M13-025 DATA` Track model, tool, and optional infrastructure cost categories.
- [ ] `M13-026` Define warning thresholds.
- [ ] `M13-027` Emit budget-warning events.
- [ ] `M13-028` Block new model requests at the hard cap.
- [ ] `M13-029` Allow the current request to finish if stopping it would waste already billed work, while preventing subsequent actions.
- [ ] `M13-030` Require explicit approval to raise a hard budget.
- [ ] `M13-031` Record who or what changed the budget.
- [ ] `M13-032` Make cancellation and budget exhaustion distinguishable.
- [ ] `M13-033` Show forecast, reserved, actual, and remaining amounts.
- [ ] `M13-034` Handle concurrent cost postings without overspending.

## Shadow Forecasting Preparation

- [ ] `M13-035` Record task features needed for later effort forecasting.
- [ ] `M13-036` Record counterfactual eligibility without choosing a dynamic model.
- [ ] `M13-037` Record actual outcome, latency, usage, cost, repairs, and human interventions.
- [ ] `M13-038` Define a later calibration report schema.
- [ ] `M13-039 DEFER` Do not change model or effort from learned forecasts in the prototype.

## Tests and Gate

- [ ] `M13-040 TEST` Test exact cost arithmetic.
- [ ] `M13-041 TEST` Test concurrent reservation races.
- [ ] `M13-042 TEST` Test missing pricing.
- [ ] `M13-043 TEST` Test warning and hard-cap boundaries.
- [ ] `M13-044 TEST` Test explicit budget increase.
- [ ] `M13-045 TEST` Test that retries consume budget.
- [ ] `M13-G01 GATE` A task cannot start without an inspectable policy, forecast, and budget.
- [ ] `M13-G02 GATE` No combination of concurrent requests can exceed the approved hard cap beyond a documented in-flight bound.
- [ ] `M13-G03 GATE` The fixed policy is reproducible and creates usable baseline telemetry.

---

# Milestone 14: Agent Planning and Execution Loop

Goal: implement the smallest reliable coding-agent loop with explicit plans, bounded repair, and no adaptive routing.

Plan references: §5 Human Intent; Workspace Intelligence; Execution and Review; §21 Agent Architecture; §22 Correctness and Assurance Gates; §29 Phase 1.

Depends on: `M13-G01` through `M13-G03`.

Milestone output: a bounded requirement-plan-approve-edit-test-repair-review state machine driven through persisted events and mediated tools.

## Requirement Intake

- [ ] `M14-001 BLOCKER` Persist the user's task message before planning.
- [ ] `M14-002` Classify task type using a deterministic rule or fixed model output.
- [ ] `M14-003` Identify explicit files, symbols, commands, and acceptance criteria.
- [ ] `M14-004` Detect obvious ambiguity that materially changes scope.
- [ ] `M14-005` Ask a targeted clarification when proceeding would be unsafe.
- [ ] `M14-006` Make a bounded reasonable assumption when ambiguity is non-material.
- [ ] `M14-007` Display assumptions in the plan.
- [ ] `M14-008` Produce an initial risk classification.
- [ ] `M14-009` Select the fixed validation profile.

## Plan Construction

- [ ] `M14-010 BLOCKER` Define a structured plan schema.
- [ ] `M14-011` Include goal, scope, expected files, steps, validation, risks, authority needs, and completion criteria.
- [ ] `M14-012` Bind the plan to repository and context revisions.
- [ ] `M14-013` Persist immutable plan revisions.
- [ ] `M14-014` Generate a concise user-facing plan.
- [ ] `M14-015` Generate machine-readable step IDs.
- [ ] `M14-016` Link plan steps to graph nodes.
- [ ] `M14-017` Present the forecast and budget with the plan.
- [ ] `M14-018` Require plan approval for elevated or protected work.
- [ ] `M14-019` Allow user redirection to create a new plan revision.
- [ ] `M14-020` Prevent execution of a superseded plan.

## Execution Loop

- [ ] `M14-021 BLOCKER` Implement the observe-think-act-result loop around the fixed provider.
- [ ] `M14-022` Provide only approved tool schemas.
- [ ] `M14-023` Add selected repository context.
- [ ] `M14-024` Add current plan and completed-step state.
- [ ] `M14-025` Add relevant factual task events without replaying the entire transcript.
- [ ] `M14-026` Validate model tool-call structure.
- [ ] `M14-027` Reject unknown tools.
- [ ] `M14-028` Route tool requests through permission policy.
- [ ] `M14-029` Persist tool-start before execution.
- [ ] `M14-030` Persist redacted tool result after execution.
- [ ] `M14-031` Feed the bounded result back to the model.
- [ ] `M14-032` Update plan-step state.
- [ ] `M14-033` Create checkpoints after material edit batches.
- [ ] `M14-034` Check pause, cancel, budget, and policy state between actions.
- [ ] `M14-035` Enforce round, tool-call, token, time, and cost limits.
- [ ] `M14-036` Detect repeated identical failed actions.
- [ ] `M14-037` Stop and ask for direction instead of looping indefinitely.
- [ ] `M14-038` Distinguish implementation completion from validation completion.

## Repair Loop

- [ ] `M14-039` Run the selected validation commands.
- [ ] `M14-040` Parse failures into bounded redacted summaries.
- [ ] `M14-041` Link failures to relevant changed files and plan steps.
- [ ] `M14-042` Permit a bounded repair round.
- [ ] `M14-043` Preserve the pre-repair checkpoint.
- [ ] `M14-044` Record why repair was attempted.
- [ ] `M14-045` Rerun affected validation after repair.
- [ ] `M14-046` Stop after the repair budget.
- [ ] `M14-047` Present unresolved failures honestly.
- [ ] `M14-048` Never silently weaken or skip an acceptance test.

## Completion

- [ ] `M14-049` Require final repository status and diff capture.
- [ ] `M14-050` Require final validation summary.
- [ ] `M14-051` Require budget and actual cost summary.
- [ ] `M14-052` Require an assumption and limitation summary.
- [ ] `M14-053` Transition to awaiting-review rather than auto-accepting.
- [ ] `M14-054` Support accept, request repair, rollback, and abandon.
- [ ] `M14-055` Record the user's final decision.

## Tests and Gate

- [ ] `M14-056 TEST` Run a deterministic fake-model successful edit scenario.
- [ ] `M14-057 TEST` Run a fake-model malformed-tool scenario.
- [ ] `M14-058 TEST` Run a repeated-failure loop scenario.
- [ ] `M14-059 TEST` Run a pause during tool execution scenario.
- [ ] `M14-060 TEST` Run cancellation during model streaming.
- [ ] `M14-061 TEST` Run budget exhaustion between repair rounds.
- [ ] `M14-062 TEST` Run a user-redirection plan revision.
- [ ] `M14-G01 GATE` The deterministic fake agent completes the full plan-edit-test-review state machine.
- [ ] `M14-G02 GATE` Every action is attributable to a plan revision, model request, tool request, and policy decision.
- [ ] `M14-G03 GATE` No failure path silently falls back, expands authority, or skips required validation.

---

# Milestone 15: Checkpoint, Pause, Cancellation, and Recovery

Goal: make interruption a normal, testable state rather than an exceptional afterthought.

Plan references: §23 Transactions, Migrations, and Recovery; §27 Local Runtime and Repository Isolation; Persistence, Recovery, Diagnostics, and Updates; §29 Phase 1.

Depends on: `M14-G01` through `M14-G03`.

Milestone output: versioned checkpoints, cooperative interruption, divergence-aware resume, crash classification, and patch preservation.

## Checkpoint Contents

- [ ] `M15-001 BLOCKER` Define checkpoint schema and version.
- [ ] `M15-002` Bind checkpoint to task, run, plan revision, base revision, and current worktree HEAD.
- [ ] `M15-003` Record dirty file hashes and diff identity.
- [ ] `M15-004` Record completed and pending plan steps.
- [ ] `M15-005` Record current budget ledger position.
- [ ] `M15-006` Record effective policy and tool schema versions.
- [ ] `M15-007` Record last durable event sequence.
- [ ] `M15-008` Record whether an external action may be in an ambiguous outcome state.
- [ ] `M15-009` Never serialize provider credentials or live process handles.

## Checkpoint Creation

- [ ] `M15-010` Create a checkpoint after plan approval.
- [ ] `M15-011` Create a checkpoint after each material edit batch.
- [ ] `M15-012` Create a checkpoint before a risky approved action.
- [ ] `M15-013` Create a checkpoint after successful validation.
- [ ] `M15-014` Create a checkpoint on user pause.
- [ ] `M15-015` Attempt a bounded checkpoint on graceful shutdown.
- [ ] `M15-016` Commit checkpoint and event atomically where required.
- [ ] `M15-017` Deduplicate checkpoints with identical state.

## Pause and Resume

- [ ] `M15-018 BLOCKER` Implement pause request from CLI and UI.
- [ ] `M15-019` Stop starting new model and tool operations after pause.
- [ ] `M15-020` Decide whether an in-flight safe read may finish.
- [ ] `M15-021` Cancel in-flight long-running operations when requested.
- [ ] `M15-022` Persist paused state and reason.
- [ ] `M15-023` Validate repository and worktree binding before resume.
- [ ] `M15-024` Validate policy, provider, and tool compatibility before resume.
- [ ] `M15-025` Surface user edits made while paused.
- [ ] `M15-026` Require reconciliation or a new plan revision after conflicting edits.

## Crash Recovery

- [ ] `M15-027 BLOCKER` Scan incomplete tasks on coordinator startup.
- [ ] `M15-028` Verify repository path and identity.
- [ ] `M15-029` Verify base revision availability.
- [ ] `M15-030` Verify worktree existence and ownership.
- [ ] `M15-031` Verify recorded file hashes and diff identity.
- [ ] `M15-032` Verify no unresolved Git operation appeared.
- [ ] `M15-033` Classify recovery as safe-resume, reconcile-required, patch-preservation-only, or unrecoverable.
- [ ] `M15-034` Never auto-repeat an external action with ambiguous outcome.
- [ ] `M15-035` Present the last checkpoint and divergence clearly.
- [ ] `M15-036` Preserve a patch export path when direct resume is unsafe.
- [ ] `M15-037` Record every recovery attempt and decision.

## Tests and Gate

- [ ] `M15-038 TEST` Force termination before and after every material event/checkpoint boundary.
- [ ] `M15-039 TEST` Resume an unchanged worktree.
- [ ] `M15-040 TEST` Resume after user edits a non-overlapping file.
- [ ] `M15-041 TEST` Detect a conflicting user edit.
- [ ] `M15-042 TEST` Detect a missing worktree.
- [ ] `M15-043 TEST` Detect changed tool or policy versions.
- [ ] `M15-044 TEST` Detect an ambiguous external-action outcome.
- [ ] `M15-045 TEST` Verify cancellation does not become failure.
- [ ] `M15-G01 GATE` A crash at any tested durable boundary yields a safe, explainable recovery state.
- [ ] `M15-G02 GATE` Resume never duplicates a completed model/tool/external action.
- [ ] `M15-G03 GATE` The user can always preserve the current patch even when normal resume is impossible.

---

# Milestone 16: Frontend Shell and Design Foundation

Goal: build the accessible, local application shell before conversation and graph behavior add streaming complexity.

Plan references: §27A Product Surface; Application Layout; Client, Server, and Storage Boundary; Rendering and Performance; Local Security; §27C Route Map, Component Tree, Frontend State Ownership, Shared Primitive Components, and Root and Shell Component Contracts; §25 MVP Usability.

Depends on: `M15-G01` through `M15-G03`; framework primitives depend on `M06-G01` through `M06-G05`.

Milestone output: a keyboard-accessible GoWebComponents v5 shell with thread rail, chat pane, graph pane, responsive modes, design tokens, loading/error states, and safe session bootstrap.

## Visual and Interaction Tokens

- [ ] `M16-001 UX` Define the neutral, accent, success, warning, failure, active, blocked, and invalidated color tokens.
- [ ] `M16-002 UX` Define light-theme values for every color token.
- [ ] `M16-003 UX` Define dark-theme values for every color token.
- [ ] `M16-004 UX` Verify text/background token pairs meet WCAG AA contrast.
- [ ] `M16-005 UX` Define typeface stacks that do not require a remote font request.
- [ ] `M16-006 UX` Define body, compact metadata, heading, code, and numeric typography tokens.
- [ ] `M16-007 UX` Define spacing tokens on a small consistent scale.
- [ ] `M16-008 UX` Define border, radius, shadow, and focus-ring tokens.
- [ ] `M16-009 UX` Define motion-duration and easing tokens.
- [ ] `M16-010 UX` Define reduced-motion overrides.
- [ ] `M16-011 UX` Define minimum pointer target size.
- [ ] `M16-012 UX` Define status iconography that does not depend on color.
- [ ] `M16-013 UX` Define density rules for long technical threads.
- [ ] `M16-014 UX` Implement a development-only token specimen page.
- [ ] `M16-015 TEST` Add automated contrast checks for fixed token pairs.

## Application Bootstrap

- [ ] `M16-016 BLOCKER` Create the GoWebComponents v5 application entry point.
- [ ] `M16-017` Load the per-launch session secret without placing it in persistent browser storage.
- [ ] `M16-018` Call the coordinator health endpoint on startup.
- [ ] `M16-019` Fetch the current application, API, schema, and frontend versions.
- [ ] `M16-020` Reject an incompatible client/server version with a clear reload message.
- [ ] `M16-021` Show a bounded startup loading state.
- [ ] `M16-022` Show a coordinator-unavailable state with retry.
- [ ] `M16-023` Show a migration-required or database-error state without exposing raw paths or SQL.
- [ ] `M16-024` Restore the last non-sensitive selected repository and thread.
- [ ] `M16-025` Avoid automatically reopening a repository that no longer exists.
- [ ] `M16-026` Initialize the unified session-stream client only after authentication.
- [ ] `M16-027` Dispose all subscriptions when the application root unmounts.

## Shell Regions

- [ ] `M16-028 BLOCKER UX` Implement the top application bar.
- [ ] `M16-029 UX` Add repository and branch placeholders to the top bar.
- [ ] `M16-030 UX` Add worktree status placeholder.
- [ ] `M16-031 UX` Add task-state and connection-state placeholders.
- [ ] `M16-032 UX` Add model and effort placeholders.
- [ ] `M16-033 UX` Add forecast, actual cost, and hard-budget placeholders.
- [ ] `M16-034 UX` Add pause, stop, and overflow-control placeholders.
- [ ] `M16-035 BLOCKER UX` Implement the left thread/task rail.
- [ ] `M16-036 UX` Implement collapse and expand for the rail.
- [ ] `M16-037 UX` Persist rail width as a non-sensitive UI preference.
- [ ] `M16-038 BLOCKER UX` Implement the central conversation pane.
- [ ] `M16-039 BLOCKER UX` Implement the right graph pane.
- [ ] `M16-040 UX` Implement a draggable chat/graph splitter.
- [ ] `M16-041 UX` Clamp splitter positions to usable minimum widths.
- [ ] `M16-042 UX` Persist the splitter preference.
- [ ] `M16-043 UX` Implement graph-pane collapse.
- [ ] `M16-044 UX` Implement graph-pane restore.
- [ ] `M16-045 UX` Implement a bottom connection/diagnostic status strip only if usability testing shows it adds value.

## Responsive Behavior

- [ ] `M16-046 UX` Define wide, medium, and narrow viewport breakpoints from content needs rather than device names.
- [ ] `M16-047 UX` Keep side-by-side chat and graph on wide layouts.
- [ ] `M16-048 UX` Collapse the thread rail to an overlay on medium layouts.
- [ ] `M16-049 UX` Convert graph and conversation into tabs or a drawer on narrow layouts.
- [ ] `M16-050 UX` Preserve the selected graph node while switching narrow-layout tabs.
- [ ] `M16-051 UX` Keep the composer visible above the on-screen keyboard where supported.
- [ ] `M16-052 UX` Prevent horizontal page scrolling at all supported widths.
- [ ] `M16-053 TEST` Add component-level viewport tests for all breakpoints.

## Keyboard and Accessibility

- [ ] `M16-054 UX` Add a skip link to the conversation.
- [ ] `M16-055 UX` Establish a logical heading hierarchy.
- [ ] `M16-056 UX` Define tab order across rail, chat, composer, graph, and inspector.
- [ ] `M16-057 UX` Make the splitter keyboard adjustable.
- [ ] `M16-058 UX` Expose splitter values through appropriate accessibility attributes.
- [ ] `M16-059 UX` Add visible focus for every interactive control.
- [ ] `M16-060 UX` Ensure collapsed controls retain accessible names.
- [ ] `M16-061 UX` Define keyboard shortcuts for focus-chat, focus-graph, pause, and stop.
- [ ] `M16-062 UX` Prevent shortcuts from firing while the user types unless explicitly scoped.
- [ ] `M16-063 UX` Add a shortcut help dialog.
- [ ] `M16-064 TEST` Navigate the complete empty shell with keyboard only.
- [ ] `M16-065 TEST` Run automated accessibility checks against the shell.

## Component Isolation

- [ ] `M16-066` Define route-level components.
- [ ] `M16-067` Define shell-level state separately from task/session state.
- [ ] `M16-068` Define the top-bar view model.
- [ ] `M16-069` Define the thread-rail view model.
- [ ] `M16-070` Define conversation and graph pane boundaries.
- [ ] `M16-071` Instrument render counts in development builds.
- [ ] `M16-072 TEST` Verify a top-bar cost update does not rerender the full thread.
- [ ] `M16-073 TEST` Verify a chat append does not rerender the graph viewport.
- [ ] `M16-074 TEST` Verify graph selection does not rerender every message.

## Complete Frontend Component Contract

Plan: §27C Route Map through Root and Shell Component Contracts; Shared Primitive Components; Focus, Keyboard, and Accessibility.

- [ ] `M16-075 BLOCKER` Implement the route map for repository choice, thread workspace, memory, settings, diagnostics, and first run.
- [ ] `M16-076` Implement `AppRoot`, `SessionBootstrap`, `AppRouter`, `GlobalErrorBoundary`, `GlobalShortcutManager`, `AccessibilityAnnouncer`, `DialogHost`, and `ToastHost`.
- [ ] `M16-077` Define immutable view models for top bar, thread rail, conversation, graph, review, settings, memory, diagnostics, and first run.
- [ ] `M16-078` Define authoritative remote state separately from ephemeral client state and prohibit durable task transitions from local-only reducers.
- [ ] `M16-079` Implement `SessionStore`, `WorkspaceStore`, `ThreadStore`, `TaskStore`, `GraphStore`, `ReviewStore`, `SettingsStore`, and `UIStore` ownership boundaries.
- [ ] `M16-080` Implement shared Button, IconButton, ToggleButton, Menu, Tabs, Dialog, Drawer, Popover, Tooltip, input, Badge, progress, Skeleton, InlineAlert, Disclosure, VirtualList, ResizableSplit, CopyButton, CodeBlock, EmptyState, and ErrorState primitives as actually needed.
- [ ] `M16-081 UX` Define keyboard, focus, accessible-name, disabled, busy, high-contrast, reduced-motion, and pointer-target behavior for every shared primitive before feature reuse.
- [ ] `M16-082 UX` Implement repository chooser with recent-valid workspace, browse/open, canonical-path result, warnings, loading, empty, unavailable, and retry states.
- [ ] `M16-083 UX` Implement Settings route shells for providers, models, policy, appearance, and data.
- [ ] `M16-084 UX` Implement Memory route shell with list, details, and action regions.
- [ ] `M16-085 UX` Implement Diagnostics route shell with health, versions, tasks, logs, backup, and export regions.
- [ ] `M16-086 UX` Implement First-run route shell with resumable local-promise, provider, repository, worktree/permissions, and first-thread steps.
- [ ] `M16-087` Implement route restoration that refuses missing repositories, archived inaccessible threads, expired sessions, and incompatible client/server state.
- [ ] `M16-088` Implement component-level not-requested, loading, ready-empty, ready-data, partial/stale, recoverable-error, denied, incompatible, and disconnected states.
- [ ] `M16-089 UX` Implement rate-limited accessibility announcements for connection, approval, pause, completion, validation failure, and recovery only.
- [ ] `M16-090 UX` Ensure routine events and token deltas never steal focus or create assertive announcements.
- [ ] `M16-091 UX` Add stable full labels for long atom names while permitting visual truncation only.
- [ ] `M16-092 TEST` Render every route in each top-level bootstrap state.
- [ ] `M16-093 TEST` Test focus restoration after dialog, drawer, responsive rail, and graph pane close.
- [ ] `M16-094 TEST` Test shared primitives in keyboard, disabled, busy, high-contrast, and reduced-motion modes.
- [ ] `M16-095 TEST` Test route and draft preservation across recoverable component failure.
- [ ] `M16-096 TEST` Verify settings, memory, diagnostics, and first-run shells make no unauthorized data fetches.
- [ ] `M16-097 TEST` Verify client stores cannot create an unsupported durable task transition.
- [ ] `M16-098 TEST` Verify every data-owning component has explicit empty, error, and disconnected presentation.
- [ ] `M16-099 TEST` Verify no embedded asset or UI primitive performs an external network request.
- [ ] `M16-100 TEST` Verify user-facing terminology consistently distinguishes Thread, Task, Attempt, Plan revision, Approval, Checkpoint, and Recovery.

## Gate

- [ ] `M16-G01 GATE` The empty shell loads from the embedded local server with no external asset requests.
- [ ] `M16-G02 GATE` Every shell action is keyboard accessible and has a visible focus state.
- [ ] `M16-G03 GATE` Wide, medium, and narrow layouts remain usable without lost state.
- [ ] `M16-G04 GATE` Component render instrumentation confirms chat and graph update isolation.
- [ ] `M16-G05 GATE` Every route, shared primitive, store, and shell component has explicit ownership, loading/empty/error/disconnected behavior, and keyboard/accessibility coverage.

---

# Milestone 17: Thread Rail, Conversation Timeline, and Composer

Goal: make the chat thread the complete primary control surface rather than a styled log viewer.

Plan references: §27A Conversation Model; Product Surface; Application Layout; Primary Interaction Journey; Frontend MVP Boundary; §27C Timeline Contracts, Composer Contract, Review Drawer Contracts, and Detailed Frontend Flows; §5 Human Intent.

Depends on: `M16-G01` through `M16-G05`; data methods depend on `M07-G01` through `M07-G04`.

Milestone output: resumable thread navigation, virtualized typed event cards, a task-aware composer, inline approvals, and stable graph identity links.

## Thread Rail Data

- [ ] `M17-001 BLOCKER` Fetch the first page of threads for the open repository.
- [ ] `M17-002` Render thread title, task state, last activity, and unread/attention indicator.
- [ ] `M17-003` Sort active attention-required threads before inactive threads.
- [ ] `M17-004` Preserve stable ordering when two timestamps are equal.
- [ ] `M17-005` Load the next page when the rail approaches its end.
- [ ] `M17-006` Avoid duplicate rows across pagination boundaries.
- [ ] `M17-007` Render a thread-list skeleton during first load.
- [ ] `M17-008` Render a retryable list error.
- [ ] `M17-009` Render an empty-repository thread state.
- [ ] `M17-010` Select a thread and update the route.
- [ ] `M17-011` Restore selection after reload.
- [ ] `M17-012` Create a new thread with an idempotency key.
- [ ] `M17-013` Optimistically show a pending new thread without duplicating the committed thread.
- [ ] `M17-014` Rename a thread.
- [ ] `M17-015` Archive a thread after confirmation.
- [ ] `M17-016` Remove an archived thread from the default view.
- [ ] `M17-017` Add an archived-thread filter.
- [ ] `M17-018 TEST` Test 1,000 thread rows with virtualized scrolling.

## Timeline Pagination and Anchoring

- [ ] `M17-019 BLOCKER` Fetch the newest thread page on selection.
- [ ] `M17-020` Group events into presentation items without changing durable order.
- [ ] `M17-021` Use event sequence as the stable presentation key.
- [ ] `M17-022` Load older events when the user scrolls near the top.
- [ ] `M17-023` Preserve visual scroll position after prepending older events.
- [ ] `M17-024` Avoid duplicate events after replay joins pagination.
- [ ] `M17-025` Show a clear beginning-of-thread marker.
- [ ] `M17-026` Auto-follow new events only when the user is already near the bottom.
- [ ] `M17-027` Show a new-events button when the user has scrolled upward.
- [ ] `M17-028` Return to live position when the button is activated.
- [ ] `M17-029` Preserve readable ordering for simultaneous event timestamps.
- [ ] `M17-030` Render a gap-recovery indicator if sequence continuity is temporarily unresolved.
- [ ] `M17-031 TEST` Test pagination/replay joining at every page boundary.
- [ ] `M17-032 TEST` Test anchor preservation with variable-height cards.

## Message Presentation

- [ ] `M17-033 UX` Implement user message bubbles.
- [ ] `M17-034 UX` Implement agent message bubbles.
- [ ] `M17-035 UX` Render streamed deltas into one in-progress message.
- [ ] `M17-036 UX` Replace the in-progress message with the durable final message.
- [ ] `M17-037 UX` Indicate interrupted or incomplete model output.
- [ ] `M17-038 UX` Render safe Markdown without executable HTML.
- [ ] `M17-039 SECURITY` Sanitize links and block unsafe URL schemes.
- [ ] `M17-040 UX` Render code blocks with copy action.
- [ ] `M17-041 UX` Add line wrapping and horizontal scroll behavior for code.
- [ ] `M17-042 UX` Render stable graph-node identity chips.
- [ ] `M17-043 UX` Focus the associated graph node when a node chip is activated.
- [ ] `M17-044 UX` Explain when the associated graph revision is no longer current.
- [ ] `M17-045 UX` Add message timestamps through an unobtrusive details affordance.
- [ ] `M17-046 UX` Add copy-message action.
- [ ] `M17-047 UX` Add accessible labels for user, agent, system event, and status.

## Typed Cards

- [ ] `M17-048 UX` Implement requirement/ambiguity card.
- [ ] `M17-049 UX` Implement forecast card with P50/P90 ranges.
- [ ] `M17-050 UX` Implement plan card with step status.
- [ ] `M17-051 UX` Implement plan-revision diff card.
- [ ] `M17-052 UX` Implement context-selection card.
- [ ] `M17-053 UX` Implement collapsed tool-started card.
- [ ] `M17-054 UX` Update the same card for tool completion.
- [ ] `M17-055 UX` Show command, scope, duration, exit state, and summarized output.
- [ ] `M17-056 UX` Keep raw redacted output collapsed by default.
- [ ] `M17-057 UX` Lazy-load large redacted output pages.
- [ ] `M17-058 UX` Implement approval card with exact requested authority.
- [ ] `M17-059 UX` Add allow-once action.
- [ ] `M17-060 UX` Add allow-for-task action with displayed scope.
- [ ] `M17-061 UX` Add deny action.
- [ ] `M17-062 UX` Disable approval actions after one resolution commits.
- [ ] `M17-063 UX` Show who or what resolved the approval.
- [ ] `M17-064 UX` Implement checkpoint card.
- [ ] `M17-065 UX` Implement validation summary card.
- [ ] `M17-066 UX` Implement diff summary card.
- [ ] `M17-067 UX` Implement cost/budget update card only for meaningful threshold events.
- [ ] `M17-068 UX` Implement error and recovery-choice card.
- [ ] `M17-069 UX` Implement final completion summary.
- [ ] `M17-070 UX` Implement unsupported-event fallback that preserves kind and sequence.

## Composer

- [ ] `M17-071 BLOCKER UX` Implement a multiline composer.
- [ ] `M17-072 UX` Submit with the chosen keyboard convention.
- [ ] `M17-073 UX` Insert a newline without submitting.
- [ ] `M17-074 UX` Disable submit for empty or whitespace-only input.
- [ ] `M17-075 UX` Show pending send state.
- [ ] `M17-076` Generate and retain an idempotency key until send resolves.
- [ ] `M17-077` Restore the unsent draft for the current thread.
- [ ] `M17-078` Keep drafts isolated per thread.
- [ ] `M17-079` Clear the draft only after committed message confirmation.
- [ ] `M17-080 UX` Show an explicit retry after send failure.
- [ ] `M17-081 UX` Add cost/speed/correctness policy selector.
- [ ] `M17-082 UX` Add hard-budget input with exact currency.
- [ ] `M17-083 UX` Add optional model override.
- [ ] `M17-084 UX` Add optional reasoning-effort override.
- [ ] `M17-085 UX` Add repository file/symbol attachment picker.
- [ ] `M17-086 UX` Display selected attachments as removable chips.
- [ ] `M17-087 SECURITY` Resolve attachments through server-side repository identities, not browser file paths.
- [ ] `M17-088 UX` Change composer actions appropriately for running, paused, awaiting-approval, and completed states.
- [ ] `M17-089 UX` Keep stop immediately reachable while the agent is running.

## Tests and Gate

- [ ] `M17-090 TEST` Render every card from a fixed event fixture.
- [ ] `M17-091 TEST` Snapshot or structurally compare every status variant.
- [ ] `M17-092 TEST` Test unsafe Markdown and URL payloads.
- [ ] `M17-093 TEST` Test double-click approval idempotency.
- [ ] `M17-094 TEST` Test message-send retry.
- [ ] `M17-095 TEST` Test per-thread draft isolation.
- [ ] `M17-096 TEST` Keyboard-test the entire thread and composer.
- [ ] `M17-097 TEST` Screen-reader-test one complete task timeline.
- [ ] `M17-098` Implement an exhaustive timeline-item registry that requires every event kind to map to a card or documented grouping rule.
- [ ] `M17-099` Implement unknown-event fallback with kind, time, sequence, safe details, and diagnostics link.
- [ ] `M17-100` Implement `ApplyMessageDelta`, `FinalizeMessage`, `MergeThreadPage`, and `ShouldAutoFollow` as pure deterministic reducers.
- [ ] `M17-101 UX` Ensure streamed text is visibly provisional until the durable final message arrives.
- [ ] `M17-102 UX` Ensure plan cards show assumptions, authority, validation, completion criteria, and revision history before approval.
- [ ] `M17-103 UX` Ensure context cards explain inclusion reason and revision without dumping full source.
- [ ] `M17-104 UX` Ensure tool cards update in place and do not create one row per progress chunk.
- [ ] `M17-105 UX` Ensure approval cards do not steal typing focus and retain attributable resolution after actions disappear.
- [ ] `M17-106 UX` Ensure validation cards distinguish passed, failed, waived, skipped, unavailable, cancelled, and stale.
- [ ] `M17-107 UX` Ensure completion cards distinguish implemented, validated, reviewed, accepted, rejected, and rolled-back outcomes.
- [ ] `M17-108 UX` Implement first-message latency presentation that shows the current phase and Stop after the threshold instead of an indefinite spinner.
- [ ] `M17-109 UX` Preserve thread and graph position when opening and closing review.
- [ ] `M17-110 TEST` Test new-thread pending row replacement without selection or focus jump.
- [ ] `M17-111 TEST` Test plan revision resets approval and preserves prior plan history.
- [ ] `M17-112 TEST` Test graph auto-highlighting never pans away from deliberate user inspection without a Return to current action control.
- [ ] `M17-113 TEST` Test repair feedback attached to task, file, hunk, validation, and graph node identities.
- [ ] `M17-114 TEST` Test raw output pagination, redaction, truncation, and copy behavior.
- [ ] `M17-115 TEST` Verify no routine progress event creates a toast, modal, or assertive announcement.
- [ ] `M17-G01 GATE` A user can create, leave, reopen, paginate, and continue a thread without lost or duplicated content.
- [ ] `M17-G02 GATE` Every correctness-bearing event has a distinct, inspectable presentation.
- [ ] `M17-G03 GATE` Approval and stop actions remain reachable without expanding raw tool output.
- [ ] `M17-G04 GATE` Every timeline event, card, composer state, and review transition has deterministic reducer, progressive-disclosure, focus, replay, and failure behavior.

---

# Milestone 18: Live Task Controls, Connection State, and Cost Surface

Goal: make live execution interruptible, attributable, and honest under normal operation and transport failure.

Plan references: §27A Unified Session Stream; Primary Interaction Journey; Frontend Acceptance Criteria; §27C Frontend Stores and Reducers, Command Functions, Task State and Available Action Matrix, Detailed Frontend Flows, and Frontend Telemetry; §21 Progress Monitor and Dynamic Escalation; §25 Speed and Cost.

Depends on: `M17-G01` through `M17-G04`; budget semantics depend on `M13-G01` through `M13-G03`.

Milestone output: reconnectable live session state, top-bar controls, cost/forecast display, safe interruption, and recovery presentation.

## Session Connection State

- [ ] `M18-001 BLOCKER` Define UI connection states: connecting, live, replaying, degraded, disconnected, incompatible, and unauthorized.
- [ ] `M18-002` Display connection state in the top bar.
- [ ] `M18-003` Begin subscription from the last applied durable sequence.
- [ ] `M18-004` Apply replay events before live events.
- [ ] `M18-005` Detect duplicate sequence delivery.
- [ ] `M18-006` Detect sequence gaps.
- [ ] `M18-007` Pause correctness-bearing UI mutations until a gap is repaired.
- [ ] `M18-008` Retry transient disconnects with bounded exponential backoff.
- [ ] `M18-009` Stop retrying on authentication or version mismatch.
- [ ] `M18-010` Expose manual reconnect.
- [ ] `M18-011` Preserve unsent drafts during reconnect.
- [ ] `M18-012` Disable mutating controls when delivery certainty is unknown.
- [ ] `M18-013` Re-enable controls after replay reaches live state.
- [ ] `M18-014` Report last successfully applied sequence in diagnostics.
- [ ] `M18-015 TEST` Inject disconnects before, during, and after each event category.

## Task State Projection

- [ ] `M18-016 BLOCKER` Project task state from snapshot plus ordered events.
- [ ] `M18-017` Project current plan revision.
- [ ] `M18-018` Project active tool and model operation.
- [ ] `M18-019` Project pending approval.
- [ ] `M18-020` Project latest checkpoint.
- [ ] `M18-021` Project validation state.
- [ ] `M18-022` Project change-acceptance state.
- [ ] `M18-023` Reject an event that attempts an impossible state transition.
- [ ] `M18-024` Trigger a fresh snapshot after projection inconsistency.
- [ ] `M18-025` Log a safe client diagnostic without raw task content.
- [ ] `M18-026 TEST` Compare client projection with server projection over recorded event fixtures.

## Top-Bar Task Controls

- [ ] `M18-027 UX` Display task state with icon and text.
- [ ] `M18-028 UX` Display current phase: planning, editing, validating, repairing, or reviewing.
- [ ] `M18-029 UX` Display selected provider, model, and effort.
- [ ] `M18-030 UX` Display forecast P50/P90 without implying certainty.
- [ ] `M18-031 UX` Display actual token usage.
- [ ] `M18-032 UX` Display actual cost using the task pricing snapshot.
- [ ] `M18-033 UX` Display unknown cost honestly.
- [ ] `M18-034 UX` Display remaining hard budget.
- [ ] `M18-035 UX` Add warning styling at the configured threshold.
- [ ] `M18-036 UX` Add pause control only in pausable states.
- [ ] `M18-037 UX` Add resume control only in resumable states.
- [ ] `M18-038 UX` Add stop control in every active state.
- [ ] `M18-039 UX` Require confirmation only when stopping has a non-obvious consequence.
- [ ] `M18-040 UX` Add budget-adjust action.
- [ ] `M18-041 UX` Show exact old and new budget before confirmation.
- [ ] `M18-042 UX` Prevent repeated clicks from producing duplicate commands.

## Recovery Presentation

- [ ] `M18-043 UX` Show recovery-required status at thread and top-bar level.
- [ ] `M18-044 UX` Display last valid checkpoint time and plan step.
- [ ] `M18-045 UX` Display repository/worktree divergence summary.
- [ ] `M18-046 UX` Offer safe resume when verified.
- [ ] `M18-047 UX` Offer reconcile when user edits require it.
- [ ] `M18-048 UX` Offer patch preservation when direct resume is unsafe.
- [ ] `M18-049 UX` Explain ambiguous external-action outcomes prominently.
- [ ] `M18-050 UX` Never label unsafe auto-repeat as retry.
- [ ] `M18-051 UX` Link recovery details to relevant events and files.

## State, Command, and Flow UX

Plan: §27C Command Functions; Task State and Available Action Matrix; Detailed Frontend Flows; Empty, Loading, Error, and Offline States; Frontend Telemetry.

- [ ] `M18-052 BLOCKER` Implement `ApplySessionSnapshot` and `ApplySessionEvent` as the only authoritative remote-state entry points.
- [ ] `M18-053` Implement pure reducers for task transition, budget, approval, validation, graph patch, and review revision.
- [ ] `M18-054` Detect impossible task transitions, stale graph patches, and sequence gaps and request snapshot repair rather than ignoring them.
- [ ] `M18-055` Implement `AvailableTaskActions` from task state, connection certainty, policy, pending command, approval, review staleness, and recovery classification.
- [ ] `M18-056 UX` Implement the complete Draft through Rolled-back state/action matrix from §27C.
- [ ] `M18-057 UX` Omit or explain unavailable actions before click rather than returning avoidable server errors.
- [ ] `M18-058` Wrap every UI mutation in a command state that owns one idempotency key until commit or deliberate abandonment.
- [ ] `M18-059` Implement stale-revision command handling that refreshes state and explains the changed entity.
- [ ] `M18-060 UX` Distinguish disconnected UI, backend task state, and sequence uncertainty.
- [ ] `M18-061 UX` Keep the timeline readable during disconnection while disabling only mutations whose delivery/state certainty is unsafe.
- [ ] `M18-062 UX` Implement one non-spamming budget warning and hard-cap decision surface.
- [ ] `M18-063 UX` Implement one calm recovery surface that leads with known state, ambiguity, and safest recommended action.
- [ ] `M18-064 UX` Implement review staleness presentation when diff, plan, validation, evidence, or graph revision changes.
- [ ] `M18-065 UX` Implement exact estimate-versus-actual labeling and never substitute missing price with zero.
- [ ] `M18-066` Record local UX telemetry for first-run, time to plan/diff, approval, pause/stop, review, graph use, reconnect, recovery, and slow renders without keystrokes or hidden content.
- [ ] `M18-067 UX` Add local telemetry inspection and deletion.
- [ ] `M18-068 TEST` Exercise every row of the task state/action matrix.
- [ ] `M18-069 TEST` Exercise first-run, new task, plan review, live work, approval, review, repair, reconnect, recovery, graph exploration, and budget flows.
- [ ] `M18-070 TEST` Verify a user can always identify current state, cost, authority, evidence, uncertainty, and next safe action without raw logs.

## Gate

- [ ] `M18-G01 GATE` Refreshing or reconnecting during an active task yields the same task, budget, approval, and validation state.
- [ ] `M18-G02 GATE` Pause, resume, stop, and budget change are idempotent from the UI.
- [ ] `M18-G03 GATE` The interface never shows a stale approval as actionable.
- [ ] `M18-G04 GATE` Unknown or delayed cost is never displayed as zero.
- [ ] `M18-G05 GATE` Every task state and user command has explicit live, busy, committed, stale, denied, disconnected, recovery, and accessibility behavior.

---

# Milestone 19: Task Graph Storage, Projection, Query, and Rendering

Goal: provide a stable semantic map of the current task without making graph authoring a prerequisite for ordinary coding.

Plan references: §5 Functional Graph and Core Graph Entities; §18 Stable Graph Identity; §23 Graph Storage; §27A Graph Modes; Graph Rendering Rules; Node Inspector; Frontend MVP Boundary; §30 Graph Medium Failure.

Depends on: `M18-G01` through `M18-G05`; stable IDs depend on `M02-G01`; database work depends on `M03-G04`.

Milestone output: immutable task-graph revisions in SQLite, bounded graph queries, stable layout hints, Program/Execution/Evidence projections, accessible SVG rendering, and bidirectional chat links.

## Minimal Graph Contract

- [ ] `M19-001 BLOCKER` Define graph identity separately from graph revision identity.
- [ ] `M19-002 BLOCKER` Define stable node and edge identity.
- [ ] `M19-003` Define node classes: requirement, plan region, atom/operation, effect, branch/match/merge, obligation, artifact/result.
- [ ] `M19-004` Define edge classes: control, data/provenance, evidence dependency, retry, reconciliation, compensation.
- [ ] `M19-005` Define node statuses: pending, active, passed, warning, failed, blocked, invalidated.
- [ ] `M19-006` Define graph modes: Program, Execution, Evidence.
- [ ] `M19-007` Define immutable revision metadata.
- [ ] `M19-008` Define revision parentage.
- [ ] `M19-009` Define source event and plan-step links.
- [ ] `M19-010` Define node contract summary without requiring deep semantic atom contracts.
- [ ] `M19-011` Define bounded arbitrary metadata fields or prohibit them in favor of typed columns.
- [ ] `M19-012` Version the graph schema independently from the SQLite schema.

## SQLite Graph Schema

- [ ] `M19-013 DATA` Create graph identity table.
- [ ] `M19-014 DATA` Create immutable graph revision table.
- [ ] `M19-015 DATA` Create node identity table.
- [ ] `M19-016 DATA` Create node revision table.
- [ ] `M19-017 DATA` Create edge identity table.
- [ ] `M19-018 DATA` Create edge revision table.
- [ ] `M19-019 DATA` Create graph-to-task and graph-to-plan bindings.
- [ ] `M19-020 DATA` Create graph-to-event and graph-to-message links.
- [ ] `M19-021 DATA` Create source-location links.
- [ ] `M19-022 DATA` Create layout-hint table scoped by graph revision and layout algorithm version.
- [ ] `M19-023 DATA` Add uniqueness and foreign-key constraints.
- [ ] `M19-024 DATA` Add indexes for task slice, node lookup, neighbor expansion, evidence cone, and message link.
- [ ] `M19-025 DATA` Add migration-forward and backup/restore tests.

## Graph Projection

- [ ] `M19-026 BLOCKER` Project requirement nodes from accepted user intent.
- [ ] `M19-027` Project plan-region and plan-step nodes.
- [ ] `M19-028` Project repository inspection operations.
- [ ] `M19-029` Project file edit operations as atom/operation nodes.
- [ ] `M19-030` Project command and provider calls as effect nodes.
- [ ] `M19-031` Project approval boundaries.
- [ ] `M19-032` Project validation obligations and results.
- [ ] `M19-033` Project changed files/diff as artifact nodes.
- [ ] `M19-034` Project retries with explicit retry edges.
- [ ] `M19-035` Project checkpoint and recovery relationships where useful.
- [ ] `M19-036` Derive Program-mode visibility.
- [ ] `M19-037` Derive Execution-mode visibility and status.
- [ ] `M19-038` Derive Evidence-mode visibility.
- [ ] `M19-039` Create a new immutable graph revision after a material projection change.
- [ ] `M19-040` Avoid a new revision for token-only text deltas.
- [ ] `M19-041` Emit a bounded graph patch after revision commit.
- [ ] `M19-042 TEST` Replay the same task events and compare graph revisions deterministically.

## Query Service

- [ ] `M19-043 BLOCKER` Implement task-scoped initial slice query.
- [ ] `M19-044` Implement mode filtering.
- [ ] `M19-045` Implement node lookup by stable ID and revision.
- [ ] `M19-046` Implement one-hop neighbor expansion.
- [ ] `M19-047` Implement bounded multi-hop expansion.
- [ ] `M19-048` Implement evidence-cone isolation.
- [ ] `M19-049` Implement dependency-cone isolation.
- [ ] `M19-050` Implement text and identity search.
- [ ] `M19-051` Implement graph revision comparison.
- [ ] `M19-052` Return continuation tokens when node/edge bounds are reached.
- [ ] `M19-053` Reject unbounded full-database graph requests.
- [ ] `M19-054` Include layout hints and algorithm version.
- [ ] `M19-055 TEST` Test cycles, missing nodes, stale revisions, and expansion limits.

## Layout

- [ ] `M19-056 BLOCKER` Implement or integrate a deterministic left-to-right layered layout.
- [ ] `M19-057` Rank requirement and plan nodes before effects and artifacts.
- [ ] `M19-058` Collapse strongly connected components before ranking.
- [ ] `M19-059` Define stable sibling ordering by stable identity.
- [ ] `M19-060` Reuse prior coordinates when topology permits.
- [ ] `M19-061` Place newly added nodes near their stable neighbors.
- [ ] `M19-062` Compute bounding boxes for viewport fitting.
- [ ] `M19-063` Version layout algorithm changes.
- [ ] `M19-064` Cache layout hints in SQLite.
- [ ] `M19-065 TEST` Snapshot layout coordinates for deterministic fixtures.
- [ ] `M19-066 TEST` Verify unrelated node additions do not move the entire graph.

## Graph Viewport

- [ ] `M19-067 BLOCKER UX` Implement accessible SVG graph root.
- [ ] `M19-068 UX` Render node shapes by class.
- [ ] `M19-069 UX` Render status icon, border, and text independently of color.
- [ ] `M19-070 UX` Render edge style by relationship.
- [ ] `M19-071 UX` Add a visible legend.
- [ ] `M19-072 UX` Implement pointer pan.
- [ ] `M19-073 UX` Implement wheel/trackpad zoom around the cursor.
- [ ] `M19-074 UX` Implement zoom controls.
- [ ] `M19-075 UX` Implement fit-to-slice.
- [ ] `M19-076 UX` Implement reset view.
- [ ] `M19-077 UX` Implement node selection.
- [ ] `M19-078 UX` Implement keyboard traversal of visible nodes.
- [ ] `M19-079 UX` Implement focus-visible state.
- [ ] `M19-080 UX` Announce selected-node summary to assistive technology.
- [ ] `M19-081 UX` Center and select a node activated from chat.
- [ ] `M19-082 UX` Highlight messages related to the selected node.
- [ ] `M19-083 UX` Apply graph patches without resetting viewport.
- [ ] `M19-084 UX` Show a new-revision indicator when comparison is available.
- [ ] `M19-085 UX` Add Program, Execution, and Evidence mode tabs.
- [ ] `M19-086 UX` Default to Execution while running.
- [ ] `M19-087 UX` Default to Evidence after completion.
- [ ] `M19-088 UX` Preserve selection across compatible mode changes.

## Node Inspector

- [ ] `M19-089 UX` Display stable identity and revision.
- [ ] `M19-090 UX` Display node class, status, and contract summary.
- [ ] `M19-091 UX` Display inputs, outputs, and effects when known.
- [ ] `M19-092 UX` Display supporting evidence and guarantee level.
- [ ] `M19-093 UX` Display time, token, and cost attribution when known.
- [ ] `M19-094 UX` List related messages and events.
- [ ] `M19-095 UX` List related source locations.
- [ ] `M19-096 UX` Add explain-in-chat action.
- [ ] `M19-097 UX` Add expand-neighbors action.
- [ ] `M19-098 UX` Add isolate-dependency-cone action.
- [ ] `M19-099 UX` Add isolate-evidence-cone action.
- [ ] `M19-100 UX` Add compare-revision action.
- [ ] `M19-101 UX` Add open-in-editor action for validated repository paths.
- [ ] `M19-102 UX` State clearly when information is unknown rather than leaving a blank.

## Performance and Gate

- [ ] `M19-103 TEST` Benchmark initial 300-node layout.
- [ ] `M19-104 TEST` Benchmark 100 sequential graph patches.
- [ ] `M19-105 TEST` Measure SVG node, edge, label, and DOM counts.
- [ ] `M19-106 TEST` Verify chat streaming remains responsive during patches.
- [ ] `M19-107 TEST` Verify graph interaction remains responsive during token streaming.
- [ ] `M19-108 TEST` Test high-contrast and color-vision-independent statuses.
- [ ] `M19-G01 GATE` Program, Execution, and Evidence modes derive deterministically from one task history.
- [ ] `M19-G02 GATE` Chat and graph resolve the same stable node identities in both directions.
- [ ] `M19-G03 GATE` The viewport remains stable across normal graph revisions.
- [ ] `M19-G04 GATE` The graph remains optional; the complete task journey still works with the pane collapsed.

---

# Milestone 20: Validation, Review, Evidence, and Change Acceptance

Goal: turn “the agent finished” into an inspectable claim backed by exact commands, results, revisions, and limitations.

Plan references: §5 Execution and Review; §9 Proof Obligations as the Unit of Assurance; §10 Guarantee Provenance; §19 Review and Source Mapping; §22 Correctness and Assurance Gates; §27A Evidence mode; §29 Phase 2.

Depends on: `M19-G01` through `M19-G04`; command execution depends on `M10-G01` through `M10-G03`.

Milestone output: risk-based validation profiles, immutable validation evidence, source-linked diff review, acceptance/repair/rollback actions, and a final evidence report with no inflated assurance claims.

## Risk Classification

- [ ] `M20-001 BLOCKER` Define routine-change signals.
- [ ] `M20-002 BLOCKER` Define elevated-change signals.
- [ ] `M20-003 BLOCKER` Define protected-change signals.
- [ ] `M20-004` Include authentication, authorization, payment, migration, credential, concurrency, and external-effect signals.
- [ ] `M20-005` Include breadth, generated-code, dependency, configuration, and test-removal signals.
- [ ] `M20-006` Include user-selected risk override.
- [ ] `M20-007` Version the risk-classification policy.
- [ ] `M20-008` Persist input signals, selected risk, and explanation.
- [ ] `M20-009` Allow risk escalation after new evidence.
- [ ] `M20-010` Prohibit automatic risk demotion during a task.
- [ ] `M20-011 TEST` Build positive and negative fixtures for every protected signal.

## Validation Profiles

- [ ] `M20-012 BLOCKER` Define routine validation requirements.
- [ ] `M20-013 BLOCKER` Define elevated validation requirements.
- [ ] `M20-014 BLOCKER` Define protected validation requirements.
- [ ] `M20-015` Map repository-discovered formatter commands to profiles.
- [ ] `M20-016` Map targeted test commands to profiles.
- [ ] `M20-017` Map broader package or repository tests to profiles.
- [ ] `M20-018` Map build commands to profiles.
- [ ] `M20-019` Map static-analysis commands to profiles.
- [ ] `M20-020` Require user approval before a discovered command first runs if policy requires it.
- [ ] `M20-021` Define required versus advisory checks.
- [ ] `M20-022` Define timeout and retry behavior per check.
- [ ] `M20-023` Define skip reasons.
- [ ] `M20-024` Require explicit user authority to waive a required check.
- [ ] `M20-025` Record a waived check as waived, never passed.
- [ ] `M20-026` Version each validation profile.

## Test Selection

- [ ] `M20-027` Select tests in changed packages.
- [ ] `M20-028` Select tests linked through deterministic file-to-test mappings.
- [ ] `M20-029` Select tests implicated by failing baseline commands.
- [ ] `M20-030` Select repository-wide tests for protected changes when feasible.
- [ ] `M20-031` Preserve user-provided acceptance commands.
- [ ] `M20-032` Deduplicate equivalent test commands.
- [ ] `M20-033` Order cheap high-signal checks before expensive broad checks.
- [ ] `M20-034` Record why each check was selected.
- [ ] `M20-035` Record which changed files each check covers when known.

## Validation Execution

- [ ] `M20-036 BLOCKER` Create an immutable validation-run record before execution.
- [ ] `M20-037` Bind the run to exact worktree revision and dirty diff identity.
- [ ] `M20-038` Bind the run to command definition and executable identity.
- [ ] `M20-039` Emit validation-start event.
- [ ] `M20-040` Execute through the mediated command runner.
- [ ] `M20-041` Capture exit status, duration, timeout, cancellation, and truncation.
- [ ] `M20-042` Redact output before persistence.
- [ ] `M20-043` Parse Go test package/test names when possible.
- [ ] `M20-044` Parse formatter changes.
- [ ] `M20-045` Parse build and vet diagnostics.
- [ ] `M20-046` Preserve the raw redacted summary when parsing fails.
- [ ] `M20-047` Emit validation-result event after commit.
- [ ] `M20-048` Invalidate validation when the underlying diff changes.
- [ ] `M20-049` Rerun invalidated required checks before completion.
- [ ] `M20-050 TEST` Test invalidation after a one-line post-test edit.

## Baseline Failure Handling

- [ ] `M20-051` Run or record a baseline check before changes when affordable.
- [ ] `M20-052` Distinguish pre-existing failure from introduced failure.
- [ ] `M20-053` Bind comparison to exact revisions and command.
- [ ] `M20-054` Avoid claiming non-regression when baseline evidence is unavailable.
- [ ] `M20-055` Surface flaky or nondeterministic results.
- [ ] `M20-056` Require repeated evidence before labeling a failure flaky.
- [ ] `M20-057` Record unresolved baseline failures in the final report.

## Diff Review

- [ ] `M20-058 BLOCKER UX` Render changed-file list with status and line counts.
- [ ] `M20-059 UX` Filter source, test, generated, dependency, and configuration files.
- [ ] `M20-060 UX` Render a safe unified diff view for selected files.
- [ ] `M20-061 UX` Preserve whitespace visibility controls.
- [ ] `M20-062 UX` Link diff hunks to plan steps.
- [ ] `M20-063 UX` Link diff hunks to tool/edit events.
- [ ] `M20-064 UX` Link diff hunks to validation evidence.
- [ ] `M20-065 UX` Flag files outside proposed plan scope.
- [ ] `M20-066 UX` Flag broad formatting churn.
- [ ] `M20-067 UX` Flag binary or generated changes.
- [ ] `M20-068 UX` Open a validated source location in the external editor.
- [ ] `M20-069 SECURITY` Reject editor-open requests outside the bound repository.

## Evidence Report

- [ ] `M20-070 BLOCKER` Define final evidence-report schema.
- [ ] `M20-071` Include requirement and accepted plan revision.
- [ ] `M20-072` Include base revision and final diff identity.
- [ ] `M20-073` Include changed-file summary.
- [ ] `M20-074` Include every required validation and status.
- [ ] `M20-075` Include waived, skipped, unavailable, failed, and invalidated checks.
- [ ] `M20-076` Include risk level and classification explanation.
- [ ] `M20-077` Include user approvals and authority used.
- [ ] `M20-078` Include model/provider/tool/policy versions.
- [ ] `M20-079` Include forecast and actual time/tokens/cost.
- [ ] `M20-080` Include assumptions and unresolved limitations.
- [ ] `M20-081` Include graph revision and evidence-node links.
- [ ] `M20-082` Assign guarantee level per claim instead of one global badge.
- [ ] `M20-083` Mark external-system behavior as contract-checked or runtime-only.
- [ ] `M20-084` Persist the report as structured SQLite rows, not a Markdown sidecar.
- [ ] `M20-085` Render a readable report card from the structured data.

## Acceptance and Repair

- [ ] `M20-086 UX` Disable accept while required validations are running.
- [ ] `M20-087 UX` Require explicit acknowledgement before accepting failed or waived required checks.
- [ ] `M20-088` Persist acceptance with report and diff revision.
- [ ] `M20-089` Detect a diff change between review and acceptance.
- [ ] `M20-090` Require renewed review after a diff change.
- [ ] `M20-091 UX` Allow a repair request tied to selected failures or hunks.
- [ ] `M20-092` Create a new plan/checkpoint lineage for repair.
- [ ] `M20-093 UX` Allow rollback to the pre-repair checkpoint.
- [ ] `M20-094 UX` Allow rejection/abandonment without destroying the patch.

## Gate

- [ ] `M20-G01 GATE` No changed diff can inherit stale passed validation.
- [ ] `M20-G02 GATE` The final report distinguishes passed, failed, waived, skipped, unavailable, runtime-only, and invalidated evidence.
- [ ] `M20-G03 GATE` Acceptance is bound to the exact reviewed diff and report.
- [ ] `M20-G04 GATE` The UI makes unsupported external guarantees impossible to mistake for verified claims.

---

# Milestone 21: Deterministic Project Memory and Exact Reuse

Goal: make accepted work lower future context and execution cost without allowing similarity or model self-report to become authority.

Plan references: §5 Task Fingerprint and Retrieval; §23 Atom Storage and Vector Storage; §29 Phase 2; §31 Evidence-Driven Reuse and Learning; Learning Artifact Types; Chronological Episodes; Influence Lineage; Versioned Task Fingerprints; Retrieval and Pre-Work Gate.

Depends on: `M20-G01` through `M20-G04`.

Milestone output: project-scoped factual episodes, deterministic repository facts and commands, descriptively named and richly documented revision-bound atoms, exact compatibility-gated reuse, optional vector candidate discovery, lineage, invalidation, and user inspection/deletion.

## Memory Boundary

- [ ] `M21-001 BLOCKER` Define project-memory authority separately from raw task history.
- [ ] `M21-002` Define factual repository fact type.
- [ ] `M21-003` Define reviewed command type.
- [ ] `M21-004` Define file-to-test mapping type.
- [ ] `M21-005` Define repository convention type.
- [ ] `M21-006` Define accepted regression case type.
- [ ] `M21-007` Define execution recipe type.
- [ ] `M21-008` Define executable atom reference type without requiring deep semantic atoms.
- [ ] `M21-009` Define observation/hypothesis type with `evidence_strength: none`.
- [ ] `M21-010` Define maturity states: candidate, validated, preferred-for-experiment, quarantined, invalidated, retired.
- [ ] `M21-011` Prohibit model self-report from creating validated status.
- [ ] `M21-012` Define project ownership and cross-project isolation.
- [ ] `M21-013` Define user inspection, correction, export, and deletion semantics.

## SQLite Memory Schema

- [ ] `M21-014 DATA` Create memory artifact identity table.
- [ ] `M21-015 DATA` Create immutable artifact revision table.
- [ ] `M21-016 DATA` Create artifact type and maturity fields.
- [ ] `M21-017 DATA` Create project and repository-revision bindings.
- [ ] `M21-018 DATA` Create supporting-evidence links.
- [ ] `M21-019 DATA` Create `derived_from` lineage.
- [ ] `M21-020 DATA` Create `influenced_by` lineage.
- [ ] `M21-021 DATA` Create invalidation and quarantine records.
- [ ] `M21-022 DATA` Create applicability-predicate records.
- [ ] `M21-023 DATA` Create task-fingerprint schema-version table.
- [ ] `M21-024 DATA` Create vector-model/version metadata tables.
- [ ] `M21-025 DATA` Create vector rows linked to artifact revision.
- [ ] `M21-026 DATA` Create retrieval-candidate and retrieval-decision logs.
- [ ] `M21-027 DATA` Add project-boundary foreign keys and indexes.
- [ ] `M21-028 TEST` Test cascading logical deletion without cross-project impact.

## Chronological Episode Capture

- [ ] `M21-029 BLOCKER` Define episode start and end boundaries.
- [ ] `M21-030` Record requirement and accepted plan revisions.
- [ ] `M21-031` Record repository/context revisions.
- [ ] `M21-032` Record ordered actions and results by event reference.
- [ ] `M21-033` Record user interventions and approvals.
- [ ] `M21-034` Record validation and final decision.
- [ ] `M21-035` Record forecast and actual metrics.
- [ ] `M21-036` Record failures before repairs.
- [ ] `M21-037` Record whether the outcome was accepted, rejected, abandoned, or unresolved.
- [ ] `M21-038` Freeze the episode after terminal user decision.
- [ ] `M21-039` Allow later invalidation overlays without mutating historical facts.

## Deterministic Fact Extraction

- [ ] `M21-040` Extract successful build commands only from attributable executions.
- [ ] `M21-041` Extract successful test commands only from attributable executions.
- [ ] `M21-042` Bind commands to repository revision and relevant path scope.
- [ ] `M21-043` Extract file-to-test mappings from observed successful validations.
- [ ] `M21-044` Extract stable project instructions only after user approval.
- [ ] `M21-045` Extract formatting/lint conventions from configuration and accepted work.
- [ ] `M21-046` Deduplicate facts by normalized identity.
- [ ] `M21-047` Track first observed, last confirmed, and supporting episode count.
- [ ] `M21-048` Invalidate facts when supporting files or versions change.
- [ ] `M21-049` Require revalidation before an invalidated fact regains influence.
- [ ] `M21-050 TEST` Test a changed test runner invalidates its stored command.

## Task Fingerprint

- [ ] `M21-051 BLOCKER` Define fingerprint schema version 1.
- [ ] `M21-052` Include project and repository identity.
- [ ] `M21-053` Include base revision or compatibility range.
- [ ] `M21-054` Include normalized task class.
- [ ] `M21-055` Include affected package/symbol/path hints.
- [ ] `M21-056` Include language/toolchain/dependency bindings.
- [ ] `M21-057` Include risk and validation requirements.
- [ ] `M21-058` Include requested effects/authority class.
- [ ] `M21-059` Separate exact-match fields from descriptive retrieval text.
- [ ] `M21-060` Serialize exact fields canonically.
- [ ] `M21-061` Hash the canonical exact fingerprint.
- [ ] `M21-062 TEST` Verify identical inputs produce identical fingerprints.
- [ ] `M21-063 TEST` Verify material dependency or revision changes alter relevant bindings.

## Pre-Work Retrieval Gate

- [ ] `M21-064 BLOCKER` Run exact identity/fingerprint lookup before planning from scratch.
- [ ] `M21-065` Retrieve reviewed project facts relevant to selected context.
- [ ] `M21-066` Retrieve compatible commands and file-to-test mappings.
- [ ] `M21-067` Retrieve exact reusable atoms or recipes only when applicability predicates pass.
- [ ] `M21-068` Reject candidate with project-boundary mismatch.
- [ ] `M21-069` Reject candidate with toolchain/dependency mismatch.
- [ ] `M21-070` Reject candidate with invalidated evidence.
- [ ] `M21-071` Reject candidate whose assurance is below the current task requirement.
- [ ] `M21-072` Record every accepted and rejected retrieval candidate with reason.
- [ ] `M21-073` Present influential memory items to the user.
- [ ] `M21-074` Let the agent use, adapt, or reject an eligible item.
- [ ] `M21-075` Record actual influence rather than mere retrieval.
- [ ] `M21-076` Fall back to ordinary planning when no eligible item exists.
- [ ] `M21-077` Never treat vector similarity as eligibility.

## Optional Vector Candidate Discovery

- [ ] `M21-078` Measure deterministic retrieval misses before enabling embeddings.
- [ ] `M21-079` Select an embedding provider/model only if the measured problem justifies it.
- [ ] `M21-080` Record model, dimensions, normalization, and input-schema version.
- [ ] `M21-081` Generate embeddings from scrubbed descriptive fields.
- [ ] `M21-082` Store vectors in SQLite linked to exact artifact revision.
- [ ] `M21-083` Implement brute-force cosine search for prototype-scale data.
- [ ] `M21-084` Apply project scope before similarity ranking.
- [ ] `M21-085` Apply compatibility and assurance gates after candidate discovery.
- [ ] `M21-086` Record candidate rank and final rejection/acceptance.
- [ ] `M21-087` Re-embed when embedding model or input schema changes.
- [ ] `M21-088` Delete vectors when the owning artifact is deleted.
- [ ] `M21-089 DEFER` Do not add a separate vector database.

## Inspection and Correction UI

- [ ] `M21-090 UX` Add project-memory list.
- [ ] `M21-091 UX` Filter by type, maturity, validity, and last confirmation.
- [ ] `M21-092 UX` Show supporting episodes and lineage.
- [ ] `M21-093 UX` Show bindings and applicability predicate.
- [ ] `M21-094 UX` Show retrieval/influence history.
- [ ] `M21-095 UX` Allow user correction by creating a new revision.
- [ ] `M21-096 UX` Allow quarantine.
- [ ] `M21-097 UX` Allow invalidation with reason.
- [ ] `M21-098 UX` Allow deletion with affected-descendant preview.
- [ ] `M21-099 UX` Export selected structured records without secrets.

## Atom Documentation Extraction and Embedding

Plan: §7 Atom Documentation as Retrieval Material; §23 Atom Storage and Vector Storage; §31 Versioned Task Fingerprints and Retrieval and Pre-Work Gate.

- [ ] `M21-100 BLOCKER` Define atom-documentation schema version 1 using the exact field names in `AGENTS.md`.
- [ ] `M21-101` Define which atom categories are source-authored, SQLite-authored, or generated projections.
- [ ] `M21-102` Define immutable atom-documentation revision identity separately from atom and atom-version identity.
- [ ] `M21-103 DATA` Add `atom_documentation_revisions` with atom ID, atom version, schema version, repository revision, source comment hash, contract hash, normalized input hash, dependency bindings, validation status, and timestamps.
- [ ] `M21-104 DATA` Add normalized atom-documentation field storage without discarding the original scrubbed comment text.
- [ ] `M21-105 DATA` Link each atom embedding to the exact documentation revision that produced it.
- [ ] `M21-106 DATA` Add uniqueness constraints preventing two different normalized documents from claiming one documentation revision.
- [ ] `M21-107 DATA` Add indexes for atom identity, comment hash, contract hash, validity, and pending re-embedding.
- [ ] `M21-108` Parse source-authored atom comments with the Go parser and AST rather than regular expressions alone.
- [ ] `M21-109` Locate the doc group attached to the declared atom identifier.
- [ ] `M21-110` Parse and validate the schema-version header.
- [ ] `M21-111` Parse required top-level fields without losing list structure.
- [ ] `M21-112` Normalize indentation and insignificant whitespace.
- [ ] `M21-113` Preserve meaningful units, punctuation, domain terms, and negative examples.
- [ ] `M21-114` Reject missing, duplicate, unknown, or out-of-order fields according to the schema policy.
- [ ] `M21-115` Reject empty fields and unexplained `None` values.
- [ ] `M21-116` Validate that the opening sentence begins with the Go identifier.
- [ ] `M21-117` Flag likely keyword stuffing or repeated boilerplate for review rather than silently embedding it.
- [ ] `M21-118 SECURITY` Run documentation through the same secret and sensitive-data scrubber used before persistence.
- [ ] `M21-119 SECURITY` Reject comments containing known credentials, private keys, or prohibited sensitive fixtures.
- [ ] `M21-120` Compute the exact source-comment hash before normalization.
- [ ] `M21-121` Compute the normalized documentation-input hash after parsing and scrubbing.
- [ ] `M21-122` Bind the parsed document to the current atom contract hash and dependency bindings.
- [ ] `M21-123` Persist admission success or rejection reason.
- [ ] `M21-124` Generate Go comments for SQLite-authored atoms from the same structured documentation revision.
- [ ] `M21-125 TEST` Round-trip a structured SQLite atom document through generated Go comment and AST extraction.
- [ ] `M21-126 TEST` Verify round-trip preservation of semantic fields and stable normalized hash.
- [ ] `M21-127` Define embedding-input schema version 1 separately from documentation schema version 1.
- [ ] `M21-128` Include Purpose, Use when, Do not use when, Semantics, input/output meaning, Effects, Failure semantics, and Retrieval concepts in the default embedding input.
- [ ] `M21-129` Include retry, reconciliation, security, dependency, and limit fields only through concise semantic normalization.
- [ ] `M21-130` Exclude source paths, line numbers, timestamps, evidence run IDs, hashes, and repeated field labels from embedding input.
- [ ] `M21-131` Preserve negative selection examples so semantically close but invalid atoms can be distinguished.
- [ ] `M21-132` Record embedding model, dimensions, normalization, input-schema version, and input hash.
- [ ] `M21-133` Queue re-embedding when normalized input or embedding configuration changes.
- [ ] `M21-134` Invalidate retrieval influence immediately when comment, contract, binding, or evidence validity changes.
- [ ] `M21-135` Keep prior vectors for historical lineage while excluding them from active retrieval.
- [ ] `M21-136` Require project, compatibility, applicability, evidence, and assurance gates after vector candidate discovery.
- [ ] `M21-137` Record whether an atom was retrieved from exact identity, structured fields, vector similarity, or several channels.
- [ ] `M21-138` Record whether the agent used, adapted, or rejected the atom and why.
- [ ] `M21-139 TEST` Test semantic comment change creates a new documentation revision and pending vector.
- [ ] `M21-140 TEST` Test formatting-only comment change changes the source hash but can preserve the normalized input hash.
- [ ] `M21-141 TEST` Test contract change invalidates an otherwise unchanged comment vector.
- [ ] `M21-142 TEST` Test dependency-binding change invalidates active retrieval.
- [ ] `M21-143 TEST` Test embedding-model change regenerates vectors without rewriting historical documentation.
- [ ] `M21-144 TEST` Test a high-similarity atom with failed applicability is rejected.
- [ ] `M21-145 TEST` Test a richly documented atom cannot self-promote its assurance level.

## Atom Name Storage, Aliases, and Embedding

Plan: §7 Atom Naming and Retrieval Identity; §23 Atom Storage; §31 Retrieval and Pre-Work Gate.

- [ ] `M21-146 BLOCKER` Define atom naming-schema version 1 independently from documentation and embedding-input schemas.
- [ ] `M21-147` Define canonical Go identifier validation and maximum practical display behavior without imposing a short semantic length limit.
- [ ] `M21-148` Derive a human-readable display name from the canonical semantic phrase.
- [ ] `M21-149` Derive the normalized word-split phrase deterministically from the Go identifier.
- [ ] `M21-150` Preserve meaningful initialisms during word splitting.
- [ ] `M21-151` Define the allowlist and project extension mechanism for established domain abbreviations.
- [ ] `M21-152` Require a short naming rationale explaining the nearest confusing alternative and the qualifier that distinguishes this atom.
- [ ] `M21-153 DATA` Add `atom_names` with atom ID, atom version, canonical name, display name, normalized phrase, schema version, rationale, validity, and revision metadata.
- [ ] `M21-154 DATA` Add `atom_name_aliases` with alias text, normalized form, source, active interval, and target atom identity.
- [ ] `M21-155 DATA` Add a uniqueness constraint for active normalized canonical names within project and semantic scope.
- [ ] `M21-156 DATA` Preserve prior canonical names as immutable aliases after a semantic-preserving rename.
- [ ] `M21-157` Classify a proposed rename as formatting-only, semantic-preserving, or semantic-breaking.
- [ ] `M21-158` Require explicit review for semantic-preserving rename classification.
- [ ] `M21-159` Require a new atom version or identity for semantic-breaking rename classification.
- [ ] `M21-160` Create a new documentation revision after an accepted canonical rename.
- [ ] `M21-161` Include canonical name and normalized semantic phrase exactly once in embedding input.
- [ ] `M21-162` Include reviewed aliases as low-weight discovery text without duplicating the canonical phrase.
- [ ] `M21-163` Exclude obsolete or invalidated aliases from active candidate generation while retaining lineage.
- [ ] `M21-164` Invalidate and regenerate derived vectors after canonical name, normalized phrase, or active alias changes.
- [ ] `M21-165` Render the display name as the primary graph-node label and preserve the stable atom ID separately.
- [ ] `M21-166` Truncate only the visual graph label when space requires it; expose the full name in tooltip, inspector, search result, and accessibility label.
- [ ] `M21-167` Search canonical name, normalized phrase, and active aliases before vector similarity.
- [ ] `M21-168` Record which name or alias caused an atom to enter the candidate set.
- [ ] `M21-169 TEST` Test deterministic conversion among canonical, display, and normalized names.
- [ ] `M21-170 TEST` Test collision detection within one semantic scope.
- [ ] `M21-171 TEST` Test that equivalent names in separate project scopes remain isolated.
- [ ] `M21-172 TEST` Test semantic-preserving rename retains atom ID and creates alias/documentation revision lineage.
- [ ] `M21-173 TEST` Test semantic-breaking rename cannot retain the old compatible identity silently.
- [ ] `M21-174 TEST` Test an old alias finds the renamed atom but does not bypass applicability.
- [ ] `M21-175 TEST` Test graph truncation never changes stored canonical or accessible names.

## Gate

- [ ] `M21-G01 GATE` No memory item influences a task without project, compatibility, validity, and assurance checks.
- [ ] `M21-G02 GATE` Similarity produces candidates only; exact predicates determine eligibility.
- [ ] `M21-G03 GATE` The user can identify every memory item that influenced a completed task.
- [ ] `M21-G04 GATE` Changed support invalidates dependent facts and vectors transitively.
- [ ] `M21-G05 GATE` The prototype still works when vector discovery is disabled.
- [ ] `M21-G06 GATE` Every active atom vector is traceable to one validated documentation revision, contract hash, repository revision, embedding model, and input-schema version.
- [ ] `M21-G07 GATE` Rich atom comments improve candidate discovery without bypassing exact applicability, evidence, or assurance checks.
- [ ] `M21-G08 GATE` Every reusable atom has a standalone-descriptive canonical name, deterministic display and normalized forms, collision control, and rename lineage bound to its embeddings.

---

# Milestone 22: Test Harness, Benchmarks, and Observability

Goal: produce enough independent evidence to distinguish a working prototype from a persuasive demo.

Plan references: §3 Load-Bearing Experiments; §24 Specification Review; §25 Metrics; §26 Benchmark Timing; §28 Initial Demonstrations; §30 Kill and Pivot Criteria.

Depends on: `M21-G01` through `M21-G08`; individual harnesses may be built earlier alongside their owning milestones.

Milestone output: deterministic fakes, integration fixtures, fault injection, security cases, performance benchmarks, metric queries, and a reproducible prototype scorecard.

## Test Pyramid and Fixtures

- [ ] `M22-001 BLOCKER` Define fast unit, real-SQLite integration, process integration, browser component, and end-to-end suites.
- [ ] `M22-002` Define suite naming and build tags.
- [ ] `M22-003` Define deterministic clocks and ID generators for tests.
- [ ] `M22-004` Define deterministic fake model provider.
- [ ] `M22-005` Define scripted tool-call responses.
- [ ] `M22-006` Define fake pricing and usage.
- [ ] `M22-007` Define temporary Git repository fixture builder.
- [ ] `M22-008` Define representative clean Go repository fixture.
- [ ] `M22-009` Define dirty-worktree fixture.
- [ ] `M22-010` Define malicious-repository fixture.
- [ ] `M22-011` Define failing-test fixture.
- [ ] `M22-012` Define dependency/configuration-change fixture.
- [ ] `M22-013` Define protected-workflow fixture without claiming deep proof.
- [ ] `M22-014` Ensure fixtures contain no real credentials or private code.

## Unit and Property Tests

- [ ] `M22-015 TEST` Cover domain transition validators.
- [ ] `M22-016 TEST` Cover exact money and budget arithmetic.
- [ ] `M22-017 TEST` Cover task fingerprints and canonicalization.
- [ ] `M22-018 TEST` Cover context ranking determinism.
- [ ] `M22-019 TEST` Cover permission matching and expiration.
- [ ] `M22-020 TEST` Cover graph projection determinism.
- [ ] `M22-021 TEST` Cover graph layout stability.
- [ ] `M22-022 TEST` Cover assurance and evidence invalidation.
- [ ] `M22-023 TEST` Cover retrieval applicability predicates.
- [ ] `M22-024 TEST` Fuzz ID and cursor parsers.
- [ ] `M22-025 TEST` Fuzz protobuf/domain conversion.
- [ ] `M22-026 TEST` Fuzz safe path resolution.
- [ ] `M22-027 TEST` Fuzz event replay/projection.

## Real SQLite Integration

- [ ] `M22-028 TEST` Run every repository test against real SQLite.
- [ ] `M22-029 TEST` Run foreign-key and check-constraint failure cases.
- [ ] `M22-030 TEST` Run concurrent writer/read replay cases.
- [ ] `M22-031 TEST` Run WAL recovery after forced termination.
- [ ] `M22-032 TEST` Run every migration upgrade path.
- [ ] `M22-033 TEST` Run backup and restore.
- [ ] `M22-034 TEST` Run deletion and project-boundary isolation.
- [ ] `M22-035 TEST` Scan database bytes for seeded secrets after end-to-end use.

## Process and Fault Injection

- [ ] `M22-036 TEST` Kill worker during repository read.
- [ ] `M22-037 TEST` Kill worker during file edit.
- [ ] `M22-038 TEST` Kill worker during command execution.
- [ ] `M22-039 TEST` Kill worker during model streaming.
- [ ] `M22-040 TEST` Kill coordinator before event commit.
- [ ] `M22-041 TEST` Kill coordinator after event commit but before client delivery.
- [ ] `M22-042 TEST` Disconnect browser during approval.
- [ ] `M22-043 TEST` Disconnect browser during budget increase.
- [ ] `M22-044 TEST` Exhaust disk space during event append.
- [ ] `M22-045 TEST` Simulate database busy timeout.
- [ ] `M22-046 TEST` Simulate corrupted or missing worktree.
- [ ] `M22-047 TEST` Simulate provider rate limit.
- [ ] `M22-048 TEST` Simulate provider partial stream then failure.
- [ ] `M22-049 TEST` Simulate delayed provider usage.
- [ ] `M22-050 TEST` Simulate command timeout with child processes.

## Security and Abuse Cases

- [ ] `M22-051 SECURITY TEST` Attempt path traversal in every file API.
- [ ] `M22-052 SECURITY TEST` Attempt symlink escape.
- [ ] `M22-053 SECURITY TEST` Attempt unsafe editor-open target.
- [ ] `M22-054 SECURITY TEST` Attempt approval bypass through alternate tool.
- [ ] `M22-055 SECURITY TEST` Attempt repeated idempotency-key mutation.
- [ ] `M22-056 SECURITY TEST` Attempt repository prompt injection.
- [ ] `M22-057 SECURITY TEST` Attempt credential exfiltration through command output.
- [ ] `M22-058 SECURITY TEST` Attempt credential exfiltration through diagnostic export.
- [ ] `M22-059 SECURITY TEST` Attempt non-loopback browser connection.
- [ ] `M22-060 SECURITY TEST` Attempt cross-origin session use.
- [ ] `M22-061 SECURITY TEST` Attempt old per-launch session-secret reuse.
- [ ] `M22-062 SECURITY TEST` Attempt oversized message, tool output, and graph payloads.

## Browser and Accessibility Tests

- [ ] `M22-063 TEST` Automate the empty-shell journey.
- [ ] `M22-064 TEST` Automate create-thread and send-message.
- [ ] `M22-065 TEST` Automate plan approval.
- [ ] `M22-066 TEST` Automate command approval and denial.
- [ ] `M22-067 TEST` Automate pause and resume.
- [ ] `M22-068 TEST` Automate reconnect/replay.
- [ ] `M22-069 TEST` Automate graph-node/chat-message cross-selection.
- [ ] `M22-070 TEST` Automate diff review and acceptance.
- [ ] `M22-071 TEST` Automate crash recovery choice.
- [ ] `M22-072 TEST` Run automated accessibility scans for every major route/state.
- [ ] `M22-073 TEST` Run a keyboard-only end-to-end journey.
- [ ] `M22-074 TEST` Run a screen-reader smoke journey.
- [ ] `M22-075 TEST` Run reduced-motion and high-contrast journeys.

## Performance Benchmarks

- [ ] `M22-076` Benchmark cold coordinator startup.
- [ ] `M22-077` Benchmark warm coordinator startup.
- [ ] `M22-078` Benchmark database migration from the prior schema.
- [ ] `M22-079` Benchmark repository map on small, medium, and large Go fixtures.
- [ ] `M22-080` Benchmark context selection.
- [ ] `M22-081` Benchmark event append throughput and tail latency.
- [ ] `M22-082` Benchmark reconnect replay for 100, 1,000, and 10,000 events.
- [ ] `M22-083` Benchmark thread initial render and upward pagination.
- [ ] `M22-084` Benchmark simultaneous token and cost updates.
- [ ] `M22-085` Benchmark 300-node graph layout and render.
- [ ] `M22-086` Benchmark 100 graph patches without viewport reset.
- [ ] `M22-087` Benchmark SQLite vector search at expected prototype scale if enabled.
- [ ] `M22-088` Record CPU, memory, wall time, and allocation data.
- [ ] `M22-089` Run benchmarks on an ordinary target hobbyist laptop.
- [ ] `M22-090` Store benchmark methodology and results in Git-tracked documentation, not runtime SQLite.

## Metrics and Scorecard

- [ ] `M22-091` Implement queries for task success and user acceptance.
- [ ] `M22-092` Implement queries for regressions and unresolved failures.
- [ ] `M22-093` Implement queries for time-to-plan, first action, first diff, validation, and completion.
- [ ] `M22-094` Implement queries for tokens, cost, retries, and repairs.
- [ ] `M22-095` Implement queries for forecast error and interval coverage.
- [ ] `M22-096` Implement queries for approvals and denied actions.
- [ ] `M22-097` Implement queries for pause, cancel, recovery, and resume.
- [ ] `M22-098` Implement queries for retrieved and influential memory.
- [ ] `M22-099` Implement queries for graph usage and collapse rate.
- [ ] `M22-100` Build a redacted local prototype scorecard.
- [ ] `M22-101` Compare the frozen Codeflux run against the frozen baseline.
- [ ] `M22-102` Record failures and surprises, not only aggregate success.

## Developer Harness and Replay

Plan: §27D Deterministic Test Kit; Replay and Debugging; Logging, Tracing, and Profiling; Test Layers and Ownership; CI and Local Parity.

- [ ] `M22-103 BLOCKER` Implement deterministic test clock with manual advance and no wall-clock sleeps in state-machine tests.
- [ ] `M22-104 BLOCKER` Implement deterministic typed-ID sequence for repeatable snapshots and event fixtures.
- [ ] `M22-105 BLOCKER` Implement real temporary SQLite test database with migrations, integrity assertion, and registered cleanup.
- [ ] `M22-106 BLOCKER` Implement temporary Git repository/worktree fixture builder for clean, dirty, detached, conflicted, nested, and malicious cases.
- [ ] `M22-107 BLOCKER` Implement scripted provider with text, tool, usage, partial stream, delay, rate limit, authentication failure, and cancellation steps.
- [ ] `M22-108` Implement fake credential store that can assert no secret crosses a requested boundary.
- [ ] `M22-109` Implement event recorder with sequence, causation, transaction, replay, and wait-with-timeout assertions.
- [ ] `M22-110` Implement coordinator harness with isolated database, repository, worker, provider, port, clock, and IDs.
- [ ] `M22-111` Implement browser scenario harness with session bootstrap, event fixture, keyboard actions, accessibility checks, and screenshot-on-failure.
- [ ] `M22-112 SECURITY` Validate every test cleanup target before recursive deletion and preserve artifacts only behind an explicit failure flag.
- [ ] `M22-113` Implement named interactive fake scenarios for success, plan revision, approval, denial, repair, budget cap, provider failure, reconnect, worker crash, coordinator crash, concurrent edit, and recovery.
- [ ] `M22-114` Implement event replay from named fixture and redacted exported session.
- [ ] `M22-115` Implement replay stop-at-sequence, step-event, duplicate-delivery, gap, reconnect, and snapshot-repair controls.
- [ ] `M22-116` Implement server/client projection comparison during replay.
- [ ] `M22-117` Implement graph revision rebuild and comparison during replay.
- [ ] `M22-118` Implement safe read-only database inspection by domain entity, sequence, revision, lineage, and invalidation.
- [ ] `M22-119` Add development-only structured logs for transaction, event append/publish, worker lease, provider, tool, reducer, render, graph, and retrieval timing.
- [ ] `M22-120` Add authenticated loopback-only CPU, heap, goroutine, mutex, and block profiling in development builds.
- [ ] `M22-121` Add browser performance marks correlating event sequence with reducer and render duration.
- [ ] `M22-122 SECURITY` Verify replay, logs, profiles, screenshots, and failure artifacts contain no seeded credentials.
- [ ] `M22-123 TEST` Run each named fake scenario through backend-only, generated-client, and browser layers where applicable.
- [ ] `M22-124 TEST` Verify local and CI invoke the same development-helper command graph.
- [ ] `M22-125 TEST` Verify event schema additions fail CI without reducer and presentation/grouping coverage.
- [ ] `M22-126 TEST` Verify generated drift fails before tests that depend on generated output.
- [ ] `M22-127 TEST` Verify ordinary local and CI tests perform no external network request.
- [ ] `M22-128 DOC` Document failure-artifact locations, replay commands, safe database inspection, and profiling.
- [ ] `M22-129 DOC` Document golden paths for adding a backend use case, event/card, frontend component, graph projection, atom, migration, and provider.
- [ ] `M22-130 TEST` Give the golden-path documentation to a clean contributor/agent session and verify it can identify the correct plan section, TODO, test layer, event, and transaction for a sample vertical change.

## Gate

- [ ] `M22-G01 GATE` Fast tests are reliable enough to run on every change.
- [ ] `M22-G02 GATE` Full integration and browser suites pass from a fresh database.
- [ ] `M22-G03 GATE` Fault injection demonstrates zero duplicated correctness-bearing actions.
- [ ] `M22-G04 GATE` Secret, path, origin, authority, and payload abuse suites pass.
- [ ] `M22-G05 GATE` The prototype scorecard can be reproduced from documented commands.
- [ ] `M22-G06 GATE` Deterministic fakes, replay, projection comparison, diagnostics, profiling, and golden-path documentation make every vertical flow locally reproducible without paid providers or manual database mutation.

---

# Milestone 23: Diagnostics, Packaging, Updates, and Local Hardening

Goal: make the prototype installable, diagnosable, recoverable, and honest outside the developer's checkout.

Plan references: §27 Hobbyist MVP Decisions; Persistence, Recovery, Diagnostics, and Updates; Honest Cost Display; §27A Local Security; §30 Code-First Agent Failure.

Depends on: `M22-G01` through `M22-G06`.

Milestone output: signed/versioned local artifacts, first-run setup, OS credential integration, doctor/backup/diagnostic workflows, safe updates, and a complete limitation guide.

## CLI Surface

- [ ] `M23-001 BLOCKER` Implement `codeflux start`.
- [ ] `M23-002` Implement automatic browser open with opt-out.
- [ ] `M23-003` Print the loopback URL without printing the session secret in shell history where avoidable.
- [ ] `M23-004` Implement `codeflux doctor`.
- [ ] `M23-005` Implement `codeflux version`.
- [ ] `M23-006` Implement `codeflux backup`.
- [ ] `M23-007` Implement `codeflux integrity-check`.
- [ ] `M23-008` Implement `codeflux diagnostics export`.
- [ ] `M23-009` Implement `codeflux provider set`.
- [ ] `M23-010` Implement `codeflux provider test`.
- [ ] `M23-011` Implement `codeflux provider delete`.
- [ ] `M23-012` Implement clear exit codes.
- [ ] `M23-013` Add contextual command help.
- [ ] `M23-014` Avoid interactive prompts when a command is explicitly non-interactive.

## First-Run Journey

- [ ] `M23-015 UX` Detect first run.
- [ ] `M23-016 UX` Explain local-only architecture and data location.
- [ ] `M23-017 UX` Explain what remains in Git versus SQLite.
- [ ] `M23-018 UX` Let the user configure one provider.
- [ ] `M23-019 UX` Test the provider before continuing.
- [ ] `M23-020 UX` Let the user select a repository.
- [ ] `M23-021 UX` Inspect repository and show permissions.
- [ ] `M23-022 UX` Explain task worktree creation.
- [ ] `M23-023 UX` Offer a safe sample task or open an empty thread.
- [ ] `M23-024 UX` Show data deletion and backup locations.
- [ ] `M23-025 TEST` Time a fresh user's first-run journey.

## Doctor and Diagnostics

- [ ] `M23-026` Check supported Go toolchain availability.
- [ ] `M23-027` Check Git availability and version.
- [ ] `M23-028` Check database path permissions.
- [ ] `M23-029` Check SQLite integrity and schema compatibility.
- [ ] `M23-030` Check credential-store availability.
- [ ] `M23-031` Check configured provider connectivity without exposing credentials.
- [ ] `M23-032` Check worktree root availability and disk space.
- [ ] `M23-033` Check loopback port binding.
- [ ] `M23-034` Report active and recovery-required tasks.
- [ ] `M23-035` Report application and migration versions.
- [ ] `M23-036` Give actionable remediation per failed check.
- [ ] `M23-037` Create a redacted diagnostic manifest before export.
- [ ] `M23-038` Include versions, non-secret settings, health results, redacted logs, and selected task metadata.
- [ ] `M23-039` Exclude prompts/source/task content by default.
- [ ] `M23-040` Preview export contents and size.
- [ ] `M23-041` Require explicit confirmation for any optional sensitive content.
- [ ] `M23-042 TEST` Scan exported bundles against seeded secrets.

## Logging

- [ ] `M23-043` Define structured log levels and stable event names.
- [ ] `M23-044` Add request, task, run, and correlation IDs.
- [ ] `M23-045` Redact before serialization.
- [ ] `M23-046` Avoid raw prompts and source by default.
- [ ] `M23-047` Add bounded log rotation.
- [ ] `M23-048` Add retention settings.
- [ ] `M23-049` Add a user action to clear logs.
- [ ] `M23-050` Ensure clearing logs does not delete task evidence.
- [ ] `M23-051` Add development-only verbose logging with explicit warning.

## Packaging

- [ ] `M23-052 BLOCKER` Produce reproducible binaries for declared prototype platforms.
- [ ] `M23-053` Embed frontend assets.
- [ ] `M23-054` Embed migrations.
- [ ] `M23-055` Include license and notices.
- [ ] `M23-056` Include version/commit metadata.
- [ ] `M23-057` Create checksums.
- [ ] `M23-058` Sign release artifacts.
- [ ] `M23-059` Verify signatures in the release process.
- [ ] `M23-060` Test installation into a clean user profile.
- [ ] `M23-061` Test paths containing spaces and non-ASCII characters.
- [ ] `M23-062` Test operation without administrator privileges.
- [ ] `M23-063` Ensure uninstall instructions preserve or explicitly remove user data.

## Updates

- [ ] `M23-064` Keep manual update as the default prototype behavior.
- [ ] `M23-065` Check release compatibility before database migration.
- [ ] `M23-066` Back up the database before first launch of a newer schema.
- [ ] `M23-067` Display release notes and migration warning.
- [ ] `M23-068` Refuse a downgrade that cannot read the current schema.
- [ ] `M23-069` Document restoration to the backed-up database and older binary.
- [ ] `M23-070 TEST` Test supported upgrade paths from packaged artifacts.

## Documentation

- [ ] `M23-071 DOC` Write installation instructions.
- [ ] `M23-072 DOC` Write provider setup instructions.
- [ ] `M23-073 DOC` Write first-task walkthrough.
- [ ] `M23-074 DOC` Explain worktrees, acceptance, repair, rollback, and cleanup.
- [ ] `M23-075 DOC` Explain cost forecasts, actual cost, unknown pricing, and hard budgets.
- [ ] `M23-076 DOC` Explain permission tiers and optional container isolation.
- [ ] `M23-077 DOC` Explain SQLite data location, backup, inspection, export, and deletion.
- [ ] `M23-078 DOC` Explain graph modes and their non-proof status.
- [ ] `M23-079 DOC` Explain memory eligibility, lineage, invalidation, and vector candidate discovery.
- [ ] `M23-080 DOC` Document crash recovery.
- [ ] `M23-081 DOC` Document diagnostic export.
- [ ] `M23-082 DOC` Publish known limitations and unsupported guarantees.
- [ ] `M23-083 DOC` Document that the prototype is not a perfect security sandbox.
- [ ] `M23-084 DOC` Document that external systems may violate their contracts.
- [ ] `M23-085 DOC` Document deferred enterprise and deep-verification work.

## Gate

- [ ] `M23-G01 GATE` A clean machine/profile can install and reach the first-run screen from one artifact.
- [ ] `M23-G02 GATE` Doctor identifies representative Git, Go, database, credential, provider, and worktree failures.
- [ ] `M23-G03 GATE` Diagnostic export contains no seeded secrets.
- [ ] `M23-G04 GATE` Upgrade, backup, refusal, and restoration paths are documented and tested.

---

# Milestone 24: End-to-End Vertical Slice and Prototype Exit

Goal: prove the integrated product journey on frozen tasks, record failures honestly, and decide whether to proceed, narrow, or pivot.

Plan references: §3 Load-Bearing Experiments; §25 Metrics; §26 Benchmark Timing; §28 Initial Demonstrations; §28 ReserveFlow Dogfood API Refinement Trial; §29 Revised Development Sequence; §30 Kill and Pivot Criteria; §32 Central Design Principles.

Depends on: every prior milestone gate.

Milestone output: reproducible frozen demonstration runs, a chronological ReserveFlow API dogfood run, independently evaluated acceptance evidence, a controlled refinement ledger, comparison scorecard, issue inventory, kill/pivot decision, and a tagged prototype build.

## Clean-Room Setup

- [ ] `M24-001 BLOCKER` Create a clean user profile or disposable VM for the exit run.
- [ ] `M24-002` Install the packaged Codeflux artifact.
- [ ] `M24-003` Verify no development configuration leaks into the run.
- [ ] `M24-004` Verify no prior Codeflux database exists.
- [ ] `M24-005` Configure one provider through the documented journey.
- [ ] `M24-006` Clone or copy the frozen demonstration repository.
- [ ] `M24-007` Verify the exact frozen base revision.
- [ ] `M24-008` Verify hidden acceptance tests remain unavailable to the agent.
- [ ] `M24-009` Start screen recording or structured observer notes if allowed by the benchmark protocol.

## First-Run and Repository Journey

- [ ] `M24-010` Measure install-to-first-screen time.
- [ ] `M24-011` Complete first-run explanation.
- [ ] `M24-012` Test provider connection.
- [ ] `M24-013` Open the frozen repository.
- [ ] `M24-014` Inspect repository status and proposed worktree policy.
- [ ] `M24-015` Verify selected context is explainable.
- [ ] `M24-016` Create a new thread.
- [ ] `M24-017` Submit the frozen task requirement verbatim.

## Plan, Forecast, and Approval Journey

- [ ] `M24-018` Record time to first forecast.
- [ ] `M24-019` Record time to first plan.
- [ ] `M24-020` Inspect scope, expected files, validation, risk, and assumptions.
- [ ] `M24-021` Inspect P50/P90 time/token/cost estimates.
- [ ] `M24-022` Inspect fixed provider, model, effort, and policy version.
- [ ] `M24-023` Set the frozen hard budget.
- [ ] `M24-024` Approve or redirect according to the benchmark script.
- [ ] `M24-025` Verify plan revision and approval appear in SQLite-backed replay.

## Execution Journey

- [ ] `M24-026` Start the task.
- [ ] `M24-027` Verify isolated worktree creation.
- [ ] `M24-028` Verify the execution graph highlights the active path.
- [ ] `M24-029` Verify tool output remains summarized by default.
- [ ] `M24-030` Exercise at least one permission request.
- [ ] `M24-031` Verify allow-once or deny behavior according to the script.
- [ ] `M24-032` Verify cost and budget update during execution.
- [ ] `M24-033` Pause the task at the scripted point.
- [ ] `M24-034` Reload or restart Codeflux while paused.
- [ ] `M24-035` Verify replay reconstructs exact task state.
- [ ] `M24-036` Resume from the validated checkpoint.
- [ ] `M24-037` Record time to first diff.
- [ ] `M24-038` Record unexpected tool calls, retries, loops, or user interventions.

## Validation and Review Journey

- [ ] `M24-039` Verify required validation selection.
- [ ] `M24-040` Verify validation runs against the exact current diff.
- [ ] `M24-041` Inspect Program graph mode.
- [ ] `M24-042` Inspect Execution graph mode.
- [ ] `M24-043` Inspect Evidence graph mode.
- [ ] `M24-044` Select a graph node and verify related chat highlighting.
- [ ] `M24-045` Select a chat node chip and verify graph focus.
- [ ] `M24-046` Inspect changed-file and diff summaries.
- [ ] `M24-047` Open one changed source location in the external editor.
- [ ] `M24-048` Inspect every required, passed, failed, skipped, waived, or unavailable check.
- [ ] `M24-049` Inspect risk, approvals, model/tool versions, assumptions, and limitations.
- [ ] `M24-050` Verify forecast-versus-actual time/tokens/cost.
- [ ] `M24-051` Accept, repair, or roll back according to the benchmark result.

## Independent Evaluation

- [ ] `M24-052 BLOCKER` Run hidden acceptance tests after Codeflux stops.
- [ ] `M24-053` Record functional correctness.
- [ ] `M24-054` Record regressions.
- [ ] `M24-055` Review code quality independently of Codeflux's report.
- [ ] `M24-056` Review whether the diff stayed within intended scope.
- [ ] `M24-057` Verify no unapproved external effects occurred.
- [ ] `M24-058` Verify no secret exists in database, logs, events, worktree metadata, or diagnostics.
- [ ] `M24-059` Verify every correctness-bearing UI claim has backing evidence.
- [ ] `M24-060` Verify the evidence report did not overstate external guarantees.
- [ ] `M24-061` Compare outcome, latency, cost, and intervention count with the frozen baseline.

## Recovery Exit Scenarios

- [ ] `M24-062` Run the frozen task with browser disconnect during streaming.
- [ ] `M24-063` Verify gap-free replay.
- [ ] `M24-064` Run the frozen task with worker termination.
- [ ] `M24-065` Verify safe recovery-required presentation.
- [ ] `M24-066` Run the frozen task with coordinator termination after an edit.
- [ ] `M24-067` Verify worktree and checkpoint reconciliation.
- [ ] `M24-068` Run a hard-budget exhaustion scenario.
- [ ] `M24-069` Verify no unapproved post-cap model request begins.
- [ ] `M24-070` Run a concurrent user-edit scenario.
- [ ] `M24-071` Verify the user's edit is not overwritten.

## Memory Exit Scenarios

- [ ] `M24-072` Complete and accept a task that yields a deterministic repository fact.
- [ ] `M24-073` Start a related second task.
- [ ] `M24-074` Verify pre-work retrieval finds the eligible fact.
- [ ] `M24-075` Verify the UI shows that the fact influenced the task.
- [ ] `M24-076` Change the supporting repository configuration.
- [ ] `M24-077` Verify the fact becomes ineligible or invalidated.
- [ ] `M24-078` Verify no vector candidate bypasses the compatibility gate.
- [ ] `M24-079` Delete the test memory and verify dependent vectors/links are removed or invalidated correctly.

## ReserveFlow Dogfood Control Plane

- [ ] `M24-101 BLOCKER` Create a separate frozen ReserveFlow repository containing only the Go module, first-task README, empty command entry point, test-helper skeleton, license, and Git configuration specified by §28.
- [ ] `M24-102` Record and verify the cryptographic identity of the frozen ReserveFlow scaffold revision.
- [ ] `M24-103 BLOCKER` Create a separate evaluator repository for hidden acceptance, concurrency, security, recovery, migration, and contract tests.
- [ ] `M24-104 SECURITY` Verify the Codeflux coordinator, worker, tools, provider context, and ReserveFlow worktree cannot read the evaluator repository.
- [ ] `M24-105 DATA` Allocate a fresh Codeflux runtime database dedicated to each evaluated dogfood track.
- [ ] `M24-106 DATA` Allocate a ReserveFlow application database independently of the Codeflux runtime database.
- [ ] `M24-107 TEST` Build a reset operation that restores the exact accepted ReserveFlow commit, removes only run-scoped application state, and creates a fresh isolated Codeflux database.
- [ ] `M24-108` Freeze Go, dependency, operating-system, architecture, Codeflux, provider, model, effort, tool, price, validation-policy, and routing-policy versions in the run manifest.
- [ ] `M24-109` Write fifteen separately revealable requirement packets matching the chronological sequence in §28.
- [ ] `M24-110 TEST` Verify a task run can access its current and prior accepted requirements but cannot access any future requirement packet.
- [ ] `M24-111` Define one accepted ReserveFlow commit chain and require every comparison track to advance through equivalent accepted states.
- [ ] `M24-112 DATA` Define an append-only intervention ledger for clarifications, approvals, redirects, denials, rollbacks, manual commands, manual source edits, evaluator actions, and contamination decisions.
- [ ] `M24-113 GATE` Configure the evaluated Codeflux track so any manual source edit marks the run contaminated and ineligible for the no-intervention exit claim.
- [ ] `M24-114 DATA` Retain the task requirement, forecast, plan revisions, budget, events, checkpoints, tool summaries, worktree diff, validation, evidence, cost, and acceptance decision for every dogfood task.
- [ ] `M24-115` Define Track A, Track B, Track C, and later Track D configuration manifests without changing the chronological requirements or acceptance authority.

## ReserveFlow Independent Evaluation Harness

- [ ] `M24-116 TEST` Provide evaluator-controlled deterministic clock fixtures for expiration boundaries and retry schedules.
- [ ] `M24-117 TEST` Provide evaluator-controlled stable identity fixtures for resources, reservations, outbox events, and deliveries.
- [ ] `M24-118 TEST` Build a mock webhook receiver that records delivery identity, signature, headers, payload hash, receipt time, and response behavior without exposing its assertions to Codeflux.
- [ ] `M24-119 TEST` Add webhook ambiguity modes for accepted-then-timeout, connection refusal, slow response, terminal 4xx, retryable 5xx, and duplicate receipt.
- [ ] `M24-120 TEST` Add an in-process concurrency driver for same-resource and same-idempotency-key races.
- [ ] `M24-121 TEST` Add a multi-process concurrency driver for SQLite lock, stale-version, worker-ownership, and shutdown races.
- [ ] `M24-122 TEST` Add named crash points before and after reservation commit, expiration selection, expiration commit, outbox claim, receiver acceptance, delivery-state commit, and migration commit.
- [ ] `M24-123 SECURITY` Create malformed, missing, invalid, revoked, and scope-mismatched API-key fixtures.
- [ ] `M24-124 SECURITY` Seed synthetic secret markers into credentials, callback configuration, request bodies, and tool output so leakage can be detected across every persisted and displayed surface.
- [ ] `M24-125 TEST` Create database snapshots for empty, prior-schema, populated, interrupted-migration, and unsupported-newer-schema cases.
- [ ] `M24-126 TEST` Build an OpenAPI-versus-runtime verifier for paths, methods, request schemas, response schemas, status codes, pagination, idempotency, concurrency headers, and error envelopes.
- [ ] `M24-127 TEST` Freeze the visible test suite that supplies legitimate local feedback for each revealed requirement.
- [ ] `M24-128 BLOCKER TEST` Freeze the hidden behavioral suite and its pass criteria before the evaluated run begins.
- [ ] `M24-129 TEST` Review hidden tests to ensure they assert required behavior rather than an undisclosed preferred implementation shape.
- [ ] `M24-130 TEST` Hash the evaluator repository, requirement packets, visible fixtures, hidden fixtures, and scoring configuration so post-run changes are detectable.

## Chronological ReserveFlow Tasks

For each pair below, the first item opens a new Codeflux task from the prior accepted commit with a fresh forecast, plan, budget, worktree, and episode. The second item runs visible and hidden acceptance, records the decision, and advances the accepted chain only on success.

- [ ] `M24-131` Run ReserveFlow Task 1 for server lifecycle, health, readiness, request IDs, JSON behavior, and safe errors.
- [ ] `M24-132 TEST` Independently accept or reject Task 1 against port, cancellation, signal, malformed-path, and deterministic-health cases.
- [ ] `M24-133` Run ReserveFlow Task 2 for SQLite resource persistence, capacity validation, stable identity, timestamps, and bounded cursor pagination.
- [ ] `M24-134 TEST` Independently accept or reject Task 2 against clean migration, restart, invalid capacity, ordering, cursor, and duplicate-request cases.
- [ ] `M24-135` Run ReserveFlow Task 3 for atomic pending-reservation creation and capacity decrement.
- [ ] `M24-136 TEST` Independently accept or reject Task 3 against invalid quantity, unknown resource, insufficient capacity, rollback, and error-shape cases.
- [ ] `M24-137` Run ReserveFlow Task 4 for canonical-request idempotency, original-response replay, expiry, and semantic-input conflict.
- [ ] `M24-138 TEST` Independently accept or reject Task 4 against JSON reordering, concurrent same-key calls, transport retries, expiry, and changed-input cases.
- [ ] `M24-139` Run ReserveFlow Task 5 for expected-version confirm and cancel transitions with explicit repeated-request semantics.
- [ ] `M24-140 TEST` Independently accept or reject Task 5 against valid, stale, repeated, forbidden, capacity-release, and confirm/cancel race cases.
- [ ] `M24-141` Run ReserveFlow Task 6 for concurrent capacity safety across reservation creation and cancellation.
- [ ] `M24-142 TEST` Independently accept or reject Task 6 with in-process and multi-process contention proving no oversubscription, negative capacity, lost update, duplicate reservation, or deadlock.
- [ ] `M24-143` Run ReserveFlow Task 7 for deterministic expiration, exact-once capacity release, worker ownership, bounded scans, shutdown, and restart.
- [ ] `M24-144 TEST` Independently accept or reject Task 7 at clock boundaries, with multiple workers, injected crashes, repeated scans, shutdown, and late confirmation.
- [ ] `M24-145` Run ReserveFlow Task 8 for transactional outbox creation, bounded polling, ordering, and publish-state transitions.
- [ ] `M24-146 TEST` Independently accept or reject Task 8 against rollback, duplicate polling, restart, poison-event, ordering, and one-event-per-transition cases.
- [ ] `M24-147` Run ReserveFlow Task 9 for signed webhook delivery, stable delivery identity, bounded retry/backoff, secret references, and disabled endpoints.
- [ ] `M24-148 TEST` Independently accept or reject Task 9 against success, ambiguity, connection failure, 4xx, 5xx, duplicate receipt, signature, disablement, output bounds, and redaction.
- [ ] `M24-149` Run ReserveFlow Task 10 for API-key authorization of administrative operations and explicit policy for reservation operations.
- [ ] `M24-150 SECURITY TEST` Independently accept or reject Task 10 against missing, malformed, invalid, revoked, scope, comparison, logging, error, and capability-leakage cases.
- [ ] `M24-151` Run ReserveFlow Task 11 for correlated structured logs, stable error codes, local metrics, readiness dependencies, and redacted diagnostics.
- [ ] `M24-152 TEST` Independently accept or reject Task 11 by tracing request, database, worker, and webhook activity while verifying no body or secret leakage.
- [ ] `M24-153` Run ReserveFlow Task 12 for an OpenAPI contract that describes only implemented behavior, examples, errors, pagination, idempotency, and concurrency.
- [ ] `M24-154 TEST` Independently accept or reject Task 12 with contract-versus-runtime verification and unsupported-guarantee review.
- [ ] `M24-155` Run ReserveFlow Task 13 from the frozen defect revision without revealing the defect root cause and require Codeflux to diagnose it.
- [ ] `M24-156 TEST` Independently accept or reject Task 13 only after a reproducing regression test and a behaviorally correct fix pass race and prior-regression suites.
- [ ] `M24-157` Run ReserveFlow Task 14 for the frozen post-memory domain-rule change without supplying an affected-file list.
- [ ] `M24-158 TEST` Independently accept or reject Task 14 across state transitions, capacity, HTTP contract, outbox, webhooks, tests, documentation, graph, evidence, and memory invalidation.
- [ ] `M24-159` Run ReserveFlow Task 15 for the frozen dependency upgrade and backwards-compatible schema addition.
- [ ] `M24-160 TEST` Independently accept or reject Task 15 across migration, data preservation, API compatibility, version binding, cached-artifact eligibility, and evidence invalidation.

## Dogfood Product and Recovery Observations

- [ ] `M24-161 UX` Run at least one complete task with the graph collapsed and verify every required plan, approval, execution, validation, review, and recovery action remains available.
- [ ] `M24-162 UX` Run a later task while inspecting Program, Execution, and Evidence graph modes and record whether each mode changes a decision or merely adds visual activity.
- [ ] `M24-163 UX` Verify message-to-node and node-to-message navigation on one planning decision, one active tool action, one changed symbol, one failed check, and one accepted evidence claim.
- [ ] `M24-164 TEST` Pause a dogfood task after a durable edit, restart the browser, and verify ordered replay and retained control state.
- [ ] `M24-165 TEST` Terminate a worker at a named crash boundary and verify recovery does not duplicate an edit, command, reservation-side test effect, or provider request.
- [ ] `M24-166 TEST` Terminate the coordinator after a committed event and verify database, checkpoint, worktree, budget, and UI reconciliation.
- [ ] `M24-167 TEST` Change one ReserveFlow file concurrently outside Codeflux and verify the user edit is detected, preserved, and resolved explicitly.
- [ ] `M24-168 TEST` Exhaust the hard budget during one controlled attempt and verify no post-cap provider request or silent cheaper-model fallback begins.
- [ ] `M24-169 SECURITY` Exercise one scoped network approval for the webhook test and verify allow-once, allow-for-task, denial, expiry, and replay presentation.
- [ ] `M24-170 UX` Record every point where the operator cannot determine current state, authority, cost, next action, failure ownership, or recovery safety without opening raw storage or logs.

## Controlled Codeflux Refinement Loop

- [ ] `M24-171 DATA` Give every dogfood failure or serious friction report a stable identity linked to the exact task, accepted base, Codeflux version, run, episode, and evaluator result.
- [ ] `M24-172 DATA` Freeze the failing event sequence, worktree diff, provider/model, policy, budget, environment, tool versions, and relevant redacted diagnostics before attempting repair.
- [ ] `M24-173` Assign one primary failure category from §28 and record severity, frequency, reproducibility, symptom, and first responsible Section 0 layer.
- [ ] `M24-174` Classify ownership as Codeflux, provider/model, ReserveFlow requirement, visible test, hidden test, environment, or evaluation protocol.
- [ ] `M24-175` Record every workaround and mark whether it contaminated the run or changed the acceptance conditions.
- [ ] `M24-176 BLOCKER TEST` Reproduce a Codeflux-owned failure outside the partial ReserveFlow run using the smallest deterministic fixture that still fails.
- [ ] `M24-177 TEST` Add a failing regression test at the lowest responsible Codeflux layer before implementing the repair.
- [ ] `M24-178` State the general failure class and observable invariant the proposed repair addresses.
- [ ] `M24-179` Implement the smallest general repair without weakening validation, permission, evidence, budget, project-boundary, or recovery policy.
- [ ] `M24-180 TEST` Run the targeted regression test and the owning subsystem suite.
- [ ] `M24-181 TEST` Run relevant security, replay, migration, concurrency, frontend reducer, and end-to-end suites selected from the changed invariant.
- [ ] `M24-182` Rebuild and version Codeflux after the repair.
- [ ] `M24-183 TEST` Reset ReserveFlow to the original accepted base and rerun the original revealed requirement instead of continuing from repaired partial output.
- [ ] `M24-184 TEST` Run the first repair verification with project memory and vector discovery disabled so stored hints cannot hide a tool defect.
- [ ] `M24-185 TEST` Rerun the chronological path with only previously accepted ReserveFlow memory enabled and record its actual influence.
- [ ] `M24-186 TEST` Re-run all earlier affected ReserveFlow acceptance tests.
- [ ] `M24-187 TEST` Run one unrelated repository fixture that expresses the same general failure class.
- [ ] `M24-188` Reject a repair that passes only the hidden case, relies on future-requirement knowledge, or adds task-specific prompt text without a general invariant.
- [ ] `M24-189 DATA` Record correctness, speed, cost, UX, DevX, and any newly introduced tradeoff before closing the defect.
- [ ] `M24-190` Close each defect as fixed, accepted limitation, deferred with owner and trigger, evaluator defect, or product-scope rejection; do not silently discard it.
- [ ] `M24-216 BLOCKER EVAL` Before use, freeze the adversarial reviewer's prompt, model, input allowlist, output schema, no-edit and no-approval authority, execution timing, exact budget, and cost accounting.
- [ ] `M24-217 BLOCKER SECURITY TEST` Extend M24-104 isolation to the adversarial reviewer and verify it cannot access evaluator source, hidden assertions or answers, future requirements, or live authority.
- [ ] `M24-218 DATA` After each complete evaluated run and proposed refinement, run the evaluation-only adversarial reviewer without influencing the active run and record its time, tokens, cost, findings, and resulting interventions.
- [ ] `M24-219 DATA` Version every prompt or process candidate, change one general invariant at a time, and preregister the exact diff, tuning cohort, primary endpoint, minimum effect, repetitions, analysis, stop rule, multiple-comparison treatment, and frozen execution envelope.
- [ ] `M24-220 TEST` Select at most one candidate on the exposed tuning cohort, keep the lineage-unexposed held-out cohort frozen until selection, allow one confirmation, and never use ReserveFlow hidden-evaluator results for prompt selection or revision.
- [ ] `M24-221 GATE` Reject candidates with any correctness, validation, authority, security, secrecy, recovery, or independent-acceptance regression; retain only a candidate meeting its preregistered gate for the named frozen stratum and otherwise report inconclusive or retired.

## Dogfood Measurement and Comparison

- [ ] `M24-191 DATA` Record visible acceptance, hidden acceptance, independent diff review, regressions, and delayed defects per task.
- [ ] `M24-192 DATA` Record time to forecast, plan, first action, first diff, validation, review, and acceptance per task.
- [ ] `M24-193 DATA` Record input, cached, and output tokens plus provider, model, tool, and estimated human cost per task.
- [ ] `M24-194 DATA` Record forecast P50/P90 coverage, absolute error, and systematic under- or over-estimation.
- [ ] `M24-195 DATA` Record plan revisions, repair rounds, repeated actions, escalations, and manual interventions.
- [ ] `M24-196 DATA` Compare files selected, files changed, and files independently judged necessary.
- [ ] `M24-197 DATA` Record approvals requested, granted, denied, expired, and retrospectively judged unnecessary or too broad.
- [ ] `M24-198 DATA` Record checkpoint, reconnect, worker recovery, coordinator recovery, and resume outcomes.
- [ ] `M24-199 DATA` Record graph opens, mode use, cross-navigation, decisions changed, confusion, and user-rated usefulness.
- [ ] `M24-200 DATA` Record exact and vector retrieval candidates, eligibility decisions, influence, acceptance outcome, and invalidation.
- [ ] `M24-201 DATA` Record atoms reused, adapted, rejected, invalidated, newly admitted, and renamed.
- [ ] `M24-202` Run Track A with the frozen strong coding-agent baseline and no Codeflux project memory.
- [ ] `M24-203 BLOCKER` Run Track B with Codeflux's fixed model/effort policy and vector discovery disabled.
- [ ] `M24-204` Run Track C with the same fixed policy plus only admitted deterministic ReserveFlow project memory.
- [ ] `M24-205` Keep Track D unexecuted, record the later authorization trigger, and exclude adaptive-policy claims from the prototype dogfood result.
- [ ] `M24-206` Compare correctness before speed or cost, including all failed cheap attempts, escalations, interventions, and evaluator failures.
- [ ] `M24-207` Report whether marginal time, cost, context size, and repair rounds decline across accepted tasks without correctness regression.
- [ ] `M24-208` Separate observed Codeflux benefit from model variance, benchmark learning, evaluator leakage, and operator learning.
- [ ] `M24-209 TEST` Perform one final Track B rerun from the untouched scaffold with fresh Codeflux and ReserveFlow databases.
- [ ] `M24-210 TEST` Run the complete visible and hidden suites against the final accepted ReserveFlow revision and verify API/OpenAPI agreement.
- [ ] `M24-211 SECURITY` Scan Codeflux state, ReserveFlow state, Git history, logs, events, diagnostics, graph data, comments, and fixtures for seeded secret markers.
- [ ] `M24-212` Produce the final dogfood scorecard with raw counts, denominators, confidence limits where meaningful, and links to attributable evidence.
- [ ] `M24-213` Produce a prioritized Codeflux refinement inventory ordered by correctness risk, then user-blocking friction, then speed, then cost.
- [ ] `M24-214` Record continue, narrow, redesign, defer, or kill decisions for the agent loop, graph, atoms, deterministic memory, vectors, forecasting, routing, recovery, and frontend.
- [ ] `M24-215` Update the plan only with findings supported by the frozen trial, while retaining unresolved observations in the backlog rather than converting them into speculative architecture.

## Scorecard and Decision

These stable earlier IDs execute after the ReserveFlow trial so the final prototype decision includes the dogfood evidence.

- [ ] `M24-080 BLOCKER` Populate the frozen correctness metrics.
- [ ] `M24-081` Populate latency metrics.
- [ ] `M24-082` Populate token and cost metrics.
- [ ] `M24-083` Populate forecast calibration observations.
- [ ] `M24-084` Populate usability observations.
- [ ] `M24-085` Populate interruption and recovery results.
- [ ] `M24-086` Populate permission and security-boundary results.
- [ ] `M24-087` Populate graph usefulness and confusion observations.
- [ ] `M24-088` Populate memory influence and invalidation results.
- [ ] `M24-089` List every manual workaround used.
- [ ] `M24-090` List every flaky or irreproducible result.
- [ ] `M24-091` List every unsupported claim users could plausibly misunderstand.
- [ ] `M24-092` Classify failures as implementation bug, specification defect, model limitation, tooling limitation, UX failure, or experiment-design problem.
- [ ] `M24-093` Compare results to §30 kill and pivot criteria.
- [ ] `M24-094` Decide continue, narrow, redesign, or stop for each major subsystem.
- [ ] `M24-095` Keep adaptive routing disabled unless its later evidence gate is met.
- [ ] `M24-096` Keep deep graph verification disabled unless its independent graph gate is met.
- [ ] `M24-097` Create a prioritized post-prototype defect list.
- [ ] `M24-098` Tag the exact prototype source revision.
- [ ] `M24-099` Archive reproducible benchmark methodology and redacted results.
- [ ] `M24-100` Update `docs/plan.md` with evidence-driven decisions instead of speculative additions.

## Final Gate

- [ ] `M24-G01 GATE` The frozen task passes independent acceptance without an unauthorized action.
- [ ] `M24-G02 GATE` The full journey works after clean installation and without developer intervention.
- [ ] `M24-G03 GATE` Pause, reconnect, worker crash, coordinator crash, budget exhaustion, and concurrent-edit scenarios all preserve correctness-bearing state.
- [ ] `M24-G04 GATE` Final evidence, cost, limitations, and graph views agree with durable SQLite state.
- [ ] `M24-G05 GATE` The team records explicit continue/narrow/pivot decisions against the plan's kill criteria.
- [ ] `M24-G06 GATE` Track B builds the full chronological ReserveFlow API from the frozen scaffold without manual source edits, future-requirement leakage, an unauthorized action, a secret leak, or an unacknowledged false correctness claim.
- [ ] `M24-G07 GATE` Every ReserveFlow task passes visible and independent hidden acceptance before the accepted commit chain advances.
- [ ] `M24-G08 GATE` Every Codeflux-owned dogfood defect has a frozen reproduction, lowest-layer regression test, general fix or explicit defer decision, clean-base memory-off rerun, chronological memory-on rerun, and unrelated-fixture result.
- [ ] `M24-G09 GATE` The final clean Track B rerun and complete evaluator suite pass without regressing the original accepted scorecard.
- [ ] `M24-G10 GATE` The dogfood evidence supports explicit keep, narrow, redesign, defer, or kill decisions without treating one API as proof of general superiority.

---

# Explicitly Deferred Until After Prototype Exit

These are guardrails, not hidden TODOs for the prototype critical path.

- [ ] `POST-001 DEFER` Run the disposable graph-medium experiment before production semantic graph engineering.
- [ ] `POST-002 DEFER` Scope and freeze the tier-zero kernel only if the graph experiment passes.
- [ ] `POST-003 DEFER` Implement graph-native atoms only after kernel scope is accepted.
- [ ] `POST-004 DEFER` Implement modeled Go atoms and reference models only after correlation controls are specified.
- [ ] `POST-005 DEFER` Implement Go lowering and source maps only after the lowering conformance suite is frozen.
- [ ] `POST-006 DEFER` Implement determinism conformance across architecture/toolchain matrices only for the authorized deep-verification track.
- [ ] `POST-007 DEFER` Implement request-side effect proof obligations only after the medium and validator gates pass.
- [ ] `POST-008 DEFER` Implement semantic graph diff only after immutable semantic revisions exist.
- [ ] `POST-009 DEFER` Enable learned routing only after fixed-policy telemetry and shadow calibration pass.
- [ ] `POST-010 DEFER` Enable advisory patterns only after clean-room evaluation and lineage independence.
- [ ] `POST-011 DEFER` Promote mechanical rules only through replay, false-positive, expiry, override, and demotion governance.
- [ ] `POST-012 DEFER` Add ANN/vector infrastructure only if SQLite brute-force retrieval becomes a measured bottleneck.
- [ ] `POST-013 DEFER` Add multi-agent orchestration only after the single-agent baseline exposes a measured bottleneck.
- [ ] `POST-014 DEFER` Add hosted sync, teams, enterprise identity, policy administration, or audit export only after hobbyist product evidence.
- [ ] `POST-015 DEFER` Add direct graph editing only after user studies show conversational revisions are insufficient.

## Completion Record

When a milestone gate is completed, append:

```text
Completed:
Source revision:
Database schema version:
Frontend version:
Test/benchmark command:
Result location:
Known limitations:
Decision owner:
```
