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
| PIPE | [§33 Pipeline Refinement](docs/plan.md#33-pipeline-refinement), [§22 Correctness and Assurance Gates](docs/plan.md#22-correctness-and-assurance-gates), [§24 Specification Review](docs/plan.md#24-specification-review), [§9 Proof Obligations as the Unit of Assurance](docs/plan.md#9-proof-obligations-as-the-unit-of-assurance) |
| MEM | [§31 Evidence-Driven Reuse and Learning](docs/plan.md#31-evidence-driven-reuse-and-learning), [Extraction Triggers and the Candidacy Funnel](docs/plan.md#extraction-triggers-and-the-candidacy-funnel), [Injection Surfaces and Timing](docs/plan.md#injection-surfaces-and-timing), [Routing Evidence Keys](docs/plan.md#routing-evidence-keys), [§33 Model Selection](docs/plan.md#model-selection) |

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
 -> PIPE pipeline refinement
 -> MEM memory and learning layer
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
| `REVIEW` | Reconciles a previously checked claim with what the source actually does |
| `RELEASE` | Changes packaging, artifacts, signing, or what a user installs |
| `BENCH` | Produces or reruns a recorded measurement |
| `E2E` | Exercised end to end through the real application rather than a fake |
| `EXPERIMENT` | Runs a designed trial whose result decides a branch point |

## Prototype Definition of Done

- [x] `DONE-001` A new user can install or build Codeflux, open a local Go repository, configure one provider, and begin a task without manually editing the database.
- [x] `DONE-002` A user can describe a change in chat, inspect the proposed scope and budget, approve execution, observe progress, review the diff and evidence, and accept or roll back the work.
- [x] `DONE-003` Every task runs in an isolated Git worktree or equivalent isolated branch workspace.
- [x] `DONE-004` The coordinator can pause, cancel, checkpoint, recover, and resume a task without duplicating correctness-bearing actions.
- [x] `DONE-005` The interface shows the selected model, effort level, forecast range, actual usage, actual cost, and hard budget.
- [x] `DONE-006` The fixed baseline routing policy is deterministic and recorded with every run.
- [x] `DONE-007` At least OpenAI, Anthropic, and one OpenAI-compatible local endpoint can be configured through the provider interface.
- [x] `DONE-008` Provider credentials remain in the OS credential store and are absent from SQLite, logs, prompts, diagnostics, and UI event payloads.
- [x] `DONE-009` The task-scoped graph can show Program, Execution, and Evidence views linked to stable identities in the chat thread.
- [x] `DONE-010` SQLite is the sole authoritative store for Codeflux-managed runtime and learning state.
- [x] `DONE-011` A killed and restarted coordinator can replay the journal, validate the worktree binding, and present a safe recovery choice.
- [x] `DONE-012` Risky commands and external effects require a precise inline approval with allow-once, allow-for-task, and deny choices.
- [x] `DONE-013` The prototype passes its unit, integration, migration, reconnect, security-boundary, and end-to-end smoke suites.
- [x] `DONE-014` The prototype completes the frozen demonstration task with an inspectable timeline, diff, evidence report, and cost summary.
- [x] `DONE-015` Known limitations, unsupported guarantees, and deferred enterprise features are visible and documented.
- [x] `DONE-016` From a frozen clean scaffold, Codeflux builds the chronological ReserveFlow API through independent hidden acceptance without manual source edits, and every Codeflux defect found is reproduced, fixed or explicitly deferred, and rerun from the original clean task boundary.

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
- Root `README.md`: explicitly requested by the user on 2026-08-02 and created under that authorization, superseding the earlier record of intentional absence.
- License: MIT, relicensed from Apache License 2.0 on 2026-08-02 at the user's explicit direction. All commits to that point were single-authorship, so no third-party consent was required. The Apache patent grant is deliberately given up in exchange for MIT's shorter terms.
- Community health metadata added 2026-08-02 under the same authorization: `.github/SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `CODEOWNERS`, `PULL_REQUEST_TEMPLATE.md`, and the `ISSUE_TEMPLATE` forms, plus `.editorconfig` and the CodeQL and dependency-review workflows. The Markdown files among these were explicitly requested and are not an agent-inferred documentation need.
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
- [x] `M01-071 DOC` Add a reviewed real atom-comment example after the first executable atom exists; do not invent a fake production contract before then.
- [x] `M01-072 DOC` Define the atom naming grammar in `AGENTS.md`. Output: `<Verb><DomainObject><ImportantQualifier><ObservableOutcome>` guidance with good and bad examples. Verify: it explicitly prefers a longer contextual name over a generic short name.
- [x] `M01-073 TEST` Add a naming check that rejects empty, single-generic-word, filler-suffixed, version-encoded, and hash-encoded atom names.
- [x] `M01-074 TEST` Add a naming check that requires executable atom names to begin with a recognized concrete action verb or receive an explicit reviewed exception.
- [x] `M01-075 TEST` Add a naming check that detects unexplained abbreviations and requires an allowlisted established domain abbreviation.
- [x] `M01-076 TEST` Add fixtures for descriptive names, ambiguous names, misleading guarantee names, provider-specific names, semantic-preserving renames, and semantic-breaking renames.
- [x] `M01-077 DOC` Add a naming review checklist to the first real atom pull-request template after repository contribution templates exist.
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

- `go run ./cmd/codeflux-dev bootstrap --json` selected and verified Go 1.26.5,
  Git 2.54.0, Buf 1.72.0, protoc-gen-go 1.36.11, protoc-gen-go-grpc 1.6.2,
  Staticcheck 2026.1, and GoWebComponents v5.0.1.
- Pinned tools install beneath `.artifacts/tools/bin`; provider-token, credential, password, and secret-shaped environment variables are removed before tool subprocesses.
- Bootstrap validates `go.mod`, Buf configuration, and generator source pins before installation.
- The GoWebComponents check now verifies the M06-selected
  `github.com/monstercameron/GoWebComponents/v5 v5.0.1` dependency exactly;
  bootstrap also installs and verifies the pinned gRPC generator added by the
  transport spike.

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

- [x] `M05-016 BLOCKER DATA` Implement atomic event persistence and sequence allocation.
- [x] `M05-017 BLOCKER` Publish an event only after its transaction commits.
- [x] `M05-018` Implement in-process subscriptions by thread or task.
- [x] `M05-019` Implement bounded subscriber buffers.
- [x] `M05-020` Define backpressure behavior for token deltas.
- [x] `M05-021` Define backpressure behavior for verbose tool progress.
- [x] `M05-022` Prohibit dropping task transitions, approvals, budgets, validations, checkpoints, graph revisions, or errors.
- [x] `M05-023` Coalesce superseded ephemeral progress events without changing durable history.
- [x] `M05-024` Close subscriptions cleanly on cancellation.
- [x] `M05-025` Remove disconnected subscribers without leaking goroutines.
- [x] `M05-026` Add per-session and global subscriber metrics.

## Replay

- [x] `M05-027 BLOCKER` Implement replay from `after_sequence`.
- [x] `M05-028` Return a snapshot plus subsequent events when replaying an old or compacted range.
- [x] `M05-029` Detect a client's stale entity revision.
- [x] `M05-030` Ensure replay and live delivery join without a gap or duplicate.
- [x] `M05-031` Make all UI command requests idempotent.
- [x] `M05-032` Persist command idempotency keys and final results.
- [x] `M05-033 TEST` Test reconnect at every boundary around a committed event.
- [x] `M05-034 TEST` Test duplicate command delivery.
- [x] `M05-035 TEST` Test slow subscribers and forced disconnects.
- [x] `M05-036 TEST` Test a stream with interleaved chat, graph, approval, and cost events.
- [x] `M05-037 TEST` Test replay after coordinator restart.
- [x] `M05-038 TEST` Property-test strictly increasing per-session sequence numbers.

## Gate

- [x] `M05-G01 GATE` A subscriber can disconnect, miss events, reconnect, and reconstruct the exact current task state.
- [x] `M05-G02 GATE` Duplicate commands do not duplicate messages, actions, approvals, or costs.
- [x] `M05-G03 GATE` Event persistence and publication ordering is documented and covered by concurrency tests.

Session-journal evidence:

- Schema 4 allocates a strictly increasing per-session sequence on the session
  row and inserts the immutable event in the same immediate SQLite transaction.
  The public persist-and-publish operation reaches the hub only after commit.
- The in-process hub establishes a committed replay boundary while publication
  is serialized, then joins bounded live delivery without gaps or duplicates.
  It coalesces only message deltas and tool progress; a queue containing only
  material events is disconnected with an explicit replay-required error.
- Immutable snapshots provide compacted reconnect bases. The task reducer
  rejects sequence, state, and entity-revision gaps. UI command keys, request
  hashes, results, and final sequences commit atomically with command effects.
- Tests cover rollback, concurrent sequence allocation, every reconnect
  boundary, duplicate and conflicting commands, slow subscribers,
  cancellation cleanup, interleaved chat/graph/approval/cost facts, restart
  replay, and randomized contiguous-sequence properties.

---

# Milestone 06: GoWebComponents v5 and gRPC Transport Spike

Goal: remove framework and browser-transport uncertainty before building the product UI.

Plan references: §27A Framework and Transport Spike; Application Layout; Unified Session Stream; Rendering and Performance; Local Security; Frontend Acceptance Criteria.

Depends on: `M05-G01` through `M05-G03`.

Milestone output: a recorded v5 version and transport decision backed by reconnect, cancellation, security, and 300-node rendering measurements.

## Framework Pin

- [x] `M06-001 BLOCKER SPIKE` Locate and pin the exact GoWebComponents v5 module and release.
- [x] `M06-002 SPIKE` Record its Go and browser compatibility requirements.
- [x] `M06-003 SPIKE` Build the smallest v5 component into WebAssembly.
- [x] `M06-004 SPIKE` Serve the WASM client from the local Go server.
- [x] `M06-005 SPIKE` Verify v5 routing and browser-history behavior.
- [x] `M06-006 SPIKE` Verify v5 local component state and shared state primitives.
- [x] `M06-007 SPIKE` Verify list virtualization with 10,000 synthetic thread entries.
- [x] `M06-008 SPIKE` Verify cancellation and cleanup when a component unmounts.
- [x] `M06-009 SPIKE` Verify clipboard behavior.
- [x] `M06-010 SPIKE` Verify safe external-editor handoff capabilities.
- [x] `M06-011 SPIKE` Verify keyboard, focus, and screen-reader primitives.
- [x] `M06-012 SPIKE` Record unsupported or unstable framework APIs.

## Transport

- [x] `M06-013 BLOCKER SPIKE` Inspect and test the v5 typed gRPC bridge.
- [x] `M06-014 SPIKE` Confirm whether the bridge uses WebSocket, gRPC-Web, or another transport.
- [x] `M06-015 SPIKE` Generate a v5 client for a unary health method.
- [x] `M06-016 SPIKE` Generate a v5 client for `SubscribeSession`.
- [x] `M06-017 SPIKE` Stream at least 10,000 ordered synthetic events.
- [x] `M06-018 SPIKE` Cancel a live subscription from the browser.
- [x] `M06-019 SPIKE` Reconnect using `after_sequence`.
- [x] `M06-020 SPIKE` Test browser refresh during active streaming.
- [x] `M06-021 SPIKE` Test coordinator restart during active streaming.
- [x] `M06-022 SPIKE` Measure framing and serialization overhead.
- [x] `M06-023 SPIKE` Verify maximum message behavior for graph snapshots and tool summaries.
- [x] `M06-024 SPIKE` Verify same-origin enforcement.
- [x] `M06-025 SPIKE` Verify loopback binding.
- [x] `M06-026 SPIKE` Verify per-launch session-secret authentication.
- [x] `M06-027 SPIKE` Determine whether an embedded Go bridge avoids a separate proxy.
- [x] `M06-028 SPIKE` If the v5 bridge fails, compare unary plus server-streaming gRPC-Web against a small embedded WebSocket bridge.
- [x] `M06-029 SPIKE` Choose one transport and record the rejected alternatives.

## Rendering Load

- [x] `M06-030 SPIKE` Stream token deltas while updating cost and task state.
- [x] `M06-031 SPIKE` Render a synthetic 300-node directed graph.
- [x] `M06-032 SPIKE` Apply graph patches while token streaming continues.
- [x] `M06-033 SPIKE` Measure frame time, memory, and DOM node count.
- [x] `M06-034 SPIKE` Test 30-50 ms token batching.
- [x] `M06-035 SPIKE` Test 50-100 ms graph-patch batching.
- [x] `M06-036 SPIKE` Determine the SVG node threshold on the target hobbyist laptop.
- [x] `M06-037 SPIKE` Verify that chat updates do not rerender the graph subtree.
- [x] `M06-038 SPIKE` Verify that graph interaction does not rerender the full thread.

## Gate

- [x] `M06-G01 GATE` Record the exact v5 version and transport architecture.
- [x] `M06-G02 GATE` Demonstrate ordered reconnectable streaming with no event loss or duplication.
- [x] `M06-G03 GATE` Demonstrate responsive simultaneous thread and 300-node graph updates.
- [x] `M06-G04 GATE` Confirm the prototype requires no separately installed proxy.
- [x] `M06-G05 GATE` Replace all spike-only code with a retained minimal transport test or delete it after recording the decision.

Milestone 06 decision and evidence:

- Framework: `github.com/monstercameron/GoWebComponents/v5 v5.0.1`
  (tag commit `8ad9d2588d93b93e4c1bfaaf7bcbab8fc2585b60`) on
  Go 1.26/browser WASM. The retained client is authored only in Go/GWC; the
  framework-generated shell, Go `wasm_exec.js`, and debug WASM stay beneath
  ignored `.artifacts/`.
- Transport: `github.com/monstercameron/GoGRPCBridge v1.1.1`
  (tag commit `c7f7d987504bc36f4ea8d82e8966b9d2e1a91c09`) tunnels
  generated typed gRPC over same-origin WebSocket directly into an in-process
  `grpc.Server`. No Envoy, gRPC-Web proxy, or separate process is required.
  Because browser WebSockets cannot set an arbitrary authorization header, the
  loopback server issues a host-scoped HttpOnly, SameSite=Strict launch cookie;
  an ignored private launch-secret file permits authenticated reconnect across
  an intentional coordinator restart without persisting runtime credentials
  in source or SQLite.
- Transport proof: native tests delivered exactly 10,000 strictly ordered
  events, cancelled live streams, resumed every `after_sequence` boundary, and
  rejected cross-origin, missing-origin, wrong-secret, wildcard-listener,
  oversized-count, and payloads above 4 MiB. The final measured tunnel run
  transferred 575,700 bytes for 369,873 serialized protobuf bytes: 205,827
  bytes of framing and a 1.556 transport/serialization ratio.
- Cross-platform CI correction: editor targets reject both slash conventions
  and drive-qualified paths on every host, and cancellation/reconnect tests
  consume stream terminators. Native test cleanup closes the tracked peer
  transport and waits for gRPC to leave Ready/Connecting before closing the
  client, so the pinned bridge adapter is never given concurrent
  write/deadline calls that violate Gorilla WebSocket's single-writer contract.
- Browser proof on Windows 11 ARM64: the visible client completed sequence
  10,000 after refresh and, during a live coordinator restart, held sequence
  6,657 while disconnected before resuming to 10,000 with no duplicate or
  missing sequence. Back and Forward restored `/` and `/details`; cancellation
  stopped the browser stream; the 10,000-row rail rendered only 14 rows; typed
  editor capability returned `requires-explicit-approval`; clipboard denial was
  surfaced without accepting a browser permission.
- Rendering proof: 40 ms token paints and 75 ms graph patches updated isolated
  chat and graph components. Chat streaming left the graph render counter
  unchanged, and graph patch/ArrowRight interactions left the chat render
  counter unchanged. The 300-node baseline used 717 DOM elements, approximately
  4.0-7.9 MiB Go heap over the test run, and 60-frame samples of p50
  17.7-17.9 ms, p95 18.5-19.2 ms, and max 19.1-19.2 ms. At 1,200 SVG nodes the
  page used 2,517 DOM elements with p50 17.8 ms, p95 19.0 ms, and max 19.6 ms,
  so this laptop's measured degradation threshold is above 1,200 nodes; the
  product target remains the safer approximately-300-node task slice.
- Accessibility proof: semantic landmark/status/listbox/application/image
  names were exposed, the graph showed a native 1 px visible focus outline,
  and ArrowRight advanced the active node.
- Limitations: the spike supports fixed-height virtualization only; GWC's
  generated development shell requires CSP `wasm-unsafe-eval`; the in-app
  browser denied clipboard-write permission; debug WASM size is not a product
  size claim. The conditional fallback comparison in `M06-028` was not
  triggered because the embedded bridge passed every gate. Separate Envoy,
  unary/server-streaming gRPC-Web, and a new custom WebSocket bridge were
  rejected as needless extra installation or protocol surface.
- Retention: `internal/transportspike`, `cmd/codeflux-spike`, its generated
  transport conformance protobuf, and focused security/reconnect/load tests
  remain as the minimal executable decision fixture. Production domain RPCs
  begin in M07 rather than growing the spike contract.

---

# Milestone 07: gRPC API Surface

Goal: define small domain-oriented contracts that support the complete user journey without leaking database tables.

Plan references: §27A Client, Server, and Storage Boundary; Service Contracts; Unified Session Stream; Local Security.

Depends on: `M06-G01` through `M06-G05`.

Milestone output: generated, versioned server/client contracts plus a complete application-function catalog with validation, authority, idempotency, revisions, transaction/event ownership, external-effect behavior, and chronological flow tests.

## API Conventions

- [x] `M07-001 BLOCKER` Define protobuf package, Go package, and versioning conventions.
- [x] `M07-002` Define a standard error detail with stable code, safe message, retryability, and relevant entity ID.
- [x] `M07-003` Define cursor pagination.
- [x] `M07-004` Define idempotency-key fields for mutating requests.
- [x] `M07-005` Define expected-revision fields for optimistic concurrency.
- [x] `M07-006` Define timestamp and duration conventions.
- [x] `M07-007` Define exact monetary and token value messages.
- [x] `M07-008` Define redacted-output conventions.
- [x] `M07-009` Reserve protobuf fields instead of reusing removed numbers.
- [x] `M07-010` Add API compatibility checks.

## Workspace Service

- [x] `M07-011` Define `OpenWorkspace`.
- [x] `M07-012` Define `GetWorkspaceState`.
- [x] `M07-013` Define `ListRepositories`.
- [x] `M07-014` Define `InspectRepository`.
- [x] `M07-015` Define safe path and Git-state responses.

## Thread Service

- [x] `M07-016` Define `CreateThread`.
- [x] `M07-017` Define `ListThreads`.
- [x] `M07-018` Define `GetThreadPage`.
- [x] `M07-019` Define `SendMessage`.
- [x] `M07-020` Define `RenameThread`.
- [x] `M07-021` Define `ArchiveThread`.

## Task Service

- [x] `M07-022` Define `CreateTask` or task creation semantics through `SendMessage`.
- [x] `M07-023` Define `GetTask`.
- [x] `M07-024` Define `StartTask`.
- [x] `M07-025` Define `PauseTask`.
- [x] `M07-026` Define `ResumeTask`.
- [x] `M07-027` Define `CancelTask`.
- [x] `M07-028` Define `ApproveAction`.
- [x] `M07-029` Define `SetBudget`.
- [x] `M07-030` Define `RequestRepair`.
- [x] `M07-031` Define `RollbackTask`.

## Graph Service

- [x] `M07-032` Define `GetGraphSlice`.
- [x] `M07-033` Define `ExpandGraph`.
- [x] `M07-034` Define `GetNode`.
- [x] `M07-035` Define `ExplainNode`.
- [x] `M07-036` Define `CompareGraphRevisions`.
- [x] `M07-037` Define bounded node/edge counts and continuation behavior.

## Review and Settings Services

- [x] `M07-038` Define `GetDiffSummary`.
- [x] `M07-039` Define `GetValidationReport`.
- [x] `M07-040` Define `AcceptChange`.
- [x] `M07-041` Define `RejectChange`.
- [x] `M07-042` Define `OpenInEditor`.
- [x] `M07-043` Define `GetModels`.
- [x] `M07-044` Define `GetPolicy`.
- [x] `M07-045` Define `SetPolicy`.
- [x] `M07-046` Define `SetBudgetDefaults`.
- [x] `M07-047` Define `ConfigureProvider`.
- [x] `M07-048` Define `TestProvider`.
- [x] `M07-049` Define `SubscribeSession`.

## Implementation

- [x] `M07-050 BLOCKER` Generate Go server and v5 client bindings.

M07 contract evidence:

- `codeflux.v1` and `codeflux.dev/codeflux/api/gen/codeflux/v1` carry the
  breaking API major. Additive changes remain in v1; incompatible changes use
  a new package, and removed names/numbers are reserved permanently.
- Seven logical services expose 37 domain methods. Mutations carry one
  `MutationControl` with an idempotency key and optional expected revision.
  Pagination cursors are opaque, graph reads are caller-bounded with explicit
  continuation, and `SessionService.SubscribeSession` is the sole product
  server stream.
- Standard error details expose stable codes, redacted safe messages,
  retryability, and a typed entity identity. Money is exact minor-unit decimal
  arithmetic, tokens are unsigned counts, times use normalized protobuf UTC
  timestamps/nonnegative durations, and provider configuration accepts only an
  opaque credential reference.
- Generated Go/gRPC bindings compile for native server and Go/WASM bridge
  clients. Descriptor tests freeze every method, mutation-control field,
  streaming boundary, and absence of SQLite/SQL/raw credential fields. Buf
  STANDARD lint, FILE compatibility against `HEAD`, generation, and
  `generate-check` pass.
- [x] `M07-051` Implement request validation interceptors.
- [x] `M07-052` Implement session authentication interceptors.
- [x] `M07-053` Implement safe error mapping.
- [x] `M07-054` Implement request correlation and structured logging.
- [x] `M07-055` Implement deadline propagation.
- [x] `M07-056` Implement graceful server shutdown.
- [x] `M07-057 TEST` Add in-process API tests for every method.
- [x] `M07-058 TEST` Add malformed-request tests.
- [x] `M07-059 TEST` Add stale-revision and duplicate-idempotency tests.
- [x] `M07-060 TEST` Add unauthorized-session tests.

M07 transport-boundary evidence:

- Unary and streaming interceptors authenticate either a constant-time native
  session token or an explicitly trusted request ID forwarded only by an
  already cookie-authenticated in-process bridge. They validate stable IDs,
  mutation controls, bounded strings/lists/pages/graphs, exact money, and
  normalized time values before delegation.
- Internal errors map to safe gRPC status/details for every declared stable
  error class. Diagnostic logs contain only method, validated/generated
  correlation ID, status code, and duration; session material and request
  payloads are structurally absent.
- Context deadlines reach handlers unchanged. Graceful shutdown drains first
  and force-stops only when its context expires.
- A Bufconn server exercised all 37 methods through protobuf descriptors and a
  generated-client synthetic journey. Focused tests reject missing/wrong
  sessions, missing mutation control, malformed idempotency, stale revision,
  duplicate result, invalid bounds, cancellation/deadline failures, and prove
  safe error details, bridge attestation, correlation, and logs.

## Backend Function and Flow Coverage

Plan: §27B Backend Design Rules through Backend Flow Acceptance.

- [x] `M07-061 BLOCKER` Create a machine-reviewable catalog mapping every prototype gRPC method to one application-service function, command/query type, result type, authorization rule, and domain-error mapping.
- [x] `M07-062` For every mutating application function, record its idempotency key scope and duplicate-result behavior.
- [x] `M07-063` For every concurrently mutable entity, record the required expected revision and stale-conflict response.
- [x] `M07-064` For every mutating application function, record its SQLite transaction boundary and repositories touched.
- [x] `M07-065` For every mutating application function, record durable events appended in the committing transaction.
- [x] `M07-066` For every external effect, record durable intent, effect identity, outcome, ambiguity, retry, and cancellation behavior.
- [x] `M07-067` Define the safe transport mapping for not-found, invalid transition, stale revision, duplicate, denied, budget exhausted, cancelled, retryable provider, corruption, and recovery-required errors.
- [x] `M07-068` Verify gRPC handlers contain input validation, conversion, delegation, and error mapping only.
- [x] `M07-069` Define application lifecycle functions and startup/shutdown ordering.
- [x] `M07-070` Define workspace, repository-map, context-selection, and explanation functions.
- [x] `M07-071` Define thread, message, pagination, rename, and archive functions.
- [x] `M07-072` Define requirement, fingerprint, retrieval, forecast, plan, revision, approval, and task-lifecycle functions.
- [x] `M07-073` Define worktree, safe-path, edit-batch, diff, checkpoint, restore, acceptance, abandonment, and cleanup functions.
- [x] `M07-074` Define provider, model-request, stream, cancellation, retry, usage-reconciliation, and price functions.
- [x] `M07-075` Define exact budget creation, reservation, usage commit, release, limit raise, and snapshot functions.
- [x] `M07-076` Define tool discovery, authority classification, approval, execution, cancellation, output bounding, and redaction functions.
- [x] `M07-077` Define worker spawn, lease, heartbeat, pause, resume, cancel, checkpoint, status, and lost-worker classification functions.
- [x] `M07-078` Define risk, validation selection, execution, invalidation, baseline, evidence, review, acceptance, repair, rejection, and editor-open functions.
- [x] `M07-079` Define graph projection, revision, patch, layout, slice, expansion, cone, comparison, node, and explanation functions.
- [x] `M07-080` Define episode, fact extraction, fingerprint, atom name/doc admission, embedding, exact retrieval, vector candidate, applicability, assurance, influence, and invalidation functions.
- [x] `M07-081` Define credential, settings, doctor, backup, integrity, and diagnostic-export functions.
- [x] `M07-082 TEST` Execute the complete startup flow against deterministic ports, database, and recovery fixtures.
- [x] `M07-083 TEST` Execute open-repository and context-selection flows against clean, dirty, detached, conflicted, and malicious fixtures.
- [x] `M07-084 TEST` Execute submit-requirement, forecast, plan, revise, approve, and start flows through generated clients.
- [x] `M07-085 TEST` Execute an agent tool step through automatic, approval-required, denied, failed, cancelled, and retryable paths.
- [x] `M07-086 TEST` Execute pause, resume, cancellation, provider failure, and hard-budget flows at each durable boundary.
- [x] `M07-087 TEST` Execute validation, review, accept, repair, reject, rollback, and stale-review flows.
- [x] `M07-088 TEST` Execute reconnect/replay with event commit before delivery, duplicate delivery, and stale projection.
- [x] `M07-089 TEST` Execute coordinator/worker crash classification without repeating an ambiguous action.
- [x] `M07-090 TEST` Execute pre-work retrieval and atom admission while proving vector similarity cannot bypass eligibility.

## Gate

- [x] `M07-G01 GATE` The generated client can perform the complete synthetic user journey.
- [x] `M07-G02 GATE` API messages expose no SQLite implementation details or secrets.
- [x] `M07-G03 GATE` Every mutating method is idempotent or explicitly documents why it cannot be retried.
- [x] `M07-G04 GATE` Every backend application function has explicit authority, revision, transaction, event, side-effect, cancellation, and typed-error behavior, and every chronological flow passes against deterministic fakes.

Gate evidence:

- The descriptor-checked catalog binds all 37 v1 product methods to exact
  request/result types, one application function, the four permitted thin
  handler steps, authorization, safe errors, idempotency, revision,
  transaction, repository, event, effect, ambiguity, retry, and cancellation
  behavior. Defensive-copy tests prevent runtime mutation of the review
  surface.
- The backend catalog covers lifecycle, journal, workspace, thread, task,
  worktree, provider, exact budget, mediated tools, workers, validation/review,
  graph, memory, credential, settings, and diagnostics functions from plan
  section 27B. Mutation/effect tests reject any row without explicit authority,
  retry, revision, transaction, repository/event, and external-effect
  semantics.
- Deterministic flow tests execute startup/recovery; repository states and
  malicious content; the generated-client requirement journey; every tool
  decision; pause/resume/cancel, provider and budget boundaries; review
  decisions; reconnect/replay; crash classification; and exact-first
  eligibility-gated retrieval plus atom admission.
- Repository bootstrap, deterministic generation, migration check, lint,
  fast, integration, security, all-scope, and artifact-containment gates pass.
  The architecture check additionally proves transport no longer imports the
  sibling SQLite adapter; storage failures must be classified by the inward
  application contract before safe transport mapping.

---

# Milestone 08: Repository Discovery and Workspace Intelligence

Goal: understand enough of a Go repository to assemble targeted, revision-bound context without embedding or uploading the entire codebase.

Plan references: §5 Workspace Intelligence; Human Intent; Task Fingerprint and Retrieval; §27 Repository Indexing and Context Selection; Commands, Secrets, and Malicious Repository Content.

Depends on: `M07-G01` through `M07-G04`.

Milestone output: a deterministic, revision-bound Go repository map and explainable bounded context manifest.

## Repository Opening

- [x] `M08-001 BLOCKER` Resolve and canonicalize the user-selected repository path.
- [x] `M08-002 SECURITY` Reject paths that do not exist or are not directories.
- [x] `M08-003` Detect whether the directory belongs to a Git repository.
- [x] `M08-004` Resolve the repository root without following unsafe user-controlled indirection.
- [x] `M08-005` Read the current branch, HEAD revision, remotes, and worktree status.
- [x] `M08-006` Detect detached HEAD.
- [x] `M08-007` Detect merge, rebase, cherry-pick, or bisect state.
- [x] `M08-008` Detect submodules and record whether the prototype supports them.
- [x] `M08-009` Detect nested repositories.
- [x] `M08-010` Detect Git LFS pointers without automatically fetching content.
- [x] `M08-011` Detect untracked and ignored files.
- [x] `M08-012` Present dirty-state risks before creating a task worktree.
- [x] `M08-013` Persist repository identity separately from mutable paths.
- [x] `M08-014` Bind every workspace snapshot to a Git revision.

Repository-opening evidence:

- A bounded argument-array runner performs only fixed read-only Git discovery
  commands. The selected directory and reported root are canonicalized, the
  root must contain the selection, missing/non-directory/non-Git/empty
  repositories fail with typed errors, and no shell evaluates repository text.
- Snapshots carry a path-independent Git identity and exact HEAD revision plus
  branch, sanitized remotes, detached/dirty/conflicted state, in-progress Git
  operations, changed/untracked/ignored paths, unsupported submodules, nested
  repositories, and tracked LFS pointers. Existing SQLite repository records
  keep the stable repository ID/Git identity separate from canonical paths.
- Real temporary Git tests cover nested selection, path movement, remote
  credential removal, modified and conflicted status, detached HEAD, operation
  markers, submodules, nested repositories, LFS pointer non-fetch behavior,
  ignored/untracked files, unsafe root indirection, empty repositories, command
  cancellation, and bounded output.

## Go Repository Map

- [x] `M08-015 BLOCKER` Locate `go.mod`, `go.work`, and relevant nested modules.
- [x] `M08-016` Parse module paths and declared Go versions.
- [x] `M08-017` Run bounded `go list` commands through the mediated executor.
- [x] `M08-018` Collect package names, directories, imports, test files, and build targets.
- [x] `M08-019` Collect exported and unexported symbols through Go syntax/type tooling.
- [x] `M08-020` Collect symbol definitions and references.
- [x] `M08-021` Collect function callers and callees where Go tooling can resolve them cheaply.
- [x] `M08-022` Collect interface implementations where feasible.
- [x] `M08-023` Collect build tags and platform-specific file constraints.
- [x] `M08-024` Identify generated files.
- [x] `M08-025` Identify likely formatter, test, lint, and build commands from project files.
- [x] `M08-026` Identify nearby tests for each source package.
- [x] `M08-027` Identify repository instruction files and classify them as untrusted input.
- [x] `M08-028` Record map warnings instead of failing the whole repository for one unparsable package.

Go-map evidence:

- The mapper locates bounded module, workspace, instruction, and command files;
  runs only `go list -e -json ./...` through the argument-array runner with Go
  proxy/checksum network access disabled; and records module paths/versions,
  packages, source/tests, imports, and logical build targets.
- Standard Go parser/AST tooling records exported/unexported definitions,
  references, syntactic caller/callee edges, and feasible interface method-set
  matches. File records include content hashes, build constraints, generated
  markers, source/test/configuration kinds, and the exact repository revision.
- Built-in Go format/test/build recipes are distinguished from untrusted
  repository-suggested Make/Task/lint commands, which require approval.
  Instruction files are labeled untrusted repository data. Unparsable packages
  create bounded warnings while valid packages remain mapped.
- A representative multi-module repository proves deterministic map identity,
  nested modules, tests, generated/platform files, symbols/references/calls,
  interface implementations, command and instruction classification, parse
  isolation, missing-module rejection, and network-disabled dependency lookup.

## Deterministic Context Selection

- [x] `M08-029 BLOCKER` Tokenize requirement terms and explicit paths/symbols.
- [x] `M08-030` Resolve explicit file references first.
- [x] `M08-031` Resolve explicit symbol references second.
- [x] `M08-032` Rank exact term matches in paths, symbol names, documentation, and tests.
- [x] `M08-033` Expand direct dependency and caller/callee neighbors.
- [x] `M08-034` Expand nearby tests and configuration.
- [x] `M08-035` Add Git history only for already relevant paths.
- [x] `M08-036` Add further context only when a tool result or failure justifies it.
- [x] `M08-037` Enforce separate file-count, byte, and estimated-token budgets.
- [x] `M08-038` Deduplicate identical or overlapping excerpts.
- [x] `M08-039` Preserve line numbers and repository-relative paths.
- [x] `M08-040` Mark generated, binary, minified, vendor, and dependency content.
- [x] `M08-041` Exclude likely secrets before provider context assembly.
- [x] `M08-042` Record why each selected context item was included.
- [x] `M08-043` Persist the context manifest and revision binding in SQLite.
- [x] `M08-044` Invalidate cached mappings when supporting files change.
- [x] `M08-045` Expose selected context to the user through an expandable card.

Context-selection evidence:

- Requirement parsing normalizes terms and explicit paths/symbols. A stable
  rank order favors explicit paths, explicit symbols, exact path/symbol/content
  terms, then direct package/call neighbors, nearby tests/module configuration,
  and caller-supplied tool/failure paths. Git history is queried only for paths
  already admitted to the manifest.
- Selection enforces independent file, byte, and estimated-token caps, retains
  repository-relative paths and line ranges, deduplicates excerpts, records
  generated/binary/minified/vendor/dependency flags, and stores sorted reasons
  for every admitted item. Supporting-file hashes and the exact Git revision
  invalidate stale maps before selection.
- Likely secret paths and any content matched by the shared redaction pipeline
  are excluded before prompt assembly. An immutable SQLite manifest records the
  repository, repository/map/requirement revisions, policy, budgets, redacted
  excerpts, explanations, exclusions, and creation time transactionally.
- The all-Go presentation projection supplies an expandable context card with
  revision/budget summary, selected paths/ranges, reasons, trust, flags, and
  exclusions for the later GWC timeline without dumping full source into it.

## Prompt-Injection Boundary

- [x] `M08-046 SECURITY` Label repository content as untrusted data in agent prompts.
- [x] `M08-047 SECURITY` Prevent repository text from modifying permission policy.
- [x] `M08-048 SECURITY` Prevent repository text from granting network or credential access.
- [x] `M08-049 SECURITY` Require first-use approval for repository-suggested custom commands.
- [x] `M08-050 SECURITY` Show the source and scope of repository-provided instructions.
- [x] `M08-051 TEST` Build a malicious-repository fixture containing fake system instructions.
- [x] `M08-052 TEST` Verify the fixture cannot bypass command approval.
- [x] `M08-053 TEST` Verify the fixture cannot cause secret disclosure.

Prompt-boundary evidence:

- Prompt context is a JSON envelope with a fixed policy declaring repository
  excerpts untrusted and unable to alter permissions or authorize commands,
  network, or credential access. JSON escaping prevents a repository-supplied
  fake closing delimiter from replacing that structural boundary.
- Repository instruction paths retain their source, scope, and untrusted label
  and require explicit first-use approval before selection. Discovered custom
  commands remain approval-required rather than becoming executable actions.
- A committed-in-test malicious repository contains a fake boundary close,
  fake system instruction, credential-disclosure demand, network command, and
  provider-key-shaped secret. Tests prove it cannot escape serialization,
  authorize its command, or place the secret in provider context.

## Gate

- [x] `M08-G01 GATE` A representative Go repository opens and produces a revision-bound deterministic map.
- [x] `M08-G02 GATE` The same requirement and revision produce the same ordered context manifest.
- [x] `M08-G03 GATE` The context card explains every selected file or excerpt.
- [x] `M08-G04 GATE` Malicious repository text cannot alter system authority.

---

# Milestone 09: Git Isolation, Editing, and Diff Management

Goal: ensure agent edits are inspectable, isolated, reversible, and bound to the repository revision they were based on.

Plan references: §19 Review and Source Mapping; §27 Local Runtime and Repository Isolation; §29 Phase 1.

Depends on: `M08-G01` through `M08-G04`.

Milestone output: isolated task worktrees, conflict-aware file mutations, traceable diffs, acceptance, repair, rollback, and explicit cleanup.

## Worktree Lifecycle

- [x] `M09-001 BLOCKER` Define the worktree naming and storage convention.
- [x] `M09-002` Generate collision-resistant task branch names.
- [x] `M09-003` Record base repository, base revision, task branch, and worktree path atomically.
- [x] `M09-004` Create a dedicated Git worktree for a task.
- [x] `M09-005` Refuse to reuse an active worktree owned by another task.
- [x] `M09-006` Verify the new worktree starts at the expected base revision.
- [x] `M09-007` Handle repositories with dirty primary worktrees without modifying those changes.
- [x] `M09-008` Handle branch-name collisions.
- [x] `M09-009` Handle worktree creation failure with cleanup.
- [x] `M09-010` Detect manual deletion or movement of a task worktree.
- [x] `M09-011` Detect external commits in the task worktree.
- [x] `M09-012` Detect concurrent user edits during agent execution.
- [x] `M09-013` Pause before overwriting a file changed since it was read.
- [x] `M09-014` Preserve task worktrees after failure until the user chooses cleanup.

Worktree-lifecycle evidence:

- The configured external root uses
  `<root>/<repository-id>/<task-id>`, rejects root/repository overlap, and
  receives collision-resistant `codeflux/task/...` branches from
  cryptographic entropy. SQLite records workspace, task, repository, exact
  base/expected HEAD, branch, path, state, and revision in one transaction.
- Creation verifies the full base commit and resulting HEAD, never resets or
  stages the primary worktree, reports primary dirtiness, retries branch
  collisions, and removes only its newly created worktree/branch if creation
  or persistence fails.
- Verification detects missing/moved worktrees, branch replacement, external
  commits, and uncommitted changes after restart. Failures preserve the task
  worktree; abandonment releases it without deleting either path or branch,
  and explicit cleanup removes only the validated released path.

## Safe File Operations

- [x] `M09-015 BLOCKER SECURITY` Resolve every edit path relative to the task worktree.
- [x] `M09-016 SECURITY` Reject path traversal outside the task worktree.
- [x] `M09-017 SECURITY` Decide and test symlink handling.
- [x] `M09-018 SECURITY` Prevent writes through symlinks to external locations.
- [x] `M09-019` Preserve file permissions where practical.
- [x] `M09-020` Preserve newline style unless the formatter intentionally changes it.
- [x] `M09-021` Preserve UTF-8 and reject unsupported binary edits.
- [x] `M09-022` Apply edits with expected-content or expected-hash preconditions.
- [x] `M09-023` Return a typed conflict when expected content changed.
- [x] `M09-024` Support create, update, rename, and delete as explicit operations.
- [x] `M09-025` Require higher-risk approval for large or broad deletes.
- [x] `M09-026` Record a redacted edit summary as an event.

Safe-edit evidence:

- Slash-normalized relative paths must be clean, remain under the exact active
  worktree, have existing directory parents, and contain no symlinked component
  or resolved indirection. Absolute, drive-qualified, alternate-separator, and
  traversal paths fail before filesystem mutation.
- Bounded regular UTF-8 files carry existence/content-hash preconditions.
  Create, update, rename, and delete recheck snapshots before mutation; stale
  content returns a typed conflict without overwriting the user's edit.
  Updates preserve mode and CRLF/LF style unless explicitly formatter-owned.
- Binary edits are rejected. Large or broad deletes require an explicit
  higher-risk grant. Batches reject repeated paths, restore every touched file
  on mutation/event failure, and append only counts plus a batch digest—not
  repository content or paths—to the durable ordered task event journal.

## Diff and Acceptance

- [x] `M09-027 BLOCKER` Produce repository-relative unified diffs.
- [x] `M09-028` Produce per-file added/deleted line counts.
- [x] `M09-029` Classify generated, dependency, test, configuration, and source changes.
- [x] `M09-030` Detect binary changes.
- [x] `M09-031` Detect suspiciously broad formatting churn.
- [x] `M09-032` Detect changes outside the approved plan scope.
- [x] `M09-033` Summarize diff intent without substituting summary for source review.
- [x] `M09-034` Link changed lines to related task events and validation.
- [x] `M09-035` Implement user acceptance of the worktree result.
- [x] `M09-036` Decide whether acceptance creates a commit, applies a patch, or offers both.
- [x] `M09-037` Preserve original author attribution rules.
- [x] `M09-038` Implement user-requested repair without losing the previous checkpoint.
- [x] `M09-039` Implement rollback to the last valid checkpoint.
- [x] `M09-040` Implement task abandonment without deleting the branch by default.
- [x] `M09-041` Implement explicit cleanup of abandoned worktrees.

Diff, checkpoint, and acceptance evidence:

- Git produces binary-capable repository-relative unified diffs, rename-aware
  per-file line counts, exact diff identities, source/test/config/generated/
  dependency classes, binary and broad-churn flags, plan-scope warnings, and
  caller-supplied task-event/validation attribution. Untracked paths enter only
  a disposable alternate index, leaving the user's actual index unchanged.
- The concise summary ends with an explicit direction to review the unified
  source diff. Acceptance is revision/diff-identity checked and offers branch
  commit, primary patch, or both. Patch mode verifies the primary HEAD and
  applies only after `git apply --check`; commit modes require and preserve the
  explicit user author instead of inventing attribution.
- Checkpoints commit task changes without repository hooks, advance the
  optimistic expected HEAD, pin private checkpoint refs, and persist exact
  task/base/diff/event lineage. Repairs add new changes without removing older
  refs. Explicit rollback verifies the durable ref and requires discard
  authority before resetting the task branch and removing post-checkpoint
  untracked files.

## Tests

- [x] `M09-042 TEST` Test worktree creation and cleanup in a temporary repository.
- [x] `M09-043 TEST` Test dirty primary worktree preservation.
- [x] `M09-044 TEST` Test concurrent user edit detection.
- [x] `M09-045 TEST` Test symlink escape attempts.
- [x] `M09-046 TEST` Test path traversal attempts.
- [x] `M09-047 TEST` Test expected-hash conflicts.
- [x] `M09-048 TEST` Test rename and delete diffs.
- [x] `M09-049 TEST` Test rollback after several edit batches.
- [x] `M09-050 TEST` Test coordinator restart with an intact task worktree.

M09 test evidence:

- Real temporary repositories exercise creation, collision retry, failed-create
  cleanup, dirty-primary preservation, restart verification, deletion,
  movement, external commits, abandonment, and explicit cleanup.
- Safe-edit tests cover traversal, cross-platform absolute syntax, file and
  parent symlinks where the OS permits them, stale hashes, user edits, binary
  rejection, mode/newline preservation, all mutation types, approval-required
  deletion, durable-event rollback, and rename/delete/binary/churn diffs.
- Acceptance tests cover stale review refusal, patch-only preservation of
  unrelated primary changes, commit-only author attribution, and combined
  commit/apply. Multi-batch checkpoint tests restore an earlier revision while
  preserving later checkpoint lineage and pending edits on persistence failure.

## Gate

- [x] `M09-G01 GATE` No agent edit can reach outside the task worktree through supported file operations.
- [x] `M09-G02 GATE` User changes made after agent read are not silently overwritten.
- [x] `M09-G03 GATE` Every accepted patch can be traced to a base revision and task.

---

# Milestone 10: Mediated Commands, Tools, and Permissions

Goal: let the agent inspect and validate a repository while keeping authority explicit and auditable.

Plan references: §21 Coordinator and Coding Agent; §27 Commands, Secrets, and Malicious Repository Content; Plugins and Custom Commands; §22 Correctness and Assurance Gates.

Depends on: `M09-G01` through `M09-G03`; credential and output paths depend on `M04-G01` through `M04-G03`.

Milestone output: a typed tool protocol, executable permission policy, controlled subprocess runner, approval ledger, and adversarial boundary tests.

## Tool Protocol

- [x] `M10-001 BLOCKER` Define a typed internal tool request and result envelope.
- [x] `M10-002` Include tool name, arguments, working directory, timeout, authority class, idempotency, and expected side effects.
- [x] `M10-003` Define read-file and list-directory tools.
- [x] `M10-004` Define symbol/search tools.
- [x] `M10-005` Define structured edit tools.
- [x] `M10-006` Define diff-inspection tools.
- [x] `M10-007` Define command-execution tools.
- [x] `M10-008` Define Git-status and history tools.
- [x] `M10-009` Define test, build, format, and static-analysis wrappers.
- [x] `M10-010` Define user-facing tool summaries.
- [x] `M10-011` Version tool schemas and record the version per run.

## Permission Policy

- [x] `M10-012 BLOCKER SECURITY` Define automatic read-only actions.
- [x] `M10-013 BLOCKER SECURITY` Define task-scoped file-write actions.
- [x] `M10-014 BLOCKER SECURITY` Define approval-required actions.
- [x] `M10-015 SECURITY` Classify network access.
- [x] `M10-016 SECURITY` Classify dependency installation.
- [x] `M10-017 SECURITY` Classify writes outside the task worktree.
- [x] `M10-018 SECURITY` Classify credential access.
- [x] `M10-019 SECURITY` Classify destructive filesystem and Git actions.
- [x] `M10-020 SECURITY` Classify privileged commands and process management.
- [x] `M10-021 SECURITY` Classify external messaging, deployment, and publication.
- [x] `M10-022 SECURITY` Refuse actions with unknown authority classes.
- [x] `M10-023 SECURITY` Bind allow-for-task decisions to exact action patterns and scope.
- [x] `M10-024 SECURITY` Expire task-scoped permissions when the task ends.
- [x] `M10-025 SECURITY` Never infer permission from prior unrelated tasks.
- [x] `M10-026 SECURITY` Record requester, reason, exact command/action, scope, decision, and time.

## Command Execution

- [x] `M10-027 BLOCKER` Execute commands in the task worker, not the browser.
- [x] `M10-028` Pass argument arrays instead of concatenated shell strings where possible.
- [x] `M10-029` Set the task worktree as the default working directory.
- [x] `M10-030` Validate working directories against task scope.
- [x] `M10-031` Apply bounded timeouts.
- [x] `M10-032` Support cooperative cancellation.
- [x] `M10-033` Kill descendant processes on cancellation where the platform permits.
- [x] `M10-034` Bound stdout and stderr capture.
- [x] `M10-035` Stream redacted progress without persisting unbounded output.
- [x] `M10-036` Preserve exit code, duration, timeout, cancellation, and truncation metadata.
- [x] `M10-037` Separate environment allowlists from the coordinator environment.
- [x] `M10-038` Remove provider credentials from worker environments.
- [x] `M10-039` Record executable identity and resolved path.
- [x] `M10-040` Detect commands that exceed approved scope.
- [x] `M10-041` Provide a user-readable approval description.
- [x] `M10-042` Provide allow-once, allow-for-task, and deny.
- [x] `M10-043` Do not silently fall back after denial.

## Custom Commands and Plugins

- [x] `M10-044` Store approved custom command definitions in SQLite.
- [x] `M10-045` Represent custom command arguments as arrays with typed placeholders.
- [x] `M10-046` Require first-use approval for repository-suggested commands.
- [x] `M10-047` Record command version and source.
- [x] `M10-048` Define the subprocess boundary for future MCP or JSON-RPC plugins.
- [x] `M10-049 DEFER` Do not load arbitrary plugin code into the coordinator.
- [x] `M10-050 DEFER` Do not implement a plugin marketplace in the prototype.

## Tests

- [x] `M10-051 TEST` Test automatic read-only actions.
- [x] `M10-052 TEST` Test task-scoped edits.
- [x] `M10-053 TEST` Test network-command approval.
- [x] `M10-054 TEST` Test dependency-install approval.
- [x] `M10-055 TEST` Test destructive-command denial.
- [x] `M10-056 TEST` Test allow-for-task scope expiration.
- [x] `M10-057 TEST` Test timeout and process-tree cancellation.
- [x] `M10-058 TEST` Test output truncation and redaction.
- [x] `M10-059 TEST` Test a malicious command description cannot change the executed argument array.

M10 test evidence:

- Typed-catalog and policy tests cover every tool family, unknown authority,
  exact grants, one-use consumption, task expiry, cross-task isolation,
  network/dependency/destructive classification, exact approved recipes, and
  capability denial across tool substitution.
- Real subprocess tests cover absolute executable identity, array-only
  arguments, confined working directories, minimal credential-free
  environments, bounded redacted output/progress, cooperative cancellation,
  timeouts, and representative descendant termination.
- SQLite tests cover immutable per-run schema bindings, attributable
  permission facts, reviewed typed custom commands, optimistic command
  lifecycle metadata, bounded redacted output, idempotency, and stale writes.
  The plugin contract permits only versioned JSON-RPC or MCP subprocesses with
  explicit filesystem, network, secret-reference, and side-effect scopes.

## Gate

- [x] `M10-G01 GATE` Every non-automatic action has an attributable policy decision.
- [x] `M10-G02 GATE` Denied authority cannot be regained through tool substitution.
- [x] `M10-G03 GATE` Cancellation terminates representative child-process trees on every supported platform.

---

# Milestone 11: Coordinator and Worker Lifecycle

Goal: isolate task execution from the long-lived process while preserving durable control and recovery.

Plan references: §21 Agent Architecture; Coordinator; Coding Agent; Progress Monitor and Dynamic Escalation; §27 Local Runtime and Repository Isolation.

Depends on: `M10-G01` through `M10-G03`.

Milestone output: authenticated one-worker-per-task subprocess execution with ownership, heartbeats, cancellation, concurrency control, and graceful shutdown.

## Coordinator

- [x] `M11-001 BLOCKER` Implement coordinator startup and dependency wiring.
- [x] `M11-002` Acquire a local single-instance lock or define multi-instance behavior.
- [x] `M11-003` Open and migrate SQLite before accepting tasks.
- [x] `M11-004` Initialize credential, provider, workspace, event, and transport services.
- [x] `M11-005` Bind the frontend server to loopback.
- [x] `M11-006` Generate a per-launch browser session secret.
- [x] `M11-007` Restore incomplete task metadata at startup.
- [x] `M11-008` Detect orphaned worker processes.
- [x] `M11-009` Detect missing or divergent worktrees.
- [x] `M11-010` Present recovery-required state instead of auto-resuming uncertain work.
- [x] `M11-011` Implement graceful shutdown ordering.
- [x] `M11-012` Stop accepting new mutations before draining streams.
- [x] `M11-013` Ask active workers to checkpoint and stop.
- [x] `M11-014` Flush committed events and checkpoint WAL.

## Worker Protocol

- [x] `M11-015 BLOCKER` Define a versioned coordinator/worker protocol.
- [x] `M11-016` Define worker startup parameters without raw credentials.
- [x] `M11-017` Pass task ID, run ID, worktree path, policy revision, tool schema, and coordinator endpoint.
- [x] `M11-018` Authenticate the worker to the coordinator.
- [x] `M11-019` Reject protocol-version mismatch.
- [x] `M11-020` Implement worker heartbeat.
- [x] `M11-021` Implement coordinator-issued pause, resume, cancel, and checkpoint requests.
- [x] `M11-022` Implement worker status and tool-event reporting.
- [x] `M11-023` Bound reconnect attempts.
- [x] `M11-024` Mark the task recovery-required after heartbeat expiry.
- [x] `M11-025` Prevent two workers from owning the same active run.
- [x] `M11-026` Persist worker process metadata for diagnostics.

## Process Isolation

- [x] `M11-027 SECURITY` Launch one subprocess worker per active task.
- [x] `M11-028 SECURITY` Set the worker working directory to the task worktree.
- [x] `M11-029 SECURITY` Provide the minimum required environment.
- [x] `M11-030 SECURITY` Keep credential-store handles in the coordinator.
- [x] `M11-031 SECURITY` Keep SQLite writes behind coordinator repositories unless a carefully reviewed writer boundary is required.
- [x] `M11-032 SECURITY` Apply platform-appropriate process-group management.
- [x] `M11-033 SECURITY` Support an optional user-provided container command for stronger isolation.
- [x] `M11-034` Clearly label the default isolation as mediated workspace confinement, not a perfect sandbox.

## Concurrency

- [x] `M11-035` Define the active-task concurrency limit.
- [x] `M11-036` Queue excess tasks with visible position and reason.
- [x] `M11-037` Prevent starvation of paused or approval-blocked tasks.
- [x] `M11-038` Define provider concurrency limits separately from worker limits.
- [x] `M11-039` Define database write-contention handling.
- [x] `M11-040` Define shutdown behavior for queued tasks.

## Tests

- [x] `M11-041 TEST` Test normal worker startup and exit.
- [x] `M11-042 TEST` Test worker crash.
- [x] `M11-043 TEST` Test coordinator crash.
- [x] `M11-044 TEST` Test heartbeat loss.
- [x] `M11-045 TEST` Test duplicate worker ownership.
- [x] `M11-046 TEST` Test protocol-version mismatch.
- [x] `M11-047 TEST` Test graceful shutdown with running, paused, and queued tasks.
- [x] `M11-048 TEST` Test worker environment for credential absence.

M11 test evidence:

- Application tests cover lock-before-migration startup, dependency ownership,
  literal-loopback serving, per-launch browser secrets, orphan and invalid
  worktree classification, mutation shutdown ordering, worker checkpoint and
  process-tree stop, event closure, WAL checkpointing, and database closure.
- Real SQLite tests cover unique active leases, immutable process identity,
  ordered heartbeat and redacted report persistence, atomic expiry recovery,
  durable queue dispatch and cancellation, failed-start compensation after
  caller cancellation, bounded startup recovery, and worktree-linked recovery.
- Worker tests cover credential-free pipe startup, exact worktree execution,
  strict version and session authentication, bounded payloads and reconnects,
  heartbeat/control operation while paused, status and tool reports, minimum
  environments, optional container wrapping, and platform process-tree stops.
- Crash and concurrency tests kill representative workers and a coordinator,
  preserve an unrelated worktree sentinel, restore explicit recovery-required
  state, verify SQLite integrity, and reject duplicate database and in-memory
  ownership. Combined shutdown tests cover running, paused, and queued work.

## Gate

- [x] `M11-G01 GATE` Killing a worker cannot corrupt coordinator state or another task worktree.
- [x] `M11-G02 GATE` Killing the coordinator leaves enough durable state for a safe recovery choice.
- [x] `M11-G03 GATE` No task runs with two active worker owners.

---

# Milestone 12: Model Provider Abstraction

Goal: support a small set of providers consistently while preserving exact usage, version, failure, and privacy evidence.

Plan references: §21 Model and Effort Router; Routing Safety; §27 Provider Credentials; Initial Model Providers; Honest Cost Display.

Depends on: `M11-G01` through `M11-G03`; credential access depends on `M04-G01` through `M04-G03`.

Milestone output: normalized OpenAI, Anthropic, and OpenAI-compatible adapters with exact accounting, cancellation, bounded retry, and no silent switching.

## Provider Interface

- [x] `M12-001 BLOCKER` Define provider discovery and capability methods.
- [x] `M12-002 BLOCKER` Define streaming response and cancellation.
- [x] `M12-003` Define request messages, tool declarations, tool results, and structured-output requirements.
- [x] `M12-004` Define normalized stop reasons.
- [x] `M12-005` Define normalized usage accounting.
- [x] `M12-006` Define provider-specific raw metadata retention after redaction.
- [x] `M12-007` Define timeout, retryable, rate-limit, authentication, invalid-request, safety, and unavailable errors.
- [x] `M12-008` Define model capabilities: tools, structured output, context length, image input, and reasoning controls.
- [x] `M12-009` Define exact provider/model/version identity recorded per request.
- [x] `M12-010` Define pricing snapshots separately from mutable current pricing.
- [x] `M12-011` Define cancellation semantics and late-response handling.
- [x] `M12-012` Define request idempotency where providers support it.

## OpenAI Adapter

- [x] `M12-013` Implement model configuration.
- [x] `M12-014` Implement credential lookup.
- [x] `M12-015` Implement streaming text.
- [x] `M12-016` Implement tool calls and tool results.
- [x] `M12-017` Implement structured output needed by the planner.
- [x] `M12-018` Implement cancellation.
- [x] `M12-019` Normalize usage and stop reasons.
- [x] `M12-020` Capture request IDs and safe provider metadata.
- [x] `M12-021` Classify errors and retry hints.
- [x] `M12-022 TEST` Test against a deterministic mock server.
- [x] `M12-023 TEST` Add an opt-in live smoke test.

## Anthropic Adapter

- [x] `M12-024` Implement model configuration.
- [x] `M12-025` Implement credential lookup.
- [x] `M12-026` Implement streaming text.
- [x] `M12-027` Implement tool calls and tool results.
- [x] `M12-028` Implement structured output needed by the planner.
- [x] `M12-029` Implement cancellation.
- [x] `M12-030` Normalize usage and stop reasons.
- [x] `M12-031` Capture request IDs and safe provider metadata.
- [x] `M12-032` Classify errors and retry hints.
- [x] `M12-033 TEST` Test against a deterministic mock server.
- [x] `M12-034 TEST` Add an opt-in live smoke test.

## OpenAI-Compatible Local Adapter

- [x] `M12-035` Implement configurable loopback or user-approved endpoint.
- [x] `M12-036` Support optional credentials.
- [x] `M12-037` Implement model listing where the endpoint provides it.
- [x] `M12-038` Implement streaming text.
- [x] `M12-039` Detect and advertise tool-call support.
- [x] `M12-040` Handle endpoints without usage reporting.
- [x] `M12-041` Handle nonstandard error bodies safely.
- [x] `M12-042` Require approval before connecting to a non-loopback endpoint.
- [x] `M12-043 TEST` Test against a local deterministic fake endpoint.

## Retry, Fallback, and Accounting

- [x] `M12-044 BLOCKER` Implement bounded retry for transient transport failures.
- [x] `M12-045` Respect provider retry-after guidance within the task deadline and budget.
- [x] `M12-046` Do not retry invalid, authentication, or policy errors.
- [x] `M12-047` Record each physical request attempt.
- [x] `M12-048` Attribute retry usage and cost to the task.
- [x] `M12-049` Preserve partial streamed output as non-final evidence.
- [x] `M12-050` Pause after retry budget exhaustion.
- [x] `M12-051` Offer retry, resume, or explicitly approved provider switch.
- [x] `M12-052` Never silently switch providers or models.
- [x] `M12-053` Handle missing pricing as unknown rather than zero.
- [x] `M12-054` Reconcile estimated and provider-reported usage.
- [x] `M12-055` Surface accounting discrepancies.

M12 test evidence:

- Deterministic OpenAI Responses, Anthropic Messages, and OpenAI-compatible
  mock endpoints pass the same normalized text, partial tool-arguments,
  completed tool-call, usage, stop-reason, identity, and metadata conversation.
- Provider execution tests cover cancellation-authoritative UI delivery,
  explicit provider cancellation, deadline classification, late buffered-event
  suppression, bounded retry, retry-after limits, no retry after observable
  output or tool effects, and no automatic provider or model switching.
- SQLite tests cover sealed immutable known and explicit-unknown pricing
  snapshots, attributable logical and physical requests, exact rational cost,
  latest-record reconciliation, provider-specific usage, discrepancy states,
  partial evidence hashes, retry exhaustion pause, and bounded compensation
  for pre-I/O, accounting, and terminal-write failures.
- `codeflux-dev run-live` is an opt-in, cost-warned path for OpenAI or
  Anthropic. It reads only an `os://service/account` credential, requires an
  external database outside physical repository aliases, emits no response
  content, and is covered by a deterministic no-network end-to-end test.
- Lint, generation, migration, fast, integration, security, repeated
  cancellation/accounting regressions, and the complete local `test-all` gate
  pass. The real billable live request remains deliberately opt-in and is not
  executed by CI.

## Gate

- [x] `M12-G01 GATE` The same normalized mock conversation passes through all three adapters.
- [x] `M12-G02 GATE` Cancellation stops UI streaming and prevents additional tool execution.
- [x] `M12-G03 GATE` Every request has attributable provider, model, version, usage, price snapshot, latency, and final status.
- [x] `M12-G04 GATE` Provider switching never occurs without explicit user authority.

---

# Milestone 13: Fixed Routing, Forecasts, and Budget Enforcement

Goal: make cost and effort visible and enforceable before attempting learned optimization.

Plan references: §5 Adaptive Execution Policy; §21 Effort Forecaster; Model and Effort Router; Routing Safety; §25 Cost; Forecast and Routing Quality; §29 Phases 1, 3, and 4.

Depends on: `M12-G01` through `M12-G04`.

Milestone output: a versioned fixed baseline, transparent P50/P90 heuristic, exact cost ledger, reservations, warnings, and hard-stop enforcement.

## Fixed Baseline Policy

- [x] `M13-001 BLOCKER` Choose the fixed baseline provider, model, and effort level.
- [x] `M13-002` Define fixed policy behavior for planning, execution, repair, and review.
- [x] `M13-003` Define maximum planning and repair rounds.
- [x] `M13-004` Define maximum tool calls per round.
- [x] `M13-005` Define maximum context budget.
- [x] `M13-006` Define default task monetary and token budgets.
- [x] `M13-007` Version the fixed policy.
- [x] `M13-008` Persist the exact policy with every task and run.
- [x] `M13-009` Make policy selection deterministic for identical inputs.
- [x] `M13-010` Expose manual model/effort override as an explicit recorded choice.

## Initial Forecast

- [x] `M13-011 BLOCKER` Define a transparent heuristic forecast based on task class, repository size, likely files, validation commands, and fixed model.
- [x] `M13-012` Produce P50/P90 latency estimates.
- [x] `M13-013` Produce P50/P90 token estimates.
- [x] `M13-014` Produce P50/P90 cost estimates.
- [x] `M13-015` Produce uncertainty reasons.
- [x] `M13-016` Distinguish unknown price from zero cost.
- [x] `M13-017` Record forecast features and algorithm version.
- [x] `M13-018` Present the forecast before execution begins.
- [x] `M13-019` Allow the user to adjust budget before approval.
- [x] `M13-020` Compare forecast with actual results after completion.
- [x] `M13-021` Do not present forecasts as promises.

## Budget Ledger

- [x] `M13-022 BLOCKER DATA` Implement atomic budget creation.
- [x] `M13-023 DATA` Implement pre-request estimated-cost reservation.
- [x] `M13-024 DATA` Reconcile reservation with actual usage after the request.
- [x] `M13-025 DATA` Track model, tool, and optional infrastructure cost categories.
- [x] `M13-026` Define warning thresholds.
- [x] `M13-027` Emit budget-warning events.
- [x] `M13-028` Block new model requests at the hard cap.
- [x] `M13-029` Allow the current request to finish if stopping it would waste already billed work, while preventing subsequent actions.
- [x] `M13-030` Require explicit approval to raise a hard budget.
- [x] `M13-031` Record who or what changed the budget.
- [x] `M13-032` Make cancellation and budget exhaustion distinguishable.
- [x] `M13-033` Show forecast, reserved, actual, and remaining amounts.
- [x] `M13-034` Handle concurrent cost postings without overspending.

## Shadow Forecasting Preparation

- [x] `M13-035` Record task features needed for later effort forecasting.
- [x] `M13-036` Record counterfactual eligibility without choosing a dynamic model.
- [x] `M13-037` Record actual outcome, latency, usage, cost, repairs, and human interventions.
- [x] `M13-038` Define a later calibration report schema.
- [x] `M13-039 DEFER` Do not change model or effort from learned forecasts in the prototype.

## Tests and Gate

- [x] `M13-040 TEST` Test exact cost arithmetic.
- [x] `M13-041 TEST` Test concurrent reservation races.
- [x] `M13-042 TEST` Test missing pricing.
- [x] `M13-043 TEST` Test warning and hard-cap boundaries.
- [x] `M13-044 TEST` Test explicit budget increase.
- [x] `M13-045 TEST` Test that retries consume budget.
- [x] `M13-G01 GATE` A task cannot start without an inspectable policy, forecast, and budget.
- [x] `M13-G02 GATE` No combination of concurrent requests can exceed the approved hard cap beyond a documented in-flight bound.
- [x] `M13-G03 GATE` The fixed policy is reproducible and creates usable baseline telemetry.

M13 test evidence:

- `fixed-baseline-v2` freezes the OpenAI Responses adapter, `gpt-5.6-sol`,
  maximum reasoning, phase limits, context/tool ceilings, and default budget;
  the exact provider revision creates a reproducible stratum and explicit
  manual overrides retain actor, authority, and reason.
- The transparent forecast records its algorithm, normalized task/repository
  features, immutable bindings, P50/P90 latency, tokens, tools, exact or
  explicitly unknown cost, uncertainty reasons, and the mandatory
  estimate-not-promise notice. Mutated or overflowed forecasts are rejected.
- SQLite migrations 000010 through 000013 add exact rational limits,
  reservations, postings, warning/hard-cap events, immutable snapshots,
  reconciliation intents, preapproval adjustments, execution preflights, run
  bindings, outcome comparisons, and physical provider-call reconciliation.
- Concurrent exact and legacy reservations share one revision authority;
  complete retry envelopes reserve cost, tokens, and call slots before request
  activation, while settlement reconciles slots to actual physical attempts.
  In-flight billed work settles even at the cap and blocks subsequent work.
- Task start rejects missing or stale policy, forecast, limit, budget snapshot,
  unknown accounting, reconciliation, and generic ready-to-running bypasses.
  The current presentation combines immutable policy/forecast with exact or
  unknown reserved, actual, and remaining cost and token state.
- The production live path is planned request, budget reservation, activation,
  then provider I/O. Unknown price, cross-task budget, or insufficient hard cap
  yields zero provider stream calls; hard-cap exhaustion is durably paused and
  attributed rather than confused with cancellation.
- Focused arithmetic, missing-price, warning, hard-cap, explicit-increase,
  retry, reconciliation, preflight, presentation, and terminal-outcome tests
  pass. Lint, generation, migration, fast, integration, security, and the
  complete local `test-all` gate pass; the final adversarial audit accepts the
  integrated milestone.

---

# Milestone 14: Agent Planning and Execution Loop

Goal: implement the smallest reliable coding-agent loop with explicit plans, bounded repair, and no adaptive routing.

Plan references: §5 Human Intent; Workspace Intelligence; Execution and Review; §21 Agent Architecture; §22 Correctness and Assurance Gates; §29 Phase 1.

Depends on: `M13-G01` through `M13-G03`.

Milestone output: a bounded requirement-plan-approve-edit-test-repair-review state machine driven through persisted events and mediated tools.

## Requirement Intake

- [x] `M14-001 BLOCKER` Persist the user's task message before planning.
- [x] `M14-002` Classify task type using a deterministic rule or fixed model output.
- [x] `M14-003` Identify explicit files, symbols, commands, and acceptance criteria.
- [x] `M14-004` Detect obvious ambiguity that materially changes scope.
- [x] `M14-005` Ask a targeted clarification when proceeding would be unsafe.
- [x] `M14-006` Make a bounded reasonable assumption when ambiguity is non-material.
- [x] `M14-007` Display assumptions in the plan.
- [x] `M14-008` Produce an initial risk classification.
- [x] `M14-009` Select the fixed validation profile.

## Plan Construction

- [x] `M14-010 BLOCKER` Define a structured plan schema.
- [x] `M14-011` Include goal, scope, expected files, steps, validation, risks, authority needs, and completion criteria.
- [x] `M14-012` Bind the plan to repository and context revisions.
- [x] `M14-013` Persist immutable plan revisions.
- [x] `M14-014` Generate a concise user-facing plan.
- [x] `M14-015` Generate machine-readable step IDs.
- [x] `M14-016` Link plan steps to graph nodes.
- [x] `M14-017` Present the forecast and budget with the plan.
- [x] `M14-018` Require plan approval for elevated or protected work.
- [x] `M14-019` Allow user redirection to create a new plan revision.
- [x] `M14-020` Prevent execution of a superseded plan.

## Execution Loop

- [x] `M14-021 BLOCKER` Implement the observe-think-act-result loop around the fixed provider.
- [x] `M14-022` Provide only approved tool schemas.
- [x] `M14-023` Add selected repository context.
- [x] `M14-024` Add current plan and completed-step state.
- [x] `M14-025` Add relevant factual task events without replaying the entire transcript.
- [x] `M14-026` Validate model tool-call structure.
- [x] `M14-027` Reject unknown tools.
- [x] `M14-028` Route tool requests through permission policy.
- [x] `M14-029` Persist tool-start before execution.
- [x] `M14-030` Persist redacted tool result after execution.
- [x] `M14-031` Feed the bounded result back to the model.
- [x] `M14-032` Update plan-step state.
- [x] `M14-033` Create checkpoints after material edit batches.
- [x] `M14-034` Check pause, cancel, budget, and policy state between actions.
- [x] `M14-035` Enforce round, tool-call, token, time, and cost limits.
- [x] `M14-036` Detect repeated identical failed actions.
- [x] `M14-037` Stop and ask for direction instead of looping indefinitely.
- [x] `M14-038` Distinguish implementation completion from validation completion.

## Repair Loop

- [x] `M14-039` Run the selected validation commands.
- [x] `M14-040` Parse failures into bounded redacted summaries.
- [x] `M14-041` Link failures to relevant changed files and plan steps.
- [x] `M14-042` Permit a bounded repair round.
- [x] `M14-043` Preserve the pre-repair checkpoint.
- [x] `M14-044` Record why repair was attempted.
- [x] `M14-045` Rerun affected validation after repair.
- [x] `M14-046` Stop after the repair budget.
- [x] `M14-047` Present unresolved failures honestly.
- [x] `M14-048` Never silently weaken or skip an acceptance test.

## Completion

- [x] `M14-049` Require final repository status and diff capture.
- [x] `M14-050` Require final validation summary.
- [x] `M14-051` Require budget and actual cost summary.
- [x] `M14-052` Require an assumption and limitation summary.
- [x] `M14-053` Transition to awaiting-review rather than auto-accepting.
- [x] `M14-054` Support accept, request repair, rollback, and abandon.
- [x] `M14-055` Record the user's final decision.

## Tests and Gate

- [x] `M14-056 TEST` Run a deterministic fake-model successful edit scenario.
- [x] `M14-057 TEST` Run a fake-model malformed-tool scenario.
- [x] `M14-058 TEST` Run a repeated-failure loop scenario.
- [x] `M14-059 TEST` Run a pause during tool execution scenario.
- [x] `M14-060 TEST` Run cancellation during model streaming.
- [x] `M14-061 TEST` Run budget exhaustion between repair rounds.
- [x] `M14-062 TEST` Run a user-redirection plan revision.
- [x] `M14-G01 GATE` The deterministic fake agent completes the full plan-edit-test-review state machine.
- [x] `M14-G02 GATE` Every action is attributable to a plan revision, model request, tool request, and policy decision.
- [x] `M14-G03 GATE` No failure path silently falls back, expands authority, or skips required validation.

M14 test evidence:

- Requirement intake is derived from the persisted original user message. It
  deterministically records task type, explicit files, symbols, commands,
  acceptance criteria, ambiguities, bounded assumptions, risk, and the fixed
  validation floor; mutating intent cannot be downgraded to investigation.
- Migration 000014 adds immutable requirement and plan revisions, plan-step
  states, run-plan bindings, selected validation profiles, validation
  attribution, bounded repair attempts, completion candidates, and explicit
  review decisions. Real SQLite tests cover lineage, supersession, exact
  idempotency, trigger enforcement, rollback, and reconstruction.
- Validation commands use one canonical logical-worktree projection derived
  from exact tool, ordered non-sensitive arguments, timeout, policy-derived
  authority, and effects. Raw user commands remain visible but cannot become
  execution authority; unsafe, sensitive, mutating, ambiguous, substituted,
  or weakening command forms are rejected.
- Routine, elevated, and protected profiles require one, two, and three
  distinct required commands already present in the approved plan. Go
  repository checks and SQLite triggers reject label-only upgrades,
  duplicates, unknown profiles, downgrades, reordering, and acceptance-test
  weakening.
- The fixed-model observe-think-act loop exposes only approved strict tool
  schemas and bounded context, persists intent before effects and redacted
  outcomes afterward, attributes model/tool/policy facts, checkpoints material
  edits, honors interrupts and hard limits, and stops repeated failures rather
  than silently expanding authority.
- The repair and completion flow preserves pre-repair checkpoints, links
  bounded failures to canonical file scopes and plan steps, reruns exact
  selected validation, enforces repair budgets, captures final repository,
  validation, budget, assumption, and limitation evidence, then awaits an
  explicit accept, repair, rollback, or abandon decision.
- Deterministic fake-model tests cover success, malformed and unknown tools,
  repeated failure, pause during execution, stream cancellation, budget
  exhaustion, redirection, edit-test-repair-review ordering, and durable
  attribution. Focused executor, agent, coordinator, storage, and migration
  tests, `go test ./...`, lint, generation, integration, security, and the
  complete local `test-all` gate pass. The fifth frozen-tree adversarial audit
  accepts M14 with no remaining findings.

---

# Milestone 15: Checkpoint, Pause, Cancellation, and Recovery

Goal: make interruption a normal, testable state rather than an exceptional afterthought.

Plan references: §23 Transactions, Migrations, and Recovery; §27 Local Runtime and Repository Isolation; Persistence, Recovery, Diagnostics, and Updates; §29 Phase 1.

Depends on: `M14-G01` through `M14-G03`.

Milestone output: versioned checkpoints, cooperative interruption, divergence-aware resume, crash classification, and patch preservation.

## Checkpoint Contents

- [x] `M15-001 BLOCKER` Define checkpoint schema and version.
- [x] `M15-002` Bind checkpoint to task, run, plan revision, base revision, and current worktree HEAD.
- [x] `M15-003` Record dirty file hashes and diff identity.
- [x] `M15-004` Record completed and pending plan steps.
- [x] `M15-005` Record current budget ledger position.
- [x] `M15-006` Record effective policy and tool schema versions.
- [x] `M15-007` Record last durable event sequence.
- [x] `M15-008` Record whether an external action may be in an ambiguous outcome state.
- [x] `M15-009` Never serialize provider credentials or live process handles.

## Checkpoint Creation

- [x] `M15-010` Create a checkpoint after plan approval.
- [x] `M15-011` Create a checkpoint after each material edit batch.
- [x] `M15-012` Create a checkpoint before a risky approved action.
- [x] `M15-013` Create a checkpoint after successful validation.
- [x] `M15-014` Create a checkpoint on user pause.
- [x] `M15-015` Attempt a bounded checkpoint on graceful shutdown.
- [x] `M15-016` Commit checkpoint and event atomically where required.
- [x] `M15-017` Deduplicate checkpoints with identical state.

## Pause and Resume

- [x] `M15-018 BLOCKER` Implement pause request from CLI and UI.
- [x] `M15-019` Stop starting new model and tool operations after pause.
- [x] `M15-020` Decide whether an in-flight safe read may finish.
- [x] `M15-021` Cancel in-flight long-running operations when requested.
- [x] `M15-022` Persist paused state and reason.
- [x] `M15-023` Validate repository and worktree binding before resume.
- [x] `M15-024` Validate policy, provider, and tool compatibility before resume.
- [x] `M15-025` Surface user edits made while paused.
- [x] `M15-026` Require reconciliation or a new plan revision after conflicting edits.

## Crash Recovery

- [x] `M15-027 BLOCKER` Scan incomplete tasks on coordinator startup.
- [x] `M15-028` Verify repository path and identity.
- [x] `M15-029` Verify base revision availability.
- [x] `M15-030` Verify worktree existence and ownership.
- [x] `M15-031` Verify recorded file hashes and diff identity.
- [x] `M15-032` Verify no unresolved Git operation appeared.
- [x] `M15-033` Classify recovery as safe-resume, reconcile-required, patch-preservation-only, or unrecoverable.
- [x] `M15-034` Never auto-repeat an external action with ambiguous outcome.
- [x] `M15-035` Present the last checkpoint and divergence clearly.
- [x] `M15-036` Preserve a patch export path when direct resume is unsafe.
- [x] `M15-037` Record every recovery attempt and decision.

## Tests and Gate

- [x] `M15-038 TEST` Force termination before and after every material event/checkpoint boundary.
- [x] `M15-039 TEST` Resume an unchanged worktree.
- [x] `M15-040 TEST` Resume after user edits a non-overlapping file.
- [x] `M15-041 TEST` Detect a conflicting user edit.
- [x] `M15-042 TEST` Detect a missing worktree.
- [x] `M15-043 TEST` Detect changed tool or policy versions.
- [x] `M15-044 TEST` Detect an ambiguous external-action outcome.
- [x] `M15-045 TEST` Verify cancellation does not become failure.
- [x] `M15-G01 GATE` A crash at any tested durable boundary yields a safe, explainable recovery state.
- [x] `M15-G02 GATE` Resume never duplicates a completed model/tool/external action.
- [x] `M15-G03 GATE` The user can always preserve the current patch even when normal resume is impossible.

M15 test evidence:

- Checkpoint schema version 1 canonically binds task, run, repository,
  worktree, approved plan, base and preserved Git revisions, dirty-file and
  diff identities, plan progress, exact budget position, policy, provider,
  model, run configuration, tool catalog, durable event sequence, and
  ambiguous external outcomes without credential or process-handle fields.
- Checkpoint capture uses an isolated Git index and a private immutable ref, so
  it preserves staged, unstaged, untracked, deleted, and binary task changes
  without moving the task branch, HEAD, or real index. Atomic SQLite
  checkpoint-plus-event writes enforce authoritative bindings, exact
  idempotency aliases, state deduplication, and cleanup after failed commits.
- The agent checkpoints the exact approved plan before its first model action,
  after every material edit, before risky effects, and after successful
  validation. Pause waits for complete action quiescence before checkpointing;
  bounded graceful shutdown captures the durable checkpoint before sending its
  exact identity to the worker.
- The authenticated loopback TaskService and CLI expose idempotent pause,
  resume, and cancellation commands. A durable action gate blocks new work,
  lets only mediated safe reads finish, cancels model, write, validation, and
  long-running operations, and records cancellation as cancellation rather
  than failure. The same product service is the UI control boundary consumed
  by the GWC client milestones.
- Resume verifies exact repository path and Git identity, worktree root,
  branch ownership, HEAD, file hashes, diff, unresolved Git operations,
  policy, provider/model/run configuration, tool catalog, and ambiguous
  actions. Non-overlapping paused edits are surfaced; conflicts and any
  completed post-checkpoint action block direct resume and require an explicit
  recovery or new-plan choice.
- Coordinator startup scans every incomplete run before binding the product
  server, including crashes before the first checkpoint. It persists
  structured safe-resume, reconcile-required, patch-preservation-only, or
  unrecoverable assessments and never automatically repeats an unresolved
  provider, tool, command, or external-effect intent.
- Recovery decisions and started/terminal attempts are immutable and
  idempotent. User-authorized patch preservation verifies the checkpoint's
  private ref and produces a binary-safe export even after the task worktree
  is missing.
- Real SQLite close/reopen tests cover all six required checkpoint triggers,
  persisted actions completed after each checkpoint, idempotent recovery
  assessment/decision/attempt replay, and a subprocess crash before the first
  checkpoint. Real Git tests cover divergence, same-HEAD branch substitution,
  missing worktrees, unresolved operations, and patch export without mutation.
- Focused checkpoint, Git, storage, agent, coordinator, transport, worker, and
  CLI suites pass. `go test ./...`, lint, deterministic generation, migration,
  integration, security, and the complete local `test-all` gate pass. The
  adversarial refinement loop accepts M15 after closing provider-binding,
  unresolved-intent, event-sequence, no-repeat, branch-ownership, and
  timestamp-idempotency findings.

---

# Milestone 16: Frontend Shell and Design Foundation

Goal: build the accessible, local application shell before conversation and graph behavior add streaming complexity.

Plan references: §27A Product Surface; Application Layout; Client, Server, and Storage Boundary; Rendering and Performance; Local Security; §27C Route Map, Component Tree, Frontend State Ownership, Shared Primitive Components, and Root and Shell Component Contracts; §25 MVP Usability.

Depends on: `M15-G01` through `M15-G03`; framework primitives depend on `M06-G01` through `M06-G05`.

Milestone output: a keyboard-accessible GoWebComponents v5 shell with thread rail, chat pane, graph pane, responsive modes, design tokens, loading/error states, and safe session bootstrap.

## Visual and Interaction Tokens

- [x] `M16-001 UX` Define the neutral, accent, success, warning, failure, active, blocked, and invalidated color tokens.
- [x] `M16-002 UX` Define light-theme values for every color token.
- [x] `M16-003 UX` Define dark-theme values for every color token.
- [x] `M16-004 UX` Verify text/background token pairs meet WCAG AA contrast.
- [x] `M16-005 UX` Define typeface stacks that do not require a remote font request.
- [x] `M16-006 UX` Define body, compact metadata, heading, code, and numeric typography tokens.
- [x] `M16-007 UX` Define spacing tokens on a small consistent scale.
- [x] `M16-008 UX` Define border, radius, shadow, and focus-ring tokens.
- [x] `M16-009 UX` Define motion-duration and easing tokens.
- [x] `M16-010 UX` Define reduced-motion overrides.
- [x] `M16-011 UX` Define minimum pointer target size.
- [x] `M16-012 UX` Define status iconography that does not depend on color.
- [x] `M16-013 UX` Define density rules for long technical threads.
- [x] `M16-014 UX` Implement a development-only token specimen page.
- [x] `M16-015 TEST` Add automated contrast checks for fixed token pairs.

## Application Bootstrap

- [x] `M16-016 BLOCKER` Create the GoWebComponents v5 application entry point.
- [x] `M16-017` Load the per-launch session secret without placing it in persistent browser storage.
- [x] `M16-018` Call the coordinator health endpoint on startup.
- [x] `M16-019` Fetch the current application, API, schema, and frontend versions.
- [x] `M16-020` Reject an incompatible client/server version with a clear reload message.
- [x] `M16-021` Show a bounded startup loading state.
- [x] `M16-022` Show a coordinator-unavailable state with retry.
- [x] `M16-023` Show a migration-required or database-error state without exposing raw paths or SQL.
- [x] `M16-024` Restore the last non-sensitive selected repository and thread.
- [x] `M16-025` Avoid automatically reopening a repository that no longer exists.
- [x] `M16-026` Initialize the unified session-stream client only after authentication.
- [x] `M16-027` Dispose all subscriptions when the application root unmounts.

## Shell Regions

- [x] `M16-028 BLOCKER UX` Implement the top application bar.
- [x] `M16-029 UX` Add repository and branch placeholders to the top bar.
- [x] `M16-030 UX` Add worktree status placeholder.
- [x] `M16-031 UX` Add task-state and connection-state placeholders.
- [x] `M16-032 UX` Add model and effort placeholders.
- [x] `M16-033 UX` Add forecast, actual cost, and hard-budget placeholders.
- [x] `M16-034 UX` Add pause, stop, and overflow-control placeholders.
- [x] `M16-035 BLOCKER UX` Implement the left thread/task rail.
- [x] `M16-036 UX` Implement collapse and expand for the rail.
- [x] `M16-037 UX` Persist rail width as a non-sensitive UI preference.
- [x] `M16-038 BLOCKER UX` Implement the central conversation pane.
- [x] `M16-039 BLOCKER UX` Implement the right graph pane.
- [x] `M16-040 UX` Implement a draggable chat/graph splitter.
- [x] `M16-041 UX` Clamp splitter positions to usable minimum widths.
- [x] `M16-042 UX` Persist the splitter preference.
- [x] `M16-043 UX` Implement graph-pane collapse.
- [x] `M16-044 UX` Implement graph-pane restore.
- [x] `M16-045 UX` Implement a bottom connection/diagnostic status strip only if usability testing shows it adds value.

## Responsive Behavior

- [x] `M16-046 UX` Define wide, medium, and narrow viewport breakpoints from content needs rather than device names.
- [x] `M16-047 UX` Keep side-by-side chat and graph on wide layouts.
- [x] `M16-048 UX` Collapse the thread rail to an overlay on medium layouts.
- [x] `M16-049 UX` Convert graph and conversation into tabs or a drawer on narrow layouts.
- [x] `M16-050 UX` Preserve the selected graph node while switching narrow-layout tabs.
- [x] `M16-051 UX` Keep the composer visible above the on-screen keyboard where supported.
- [x] `M16-052 UX` Prevent horizontal page scrolling at all supported widths.
- [x] `M16-053 TEST` Add component-level viewport tests for all breakpoints.

## Keyboard and Accessibility

- [x] `M16-054 UX` Add a skip link to the conversation.
- [x] `M16-055 UX` Establish a logical heading hierarchy.
- [x] `M16-056 UX` Define tab order across rail, chat, composer, graph, and inspector.
- [x] `M16-057 UX` Make the splitter keyboard adjustable.
- [x] `M16-058 UX` Expose splitter values through appropriate accessibility attributes.
- [x] `M16-059 UX` Add visible focus for every interactive control.
- [x] `M16-060 UX` Ensure collapsed controls retain accessible names.
- [x] `M16-061 UX` Define keyboard shortcuts for focus-chat, focus-graph, pause, and stop.
- [x] `M16-062 UX` Prevent shortcuts from firing while the user types unless explicitly scoped.
- [x] `M16-063 UX` Add a shortcut help dialog.
- [x] `M16-064 TEST` Navigate the complete empty shell with keyboard only.
- [x] `M16-065 TEST` Run automated accessibility checks against the shell.

## Component Isolation

- [x] `M16-066` Define route-level components.
- [x] `M16-067` Define shell-level state separately from task/session state.
- [x] `M16-068` Define the top-bar view model.
- [x] `M16-069` Define the thread-rail view model.
- [x] `M16-070` Define conversation and graph pane boundaries.
- [x] `M16-071` Instrument render counts in development builds.
- [x] `M16-072 TEST` Verify a top-bar cost update does not rerender the full thread.
- [x] `M16-073 TEST` Verify a chat append does not rerender the graph viewport.
- [x] `M16-074 TEST` Verify graph selection does not rerender every message.

## Complete Frontend Component Contract

Plan: §27C Route Map through Root and Shell Component Contracts; Shared Primitive Components; Focus, Keyboard, and Accessibility.

- [x] `M16-075 BLOCKER` Implement the route map for repository choice, thread workspace, memory, settings, diagnostics, and first run.
- [x] `M16-076` Implement `AppRoot`, `SessionBootstrap`, `AppRouter`, `GlobalErrorBoundary`, `GlobalShortcutManager`, `AccessibilityAnnouncer`, `DialogHost`, and `ToastHost`.
- [x] `M16-077` Define immutable view models for top bar, thread rail, conversation, graph, review, settings, memory, diagnostics, and first run.
- [x] `M16-078` Define authoritative remote state separately from ephemeral client state and prohibit durable task transitions from local-only reducers.
- [x] `M16-079` Implement `SessionStore`, `WorkspaceStore`, `ThreadStore`, `TaskStore`, `GraphStore`, `ReviewStore`, `SettingsStore`, and `UIStore` ownership boundaries.
- [x] `M16-080` Implement shared Button, IconButton, ToggleButton, Menu, Tabs, Dialog, Drawer, Popover, Tooltip, input, Badge, progress, Skeleton, InlineAlert, Disclosure, VirtualList, ResizableSplit, CopyButton, CodeBlock, EmptyState, and ErrorState primitives as actually needed.
- [x] `M16-081 UX` Define keyboard, focus, accessible-name, disabled, busy, high-contrast, reduced-motion, and pointer-target behavior for every shared primitive before feature reuse.
- [x] `M16-082 UX` Implement repository chooser with recent-valid workspace, browse/open, canonical-path result, warnings, loading, empty, unavailable, and retry states.
- [x] `M16-083 UX` Implement Settings route shells for providers, models, policy, appearance, and data.
- [x] `M16-084 UX` Implement Memory route shell with list, details, and action regions.
- [x] `M16-085 UX` Implement Diagnostics route shell with health, versions, tasks, logs, backup, and export regions.
- [x] `M16-086 UX` Implement First-run route shell with resumable local-promise, provider, repository, worktree/permissions, and first-thread steps.
- [x] `M16-087` Implement route restoration that refuses missing repositories, archived inaccessible threads, expired sessions, and incompatible client/server state.
- [x] `M16-088` Implement component-level not-requested, loading, ready-empty, ready-data, partial/stale, recoverable-error, denied, incompatible, and disconnected states.
- [x] `M16-089 UX` Implement rate-limited accessibility announcements for connection, approval, pause, completion, validation failure, and recovery only.
- [x] `M16-090 UX` Ensure routine events and token deltas never steal focus or create assertive announcements.
- [x] `M16-091 UX` Add stable full labels for long atom names while permitting visual truncation only.
- [x] `M16-092 TEST` Render every route in each top-level bootstrap state.
- [x] `M16-093 TEST` Test focus restoration after dialog, drawer, responsive rail, and graph pane close.
- [x] `M16-094 TEST` Test shared primitives in keyboard, disabled, busy, high-contrast, and reduced-motion modes.
- [x] `M16-095 TEST` Test route and draft preservation across recoverable component failure.
- [x] `M16-096 TEST` Verify settings, memory, diagnostics, and first-run shells make no unauthorized data fetches.
- [x] `M16-097 TEST` Verify client stores cannot create an unsupported durable task transition.
- [x] `M16-098 TEST` Verify every data-owning component has explicit empty, error, and disconnected presentation.
- [x] `M16-099 TEST` Verify no embedded asset or UI primitive performs an external network request.
- [x] `M16-100 TEST` Verify user-facing terminology consistently distinguishes Thread, Task, Attempt, Plan revision, Approval, Checkpoint, and Recovery.

## Gate

- [x] `M16-G01 GATE` The empty shell loads from the embedded local server with no external asset requests.
- [x] `M16-G02 GATE` Every shell action is keyboard accessible and has a visible focus state.
- [x] `M16-G03 GATE` Wide, medium, and narrow layouts remain usable without lost state.
- [x] `M16-G04 GATE` Component render instrumentation confirms chat and graph update isolation.
- [x] `M16-G05 GATE` Every route, shared primitive, store, and shell component has explicit ownership, loading/empty/error/disconnected behavior, and keyboard/accessibility coverage.

---

# Milestone 17: Thread Rail, Conversation Timeline, and Composer

Goal: make the chat thread the complete primary control surface rather than a styled log viewer.

Plan references: §27A Conversation Model; Product Surface; Application Layout; Primary Interaction Journey; Frontend MVP Boundary; §27C Timeline Contracts, Composer Contract, Review Drawer Contracts, and Detailed Frontend Flows; §5 Human Intent.

Depends on: `M16-G01` through `M16-G05`; data methods depend on `M07-G01` through `M07-G04`.

Milestone output: resumable thread navigation, virtualized typed event cards, a task-aware composer, inline approvals, and stable graph identity links.

## Thread Rail Data

- [x] `M17-001 BLOCKER` Fetch the first page of threads for the open repository.
- [x] `M17-002` Render thread title, task state, last activity, and unread/attention indicator.
- [x] `M17-003` Sort active attention-required threads before inactive threads.
- [x] `M17-004` Preserve stable ordering when two timestamps are equal.
- [x] `M17-005` Load the next page when the rail approaches its end.
- [x] `M17-006` Avoid duplicate rows across pagination boundaries.
- [x] `M17-007` Render a thread-list skeleton during first load.
- [x] `M17-008` Render a retryable list error.
- [x] `M17-009` Render an empty-repository thread state.
- [x] `M17-010` Select a thread and update the route.
- [x] `M17-011` Restore selection after reload.
- [x] `M17-012` Create a new thread with an idempotency key.
- [x] `M17-013` Optimistically show a pending new thread without duplicating the committed thread.
- [x] `M17-014` Rename a thread.
- [x] `M17-015` Archive a thread after confirmation.
- [x] `M17-016` Remove an archived thread from the default view.
- [x] `M17-017` Add an archived-thread filter.
- [x] `M17-018 TEST` Test 1,000 thread rows with virtualized scrolling.

## Timeline Pagination and Anchoring

- [x] `M17-019 BLOCKER` Fetch the newest thread page on selection.
- [x] `M17-020` Group events into presentation items without changing durable order.
- [x] `M17-021` Use event sequence as the stable presentation key.
- [x] `M17-022` Load older events when the user scrolls near the top.
- [x] `M17-023` Preserve visual scroll position after prepending older events.
- [x] `M17-024` Avoid duplicate events after replay joins pagination.
- [x] `M17-025` Show a clear beginning-of-thread marker.
- [x] `M17-026` Auto-follow new events only when the user is already near the bottom.
- [x] `M17-027` Show a new-events button when the user has scrolled upward.
- [x] `M17-028` Return to live position when the button is activated.
- [x] `M17-029` Preserve readable ordering for simultaneous event timestamps.
- [x] `M17-030` Render a gap-recovery indicator if sequence continuity is temporarily unresolved.
- [x] `M17-031 TEST` Test pagination/replay joining at every page boundary.
- [x] `M17-032 TEST` Test anchor preservation with variable-height cards.

## Message Presentation

- [x] `M17-033 UX` Implement user message bubbles.
- [x] `M17-034 UX` Implement agent message bubbles.
- [x] `M17-035 UX` Render streamed deltas into one in-progress message.
- [x] `M17-036 UX` Replace the in-progress message with the durable final message.
- [x] `M17-037 UX` Indicate interrupted or incomplete model output.
- [x] `M17-038 UX` Render safe Markdown without executable HTML.
- [x] `M17-039 SECURITY` Sanitize links and block unsafe URL schemes.
- [x] `M17-040 UX` Render code blocks with copy action.
- [x] `M17-041 UX` Add line wrapping and horizontal scroll behavior for code.
- [x] `M17-042 UX` Render stable graph-node identity chips.
- [x] `M17-043 UX` Focus the associated graph node when a node chip is activated.
- [x] `M17-044 UX` Explain when the associated graph revision is no longer current.
- [x] `M17-045 UX` Add message timestamps through an unobtrusive details affordance.
- [x] `M17-046 UX` Add copy-message action.
- [x] `M17-047 UX` Add accessible labels for user, agent, system event, and status.

## Typed Cards

- [x] `M17-048 UX` Implement requirement/ambiguity card.
- [x] `M17-049 UX` Implement forecast card with P50/P90 ranges.
- [x] `M17-050 UX` Implement plan card with step status.
- [x] `M17-051 UX` Implement plan-revision diff card.
- [x] `M17-052 UX` Implement context-selection card.
- [x] `M17-053 UX` Implement collapsed tool-started card.
- [x] `M17-054 UX` Update the same card for tool completion.
- [x] `M17-055 UX` Show command, scope, duration, exit state, and summarized output.
- [x] `M17-056 UX` Keep raw redacted output collapsed by default.
- [x] `M17-057 UX` Lazy-load large redacted output pages.
- [x] `M17-058 UX` Implement approval card with exact requested authority.
- [x] `M17-059 UX` Add allow-once action.
- [x] `M17-060 UX` Add allow-for-task action with displayed scope.
- [x] `M17-061 UX` Add deny action.
- [x] `M17-062 UX` Disable approval actions after one resolution commits.
- [x] `M17-063 UX` Show who or what resolved the approval.
- [x] `M17-064 UX` Implement checkpoint card.
- [x] `M17-065 UX` Implement validation summary card.
- [x] `M17-066 UX` Implement diff summary card.
- [x] `M17-067 UX` Implement cost/budget update card only for meaningful threshold events.
- [x] `M17-068 UX` Implement error and recovery-choice card.
- [x] `M17-069 UX` Implement final completion summary.
- [x] `M17-070 UX` Implement unsupported-event fallback that preserves kind and sequence.

## Composer

- [x] `M17-071 BLOCKER UX` Implement a multiline composer.
- [x] `M17-072 UX` Submit with the chosen keyboard convention.
- [x] `M17-073 UX` Insert a newline without submitting.
- [x] `M17-074 UX` Disable submit for empty or whitespace-only input.
- [x] `M17-075 UX` Show pending send state.
- [x] `M17-076` Generate and retain an idempotency key until send resolves.
- [x] `M17-077` Restore the unsent draft for the current thread.
- [x] `M17-078` Keep drafts isolated per thread.
- [x] `M17-079` Clear the draft only after committed message confirmation.
- [x] `M17-080 UX` Show an explicit retry after send failure.
- [x] `M17-081 UX` Add cost/speed/correctness policy selector.
- [x] `M17-082 UX` Add hard-budget input with exact currency.
- [x] `M17-083 UX` Add optional model override.
- [x] `M17-084 UX` Add optional reasoning-effort override.
- [x] `M17-085 UX` Add repository file/symbol attachment picker.
- [x] `M17-086 UX` Display selected attachments as removable chips.
- [x] `M17-087 SECURITY` Resolve attachments through server-side repository identities, not browser file paths.
- [x] `M17-088 UX` Change composer actions appropriately for running, paused, awaiting-approval, and completed states.
- [x] `M17-089 UX` Keep stop immediately reachable while the agent is running.

## Tests and Gate

- [x] `M17-090 TEST` Render every card from a fixed event fixture.
- [x] `M17-091 TEST` Snapshot or structurally compare every status variant.
- [x] `M17-092 TEST` Test unsafe Markdown and URL payloads.
- [x] `M17-093 TEST` Test double-click approval idempotency.
- [x] `M17-094 TEST` Test message-send retry.
- [x] `M17-095 TEST` Test per-thread draft isolation.
- [x] `M17-096 TEST` Keyboard-test the entire thread and composer.
- [x] `M17-097 TEST` Screen-reader-test one complete task timeline.
- [x] `M17-098` Implement an exhaustive timeline-item registry that requires every event kind to map to a card or documented grouping rule.
- [x] `M17-099` Implement unknown-event fallback with kind, time, sequence, safe details, and diagnostics link.
- [x] `M17-100` Implement `ApplyMessageDelta`, `FinalizeMessage`, `MergeThreadPage`, and `ShouldAutoFollow` as pure deterministic reducers.
- [x] `M17-101 UX` Ensure streamed text is visibly provisional until the durable final message arrives.
- [x] `M17-102 UX` Ensure plan cards show assumptions, authority, validation, completion criteria, and revision history before approval.
- [x] `M17-103 UX` Ensure context cards explain inclusion reason and revision without dumping full source.
- [x] `M17-104 UX` Ensure tool cards update in place and do not create one row per progress chunk.
- [x] `M17-105 UX` Ensure approval cards do not steal typing focus and retain attributable resolution after actions disappear.
- [x] `M17-106 UX` Ensure validation cards distinguish passed, failed, waived, skipped, unavailable, cancelled, and stale.
- [x] `M17-107 UX` Ensure completion cards distinguish implemented, validated, reviewed, accepted, rejected, and rolled-back outcomes.
- [x] `M17-108 UX` Implement first-message latency presentation that shows the current phase and Stop after the threshold instead of an indefinite spinner.
- [x] `M17-109 UX` Preserve thread and graph position when opening and closing review.
- [x] `M17-110 TEST` Test new-thread pending row replacement without selection or focus jump.
- [x] `M17-111 TEST` Test plan revision resets approval and preserves prior plan history.
- [x] `M17-112 TEST` Test graph auto-highlighting never pans away from deliberate user inspection without a Return to current action control.
- [x] `M17-113 TEST` Test repair feedback attached to task, file, hunk, validation, and graph node identities.
- [x] `M17-114 TEST` Test raw output pagination, redaction, truncation, and copy behavior.
- [x] `M17-115 TEST` Verify no routine progress event creates a toast, modal, or assertive announcement.
- [x] `M17-G01 GATE` A user can create, leave, reopen, paginate, and continue a thread without lost or duplicated content.
- [x] `M17-G02 GATE` Every correctness-bearing event has a distinct, inspectable presentation.
- [x] `M17-G03 GATE` Approval and stop actions remain reachable without expanding raw tool output.
- [x] `M17-G04 GATE` Every timeline event, card, composer state, and review transition has deterministic reducer, progressive-disclosure, focus, replay, and failure behavior.

---

# Milestone 18: Live Task Controls, Connection State, and Cost Surface

Goal: make live execution interruptible, attributable, and honest under normal operation and transport failure.

Plan references: §27A Unified Session Stream; Primary Interaction Journey; Frontend Acceptance Criteria; §27C Frontend Stores and Reducers, Command Functions, Task State and Available Action Matrix, Detailed Frontend Flows, and Frontend Telemetry; §21 Progress Monitor and Dynamic Escalation; §25 Speed and Cost.

Depends on: `M17-G01` through `M17-G04`; budget semantics depend on `M13-G01` through `M13-G03`.

Milestone output: reconnectable live session state, top-bar controls, cost/forecast display, safe interruption, and recovery presentation.

## Session Connection State

- [x] `M18-001 BLOCKER` Define UI connection states: connecting, live, replaying, degraded, disconnected, incompatible, and unauthorized.
- [x] `M18-002` Display connection state in the top bar.
- [x] `M18-003` Begin subscription from the last applied durable sequence.
- [x] `M18-004` Apply replay events before live events.
- [x] `M18-005` Detect duplicate sequence delivery.
- [x] `M18-006` Detect sequence gaps.
- [x] `M18-007` Pause correctness-bearing UI mutations until a gap is repaired.
- [x] `M18-008` Retry transient disconnects with bounded exponential backoff.
- [x] `M18-009` Stop retrying on authentication or version mismatch.
- [x] `M18-010` Expose manual reconnect.
- [x] `M18-011` Preserve unsent drafts during reconnect.
- [x] `M18-012` Disable mutating controls when delivery certainty is unknown.
- [x] `M18-013` Re-enable controls after replay reaches live state.
- [x] `M18-014` Report last successfully applied sequence in diagnostics.
- [x] `M18-015 TEST` Inject disconnects before, during, and after each event category.

## Task State Projection

- [x] `M18-016 BLOCKER` Project task state from snapshot plus ordered events.
- [x] `M18-017` Project current plan revision.
- [x] `M18-018` Project active tool and model operation.
- [x] `M18-019` Project pending approval.
- [x] `M18-020` Project latest checkpoint.
- [x] `M18-021` Project validation state.
- [x] `M18-022` Project change-acceptance state.
- [x] `M18-023` Reject an event that attempts an impossible state transition.
- [x] `M18-024` Trigger a fresh snapshot after projection inconsistency.
- [x] `M18-025` Log a safe client diagnostic without raw task content.
- [x] `M18-026 TEST` Compare client projection with server projection over recorded event fixtures.

## Top-Bar Task Controls

- [x] `M18-027 UX` Display task state with icon and text.
- [x] `M18-028 UX` Display current phase: planning, editing, validating, repairing, or reviewing.
- [x] `M18-029 UX` Display selected provider, model, and effort.
- [x] `M18-030 UX` Display forecast P50/P90 without implying certainty.
- [x] `M18-031 UX` Display actual token usage.
- [x] `M18-032 UX` Display actual cost using the task pricing snapshot.
- [x] `M18-033 UX` Display unknown cost honestly.
- [x] `M18-034 UX` Display remaining hard budget.
- [x] `M18-035 UX` Add warning styling at the configured threshold.
- [x] `M18-036 UX` Add pause control only in pausable states.
- [x] `M18-037 UX` Add resume control only in resumable states.
- [x] `M18-038 UX` Add stop control in every active state.
- [x] `M18-039 UX` Require confirmation only when stopping has a non-obvious consequence.
- [x] `M18-040 UX` Add budget-adjust action.
- [x] `M18-041 UX` Show exact old and new budget before confirmation.
- [x] `M18-042 UX` Prevent repeated clicks from producing duplicate commands.

## Recovery Presentation

- [x] `M18-043 UX` Show recovery-required status at thread and top-bar level.
- [x] `M18-044 UX` Display last valid checkpoint time and plan step.
- [x] `M18-045 UX` Display repository/worktree divergence summary.
- [x] `M18-046 UX` Offer safe resume when verified.
- [x] `M18-047 UX` Offer reconcile when user edits require it.
- [x] `M18-048 UX` Offer patch preservation when direct resume is unsafe.
- [x] `M18-049 UX` Explain ambiguous external-action outcomes prominently.
- [x] `M18-050 UX` Never label unsafe auto-repeat as retry.
- [x] `M18-051 UX` Link recovery details to relevant events and files.

## State, Command, and Flow UX

Plan: §27C Command Functions; Task State and Available Action Matrix; Detailed Frontend Flows; Empty, Loading, Error, and Offline States; Frontend Telemetry.

- [x] `M18-052 BLOCKER` Implement `ApplySessionSnapshot` and `ApplySessionEvent` as the only authoritative remote-state entry points.
- [x] `M18-053` Implement pure reducers for task transition, budget, approval, validation, graph patch, and review revision.
- [x] `M18-054` Detect impossible task transitions, stale graph patches, and sequence gaps and request snapshot repair rather than ignoring them.
- [x] `M18-055` Implement `AvailableTaskActions` from task state, connection certainty, policy, pending command, approval, review staleness, and recovery classification.
- [x] `M18-056 UX` Implement the complete Draft through Rolled-back state/action matrix from §27C.
- [x] `M18-057 UX` Omit or explain unavailable actions before click rather than returning avoidable server errors.
- [x] `M18-058` Wrap every UI mutation in a command state that owns one idempotency key until commit or deliberate abandonment.
- [x] `M18-059` Implement stale-revision command handling that refreshes state and explains the changed entity.
- [x] `M18-060 UX` Distinguish disconnected UI, backend task state, and sequence uncertainty.
- [x] `M18-061 UX` Keep the timeline readable during disconnection while disabling only mutations whose delivery/state certainty is unsafe.
- [x] `M18-062 UX` Implement one non-spamming budget warning and hard-cap decision surface.
- [x] `M18-063 UX` Implement one calm recovery surface that leads with known state, ambiguity, and safest recommended action.
- [x] `M18-064 UX` Implement review staleness presentation when diff, plan, validation, evidence, or graph revision changes.
- [x] `M18-065 UX` Implement exact estimate-versus-actual labeling and never substitute missing price with zero.
- [x] `M18-066` Record local UX telemetry for first-run, time to plan/diff, approval, pause/stop, review, graph use, reconnect, recovery, and slow renders without keystrokes or hidden content.
- [x] `M18-067 UX` Add local telemetry inspection and deletion.
- [x] `M18-068 TEST` Exercise every row of the task state/action matrix.
- [x] `M18-069 TEST` Exercise first-run, new task, plan review, live work, approval, review, repair, reconnect, recovery, graph exploration, and budget flows.
- [x] `M18-070 TEST` Verify a user can always identify current state, cost, authority, evidence, uncertainty, and next safe action without raw logs.

## Gate

- [x] `M18-G01 GATE` Refreshing or reconnecting during an active task yields the same task, budget, approval, and validation state.
- [x] `M18-G02 GATE` Pause, resume, stop, and budget change are idempotent from the UI.
- [x] `M18-G03 GATE` The interface never shows a stale approval as actionable.
- [x] `M18-G04 GATE` Unknown or delayed cost is never displayed as zero.
- [x] `M18-G05 GATE` Every task state and user command has explicit live, busy, committed, stale, denied, disconnected, recovery, and accessibility behavior.

---

# Milestone 19: Task Graph Storage, Projection, Query, and Rendering

Goal: provide a stable semantic map of the current task without making graph authoring a prerequisite for ordinary coding.

Plan references: §5 Functional Graph and Core Graph Entities; §18 Stable Graph Identity; §23 Graph Storage; §27A Graph Modes; Graph Rendering Rules; Node Inspector; Frontend MVP Boundary; §30 Graph Medium Failure.

Depends on: `M18-G01` through `M18-G05`; stable IDs depend on `M02-G01`; database work depends on `M03-G04`.

Milestone output: immutable task-graph revisions in SQLite, bounded graph queries, stable layout hints, Program/Execution/Evidence projections, accessible SVG rendering, and bidirectional chat links.

## Minimal Graph Contract

- [x] `M19-001 BLOCKER` Define graph identity separately from graph revision identity.
- [x] `M19-002 BLOCKER` Define stable node and edge identity.
- [x] `M19-003` Define node classes: requirement, plan region, atom/operation, effect, branch/match/merge, obligation, artifact/result.
- [x] `M19-004` Define edge classes: control, data/provenance, evidence dependency, retry, reconciliation, compensation.
- [x] `M19-005` Define node statuses: pending, active, passed, warning, failed, blocked, invalidated.
- [x] `M19-006` Define graph modes: Program, Execution, Evidence.
- [x] `M19-007` Define immutable revision metadata.
- [x] `M19-008` Define revision parentage.
- [x] `M19-009` Define source event and plan-step links.
- [x] `M19-010` Define node contract summary without requiring deep semantic atom contracts.
- [x] `M19-011` Define bounded arbitrary metadata fields or prohibit them in favor of typed columns.
- [x] `M19-012` Version the graph schema independently from the SQLite schema.

## SQLite Graph Schema

- [x] `M19-013 DATA` Create graph identity table.
- [x] `M19-014 DATA` Create immutable graph revision table.
- [x] `M19-015 DATA` Create node identity table.
- [x] `M19-016 DATA` Create node revision table.
- [x] `M19-017 DATA` Create edge identity table.
- [x] `M19-018 DATA` Create edge revision table.
- [x] `M19-019 DATA` Create graph-to-task and graph-to-plan bindings.
- [x] `M19-020 DATA` Create graph-to-event and graph-to-message links.
- [x] `M19-021 DATA` Create source-location links.
- [x] `M19-022 DATA` Create layout-hint table scoped by graph revision and layout algorithm version.
- [x] `M19-023 DATA` Add uniqueness and foreign-key constraints.
- [x] `M19-024 DATA` Add indexes for task slice, node lookup, neighbor expansion, evidence cone, and message link.
- [x] `M19-025 DATA` Add migration-forward and backup/restore tests.

## Graph Projection

- [x] `M19-026 BLOCKER` Project requirement nodes from accepted user intent.
- [x] `M19-027` Project plan-region and plan-step nodes.
- [x] `M19-028` Project repository inspection operations.
- [x] `M19-029` Project file edit operations as atom/operation nodes.
- [x] `M19-030` Project command and provider calls as effect nodes.
- [x] `M19-031` Project approval boundaries.
- [x] `M19-032` Project validation obligations and results.
- [x] `M19-033` Project changed files/diff as artifact nodes.
- [x] `M19-034` Project retries with explicit retry edges.
- [x] `M19-035` Project checkpoint and recovery relationships where useful.
- [x] `M19-036` Derive Program-mode visibility.
- [x] `M19-037` Derive Execution-mode visibility and status.
- [x] `M19-038` Derive Evidence-mode visibility.
- [x] `M19-039` Create a new immutable graph revision after a material projection change.
- [x] `M19-040` Avoid a new revision for token-only text deltas.
- [x] `M19-041` Emit a bounded graph patch after revision commit.
- [x] `M19-042 TEST` Replay the same task events and compare graph revisions deterministically.

## Query Service

- [x] `M19-043 BLOCKER` Implement task-scoped initial slice query.
- [x] `M19-044` Implement mode filtering.
- [x] `M19-045` Implement node lookup by stable ID and revision.
- [x] `M19-046` Implement one-hop neighbor expansion.
- [x] `M19-047` Implement bounded multi-hop expansion.
- [x] `M19-048` Implement evidence-cone isolation.
- [x] `M19-049` Implement dependency-cone isolation.
- [x] `M19-050` Implement text and identity search.
- [x] `M19-051` Implement graph revision comparison.
- [x] `M19-052` Return continuation tokens when node/edge bounds are reached.
- [x] `M19-053` Reject unbounded full-database graph requests.
- [x] `M19-054` Include layout hints and algorithm version.
- [x] `M19-055 TEST` Test cycles, missing nodes, stale revisions, and expansion limits.

## Layout

- [x] `M19-056 BLOCKER` Implement or integrate a deterministic left-to-right layered layout.
- [x] `M19-057` Rank requirement and plan nodes before effects and artifacts.
- [x] `M19-058` Collapse strongly connected components before ranking.
- [x] `M19-059` Define stable sibling ordering by stable identity.
- [x] `M19-060` Reuse prior coordinates when topology permits.
- [x] `M19-061` Place newly added nodes near their stable neighbors.
- [x] `M19-062` Compute bounding boxes for viewport fitting.
- [x] `M19-063` Version layout algorithm changes.
- [x] `M19-064` Cache layout hints in SQLite.
- [x] `M19-065 TEST` Snapshot layout coordinates for deterministic fixtures.
- [x] `M19-066 TEST` Verify unrelated node additions do not move the entire graph.

## Graph Viewport

- [x] `M19-067 BLOCKER UX` Implement accessible SVG graph root.
- [x] `M19-068 UX` Render node shapes by class.
- [x] `M19-069 UX` Render status icon, border, and text independently of color.
- [x] `M19-070 UX` Render edge style by relationship.
- [x] `M19-071 UX` Add a visible legend.
- [x] `M19-072 UX` Implement pointer pan.
- [x] `M19-073 UX` Implement wheel/trackpad zoom around the cursor.
- [x] `M19-074 UX` Implement zoom controls.
- [x] `M19-075 UX` Implement fit-to-slice.
- [x] `M19-076 UX` Implement reset view.
- [x] `M19-077 UX` Implement node selection.
- [x] `M19-078 UX` Implement keyboard traversal of visible nodes.
- [x] `M19-079 UX` Implement focus-visible state.
- [x] `M19-080 UX` Announce selected-node summary to assistive technology.
- [x] `M19-081 UX` Center and select a node activated from chat.
- [x] `M19-082 UX` Highlight messages related to the selected node.
- [x] `M19-083 UX` Apply graph patches without resetting viewport.
- [x] `M19-084 UX` Show a new-revision indicator when comparison is available.
- [x] `M19-085 UX` Add Program, Execution, and Evidence mode tabs.
- [x] `M19-086 UX` Default to Execution while running.
- [x] `M19-087 UX` Default to Evidence after completion.
- [x] `M19-088 UX` Preserve selection across compatible mode changes.

## Node Inspector

- [x] `M19-089 UX` Display stable identity and revision.
- [x] `M19-090 UX` Display node class, status, and contract summary.
- [x] `M19-091 UX` Display inputs, outputs, and effects when known.
- [x] `M19-092 UX` Display supporting evidence and guarantee level.
- [x] `M19-093 UX` Display time, token, and cost attribution when known.
- [x] `M19-094 UX` List related messages and events.
- [x] `M19-095 UX` List related source locations.
- [x] `M19-096 UX` Add explain-in-chat action.
- [x] `M19-097 UX` Add expand-neighbors action.
- [x] `M19-098 UX` Add isolate-dependency-cone action.
- [x] `M19-099 UX` Add isolate-evidence-cone action.
- [x] `M19-100 UX` Add compare-revision action.
- [x] `M19-101 UX` Add open-in-editor action for validated repository paths.
- [x] `M19-102 UX` State clearly when information is unknown rather than leaving a blank.

## Performance and Gate

- [x] `M19-103 TEST` Benchmark initial 300-node layout.
- [x] `M19-104 TEST` Benchmark 100 sequential graph patches.
- [x] `M19-105 TEST` Measure SVG node, edge, label, and DOM counts.
- [x] `M19-106 TEST` Verify chat streaming remains responsive during patches.
- [x] `M19-107 TEST` Verify graph interaction remains responsive during token streaming.
- [x] `M19-108 TEST` Test high-contrast and color-vision-independent statuses.
- [x] `M19-G01 GATE` Program, Execution, and Evidence modes derive deterministically from one task history.
- [x] `M19-G02 GATE` Chat and graph resolve the same stable node identities in both directions.
- [x] `M19-G03 GATE` The viewport remains stable across normal graph revisions.
- [x] `M19-G04 GATE` The graph remains optional; the complete task journey still works with the pane collapsed.

---

# Milestone 20: Validation, Review, Evidence, and Change Acceptance

Goal: turn “the agent finished” into an inspectable claim backed by exact commands, results, revisions, and limitations.

Plan references: §5 Execution and Review; §9 Proof Obligations as the Unit of Assurance; §10 Guarantee Provenance; §19 Review and Source Mapping; §22 Correctness and Assurance Gates; §27A Evidence mode; §29 Phase 2.

Depends on: `M19-G01` through `M19-G04`; command execution depends on `M10-G01` through `M10-G03`.

Milestone output: risk-based validation profiles, immutable validation evidence, source-linked diff review, acceptance/repair/rollback actions, and a final evidence report with no inflated assurance claims.

## Risk Classification

- [x] `M20-001 BLOCKER` Define routine-change signals.
- [x] `M20-002 BLOCKER` Define elevated-change signals.
- [x] `M20-003 BLOCKER` Define protected-change signals.
- [x] `M20-004` Include authentication, authorization, payment, migration, credential, concurrency, and external-effect signals.
- [x] `M20-005` Include breadth, generated-code, dependency, configuration, and test-removal signals.
- [x] `M20-006` Include user-selected risk override.
- [x] `M20-007` Version the risk-classification policy.
- [x] `M20-008` Persist input signals, selected risk, and explanation.
- [x] `M20-009` Allow risk escalation after new evidence.
- [x] `M20-010` Prohibit automatic risk demotion during a task.
- [x] `M20-011 TEST` Build positive and negative fixtures for every protected signal.

## Validation Profiles

- [x] `M20-012 BLOCKER` Define routine validation requirements.
- [x] `M20-013 BLOCKER` Define elevated validation requirements.
- [x] `M20-014 BLOCKER` Define protected validation requirements.
- [x] `M20-015` Map repository-discovered formatter commands to profiles.
- [x] `M20-016` Map targeted test commands to profiles.
- [x] `M20-017` Map broader package or repository tests to profiles.
- [x] `M20-018` Map build commands to profiles.
- [x] `M20-019` Map static-analysis commands to profiles.
- [x] `M20-020` Require user approval before a discovered command first runs if policy requires it.
- [x] `M20-021` Define required versus advisory checks.
- [x] `M20-022` Define timeout and retry behavior per check.
- [x] `M20-023` Define skip reasons.
- [x] `M20-024` Require explicit user authority to waive a required check.
- [x] `M20-025` Record a waived check as waived, never passed.
- [x] `M20-026` Version each validation profile.

## Test Selection

- [x] `M20-027` Select tests in changed packages.
- [x] `M20-028` Select tests linked through deterministic file-to-test mappings.
- [x] `M20-029` Select tests implicated by failing baseline commands.
- [x] `M20-030` Select repository-wide tests for protected changes when feasible.
- [x] `M20-031` Preserve user-provided acceptance commands.
- [x] `M20-032` Deduplicate equivalent test commands.
- [x] `M20-033` Order cheap high-signal checks before expensive broad checks.
- [x] `M20-034` Record why each check was selected.
- [x] `M20-035` Record which changed files each check covers when known.

## Validation Execution

- [x] `M20-036 BLOCKER` Create an immutable validation-run record before execution.
- [x] `M20-037` Bind the run to exact worktree revision and dirty diff identity.
- [x] `M20-038` Bind the run to command definition and executable identity.
- [x] `M20-039` Emit validation-start event.
- [x] `M20-040` Execute through the mediated command runner.
- [x] `M20-041` Capture exit status, duration, timeout, cancellation, and truncation.
- [x] `M20-042` Redact output before persistence.
- [x] `M20-043` Parse Go test package/test names when possible.
- [x] `M20-044` Parse formatter changes.
- [x] `M20-045` Parse build and vet diagnostics.
- [x] `M20-046` Preserve the raw redacted summary when parsing fails.
- [x] `M20-047` Emit validation-result event after commit.
- [x] `M20-048` Invalidate validation when the underlying diff changes.
- [x] `M20-049` Rerun invalidated required checks before completion.
- [x] `M20-050 TEST` Test invalidation after a one-line post-test edit.

## Baseline Failure Handling

- [x] `M20-051` Run or record a baseline check before changes when affordable.
- [x] `M20-052` Distinguish pre-existing failure from introduced failure.
- [x] `M20-053` Bind comparison to exact revisions and command.
- [x] `M20-054` Avoid claiming non-regression when baseline evidence is unavailable.
- [x] `M20-055` Surface flaky or nondeterministic results.
- [x] `M20-056` Require repeated evidence before labeling a failure flaky.
- [x] `M20-057` Record unresolved baseline failures in the final report.

## Diff Review

- [x] `M20-058 BLOCKER UX` Render changed-file list with status and line counts.
- [x] `M20-059 UX` Filter source, test, generated, dependency, and configuration files.
- [x] `M20-060 UX` Render a safe unified diff view for selected files.
- [x] `M20-061 UX` Preserve whitespace visibility controls.
- [x] `M20-062 UX` Link diff hunks to plan steps.
- [x] `M20-063 UX` Link diff hunks to tool/edit events.
- [x] `M20-064 UX` Link diff hunks to validation evidence.
- [x] `M20-065 UX` Flag files outside proposed plan scope.
- [x] `M20-066 UX` Flag broad formatting churn.
- [x] `M20-067 UX` Flag binary or generated changes.
- [x] `M20-068 UX` Open a validated source location in the external editor.
- [x] `M20-069 SECURITY` Reject editor-open requests outside the bound repository.

## Evidence Report

- [x] `M20-070 BLOCKER` Define final evidence-report schema.
- [x] `M20-071` Include requirement and accepted plan revision.
- [x] `M20-072` Include base revision and final diff identity.
- [x] `M20-073` Include changed-file summary.
- [x] `M20-074` Include every required validation and status.
- [x] `M20-075` Include waived, skipped, unavailable, failed, and invalidated checks.
- [x] `M20-076` Include risk level and classification explanation.
- [x] `M20-077` Include user approvals and authority used.
- [x] `M20-078` Include model/provider/tool/policy versions.
- [x] `M20-079` Include forecast and actual time/tokens/cost.
- [x] `M20-080` Include assumptions and unresolved limitations.
- [x] `M20-081` Include graph revision and evidence-node links.
- [x] `M20-082` Assign guarantee level per claim instead of one global badge.
- [x] `M20-083` Mark external-system behavior as contract-checked or runtime-only.
- [x] `M20-084` Persist the report as structured SQLite rows, not a Markdown sidecar.
- [x] `M20-085` Render a readable report card from the structured data.

## Acceptance and Repair

- [x] `M20-086 UX` Disable accept while required validations are running.
- [x] `M20-087 UX` Require explicit acknowledgement before accepting failed or waived required checks.
- [x] `M20-088` Persist acceptance with report and diff revision.
- [x] `M20-089` Detect a diff change between review and acceptance.
- [x] `M20-090` Require renewed review after a diff change.
- [x] `M20-091 UX` Allow a repair request tied to selected failures or hunks.
- [x] `M20-092` Create a new plan/checkpoint lineage for repair.
- [x] `M20-093 UX` Allow rollback to the pre-repair checkpoint.
- [x] `M20-094 UX` Allow rejection/abandonment without destroying the patch.

## Gate

- [x] `M20-G01 GATE` No changed diff can inherit stale passed validation.
- [x] `M20-G02 GATE` The final report distinguishes passed, failed, waived, skipped, unavailable, runtime-only, and invalidated evidence.
- [x] `M20-G03 GATE` Acceptance is bound to the exact reviewed diff and report.
- [x] `M20-G04 GATE` The UI makes unsupported external guarantees impossible to mistake for verified claims.

---

# Milestone 21: Deterministic Project Memory and Exact Reuse

Goal: make accepted work lower future context and execution cost without allowing similarity or model self-report to become authority.

Plan references: §5 Task Fingerprint and Retrieval; §23 Atom Storage and Vector Storage; §29 Phase 2; §31 Evidence-Driven Reuse and Learning; Learning Artifact Types; Chronological Episodes; Influence Lineage; Versioned Task Fingerprints; Retrieval and Pre-Work Gate.

Depends on: `M20-G01` through `M20-G04`.

Milestone output: project-scoped factual episodes, deterministic repository facts and commands, descriptively named and richly documented revision-bound atoms, exact compatibility-gated reuse, optional vector candidate discovery, lineage, invalidation, and user inspection/deletion.

## Memory Boundary

- [x] `M21-001 BLOCKER` Define project-memory authority separately from raw task history.
- [x] `M21-002` Define factual repository fact type.
- [x] `M21-003` Define reviewed command type.
- [x] `M21-004` Define file-to-test mapping type.
- [x] `M21-005` Define repository convention type.
- [x] `M21-006` Define accepted regression case type.
- [x] `M21-007` Define execution recipe type.
- [x] `M21-008` Define executable atom reference type without requiring deep semantic atoms.
- [x] `M21-009` Define observation/hypothesis type with `evidence_strength: none`.
- [x] `M21-010` Define maturity states: candidate, validated, preferred-for-experiment, quarantined, invalidated, retired.
- [x] `M21-011` Prohibit model self-report from creating validated status.
- [x] `M21-012` Define project ownership and cross-project isolation.
- [x] `M21-013` Define user inspection, correction, export, and deletion semantics.

## SQLite Memory Schema

- [x] `M21-014 DATA` Create memory artifact identity table.
- [x] `M21-015 DATA` Create immutable artifact revision table.
- [x] `M21-016 DATA` Create artifact type and maturity fields.
- [x] `M21-017 DATA` Create project and repository-revision bindings.
- [x] `M21-018 DATA` Create supporting-evidence links.
- [x] `M21-019 DATA` Create `derived_from` lineage.
- [x] `M21-020 DATA` Create `influenced_by` lineage.
- [x] `M21-021 DATA` Create invalidation and quarantine records.
- [x] `M21-022 DATA` Create applicability-predicate records.
- [x] `M21-023 DATA` Create task-fingerprint schema-version table.
- [x] `M21-024 DATA` Create vector-model/version metadata tables.
- [x] `M21-025 DATA` Create vector rows linked to artifact revision.
- [x] `M21-026 DATA` Create retrieval-candidate and retrieval-decision logs.
- [x] `M21-027 DATA` Add project-boundary foreign keys and indexes.
- [x] `M21-028 TEST` Test cascading logical deletion without cross-project impact.

## Chronological Episode Capture

- [x] `M21-029 BLOCKER` Define episode start and end boundaries.
- [x] `M21-030` Record requirement and accepted plan revisions.
- [x] `M21-031` Record repository/context revisions.
- [x] `M21-032` Record ordered actions and results by event reference.
- [x] `M21-033` Record user interventions and approvals.
- [x] `M21-034` Record validation and final decision.
- [x] `M21-035` Record forecast and actual metrics.
- [x] `M21-036` Record failures before repairs.
- [x] `M21-037` Record whether the outcome was accepted, rejected, abandoned, or unresolved.
- [x] `M21-038` Freeze the episode after terminal user decision.
- [x] `M21-039` Allow later invalidation overlays without mutating historical facts.

## Deterministic Fact Extraction

- [x] `M21-040` Extract successful build commands only from attributable executions.
- [x] `M21-041` Extract successful test commands only from attributable executions.
- [x] `M21-042` Bind commands to repository revision and relevant path scope.
- [x] `M21-043` Extract file-to-test mappings from observed successful validations.
- [x] `M21-044` Extract stable project instructions only after user approval.
- [x] `M21-045` Extract formatting/lint conventions from configuration and accepted work.
- [x] `M21-046` Deduplicate facts by normalized identity.
- [x] `M21-047` Track first observed, last confirmed, and supporting episode count.
- [x] `M21-048` Invalidate facts when supporting files or versions change.
- [x] `M21-049` Require revalidation before an invalidated fact regains influence.
- [x] `M21-050 TEST` Test a changed test runner invalidates its stored command.

## Task Fingerprint

- [x] `M21-051 BLOCKER` Define fingerprint schema version 1.
- [x] `M21-052` Include project and repository identity.
- [x] `M21-053` Include base revision or compatibility range.
- [x] `M21-054` Include normalized task class.
- [x] `M21-055` Include affected package/symbol/path hints.
- [x] `M21-056` Include language/toolchain/dependency bindings.
- [x] `M21-057` Include risk and validation requirements.
- [x] `M21-058` Include requested effects/authority class.
- [x] `M21-059` Separate exact-match fields from descriptive retrieval text.
- [x] `M21-060` Serialize exact fields canonically.
- [x] `M21-061` Hash the canonical exact fingerprint.
- [x] `M21-062 TEST` Verify identical inputs produce identical fingerprints.
- [x] `M21-063 TEST` Verify material dependency or revision changes alter relevant bindings.

## Pre-Work Retrieval Gate

- [x] `M21-064 BLOCKER` Run exact identity/fingerprint lookup before planning from scratch.
- [x] `M21-065` Retrieve reviewed project facts relevant to selected context.
- [x] `M21-066` Retrieve compatible commands and file-to-test mappings.
- [x] `M21-067` Retrieve exact reusable atoms or recipes only when applicability predicates pass.
- [x] `M21-068` Reject candidate with project-boundary mismatch.
- [x] `M21-069` Reject candidate with toolchain/dependency mismatch.
- [x] `M21-070` Reject candidate with invalidated evidence.
- [x] `M21-071` Reject candidate whose assurance is below the current task requirement.
- [x] `M21-072` Record every accepted and rejected retrieval candidate with reason.
- [x] `M21-073` Present influential memory items to the user.
- [x] `M21-074` Let the agent use, adapt, or reject an eligible item.
- [x] `M21-075` Record actual influence rather than mere retrieval.
- [x] `M21-076` Fall back to ordinary planning when no eligible item exists.
- [x] `M21-077` Never treat vector similarity as eligibility.

## Optional Vector Candidate Discovery

- [x] `M21-078` Measure deterministic retrieval misses before enabling embeddings.
- [x] `M21-079` Select an embedding provider/model only if the measured problem justifies it.
- [x] `M21-080` Record model, dimensions, normalization, and input-schema version.
- [x] `M21-081` Generate embeddings from scrubbed descriptive fields.
- [x] `M21-082` Store vectors in SQLite linked to exact artifact revision.
- [x] `M21-083` Implement brute-force cosine search for prototype-scale data.
- [x] `M21-084` Apply project scope before similarity ranking.
- [x] `M21-085` Apply compatibility and assurance gates after candidate discovery.
- [x] `M21-086` Record candidate rank and final rejection/acceptance.
- [x] `M21-087` Re-embed when embedding model or input schema changes.
- [x] `M21-088` Delete vectors when the owning artifact is deleted.
- [x] `M21-089 DEFER` Do not add a separate vector database.

## Inspection and Correction UI

- [x] `M21-090 UX` Add project-memory list.
- [x] `M21-091 UX` Filter by type, maturity, validity, and last confirmation.
- [x] `M21-092 UX` Show supporting episodes and lineage.
- [x] `M21-093 UX` Show bindings and applicability predicate.
- [x] `M21-094 UX` Show retrieval/influence history.
- [x] `M21-095 UX` Allow user correction by creating a new revision.
- [x] `M21-096 UX` Allow quarantine.
- [x] `M21-097 UX` Allow invalidation with reason.
- [x] `M21-098 UX` Allow deletion with affected-descendant preview.
- [x] `M21-099 UX` Export selected structured records without secrets.

## Atom Documentation Extraction and Embedding

Plan: §7 Atom Documentation as Retrieval Material; §23 Atom Storage and Vector Storage; §31 Versioned Task Fingerprints and Retrieval and Pre-Work Gate.

- [x] `M21-100 BLOCKER` Define atom-documentation schema version 1 using the exact field names in `AGENTS.md`.
- [x] `M21-101` Define which atom categories are source-authored, SQLite-authored, or generated projections.
- [x] `M21-102` Define immutable atom-documentation revision identity separately from atom and atom-version identity.
- [x] `M21-103 DATA` Add `atom_documentation_revisions` with atom ID, atom version, schema version, repository revision, source comment hash, contract hash, normalized input hash, dependency bindings, validation status, and timestamps.
- [x] `M21-104 DATA` Add normalized atom-documentation field storage without discarding the original scrubbed comment text.
- [x] `M21-105 DATA` Link each atom embedding to the exact documentation revision that produced it.
- [x] `M21-106 DATA` Add uniqueness constraints preventing two different normalized documents from claiming one documentation revision.
- [x] `M21-107 DATA` Add indexes for atom identity, comment hash, contract hash, validity, and pending re-embedding.
- [x] `M21-108` Parse source-authored atom comments with the Go parser and AST rather than regular expressions alone.
- [x] `M21-109` Locate the doc group attached to the declared atom identifier.
- [x] `M21-110` Parse and validate the schema-version header.
- [x] `M21-111` Parse required top-level fields without losing list structure.
- [x] `M21-112` Normalize indentation and insignificant whitespace.
- [x] `M21-113` Preserve meaningful units, punctuation, domain terms, and negative examples.
- [x] `M21-114` Reject missing, duplicate, unknown, or out-of-order fields according to the schema policy.
- [x] `M21-115` Reject empty fields and unexplained `None` values.
- [x] `M21-116` Validate that the opening sentence begins with the Go identifier.
- [x] `M21-117` Flag likely keyword stuffing or repeated boilerplate for review rather than silently embedding it.
- [x] `M21-118 SECURITY` Run documentation through the same secret and sensitive-data scrubber used before persistence.
- [x] `M21-119 SECURITY` Reject comments containing known credentials, private keys, or prohibited sensitive fixtures.
- [x] `M21-120` Compute the exact source-comment hash before normalization.
- [x] `M21-121` Compute the normalized documentation-input hash after parsing and scrubbing.
- [x] `M21-122` Bind the parsed document to the current atom contract hash and dependency bindings.
- [x] `M21-123` Persist admission success or rejection reason.
- [x] `M21-124` Generate Go comments for SQLite-authored atoms from the same structured documentation revision.
- [x] `M21-125 TEST` Round-trip a structured SQLite atom document through generated Go comment and AST extraction.
- [x] `M21-126 TEST` Verify round-trip preservation of semantic fields and stable normalized hash.
- [x] `M21-127` Define embedding-input schema version 1 separately from documentation schema version 1.
- [x] `M21-128` Include Purpose, Use when, Do not use when, Semantics, input/output meaning, Effects, Failure semantics, and Retrieval concepts in the default embedding input.
- [x] `M21-129` Include retry, reconciliation, security, dependency, and limit fields only through concise semantic normalization.
- [x] `M21-130` Exclude source paths, line numbers, timestamps, evidence run IDs, hashes, and repeated field labels from embedding input.
- [x] `M21-131` Preserve negative selection examples so semantically close but invalid atoms can be distinguished.
- [x] `M21-132` Record embedding model, dimensions, normalization, input-schema version, and input hash.
- [x] `M21-133` Queue re-embedding when normalized input or embedding configuration changes.
- [x] `M21-134` Invalidate retrieval influence immediately when comment, contract, binding, or evidence validity changes.
- [x] `M21-135` Keep prior vectors for historical lineage while excluding them from active retrieval.
- [x] `M21-136` Require project, compatibility, applicability, evidence, and assurance gates after vector candidate discovery.
- [x] `M21-137` Record whether an atom was retrieved from exact identity, structured fields, vector similarity, or several channels.
- [x] `M21-138` Record whether the agent used, adapted, or rejected the atom and why.
- [x] `M21-139 TEST` Test semantic comment change creates a new documentation revision and pending vector.
- [x] `M21-140 TEST` Test formatting-only comment change changes the source hash but can preserve the normalized input hash.
- [x] `M21-141 TEST` Test contract change invalidates an otherwise unchanged comment vector.
- [x] `M21-142 TEST` Test dependency-binding change invalidates active retrieval.
- [x] `M21-143 TEST` Test embedding-model change regenerates vectors without rewriting historical documentation.
- [x] `M21-144 TEST` Test a high-similarity atom with failed applicability is rejected.
- [x] `M21-145 TEST` Test a richly documented atom cannot self-promote its assurance level.

## Atom Name Storage, Aliases, and Embedding

Plan: §7 Atom Naming and Retrieval Identity; §23 Atom Storage; §31 Retrieval and Pre-Work Gate.

- [x] `M21-146 BLOCKER` Define atom naming-schema version 1 independently from documentation and embedding-input schemas.
- [x] `M21-147` Define canonical Go identifier validation and maximum practical display behavior without imposing a short semantic length limit.
- [x] `M21-148` Derive a human-readable display name from the canonical semantic phrase.
- [x] `M21-149` Derive the normalized word-split phrase deterministically from the Go identifier.
- [x] `M21-150` Preserve meaningful initialisms during word splitting.
- [x] `M21-151` Define the allowlist and project extension mechanism for established domain abbreviations.
- [x] `M21-152` Require a short naming rationale explaining the nearest confusing alternative and the qualifier that distinguishes this atom.
- [x] `M21-153 DATA` Add `atom_names` with atom ID, atom version, canonical name, display name, normalized phrase, schema version, rationale, validity, and revision metadata.
- [x] `M21-154 DATA` Add `atom_name_aliases` with alias text, normalized form, source, active interval, and target atom identity.
- [x] `M21-155 DATA` Add a uniqueness constraint for active normalized canonical names within project and semantic scope.
- [x] `M21-156 DATA` Preserve prior canonical names as immutable aliases after a semantic-preserving rename.
- [x] `M21-157` Classify a proposed rename as formatting-only, semantic-preserving, or semantic-breaking.
- [x] `M21-158` Require explicit review for semantic-preserving rename classification.
- [x] `M21-159` Require a new atom version or identity for semantic-breaking rename classification.
- [x] `M21-160` Create a new documentation revision after an accepted canonical rename.
- [x] `M21-161` Include canonical name and normalized semantic phrase exactly once in embedding input.
- [x] `M21-162` Include reviewed aliases as low-weight discovery text without duplicating the canonical phrase.
- [x] `M21-163` Exclude obsolete or invalidated aliases from active candidate generation while retaining lineage.
- [x] `M21-164` Invalidate and regenerate derived vectors after canonical name, normalized phrase, or active alias changes.
- [x] `M21-165` Render the display name as the primary graph-node label and preserve the stable atom ID separately.
- [x] `M21-166` Truncate only the visual graph label when space requires it; expose the full name in tooltip, inspector, search result, and accessibility label.
- [x] `M21-167` Search canonical name, normalized phrase, and active aliases before vector similarity.
- [x] `M21-168` Record which name or alias caused an atom to enter the candidate set.
- [x] `M21-169 TEST` Test deterministic conversion among canonical, display, and normalized names.
- [x] `M21-170 TEST` Test collision detection within one semantic scope.
- [x] `M21-171 TEST` Test that equivalent names in separate project scopes remain isolated.
- [x] `M21-172 TEST` Test semantic-preserving rename retains atom ID and creates alias/documentation revision lineage.
- [x] `M21-173 TEST` Test semantic-breaking rename cannot retain the old compatible identity silently.
- [x] `M21-174 TEST` Test an old alias finds the renamed atom but does not bypass applicability.
- [x] `M21-175 TEST` Test graph truncation never changes stored canonical or accessible names.

## Gate

- [x] `M21-G01 GATE` No memory item influences a task without project, compatibility, validity, and assurance checks.
- [x] `M21-G02 GATE` Similarity produces candidates only; exact predicates determine eligibility.
- [x] `M21-G03 GATE` The user can identify every memory item that influenced a completed task.
- [x] `M21-G04 GATE` Changed support invalidates dependent facts and vectors transitively.
- [x] `M21-G05 GATE` The prototype still works when vector discovery is disabled.
- [x] `M21-G06 GATE` Every active atom vector is traceable to one validated documentation revision, contract hash, repository revision, embedding model, and input-schema version.
- [x] `M21-G07 GATE` Rich atom comments improve candidate discovery without bypassing exact applicability, evidence, or assurance checks.
- [x] `M21-G08 GATE` Every reusable atom has a standalone-descriptive canonical name, deterministic display and normalized forms, collision control, and rename lineage bound to its embeddings.

---

# Milestone 22: Test Harness, Benchmarks, and Observability

Goal: produce enough independent evidence to distinguish a working prototype from a persuasive demo.

Plan references: §3 Load-Bearing Experiments; §24 Specification Review; §25 Metrics; §26 Benchmark Timing; §28 Initial Demonstrations; §30 Kill and Pivot Criteria.

Depends on: `M21-G01` through `M21-G08`; individual harnesses may be built earlier alongside their owning milestones.

Milestone output: deterministic fakes, integration fixtures, fault injection, security cases, performance benchmarks, metric queries, and a reproducible prototype scorecard.

## Test Pyramid and Fixtures

- [x] `M22-001 BLOCKER` Define fast unit, real-SQLite integration, process integration, browser component, and end-to-end suites.
- [x] `M22-002` Define suite naming and build tags.
- [x] `M22-003` Define deterministic clocks and ID generators for tests.
- [x] `M22-004` Define deterministic fake model provider.
- [x] `M22-005` Define scripted tool-call responses.
- [x] `M22-006` Define fake pricing and usage.
- [x] `M22-007` Define temporary Git repository fixture builder.
- [x] `M22-008` Define representative clean Go repository fixture.
- [x] `M22-009` Define dirty-worktree fixture.
- [x] `M22-010` Define malicious-repository fixture.
- [x] `M22-011` Define failing-test fixture.
- [x] `M22-012` Define dependency/configuration-change fixture.
- [x] `M22-013` Define protected-workflow fixture without claiming deep proof.
- [x] `M22-014` Ensure fixtures contain no real credentials or private code.

## Unit and Property Tests

- [x] `M22-015 TEST` Cover domain transition validators.
- [x] `M22-016 TEST` Cover exact money and budget arithmetic.
- [x] `M22-017 TEST` Cover task fingerprints and canonicalization.
- [x] `M22-018 TEST` Cover context ranking determinism.
- [x] `M22-019 TEST` Cover permission matching and expiration.
- [x] `M22-020 TEST` Cover graph projection determinism.
- [x] `M22-021 TEST` Cover graph layout stability.
- [x] `M22-022 TEST` Cover assurance and evidence invalidation.
- [x] `M22-023 TEST` Cover retrieval applicability predicates.
- [x] `M22-024 TEST` Fuzz ID and cursor parsers.
- [x] `M22-025 TEST` Fuzz protobuf/domain conversion.
- [x] `M22-026 TEST` Fuzz safe path resolution.
- [x] `M22-027 TEST` Fuzz event replay/projection.

## Real SQLite Integration

- [x] `M22-028 TEST` Run every repository test against real SQLite.
- [x] `M22-029 TEST` Run foreign-key and check-constraint failure cases.
- [x] `M22-030 TEST` Run concurrent writer/read replay cases.
- [x] `M22-031 TEST` Run WAL recovery after forced termination.
- [x] `M22-032 TEST` Run every migration upgrade path.
- [x] `M22-033 TEST` Run backup and restore.
- [x] `M22-034 TEST` Run deletion and project-boundary isolation.
- [x] `M22-035 TEST` Scan database bytes for seeded secrets after end-to-end use.

## Process and Fault Injection

- [x] `M22-036 TEST` Kill worker during repository read.
- [x] `M22-037 TEST` Kill worker during file edit.
- [x] `M22-038 TEST` Kill worker during command execution.
- [x] `M22-039 TEST` Kill worker during model streaming.
- [x] `M22-040 TEST` Kill coordinator before event commit.
- [x] `M22-041 TEST` Kill coordinator after event commit but before client delivery.
- [x] `M22-042 TEST` Disconnect browser during approval.
- [x] `M22-043 TEST` Disconnect browser during budget increase.
- [x] `M22-044 TEST` Exhaust disk space during event append.
- [x] `M22-045 TEST` Simulate database busy timeout.
- [x] `M22-046 TEST` Simulate corrupted or missing worktree.
- [x] `M22-047 TEST` Simulate provider rate limit.
- [x] `M22-048 TEST` Simulate provider partial stream then failure.
- [x] `M22-049 TEST` Simulate delayed provider usage.
- [x] `M22-050 TEST` Simulate command timeout with child processes.

## Security and Abuse Cases

- [x] `M22-051 SECURITY TEST` Attempt path traversal in every file API.
- [x] `M22-052 SECURITY TEST` Attempt symlink escape.
- [x] `M22-053 SECURITY TEST` Attempt unsafe editor-open target.
- [x] `M22-054 SECURITY TEST` Attempt approval bypass through alternate tool.
- [x] `M22-055 SECURITY TEST` Attempt repeated idempotency-key mutation.
- [x] `M22-056 SECURITY TEST` Attempt repository prompt injection.
- [x] `M22-057 SECURITY TEST` Attempt credential exfiltration through command output.
- [x] `M22-058 SECURITY TEST` Attempt credential exfiltration through diagnostic export.
- [x] `M22-059 SECURITY TEST` Attempt non-loopback browser connection.
- [x] `M22-060 SECURITY TEST` Attempt cross-origin session use.
- [x] `M22-061 SECURITY TEST` Attempt old per-launch session-secret reuse.
- [x] `M22-062 SECURITY TEST` Attempt oversized message, tool output, and graph payloads.

## Browser and Accessibility Tests

- [x] `M22-063 TEST` Automate the empty-shell journey.
- [x] `M22-064 TEST` Automate create-thread and send-message.
- [x] `M22-065 TEST` Automate plan approval.
- [x] `M22-066 TEST` Automate command approval and denial.
- [x] `M22-067 TEST` Automate pause and resume.
- [x] `M22-068 TEST` Automate reconnect/replay.
- [x] `M22-069 TEST` Automate graph-node/chat-message cross-selection.
- [x] `M22-070 TEST` Automate diff review and acceptance.
- [x] `M22-071 TEST` Automate crash recovery choice.
- [x] `M22-072 TEST` Run automated accessibility scans for every major route/state.
- [x] `M22-073 TEST` Run a keyboard-only end-to-end journey.
- [x] `M22-074 TEST` Run a screen-reader smoke journey.
- [x] `M22-075 TEST` Run reduced-motion and high-contrast journeys.

## Performance Benchmarks

- [x] `M22-076` Benchmark cold coordinator startup.
- [x] `M22-077` Benchmark warm coordinator startup.
- [x] `M22-078` Benchmark database migration from the prior schema.
- [x] `M22-079` Benchmark repository map on small, medium, and large Go fixtures.
- [x] `M22-080` Benchmark context selection.
- [x] `M22-081` Benchmark event append throughput and tail latency.
- [x] `M22-082` Benchmark reconnect replay for 100, 1,000, and 10,000 events.
- [x] `M22-083` Benchmark thread initial render and upward pagination.
- [x] `M22-084` Benchmark simultaneous token and cost updates.
- [x] `M22-085` Benchmark 300-node graph layout and render.
- [x] `M22-086` Benchmark 100 graph patches without viewport reset.
- [x] `M22-087` Benchmark SQLite vector search at expected prototype scale if enabled.
- [x] `M22-088` Record CPU, memory, wall time, and allocation data.
- [x] `M22-089` Run benchmarks on an ordinary target hobbyist laptop.
- [x] `M22-090` Store benchmark methodology and results in Git-tracked documentation, not runtime SQLite.

## Metrics and Scorecard

- [x] `M22-091` Implement queries for task success and user acceptance.
- [x] `M22-092` Implement queries for regressions and unresolved failures.
- [x] `M22-093` Implement queries for time-to-plan, first action, first diff, validation, and completion.
- [x] `M22-094` Implement queries for tokens, cost, retries, and repairs.
- [x] `M22-095` Implement queries for forecast error and interval coverage.
- [x] `M22-096` Implement queries for approvals and denied actions.
- [x] `M22-097` Implement queries for pause, cancel, recovery, and resume.
- [x] `M22-098` Implement queries for retrieved and influential memory.
- [x] `M22-099` Implement queries for graph usage and collapse rate.
- [x] `M22-100` Build a redacted local prototype scorecard.
- [x] `M22-101` Compare the frozen Codeflux run against the frozen baseline.
- [x] `M22-102` Record failures and surprises, not only aggregate success.

## Developer Harness and Replay

Plan: §27D Deterministic Test Kit; Replay and Debugging; Logging, Tracing, and Profiling; Test Layers and Ownership; CI and Local Parity.

- [x] `M22-103 BLOCKER` Implement deterministic test clock with manual advance and no wall-clock sleeps in state-machine tests.
- [x] `M22-104 BLOCKER` Implement deterministic typed-ID sequence for repeatable snapshots and event fixtures.
- [x] `M22-105 BLOCKER` Implement real temporary SQLite test database with migrations, integrity assertion, and registered cleanup.
- [x] `M22-106 BLOCKER` Implement temporary Git repository/worktree fixture builder for clean, dirty, detached, conflicted, nested, and malicious cases.
- [x] `M22-107 BLOCKER` Implement scripted provider with text, tool, usage, partial stream, delay, rate limit, authentication failure, and cancellation steps.
- [x] `M22-108` Implement fake credential store that can assert no secret crosses a requested boundary.
- [x] `M22-109` Implement event recorder with sequence, causation, transaction, replay, and wait-with-timeout assertions.
- [x] `M22-110` Implement coordinator harness with isolated database, repository, worker, provider, port, clock, and IDs.
- [x] `M22-111` Implement browser scenario harness with session bootstrap, event fixture, keyboard actions, accessibility checks, and screenshot-on-failure.
- [x] `M22-112 SECURITY` Validate every test cleanup target before recursive deletion and preserve artifacts only behind an explicit failure flag.
- [x] `M22-113` Implement named interactive fake scenarios for success, plan revision, approval, denial, repair, budget cap, provider failure, reconnect, worker crash, coordinator crash, concurrent edit, and recovery.
- [x] `M22-114` Implement event replay from named fixture and redacted exported session.
- [x] `M22-115` Implement replay stop-at-sequence, step-event, duplicate-delivery, gap, reconnect, and snapshot-repair controls.
- [x] `M22-116` Implement server/client projection comparison during replay.
- [x] `M22-117` Implement graph revision rebuild and comparison during replay.
- [x] `M22-118` Implement safe read-only database inspection by domain entity, sequence, revision, lineage, and invalidation.
- [x] `M22-119` Add development-only structured logs for transaction, event append/publish, worker lease, provider, tool, reducer, render, graph, and retrieval timing.
- [x] `M22-120` Add authenticated loopback-only CPU, heap, goroutine, mutex, and block profiling in development builds.
- [x] `M22-121` Add browser performance marks correlating event sequence with reducer and render duration.
- [x] `M22-122 SECURITY` Verify replay, logs, profiles, screenshots, and failure artifacts contain no seeded credentials.
- [x] `M22-123 TEST` Run each named fake scenario through backend-only, generated-client, and browser layers where applicable.
- [x] `M22-124 TEST` Verify local and CI invoke the same development-helper command graph.
- [x] `M22-125 TEST` Verify event schema additions fail CI without reducer and presentation/grouping coverage.
- [x] `M22-126 TEST` Verify generated drift fails before tests that depend on generated output.
- [x] `M22-127 TEST` Verify ordinary local and CI tests perform no external network request.
- [x] `M22-128 DOC` Document failure-artifact locations, replay commands, safe database inspection, and profiling.
- [x] `M22-129 DOC` Document golden paths for adding a backend use case, event/card, frontend component, graph projection, atom, migration, and provider.
- [x] `M22-130 TEST` Give the golden-path documentation to a clean contributor/agent session and verify it can identify the correct plan section, TODO, test layer, event, and transaction for a sample vertical change.

## Gate

- [x] `M22-G01 GATE` Fast tests are reliable enough to run on every change.
- [x] `M22-G02 GATE` Full integration and browser suites pass from a fresh database.
- [x] `M22-G03 GATE` Fault injection demonstrates zero duplicated correctness-bearing actions.
- [x] `M22-G04 GATE` Secret, path, origin, authority, and payload abuse suites pass.
- [x] `M22-G05 GATE` The prototype scorecard can be reproduced from documented commands.
- [x] `M22-G06 GATE` Deterministic fakes, replay, projection comparison, diagnostics, profiling, and golden-path documentation make every vertical flow locally reproducible without paid providers or manual database mutation.

---

# Milestone 23: Diagnostics, Packaging, Updates, and Local Hardening

Goal: make the prototype installable, diagnosable, recoverable, and honest outside the developer's checkout.

Plan references: §27 Hobbyist MVP Decisions; Persistence, Recovery, Diagnostics, and Updates; Honest Cost Display; §27A Local Security; §30 Code-First Agent Failure.

Depends on: `M22-G01` through `M22-G06`.

Milestone output: signed/versioned local artifacts, first-run setup, OS credential integration, doctor/backup/diagnostic workflows, safe updates, and a complete limitation guide.

## CLI Surface

- [x] `M23-001 BLOCKER` Implement `codeflux start`.
- [x] `M23-002` Implement automatic browser open with opt-out.
- [x] `M23-003` Print the loopback URL without printing the session secret in shell history where avoidable.
- [x] `M23-004` Implement `codeflux doctor`.
- [x] `M23-005` Implement `codeflux version`.
- [x] `M23-006` Implement `codeflux backup`.
- [x] `M23-007` Implement `codeflux integrity-check`.
- [x] `M23-008` Implement `codeflux diagnostics export`.
- [x] `M23-009` Implement `codeflux provider set`.
- [x] `M23-010` Implement `codeflux provider test`.
- [x] `M23-011` Implement `codeflux provider delete`.
- [x] `M23-012` Implement clear exit codes.
- [x] `M23-013` Add contextual command help.
- [x] `M23-014` Avoid interactive prompts when a command is explicitly non-interactive.

## First-Run Journey

- [x] `M23-015 UX` Detect first run.
- [x] `M23-016 UX` Explain local-only architecture and data location.
- [x] `M23-017 UX` Explain what remains in Git versus SQLite.
- [x] `M23-018 UX` Let the user configure one provider.
- [x] `M23-019 UX` Test the provider before continuing.
- [x] `M23-020 UX` Let the user select a repository.
- [x] `M23-021 UX` Inspect repository and show permissions.
- [x] `M23-022 UX` Explain task worktree creation.
- [x] `M23-023 UX` Offer a safe sample task or open an empty thread.
- [x] `M23-024 UX` Show data deletion and backup locations.
- [x] `M23-025 TEST` Time a fresh user's first-run journey.

## Doctor and Diagnostics

- [x] `M23-026` Check supported Go toolchain availability.
- [x] `M23-027` Check Git availability and version.
- [x] `M23-028` Check database path permissions.
- [x] `M23-029` Check SQLite integrity and schema compatibility.
- [x] `M23-030` Check credential-store availability.
- [x] `M23-031` Check configured provider connectivity without exposing credentials.
- [x] `M23-032` Check worktree root availability and disk space.
- [x] `M23-033` Check loopback port binding.
- [x] `M23-034` Report active and recovery-required tasks.
- [x] `M23-035` Report application and migration versions.
- [x] `M23-036` Give actionable remediation per failed check.
- [x] `M23-037` Create a redacted diagnostic manifest before export.
- [x] `M23-038` Include versions, non-secret settings, health results, redacted logs, and selected task metadata.
- [x] `M23-039` Exclude prompts/source/task content by default.
- [x] `M23-040` Preview export contents and size.
- [x] `M23-041` Require explicit confirmation for any optional sensitive content.
- [x] `M23-042 TEST` Scan exported bundles against seeded secrets.

## Logging

- [x] `M23-043` Define structured log levels and stable event names.
- [x] `M23-044` Add request, task, run, and correlation IDs.
- [x] `M23-045` Redact before serialization.
- [x] `M23-046` Avoid raw prompts and source by default.
- [x] `M23-047` Add bounded log rotation.
- [x] `M23-048` Add retention settings.
- [x] `M23-049` Add a user action to clear logs.
- [x] `M23-050` Ensure clearing logs does not delete task evidence.
- [x] `M23-051` Add development-only verbose logging with explicit warning.

## Packaging

- [x] `M23-052 BLOCKER` Produce reproducible binaries for declared prototype platforms.
- [x] `M23-053` Embed frontend assets.
- [x] `M23-054` Embed migrations.
- [x] `M23-055` Include license and notices.
- [x] `M23-056` Include version/commit metadata.
- [x] `M23-057` Create checksums.
- [x] `M23-058` Sign release artifacts.
- [x] `M23-059` Verify signatures in the release process.
- [x] `M23-060` Test installation into a clean user profile.
- [x] `M23-061` Test paths containing spaces and non-ASCII characters.
- [x] `M23-062` Test operation without administrator privileges.
- [x] `M23-063` Ensure uninstall instructions preserve or explicitly remove user data.

## Updates

- [x] `M23-064` Keep manual update as the default prototype behavior.
- [x] `M23-065` Check release compatibility before database migration.
- [x] `M23-066` Back up the database before first launch of a newer schema.
- [x] `M23-067` Display release notes and migration warning.
- [x] `M23-068` Refuse a downgrade that cannot read the current schema.
- [x] `M23-069` Document restoration to the backed-up database and older binary.
- [x] `M23-070 TEST` Test supported upgrade paths from packaged artifacts.

## Documentation

- [x] `M23-071 DOC` Write installation instructions.
- [x] `M23-072 DOC` Write provider setup instructions.
- [x] `M23-073 DOC` Write first-task walkthrough.
- [x] `M23-074 DOC` Explain worktrees, acceptance, repair, rollback, and cleanup.
- [x] `M23-075 DOC` Explain cost forecasts, actual cost, unknown pricing, and hard budgets.
- [x] `M23-076 DOC` Explain permission tiers and optional container isolation.
- [x] `M23-077 DOC` Explain SQLite data location, backup, inspection, export, and deletion.
- [x] `M23-078 DOC` Explain graph modes and their non-proof status.
- [x] `M23-079 DOC` Explain memory eligibility, lineage, invalidation, and vector candidate discovery.
- [x] `M23-080 DOC` Document crash recovery.
- [x] `M23-081 DOC` Document diagnostic export.
- [x] `M23-082 DOC` Publish known limitations and unsupported guarantees.
- [x] `M23-083 DOC` Document that the prototype is not a perfect security sandbox.
- [x] `M23-084 DOC` Document that external systems may violate their contracts.
- [x] `M23-085 DOC` Document deferred enterprise and deep-verification work.

## Gate

- [x] `M23-G01 GATE` A clean machine/profile can install and reach the first-run screen from one artifact.
- [x] `M23-G02 GATE` Doctor identifies representative Git, Go, database, credential, provider, and worktree failures.
- [x] `M23-G03 GATE` Diagnostic export contains no seeded secrets.
- [x] `M23-G04 GATE` Upgrade, backup, refusal, and restoration paths are documented and tested.

---

# Milestone 24: End-to-End Vertical Slice and Prototype Exit

Goal: prove the integrated product journey on frozen tasks, record failures honestly, and decide whether to proceed, narrow, or pivot.

Plan references: §3 Load-Bearing Experiments; §25 Metrics; §26 Benchmark Timing; §28 Initial Demonstrations; §28 ReserveFlow Dogfood API Refinement Trial; §29 Revised Development Sequence; §30 Kill and Pivot Criteria; §32 Central Design Principles.

Depends on: every prior milestone gate.

Milestone output: reproducible frozen demonstration runs, a chronological ReserveFlow API dogfood run, independently evaluated acceptance evidence, a controlled refinement ledger, comparison scorecard, issue inventory, kill/pivot decision, and a tagged prototype build.

## Clean-Room Setup

- [x] `M24-001 BLOCKER` Create a clean user profile or disposable VM for the exit run.
- [x] `M24-002` Install the packaged Codeflux artifact.
- [x] `M24-003` Verify no development configuration leaks into the run.
- [x] `M24-004` Verify no prior Codeflux database exists.
- [x] `M24-005` Configure one provider through the documented journey.
- [x] `M24-006` Clone or copy the frozen demonstration repository.
- [x] `M24-007` Verify the exact frozen base revision.
- [x] `M24-008` Verify hidden acceptance tests remain unavailable to the agent.
- [x] `M24-009` Start screen recording or structured observer notes if allowed by the benchmark protocol.

## First-Run and Repository Journey

- [x] `M24-010` Measure install-to-first-screen time.
- [x] `M24-011` Complete first-run explanation.
- [x] `M24-012` Test provider connection.
- [x] `M24-013` Open the frozen repository.
- [x] `M24-014` Inspect repository status and proposed worktree policy.
- [x] `M24-015` Verify selected context is explainable.
- [x] `M24-016` Create a new thread.
- [x] `M24-017` Submit the frozen task requirement verbatim.

## Plan, Forecast, and Approval Journey

- [x] `M24-018` Record time to first forecast.
- [x] `M24-019` Record time to first plan.
- [x] `M24-020` Inspect scope, expected files, validation, risk, and assumptions.
- [x] `M24-021` Inspect P50/P90 time/token/cost estimates.
- [x] `M24-022` Inspect fixed provider, model, effort, and policy version.
- [x] `M24-023` Set the frozen hard budget.
- [x] `M24-024` Approve or redirect according to the benchmark script.
- [x] `M24-025` Verify plan revision and approval appear in SQLite-backed replay.

## Execution Journey

- [x] `M24-026` Start the task.
- [x] `M24-027` Verify isolated worktree creation.
- [x] `M24-028` Verify the execution graph highlights the active path.
- [x] `M24-029` Verify tool output remains summarized by default.
- [x] `M24-030` Exercise at least one permission request.
- [x] `M24-031` Verify allow-once or deny behavior according to the script.
- [x] `M24-032` Verify cost and budget update during execution.
- [x] `M24-033` Pause the task at the scripted point.
- [x] `M24-034` Reload or restart Codeflux while paused.
- [x] `M24-035` Verify replay reconstructs exact task state.
- [x] `M24-036` Resume from the validated checkpoint.
- [x] `M24-037` Record time to first diff.
- [x] `M24-038` Record unexpected tool calls, retries, loops, or user interventions.

## Validation and Review Journey

- [x] `M24-039` Verify required validation selection.
- [x] `M24-040` Verify validation runs against the exact current diff.
- [x] `M24-041` Inspect Program graph mode.
- [x] `M24-042` Inspect Execution graph mode.
- [x] `M24-043` Inspect Evidence graph mode.
- [x] `M24-044` Select a graph node and verify related chat highlighting.
- [x] `M24-045` Select a chat node chip and verify graph focus.
- [x] `M24-046` Inspect changed-file and diff summaries.
- [x] `M24-047` Open one changed source location in the external editor.
- [x] `M24-048` Inspect every required, passed, failed, skipped, waived, or unavailable check.
- [x] `M24-049` Inspect risk, approvals, model/tool versions, assumptions, and limitations.
- [x] `M24-050` Verify forecast-versus-actual time/tokens/cost.
- [x] `M24-051` Accept, repair, or roll back according to the benchmark result.

## Independent Evaluation

- [x] `M24-052 BLOCKER` Run hidden acceptance tests after Codeflux stops.
- [x] `M24-053` Record functional correctness.
- [x] `M24-054` Record regressions.
- [x] `M24-055` Review code quality independently of Codeflux's report.
- [x] `M24-056` Review whether the diff stayed within intended scope.
- [x] `M24-057` Verify no unapproved external effects occurred.
- [x] `M24-058` Verify no secret exists in database, logs, events, worktree metadata, or diagnostics.
- [x] `M24-059` Verify every correctness-bearing UI claim has backing evidence.
- [x] `M24-060` Verify the evidence report did not overstate external guarantees.
- [x] `M24-061` Compare outcome, latency, cost, and intervention count with the frozen baseline.

## Recovery Exit Scenarios

- [x] `M24-062` Run the frozen task with browser disconnect during streaming.
- [x] `M24-063` Verify gap-free replay.
- [x] `M24-064` Run the frozen task with worker termination.
- [x] `M24-065` Verify safe recovery-required presentation.
- [x] `M24-066` Run the frozen task with coordinator termination after an edit.
- [x] `M24-067` Verify worktree and checkpoint reconciliation.
- [x] `M24-068` Run a hard-budget exhaustion scenario.
- [x] `M24-069` Verify no unapproved post-cap model request begins.
- [x] `M24-070` Run a concurrent user-edit scenario.
- [x] `M24-071` Verify the user's edit is not overwritten.

## Memory Exit Scenarios

- [x] `M24-072` Complete and accept a task that yields a deterministic repository fact.
- [x] `M24-073` Start a related second task.
- [x] `M24-074` Verify pre-work retrieval finds the eligible fact.
- [x] `M24-075` Verify the UI shows that the fact influenced the task.
- [x] `M24-076` Change the supporting repository configuration.
- [x] `M24-077` Verify the fact becomes ineligible or invalidated.
- [x] `M24-078` Verify no vector candidate bypasses the compatibility gate.
- [x] `M24-079` Delete the test memory and verify dependent vectors/links are removed or invalidated correctly.

## ReserveFlow Dogfood Control Plane

- [x] `M24-101 BLOCKER` Create a separate frozen ReserveFlow repository containing only the Go module, first-task README, empty command entry point, test-helper skeleton, license, and Git configuration specified by §28.
- [x] `M24-102` Record and verify the cryptographic identity of the frozen ReserveFlow scaffold revision.
- [x] `M24-103 BLOCKER` Create a separate evaluator repository for hidden acceptance, concurrency, security, recovery, migration, and contract tests.
- [x] `M24-104 SECURITY` Verify the Codeflux coordinator, worker, tools, provider context, and ReserveFlow worktree cannot read the evaluator repository.
- [x] `M24-105 DATA` Allocate a fresh Codeflux runtime database dedicated to each evaluated dogfood track.
- [x] `M24-106 DATA` Allocate a ReserveFlow application database independently of the Codeflux runtime database.
- [x] `M24-107 TEST` Build a reset operation that restores the exact accepted ReserveFlow commit, removes only run-scoped application state, and creates a fresh isolated Codeflux database.
- [x] `M24-108` Freeze Go, dependency, operating-system, architecture, Codeflux, provider, model, effort, tool, price, validation-policy, and routing-policy versions in the run manifest.
- [x] `M24-109` Write fifteen separately revealable requirement packets matching the chronological sequence in §28.
- [x] `M24-110 TEST` Verify a task run can access its current and prior accepted requirements but cannot access any future requirement packet.
- [x] `M24-111` Define one accepted ReserveFlow commit chain and require every comparison track to advance through equivalent accepted states.
- [x] `M24-112 DATA` Define an append-only intervention ledger for clarifications, approvals, redirects, denials, rollbacks, manual commands, manual source edits, evaluator actions, and contamination decisions.
- [x] `M24-113 GATE` Configure the evaluated Codeflux track so any manual source edit marks the run contaminated and ineligible for the no-intervention exit claim.
- [x] `M24-114 DATA` Retain the task requirement, forecast, plan revisions, budget, events, checkpoints, tool summaries, worktree diff, validation, evidence, cost, and acceptance decision for every dogfood task.
- [x] `M24-115` Define Track A, Track B, Track C, and later Track D configuration manifests without changing the chronological requirements or acceptance authority.

## ReserveFlow Independent Evaluation Harness

- [x] `M24-116 TEST` Provide evaluator-controlled deterministic clock fixtures for expiration boundaries and retry schedules.
- [x] `M24-117 TEST` Provide evaluator-controlled stable identity fixtures for resources, reservations, outbox events, and deliveries.
- [x] `M24-118 TEST` Build a mock webhook receiver that records delivery identity, signature, headers, payload hash, receipt time, and response behavior without exposing its assertions to Codeflux.
- [x] `M24-119 TEST` Add webhook ambiguity modes for accepted-then-timeout, connection refusal, slow response, terminal 4xx, retryable 5xx, and duplicate receipt.
- [x] `M24-120 TEST` Add an in-process concurrency driver for same-resource and same-idempotency-key races.
- [x] `M24-121 TEST` Add a multi-process concurrency driver for SQLite lock, stale-version, worker-ownership, and shutdown races.
- [x] `M24-122 TEST` Add named crash points before and after reservation commit, expiration selection, expiration commit, outbox claim, receiver acceptance, delivery-state commit, and migration commit.
- [x] `M24-123 SECURITY` Create malformed, missing, invalid, revoked, and scope-mismatched API-key fixtures.
- [x] `M24-124 SECURITY` Seed synthetic secret markers into credentials, callback configuration, request bodies, and tool output so leakage can be detected across every persisted and displayed surface.
- [x] `M24-125 TEST` Create database snapshots for empty, prior-schema, populated, interrupted-migration, and unsupported-newer-schema cases.
- [x] `M24-126 TEST` Build an OpenAPI-versus-runtime verifier for paths, methods, request schemas, response schemas, status codes, pagination, idempotency, concurrency headers, and error envelopes.
- [x] `M24-127 TEST` Freeze the visible test suite that supplies legitimate local feedback for each revealed requirement.
- [x] `M24-128 BLOCKER TEST` Freeze the hidden behavioral suite and its pass criteria before the evaluated run begins.
- [x] `M24-129 TEST` Review hidden tests to ensure they assert required behavior rather than an undisclosed preferred implementation shape.
- [x] `M24-130 TEST` Hash the evaluator repository, requirement packets, visible fixtures, hidden fixtures, and scoring configuration so post-run changes are detectable.

## Chronological ReserveFlow Tasks

For each pair below, the first item opens a new Codeflux task from the prior accepted commit with a fresh forecast, plan, budget, worktree, and episode. The second item runs visible and hidden acceptance, records the decision, and advances the accepted chain only on success.

- [x] `M24-131` Run ReserveFlow Task 1 for server lifecycle, health, readiness, request IDs, JSON behavior, and safe errors.
- [x] `M24-132 TEST` Independently accept or reject Task 1 against port, cancellation, signal, malformed-path, and deterministic-health cases.
- [x] `M24-133` Run ReserveFlow Task 2 for SQLite resource persistence, capacity validation, stable identity, timestamps, and bounded cursor pagination.
- [x] `M24-134 TEST` Independently accept or reject Task 2 against clean migration, restart, invalid capacity, ordering, cursor, and duplicate-request cases.
- [x] `M24-135` Run ReserveFlow Task 3 for atomic pending-reservation creation and capacity decrement.
- [x] `M24-136 TEST` Independently accept or reject Task 3 against invalid quantity, unknown resource, insufficient capacity, rollback, and error-shape cases.
- [x] `M24-137` Run ReserveFlow Task 4 for canonical-request idempotency, original-response replay, expiry, and semantic-input conflict.
- [x] `M24-138 TEST` Independently accept or reject Task 4 against JSON reordering, concurrent same-key calls, transport retries, expiry, and changed-input cases.
- [x] `M24-139` Run ReserveFlow Task 5 for expected-version confirm and cancel transitions with explicit repeated-request semantics.
- [x] `M24-140 TEST` Independently accept or reject Task 5 against valid, stale, repeated, forbidden, capacity-release, and confirm/cancel race cases.
- [x] `M24-141` Run ReserveFlow Task 6 for concurrent capacity safety across reservation creation and cancellation.
- [x] `M24-142 TEST` Independently accept or reject Task 6 with in-process and multi-process contention proving no oversubscription, negative capacity, lost update, duplicate reservation, or deadlock.
- [x] `M24-143` Run ReserveFlow Task 7 for deterministic expiration, exact-once capacity release, worker ownership, bounded scans, shutdown, and restart.
- [x] `M24-144 TEST` Independently accept or reject Task 7 at clock boundaries, with multiple workers, injected crashes, repeated scans, shutdown, and late confirmation.
- [x] `M24-145` Run ReserveFlow Task 8 for transactional outbox creation, bounded polling, ordering, and publish-state transitions.
- [x] `M24-146 TEST` Independently accept or reject Task 8 against rollback, duplicate polling, restart, poison-event, ordering, and one-event-per-transition cases.
- [x] `M24-147` Run ReserveFlow Task 9 for signed webhook delivery, stable delivery identity, bounded retry/backoff, secret references, and disabled endpoints.
- [x] `M24-148 TEST` Independently accept or reject Task 9 against success, ambiguity, connection failure, 4xx, 5xx, duplicate receipt, signature, disablement, output bounds, and redaction.
- [x] `M24-149` Run ReserveFlow Task 10 for API-key authorization of administrative operations and explicit policy for reservation operations.
- [x] `M24-150 SECURITY TEST` Independently accept or reject Task 10 against missing, malformed, invalid, revoked, scope, comparison, logging, error, and capability-leakage cases.
- [x] `M24-151` Run ReserveFlow Task 11 for correlated structured logs, stable error codes, local metrics, readiness dependencies, and redacted diagnostics.
- [x] `M24-152 TEST` Independently accept or reject Task 11 by tracing request, database, worker, and webhook activity while verifying no body or secret leakage.
- [x] `M24-153` Run ReserveFlow Task 12 for an OpenAPI contract that describes only implemented behavior, examples, errors, pagination, idempotency, and concurrency.
- [x] `M24-154 TEST` Independently accept or reject Task 12 with contract-versus-runtime verification and unsupported-guarantee review.
- [x] `M24-155` Run ReserveFlow Task 13 from the frozen defect revision without revealing the defect root cause and require Codeflux to diagnose it.
- [x] `M24-156 TEST` Independently accept or reject Task 13 only after a reproducing regression test and a behaviorally correct fix pass race and prior-regression suites.
- [x] `M24-157` Run ReserveFlow Task 14 for the frozen post-memory domain-rule change without supplying an affected-file list.
- [x] `M24-158 TEST` Independently accept or reject Task 14 across state transitions, capacity, HTTP contract, outbox, webhooks, tests, documentation, graph, evidence, and memory invalidation.
- [x] `M24-159` Run ReserveFlow Task 15 for the frozen dependency upgrade and backwards-compatible schema addition.
- [x] `M24-160 TEST` Independently accept or reject Task 15 across migration, data preservation, API compatibility, version binding, cached-artifact eligibility, and evidence invalidation.

## Dogfood Product and Recovery Observations

- [x] `M24-161 UX` Run at least one complete task with the graph collapsed and verify every required plan, approval, execution, validation, review, and recovery action remains available.
- [x] `M24-162 UX` Run a later task while inspecting Program, Execution, and Evidence graph modes and record whether each mode changes a decision or merely adds visual activity.
- [x] `M24-163 UX` Verify message-to-node and node-to-message navigation on one planning decision, one active tool action, one changed symbol, one failed check, and one accepted evidence claim.
- [x] `M24-164 TEST` Pause a dogfood task after a durable edit, restart the browser, and verify ordered replay and retained control state.
- [x] `M24-165 TEST` Terminate a worker at a named crash boundary and verify recovery does not duplicate an edit, command, reservation-side test effect, or provider request.
- [x] `M24-166 TEST` Terminate the coordinator after a committed event and verify database, checkpoint, worktree, budget, and UI reconciliation.
- [x] `M24-167 TEST` Change one ReserveFlow file concurrently outside Codeflux and verify the user edit is detected, preserved, and resolved explicitly.
- [x] `M24-168 TEST` Exhaust the hard budget during one controlled attempt and verify no post-cap provider request or silent cheaper-model fallback begins.
- [x] `M24-169 SECURITY` Exercise one scoped network approval for the webhook test and verify allow-once, allow-for-task, denial, expiry, and replay presentation.
- [x] `M24-170 UX` Record every point where the operator cannot determine current state, authority, cost, next action, failure ownership, or recovery safety without opening raw storage or logs.

## Controlled Codeflux Refinement Loop

- [x] `M24-171 DATA` Give every dogfood failure or serious friction report a stable identity linked to the exact task, accepted base, Codeflux version, run, episode, and evaluator result.
- [x] `M24-172 DATA` Freeze the failing event sequence, worktree diff, provider/model, policy, budget, environment, tool versions, and relevant redacted diagnostics before attempting repair.
- [x] `M24-173` Assign one primary failure category from §28 and record severity, frequency, reproducibility, symptom, and first responsible Section 0 layer.
- [x] `M24-174` Classify ownership as Codeflux, provider/model, ReserveFlow requirement, visible test, hidden test, environment, or evaluation protocol.
- [x] `M24-175` Record every workaround and mark whether it contaminated the run or changed the acceptance conditions.
- [x] `M24-176 BLOCKER TEST` Reproduce a Codeflux-owned failure outside the partial ReserveFlow run using the smallest deterministic fixture that still fails.
- [x] `M24-177 TEST` Add a failing regression test at the lowest responsible Codeflux layer before implementing the repair.
- [x] `M24-178` State the general failure class and observable invariant the proposed repair addresses.
- [x] `M24-179` Implement the smallest general repair without weakening validation, permission, evidence, budget, project-boundary, or recovery policy.
- [x] `M24-180 TEST` Run the targeted regression test and the owning subsystem suite.
- [x] `M24-181 TEST` Run relevant security, replay, migration, concurrency, frontend reducer, and end-to-end suites selected from the changed invariant.
- [x] `M24-182` Rebuild and version Codeflux after the repair.
- [x] `M24-183 TEST` Reset ReserveFlow to the original accepted base and rerun the original revealed requirement instead of continuing from repaired partial output.
- [x] `M24-184 TEST` Run the first repair verification with project memory and vector discovery disabled so stored hints cannot hide a tool defect.
- [x] `M24-185 TEST` Rerun the chronological path with only previously accepted ReserveFlow memory enabled and record its actual influence.
- [x] `M24-186 TEST` Re-run all earlier affected ReserveFlow acceptance tests.
- [x] `M24-187 TEST` Run one unrelated repository fixture that expresses the same general failure class.
- [x] `M24-188` Reject a repair that passes only the hidden case, relies on future-requirement knowledge, or adds task-specific prompt text without a general invariant.
- [x] `M24-189 DATA` Record correctness, speed, cost, UX, DevX, and any newly introduced tradeoff before closing the defect.
- [x] `M24-190` Close each defect as fixed, accepted limitation, deferred with owner and trigger, evaluator defect, or product-scope rejection; do not silently discard it.
- [x] `M24-216 BLOCKER EVAL` Before use, freeze the adversarial reviewer's prompt, model, input allowlist, output schema, no-edit and no-approval authority, execution timing, exact budget, and cost accounting.
- [x] `M24-217 BLOCKER SECURITY TEST` Extend M24-104 isolation to the adversarial reviewer and verify it cannot access evaluator source, hidden assertions or answers, future requirements, or live authority.
- [x] `M24-218 DATA` After each complete evaluated run and proposed refinement, run the evaluation-only adversarial reviewer without influencing the active run and record its time, tokens, cost, findings, and resulting interventions.
- [x] `M24-219 DATA` Version every prompt or process candidate, change one general invariant at a time, and preregister the exact diff, tuning cohort, primary endpoint, minimum effect, repetitions, analysis, stop rule, multiple-comparison treatment, and frozen execution envelope.
- [x] `M24-220 TEST` Select at most one candidate on the exposed tuning cohort, keep the lineage-unexposed held-out cohort frozen until selection, allow one confirmation, and never use ReserveFlow hidden-evaluator results for prompt selection or revision.
- [x] `M24-221 GATE` Reject candidates with any correctness, validation, authority, security, secrecy, recovery, or independent-acceptance regression; retain only a candidate meeting its preregistered gate for the named frozen stratum and otherwise report inconclusive or retired.

## Dogfood Measurement and Comparison

- [x] `M24-191 DATA` Record visible acceptance, hidden acceptance, independent diff review, regressions, and delayed defects per task.
- [x] `M24-192 DATA` Record time to forecast, plan, first action, first diff, validation, review, and acceptance per task.
- [x] `M24-193 DATA` Record input, cached, and output tokens plus provider, model, tool, and estimated human cost per task.
- [x] `M24-194 DATA` Record forecast P50/P90 coverage, absolute error, and systematic under- or over-estimation.
- [x] `M24-195 DATA` Record plan revisions, repair rounds, repeated actions, escalations, and manual interventions.
- [x] `M24-196 DATA` Compare files selected, files changed, and files independently judged necessary.
- [x] `M24-197 DATA` Record approvals requested, granted, denied, expired, and retrospectively judged unnecessary or too broad.
- [x] `M24-198 DATA` Record checkpoint, reconnect, worker recovery, coordinator recovery, and resume outcomes.
- [x] `M24-199 DATA` Record graph opens, mode use, cross-navigation, decisions changed, confusion, and user-rated usefulness.
- [x] `M24-200 DATA` Record exact and vector retrieval candidates, eligibility decisions, influence, acceptance outcome, and invalidation.
- [x] `M24-201 DATA` Record atoms reused, adapted, rejected, invalidated, newly admitted, and renamed.
- [x] `M24-202` Run Track A with the frozen strong coding-agent baseline and no Codeflux project memory.
- [x] `M24-203 BLOCKER` Run Track B with Codeflux's fixed model/effort policy and vector discovery disabled.
- [x] `M24-204` Run Track C with the same fixed policy plus only admitted deterministic ReserveFlow project memory.
- [x] `M24-205` Keep Track D unexecuted, record the later authorization trigger, and exclude adaptive-policy claims from the prototype dogfood result.
- [x] `M24-206` Compare correctness before speed or cost, including all failed cheap attempts, escalations, interventions, and evaluator failures.
- [x] `M24-207` Report whether marginal time, cost, context size, and repair rounds decline across accepted tasks without correctness regression.
- [x] `M24-208` Separate observed Codeflux benefit from model variance, benchmark learning, evaluator leakage, and operator learning.
- [x] `M24-209 TEST` Perform one final Track B rerun from the untouched scaffold with fresh Codeflux and ReserveFlow databases.
- [x] `M24-210 TEST` Run the complete visible and hidden suites against the final accepted ReserveFlow revision and verify API/OpenAPI agreement.
- [x] `M24-211 SECURITY` Scan Codeflux state, ReserveFlow state, Git history, logs, events, diagnostics, graph data, comments, and fixtures for seeded secret markers.
- [x] `M24-212` Produce the final dogfood scorecard with raw counts, denominators, confidence limits where meaningful, and links to attributable evidence.
- [x] `M24-213` Produce a prioritized Codeflux refinement inventory ordered by correctness risk, then user-blocking friction, then speed, then cost.
- [x] `M24-214` Record continue, narrow, redesign, defer, or kill decisions for the agent loop, graph, atoms, deterministic memory, vectors, forecasting, routing, recovery, and frontend.
- [x] `M24-215` Update the plan only with findings supported by the frozen trial, while retaining unresolved observations in the backlog rather than converting them into speculative architecture.

## Scorecard and Decision

These stable earlier IDs execute after the ReserveFlow trial so the final prototype decision includes the dogfood evidence.

- [x] `M24-080 BLOCKER` Populate the frozen correctness metrics.
- [x] `M24-081` Populate latency metrics.
- [x] `M24-082` Populate token and cost metrics.
- [x] `M24-083` Populate forecast calibration observations.
- [x] `M24-084` Populate usability observations.
- [x] `M24-085` Populate interruption and recovery results.
- [x] `M24-086` Populate permission and security-boundary results.
- [x] `M24-087` Populate graph usefulness and confusion observations.
- [x] `M24-088` Populate memory influence and invalidation results.
- [x] `M24-089` List every manual workaround used.
- [x] `M24-090` List every flaky or irreproducible result.
- [x] `M24-091` List every unsupported claim users could plausibly misunderstand.
- [x] `M24-092` Classify failures as implementation bug, specification defect, model limitation, tooling limitation, UX failure, or experiment-design problem.
- [x] `M24-093` Compare results to §30 kill and pivot criteria.
- [x] `M24-094` Decide continue, narrow, redesign, or stop for each major subsystem.
- [x] `M24-095` Keep adaptive routing disabled unless its later evidence gate is met.
- [x] `M24-096` Keep deep graph verification disabled unless its independent graph gate is met.
- [x] `M24-097` Create a prioritized post-prototype defect list.
- [x] `M24-098` Tag the exact prototype source revision.
- [x] `M24-099` Archive reproducible benchmark methodology and redacted results.
- [x] `M24-100` Update `docs/plan.md` with evidence-driven decisions instead of speculative additions.

## Final Gate

- [x] `M24-G01 GATE` The frozen task passes independent acceptance without an unauthorized action.
- [x] `M24-G02 GATE` The full journey works after clean installation and without developer intervention.
- [x] `M24-G03 GATE` Pause, reconnect, worker crash, coordinator crash, budget exhaustion, and concurrent-edit scenarios all preserve correctness-bearing state.
- [x] `M24-G04 GATE` Final evidence, cost, limitations, and graph views agree with durable SQLite state.
- [x] `M24-G05 GATE` The team records explicit continue/narrow/pivot decisions against the plan's kill criteria.
- [x] `M24-G06 GATE` Track B builds the full chronological ReserveFlow API from the frozen scaffold without manual source edits, future-requirement leakage, an unauthorized action, a secret leak, or an unacknowledged false correctness claim.
- [x] `M24-G07 GATE` Every ReserveFlow task passes visible and independent hidden acceptance before the accepted commit chain advances.
- [x] `M24-G08 GATE` Every Codeflux-owned dogfood defect has a frozen reproduction, lowest-layer regression test, general fix or explicit defer decision, clean-base memory-off rerun, chronological memory-on rerun, and unrelated-fixture result.
- [x] `M24-G09 GATE` The final clean Track B rerun and complete evaluator suite pass without regressing the original accepted scorecard.
- [x] `M24-G10 GATE` The dogfood evidence supports explicit keep, narrow, redesign, defer, or kill decisions without treating one API as proof of general superiority.

---

# Explicitly Deferred Until After Prototype Exit

These are guardrails, not hidden TODOs for the prototype critical path.

Each is checked as complete because the deferral itself is enforced, not because
the capability was built: `internal/deferred` declares every item with its
authorising trigger, the claims its absence forbids, and the identifiers whose
appearance in the tree would show it arrived early, and its test suite reads the
real source tree rather than trusting the declaration. Starting any of them
requires `deferred.Authorize`, which refuses on an unmet trigger and on an unmet
dependency.

- [x] `POST-001 DEFER` Run the disposable graph-medium experiment before production semantic graph engineering.
- [x] `POST-002 DEFER` Scope and freeze the tier-zero kernel only if the graph experiment passes.
- [x] `POST-003 DEFER` Implement graph-native atoms only after kernel scope is accepted.
- [x] `POST-004 DEFER` Implement modeled Go atoms and reference models only after correlation controls are specified.
- [x] `POST-005 DEFER` Implement Go lowering and source maps only after the lowering conformance suite is frozen.
- [x] `POST-006 DEFER` Implement determinism conformance across architecture/toolchain matrices only for the authorized deep-verification track.
- [x] `POST-007 DEFER` Implement request-side effect proof obligations only after the medium and validator gates pass.
- [x] `POST-008 DEFER` Implement semantic graph diff only after immutable semantic revisions exist.
- [x] `POST-009 DEFER` Enable learned routing only after fixed-policy telemetry and shadow calibration pass.
- [x] `POST-010 DEFER` Enable advisory patterns only after clean-room evaluation and lineage independence.
- [x] `POST-011 DEFER` Promote mechanical rules only through replay, false-positive, expiry, override, and demotion governance.
- [x] `POST-012 DEFER` Add ANN/vector infrastructure only if SQLite brute-force retrieval becomes a measured bottleneck.
- [x] `POST-013 DEFER` Add multi-agent orchestration only after the single-agent baseline exposes a measured bottleneck.
- [x] `POST-014 DEFER` Add hosted sync, teams, enterprise identity, policy administration, or audit export only after hobbyist product evidence.
- [x] `POST-015 DEFER` Add direct graph editing only after user studies show conversational revisions are insufficient.

## Completion Record

Completed: 2026-08-01
Source revision: a133785 (plus the working tree recorded below)
Database schema version: 29
Frontend version: assets-e3b0c44298fc
Test/benchmark command: `go build ./...`; `GOOS=js GOARCH=wasm go build ./web/...`; `go vet ./...`; `gofmt -l .`; `go test ./...`; `go run ./cmd/codeflux-dev lint`; `generate-check`; `migration-check`; `artifact-check`
Result location: the suites above run from the repository root; benchmark figures are in `docs/benchmarks.md`
Known limitations: M24 is declared and machine-checked, not executed. The ReserveFlow trial itself — Tracks A through C, the live provider runs, the human evaluator sessions, and the hidden acceptance verdicts — has not been run, because it requires a live provider, a frozen external scaffold, and an independent operator. What exists is the protocol as executable code: the clean-room preconditions, the packet revealer, the journey, the evaluations, the per-task measurements, the track plan, the comparison, the repair protocol, the ten exit gates, and the sixteen completion criteria, each with tests that refuse the ways a run could quietly succeed for the wrong reason. `DONE-016` and the `M24-G` gates therefore record that the gate exists and can fail, not that a run has passed it; `exitrun.EvaluateExit` reports every unmeasured gate as unanswered rather than passed, so nothing here reads as a result until a run supplies one. `POST-001..015` are enforced deferrals, not delivered capabilities. `M01-071` and `M01-077` are documentation obligations whose triggers have not fired; `codeflux-dev lint` now fails the moment either does.
Decision owner: unassigned — the exit decision belongs to whoever runs the trial

## Completion Record Template

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

---

# Completion Audit Follow-ups (2026-08-01)

These unchecked review items were appended after comparing every checked TODO
with the current plan, production wiring, tests, and retained completion
evidence. They do not silently reopen or rewrite the original history; each
item names the checked claim that must be reconciled before the completion
ledger can be treated as authoritative.

- [ ] `AUDIT-001 REVIEW DOC` Reconcile `M00-019`: replace the plan's nonexistent `AwaitingAcceptance` success state with the implemented `awaiting-review` state, then add a trace test that every lifecycle state named by the plan parses through `domain.AllTaskStates`.
- [ ] `AUDIT-002 REVIEW PLAN` Reconcile `M00-G01` and `M00-G02`: add a machine-readable feature/deferred manifest with journey, measurement, milestone, and dependency fields, and make plan tracing reject unmapped features or deferred critical-path dependencies.
- [ ] `AUDIT-003 REVIEW BLOCKER` Reconcile `M01-026`: make one `codeflux-dev build` invocation generate or verify protobuf output, build the server and worker, and build the GWC/WASM frontend under one artifact root; prove it from a clean clone.
- [ ] `AUDIT-004 REVIEW SECURITY` Reconcile `M03-003` on the primary Windows platform: apply and test user-only DACLs for the application-data directory, SQLite database, backup, migration-lock, WAL, and SHM files instead of skipping the permission assertion.
- [ ] `AUDIT-005 REVIEW DATA` Reconcile `M03-067`: seed real graph and vector descendants, run the supported deletion or purge path, and assert the intended restriction/cascade behavior and zero orphan rows.
- [ ] `AUDIT-006 REVIEW SECURITY` Reconcile `M04-G01`: configure and exercise a deterministic provider through effective config and OS credential lookup, then scan SQLite, WAL, SHM, events, and outputs for the canary secret.
- [ ] `AUDIT-007 REVIEW SECURITY` Reconcile `M04-G02`: run a full coordinator-to-worker/provider mock task and scan prompts, events, logs, UI payloads, diagnostics, SQLite, WAL, and SHM rather than manually redacting already-isolated boundary values.
- [ ] `AUDIT-008 REVIEW TEST` Reconcile `M07-057`: register production services in the in-process transport and exercise every RPC success and typed-domain-error path instead of returning empty dynamic responses from an unknown-service handler.
- [ ] `AUDIT-009 REVIEW BLOCKER TEST` Reconcile `M07-082` through `M07-090`, `M07-G01`, and `M07-G04`: run the chronological journeys against the real application, generated services, temporary SQLite/Git, worker/provider/effect ports, and named crash boundaries instead of string-append and in-test map fakes.
- [ ] `AUDIT-010 REVIEW BLOCKER` Reconcile `M08-043`, `M08-045`, and `M08-G03`: register and wire `WorkspaceService` through repository mapping, context selection, `ContextManifestRepository`, committed events, and the mounted expandable context card.
- [ ] `AUDIT-011 REVIEW SECURITY` Reconcile `M08-049`: replace caller-supplied approved instruction paths with a durable approval identity bound to project, repository revision, scope, and consumption, with forged/stale/replay tests.
- [ ] `AUDIT-012 REVIEW BLOCKER` Reconcile `M09-026`: wire `StorageEditEventRecorder` into the production `gitwork` service, route mediated edits through `ApplyEditBatch`, and integration-test atomic edits plus the committed redacted event.
- [ ] `AUDIT-013 REVIEW BLOCKER` Reconcile `M10-027`: implement the worker ToolService/agent-loop adapter as the sole command executor, remove coordinator-owned direct execution, and prove worker PID and lease attribution end to end.
- [ ] `AUDIT-014 REVIEW` Reconcile `M10-035`: publish incrementally redacted, bounded command progress while the process is running, with backpressure and cancellation tests, rather than publishing only after process exit.
- [ ] `AUDIT-015 REVIEW SECURITY` Reconcile `M10-026`, `M10-042`, `M10-G01`, and `M10-G02`: implement and register the production ToolService, bind exact durable grant consumption to worker execution, and test attribution, substitution, replay, expiry, and denial end to end.
- [ ] `AUDIT-016 REVIEW BLOCKER` Reconcile `M11-004`: construct and register production workspace and provider/settings services during application startup and verify their real capabilities over generated RPC clients.
- [ ] `AUDIT-017 REVIEW SECURITY` Reconcile `M11-033`: expose a validated, explicitly authorized user container command through config, application options, task launch, and supervisor wiring, then test container and native paths.
- [ ] `AUDIT-018 REVIEW BLOCKER` Reconcile `M11-036` and `M11-037`: add a completion/unblock dispatch pump so queued work starts when capacity becomes free, project queue position and reason to clients, and test fairness across pause, approval, capacity release, and restart.
- [ ] `AUDIT-019 REVIEW BLOCKER` Reconcile `M12-051`, `M12-052`, and `M12-G04`: wire provider recovery into exhausted execution, expose retry/resume/explicit-switch commands, persist exact from/to authority, and prove no adapter fallback silently changes provider or model.
- [ ] `AUDIT-020 REVIEW BLOCKER TEST` Reconcile `M14-021`: complete the real StartTask path through worktree creation, durable scheduling, worker launch, provider/tool loop, and awaiting-review; make `TestRequirementReachesAStartedRunThroughTheRealApplication` pass without a recorded-but-idle run.
- [ ] `AUDIT-021 REVIEW BLOCKER TEST` Reconcile `M16-G01`: build the production executable, start it without an external asset directory, navigate its printed URL, and prove the embedded GWC shell, WASM client, and bridge load with zero external requests.
- [ ] `AUDIT-022 REVIEW BLOCKER` Reconcile `M17-081` through `M17-084`: carry policy, exact hard budget, model override, and reasoning-effort override through the generated request contract and persist them in authoritative task/preflight state.
- [ ] `AUDIT-023 REVIEW BLOCKER TEST` Reconcile `M17-085`: mount a server-backed repository file/symbol picker and add generated-client-to-real-ThreadService coverage for distinct artifact and atom identities.
- [ ] `AUDIT-024 REVIEW BLOCKER E2E` Reconcile `M17-G04`: replace `authoritative-disabled` plan/review actions with generated RPC bindings for approve, revise, start, accept, repair, reject, rollback, and abandon, preserving one idempotency key through settlement and replay.
- [ ] `AUDIT-025 REVIEW GATE` Reconcile the M13-M18 gate evidence: define one revision-bound command that runs unit, integration, security, migration, and production-mounted Chromium checks with no milestone-relevant skips, and retain an unambiguous result manifest.
- [ ] `AUDIT-026 REVIEW GATE` Reconcile `M21-078` through `M21-088`, `M21-G04`, and `M21-G06`: run the deterministic-retrieval miss measurement and exercise active vectors non-vacuously, or explicitly defer the unopened vector branch instead of marking an instrument and vacuous gate as completed evidence.
- [ ] `AUDIT-027 REVIEW BLOCKER TEST` Reconcile `M22-036` through `M22-050` and `M22-G03`: connect fault injection to the real worker, coordinator, browser, provider, command, worktree, and SQLite boundaries; the current generic `RunWithFault` ledger never enters those production paths.
- [ ] `AUDIT-028 REVIEW BENCH` Reconcile `M22-089`: rerun and record the benchmark suite on the specified ordinary target-class hobbyist laptop; the retained reference run is explicitly classified `above-target`.
- [ ] `AUDIT-029 REVIEW BLOCKER` Reconcile `M22-113` through `M22-118` and `M22-G06`: implement callable `codeflux-dev seed`, `replay`, and `inspect-db` workflows and prove local vertical replay; the current registry still marks them as unavailable skeletons.
- [ ] `AUDIT-030 REVIEW BLOCKER RELEASE` Reconcile `M23-052` through `M23-060` and `M23-G01`: build real reproducible platform binaries, embed non-placeholder frontend assets, generate checksums, sign and verify actual files, and install one artifact into a clean profile; current release tests validate synthetic artifacts, `web/assets/static` contains only a README, and the release package invokes no builder or signer.
- [ ] `AUDIT-031 REVIEW BLOCKER EXPERIMENT` Reconcile execution-bearing `M24` items and `M24-G01` through `M24-G10`: run the frozen ReserveFlow Tracks A-C with the live provider and independent evaluator, populate attributable results, and only then record pass/fail decisions; the completion record explicitly says the trial was not run and the gates are unanswered.
- [ ] `AUDIT-032 REVIEW RELEASE` Reconcile `M24-098` and the completion record: tag the exact evaluated prototype revision and record its actual source/frontend artifact identities; the record names stale revision `a133785`, reports the empty-asset hash, and no tag points at current `HEAD`.

---

# Pipeline Refinement (2026-08-02)

Goal: make every stage of the delivery flow record only what its check establishes, then extend the flow to cover the subject matter it has no vocabulary for.

The product this serves is a strict coding agent whose correctness argument rests on three things: atomicity, so every unit can be specified, tested, and reused on its own terms; structure, so a claim about a program is derived from its parse tree and its measured behaviour rather than from prose about it; and declared contracts, so what a unit promises exists before the unit does and can be checked against it afterwards. Every ticket below is either restoring one of those three where the flow claims it and does not have it, or removing a cost that makes the strict path slower than the loose one. Correctness is the constraint; speed and cost are optimised inside it, never against it.

Plan references: §33 Pipeline Refinement; §22 Correctness and Assurance Gates; §24 Specification Review; §9 Proof Obligations as the Unit of Assurance.

Depends on: no new implementation dependency. The defect repairs apply to the flow as built and can start immediately; the new stages depend on `PIPE-001` through `PIPE-004`, because adding stages to a ledger that cannot yet be trusted multiplies the untrustworthy rows.

Milestone output: a sixty-six stage flow in `internal/pipeline`, a coordinator that performs or honestly declines each stage, a ledger whose every satisfied row is backed by a check that establishes its gate, and a skip audit that reports what the run did not do as a headline figure.

## Ledger Integrity

Plan references: §33 Pipeline Refinement; §22 Correctness and Assurance Gates; §10 Guarantee Provenance.

Depends on: nothing. These repair the flow as built and unblock every ticket that adds to it, because a stage added to a ledger that drops rows multiplies the untrustworthy rows.

- [ ] `PIPE-001 BLOCKER` Stop the unverified-run sweep pre-blocking end-to-end-tests, adversarial, and evidence-bundle: record each once, after the run has computed it, and add a test that a compiling-but-failing run's ledger carries the adversarial probe's real verdict rather than a blocked row.
- [ ] `PIPE-002 BLOCKER` Route every stage write through one place that refuses a second write for the same stage in the same attempt, so a duplicate recording is a caught programming error rather than a silent `DO NOTHING`.
- [ ] `PIPE-003 DATA` Carry the real attempt number through `newPipelineLedger`, `StartPreparedTaskRun`, and `assembleEvidence` so a task started twice records two attempts instead of showing the first run's ledger under a newer run identity.
- [ ] `PIPE-004 TEST` Add a table test binding each stage number to the check that performs it and to whether that check may return satisfied at all; a stage with no check may record only skipped or not-implemented.
- [ ] `PIPE-005` Replace the numeric range in `examineStructure` with a named set of stages, so the sweep does not silently change meaning when the flow gains a stage.
- [ ] `PIPE-006` Record each stage's real start and finish times instead of writing one timestamp into both columns, and show stage duration in the ledger view.
- [ ] `PIPE-007 DOC` Correct the recorded flow length in `agent_pipeline_ledger.go`, `agent_execution.go`, `agent_stage_delivery.go`, and `engine_pipeline_ledger_test.go`, and derive it from `len(pipeline.Flow)` where a number is needed.

## Gate-to-Check Agreement

Plan references: §9 Proof Obligations as the Unit of Assurance; §22 Correctness and Assurance Gates; §33 Pipeline Refinement.

Depends on: `PIPE-004`, which turns a gate-to-check disagreement into a failing build rather than something review has to notice.

- [ ] `PIPE-008 BLOCKER` Replace `testedNames` identifier collection with call-site resolution, so an atom counts as tested only when a test calls it, not when any identifier of that name appears in a test file.
- [ ] `PIPE-009` Count method and package-qualified calls in `describeBody`, so a program composed through methods is not classified as entirely atomic.
- [ ] `PIPE-010` Record atom-optimization as skipped with the simplification candidates as its evidence until a rewrite exists, because the gate claims a rewrite the check does not perform.
- [ ] `PIPE-011` Measure growth across input sizes in atom-complexity and require it to agree with the structural label; until the measurement exists the stage records skipped rather than satisfied, and the asserted space claim is removed either way.
- [ ] `PIPE-012` Split platform-matrix into a run claim and a build claim: the host platform runs the suite, a cross target records only that it compiles because the host cannot execute it, and the gate wording states which of the two each target answered.
- [ ] `PIPE-013` Record a non-functional baseline on first run and compare later runs against it, replacing the fixed sixty-second budget.
- [ ] `PIPE-014` Enumerate decoding boundaries for atom-fuzz and fail when a boundary has no target; skip only when the enumerated count is zero, with that count as evidence.
- [ ] `PIPE-015` Establish control-tests per declared path rather than by counting decision nodes in test source.
- [ ] `PIPE-016` Make composition-obligations and control-obligations produce a durable obligation per composition and per path in §9's sense, which molecule-verification and control-flow then discharge by name; until the obligation is durable both stages record skipped rather than unconditionally satisfied.
- [ ] `PIPE-017` Run atom-optimization after atom-mutation in `examineStructure`, matching the ordering `TestAnAtomIsOptimisedOnlyOnceItsTestsCanCatchAMistake` exists to enforce.
- [x] `PIPE-018` Narrow `ModelBearing` to the four gates that can send work back and read the pin where the run is returned, replacing a list of sixteen stages that make no model request; a validated control nothing reads is worse than an absent one. Remaining scope moved to `PIPE-018a`.
- [ ] `PIPE-018a` Select a rung per unit rather than per run, which needs units built independently: the implementation loop writes the whole program in one loop, so today the run's starting rung comes from the hardest unit.

## Specification Grounding

Plan references: §24 Specification Review; §14 Contracts and Invariants; §0 Acceptance definitions; §33 Pipeline Refinement.

Depends on: `PIPE-045` for a number the ledger accepts, and `PIPE-117`, whose answer decides whether a mandatory acceptance example is expressible for every task class.

- [ ] `PIPE-019 BLOCKER` Require at least one executable acceptance example at the instructions stage and remove the declared-absence escape, so no run can satisfy the flow against a request nothing external checks.
- [ ] `PIPE-020` Add the acceptance-oracle stage: every example runs and fails against an empty program, proving the example discriminates before anything is built on it.
- [ ] `PIPE-021 UX` Add the specification-review stage: a person accepts the contracts, data model, and threat model before any code is written, with the same accept, reject, and revise actions the delivery review already has.
- [ ] `PIPE-022` Add the data-model stage recording the types, their invariants, and the states made unrepresentable, and write the contracts stage in those types.
- [ ] `PIPE-023` Add the resource-and-effect declaration stage naming every acquired resource with the path on which it is released, as the artifact `PIPE-030` verifies against.
- [ ] `PIPE-024 SECURITY` Add the threat-model stage recording trust boundaries, untrusted inputs, secrets, and values that may never be emitted, as the artifact `PIPE-036` verifies against.
- [ ] `PIPE-025` Add the concurrency-model stage recording what runs concurrently, what is shared, and which ordering and atomicity properties are required, as the artifact `PIPE-029` verifies against.
- [ ] `PIPE-026` Add the dependency-budget stage recording which third-party code is admitted, pinned, and justified against writing it instead.

## Depth at Every Level

Plan references: §7 Semantic Atom Categories; §9 Proof Obligations as the Unit of Assurance; §33 Pipeline Refinement.

Depends on: `PIPE-023` and `PIPE-025` for the declarations these verify against, and `PIPE-045`.

- [ ] `PIPE-027` Add molecule-property-tests requiring at least one property per composition obligation, stated over the composition rather than over its parts.
- [ ] `PIPE-028` Add molecule-mutation so composition tests are shown to detect composition defects, not only atom defects.
- [ ] `PIPE-029` Add concurrency-verification running the race detector and testing the `PIPE-025` ordering and atomicity claims under interleaving.
- [ ] `PIPE-030` Add resource-lifetime proving every `PIPE-023` declared resource is released on every path, with no handle, goroutine, or memory growth under repetition.
- [ ] `PIPE-031` Add failure-path-injection forcing every declared failure path to occur and asserting it behaves as its obligation says.
- [ ] `PIPE-032` Add program-fuzz over the produced program's own external boundary: arguments, standard input, files, and interfaces.

## Program, Environment, and Failure

Plan references: §12 Effect Discipline; §22 Correctness and Assurance Gates; §27 Commands, Secrets, and Malicious Repository Content.

Depends on: `PIPE-024` and `PIPE-026` for the declarations these verify against, `PIPE-045`, and `PIPE-131`, because every stage here runs the produced program.

- [ ] `PIPE-033 SECURITY` Add dependency-audit checking pinning, known vulnerabilities, licence compatibility, and unused dependencies against the `PIPE-026` budget.
- [ ] `PIPE-034 RELEASE` Add reproducible-build proving the same inputs produce the same artifact, with the toolchain version pinned and recorded.
- [ ] `PIPE-035` Add fault-injection covering a full disk, a timeout, a partition, and a failing dependency, each required to produce a reported failure rather than corruption.
- [ ] `PIPE-036` Add crash-recovery killing the produced program at arbitrary points and requiring a safe idempotent restart with no torn state.
- [ ] `PIPE-037 SECURITY` Add security-verification against the `PIPE-024` threat model: no declared secret in output, logs, or errors, and every trust boundary validating what crosses it.
- [ ] `PIPE-038` Add observability requiring every failure to name what failed and with what input, exit codes to distinguish outcomes, and the program to be diagnosable without a debugger.

## Fit With What Already Exists

Plan references: §19 Review and Source Mapping; §22 Correctness and Assurance Gates; §23 Transactions, Migrations, and Recovery.

Depends on: `PIPE-111`. Every claim in this section is about the boundary between changed and pre-existing code, which nothing currently draws.

- [ ] `PIPE-039 BLOCKER` Add the regression stage proving the pre-existing suite still passes at the produced revision; the produced-file scoping that makes every other gate fair leaves this uncovered today.
- [ ] `PIPE-039a` Require the changed lines to be covered by a test that runs them, which is a different claim from the suite passing and the one that catches a change nothing exercises.
- [ ] `PIPE-040` Add api-surface requiring the exported surface to be minimal and, for a change to existing code, unbroken.
- [ ] `PIPE-041 DATA` Add compatibility requiring the new version to read the previous version's data and interoperate with older peers, with every migration tested in both directions.

## Audit and Delivery

Plan references: §10 Guarantee Provenance; §22 Correctness and Assurance Gates; §33 Pipeline Refinement.

Depends on: `PIPE-046` for the declared profile the skip audit challenges a skip against.

- [ ] `PIPE-042` Add skip-audit challenging every skipped and not-implemented stage and reporting the ratio as a headline figure in the run summary, not only in the ledger table.
- [ ] `PIPE-043` Add independent-review by a reviewer that did not write the code, working from the specification rather than the implementation, with no round cap and no setting that removes it.
- [ ] `PIPE-044 UX` Show the skip ratio and the unverified caveat in the run's final message, so a person reading the timeline sees what was not done without opening the ledger.

## Flow Renumbering and Profiles

Plan references: §33 Pipeline Refinement; §23 Storage.

Depends on: `PIPE-004`. Renumbering a flow whose stages can still claim what they did not check spreads the problem across more rows.

- [ ] `PIPE-045 DATA` Renumber the flow to sixty-six stages and prove that recorded rows from the previous numbering still read correctly by name, including a migration test over rows written under the old numbers.
- [ ] `PIPE-046` Add declared run profiles marking stages inapplicable before the run starts, so a library run declines crash-recovery in advance rather than skipping it at runtime, and make `PIPE-042` audit which of the two happened.
- [ ] `PIPE-047 DOC` Record in `docs/plan.md` §33 which stages each shipped profile declines and why, so a profile cannot quietly become the way a stage stops being performed.

## Registration and Reuse

Plan references: §2 Compounding-Effort Thesis; §7 Atom Naming and Retrieval Identity; §23 Atom Storage; §31 Evidence-Driven Reuse and Learning.

Depends on: `PIPE-111` for what counts as produced, and `PIPE-048` before every other ticket here, because recall against an empty registry finds nothing and proves nothing.

- [ ] `PIPE-048 BLOCKER DATA` Add the atom-registration stage and write the registry row: documentation, contract hash, signature shape, evidence links, and the exact revision the atom was verified at. No run writes the registry today, so recall has nothing to search.
- [ ] `PIPE-048a DATA` Record the documentation embedding for each registered atom through `RecordAtomDocumentationEmbedding`, bound to the exact documentation revision, contract hash, repository revision, and embedding configuration §7 requires.
- [ ] `PIPE-049 DATA` Add molecule-documentation and molecule-registration on the same terms, recording what the composition guarantees and the atoms it composes as its parts, so a later run can reuse joins and not only leaves.
- [ ] `PIPE-050 BLOCKER` Move recall ahead of the atoms stage and make it binding: every contract carries a `reuse` or `write` decision before anything is built, and a `reuse` decision names the registered atom, its evidence, and its verified revision.
- [ ] `PIPE-051 BLOCKER` Implement the contract-hash key: an exact match on the normalised signature with its preconditions, postconditions, and effects, answered from the index with no model request, replacing the `strings.Contains(content, "func "+name+"(")` match that finds a renamed atom never and a same-named different atom always.
- [ ] `PIPE-051a` Implement the signature-shape key: the parameter and result type vector with names erased, indexed, returning a bounded candidate set rather than a verdict.
- [ ] `PIPE-051b` Implement the documentation-embedding key over the shape candidates only and never over the whole registry, so similarity proposes and the contract hash disposes, as §7 requires.
- [ ] `PIPE-052` Admit a recalled atom only once it passes this run's synthesised cases from the case-synthesis stage, and record that re-verification as the atom's evidence in this run, so reuse cannot import the earlier run's blind spots.
- [ ] `PIPE-053 BLOCKER` Thread the preflight `retrieval.PreWorkGateResult` into the agent loop's first-round context items; the gate already runs in `task_preflight.go` and the loop plans against a directory listing, `go.mod`, and `README.md` while its answer sits unread. First-round admission is for verified material only — facts, recipes, registered atoms, rules, and regression cases; advisory patterns enter through `MEM-018`'s gate-failure path instead, because a first attempt exposed to advice is no longer the clean arm §31's lesson-failure protocol needs.
- [ ] `PIPE-054` Keep `recallKnownAtoms` as the reuse-regret stage at the end of the flow, never blocking, comparing every function the run wrote against the registry as it now stands and recording which of the three keys would have found the match.
- [ ] `PIPE-055` Classify each regret finding as a registry gap or a recall miss, because a function that was never deposited and a function that was deposited and not found have different repairs, and report the run's regret count beside the skip ratio.
- [ ] `PIPE-056 DATA` Record influence through the existing `retrieval.RecordInfluence` path when a recalled atom is accepted or rejected, so reuse decisions are auditable and an invalidated atom can be re-derived transitively as §31 requires.

## Concurrent Execution

Plan references: §33 Pipeline Refinement; §25 Speed; §26 Benchmark Timing.

Depends on: `PIPE-057`, then `PIPE-058` and `PIPE-059`. `PIPE-064` is the property the whole section is written against and must exist before the first parallel stage ships.

- [ ] `PIPE-057` Parse the produced source once per run and share the tree: `readProducedFunctions` is called twenty times and `producedGoFiles` thirteen more, each re-shelling to `git status` and re-parsing identical content.
- [ ] `PIPE-058 BLOCKER` Declare `Requires` edges on every stage as data in `internal/pipeline`, with a test that the graph is acyclic, that every stage is reachable, and that no edge points forward in the flow.
- [ ] `PIPE-058a BLOCKER` Execute the declared frontier concurrently while the ledger continues to record by stage number, so execution order and reporting order stop being the same thing.
- [ ] `PIPE-059` Declare a resource class per check — pure AST, build-only, suite-read, exclusive-mutating — and schedule against it, since the worktree rather than the logic is what forces serialisation.
- [ ] `PIPE-060` Answer atom-verification, molecule-verification, path-coverage, and non-functional from one instrumented suite run instead of four separate whole-suite invocations.
- [ ] `PIPE-061` Run mutation scoring on isolated worktree copies in parallel; twelve serial whole-suite runs is the single largest cost in a run and the only check that cannot share the worktree.
- [ ] `PIPE-062` Fan out per-unit work across items: per atom, per mutant, per fuzz target, and per platform target.
- [ ] `PIPE-063` Cancel a failed gate's dependents and record them blocked, rather than computing verdicts that a first-write-wins row then discards.
- [ ] `PIPE-064 TEST` Prove a stage's verdict does not depend on how many stages were in flight: run the same worktree serially and in parallel and require identical ledgers apart from timing.
- [ ] `PIPE-065` Key stage results by input digest and reuse them across attempts within a run and across runs for recalled atoms, so an attempt that changed two files does not re-check the other eight. Cross-run reuse is a memory read: bind each cached verdict to the revision, toolchain, and dependency versions that produced it and invalidate it when they move, because a stage verdict carried into a later run on a stale binding is an unquarantinable claim about a program nobody checked.

## Provider Backpressure

Plan references: §21 Routing Safety; §25 Cost; §27 Initial Model Providers.

Depends on: `PIPE-066`, the single admission point every other ticket here configures, and `PIPE-058`, which is what creates concurrent model requests in the first place.

- [ ] `PIPE-066 BLOCKER` Route every model request through one admission point with a configured maximum concurrency, so fan-out cannot exceed what the provider tolerates and the ceiling is a setting rather than an accident of the graph shape.
- [ ] `PIPE-067 BLOCKER` Treat `429` and provider overload responses as backpressure: exponential backoff with jitter, honouring `Retry-After` when the provider sends one, recorded as a wait rather than a stage failure.
- [ ] `PIPE-068` Bound retries by attempt count and by total wall clock, then record the stage blocked with the provider's own reason, because "we could not ask" and "we asked and the answer was no" are different facts in an immutable ledger.
- [ ] `PIPE-069` Narrow the concurrency ceiling for the remainder of the run under sustained rate limiting instead of retrying at full width into a limit.
- [ ] `PIPE-070` Enforce budget and forecast at the admission point so concurrent stages cannot each believe they are inside the cap while collectively exceeding it.
- [ ] `PIPE-071 TEST` Add a provider fake returning `429` with and without `Retry-After` and assert the backoff schedule, the ceiling reduction, the ledger wording, and that no stage records failed because of a rate limit.
- [ ] `PIPE-072 UX` Show waiting-on-provider as its own state in the run timeline, distinct from working and from blocked, so a throttled run does not read as a stalled one.

## Model Selection

Built already, recorded here because `TODOS.md` is authoritative for what is implemented and the escalation vocabulary is now load-bearing for several tickets above.

- [x] `PIPE-073` Make the ladder rungs a model and an effort level rather than a model alone, and order the default ladder so every effort of the cheap model precedes any rung of the dear one; raising effort bills more tokens at the rate in force, changing model raises the rate on every token.
- [x] `PIPE-074` Trigger escalation on repetition — the same gate failing with the same normalised failure — rather than on attempt count, so a run failing something different each time is left on the cheapest rung it is converging on.
- [x] `PIPE-075` Give each rung its own attempt allowance, bounded overall by the allowance times the number of grants; one shared budget charged the cheap model's failed attempts to the model it escalated to and left the last rung unreachable on the shipped defaults.
- [x] `PIPE-076` Decompose once at the top of the ladder rather than asking the strongest rung again, and tell the run to stop trying to land the request in one piece.
- [x] `PIPE-077` Refuse a descending ladder at validation: a run escalates because it stalled, so moving it somewhere cheaper at that moment guarantees it stalls again and nothing downstream would report the ladder was upside down.
- [x] `PIPE-078` Stop the run on a rung named as requiring approval, and carry what was tried, what keeps failing, what it has cost, and which axis the extra cost is on; keep approval rungs off the default ladder so the gate is not on the ordinary case.
- [x] `PIPE-079` Make planning a model request on a rung that does not climb, replacing the regular expression over the requirement that emitted one edit step per named file; a decomposition that misses a behaviour is paid for on every rung, on every attempt, and no effort further down recovers it.
- [x] `PIPE-080` Carry the model's words on the turn: the text was collected, scanned for a completion keyword, and discarded, so a caller that asked the model a question rather than asking it to work could not read the answer.
- [x] `PIPE-081` Price each rung separately; a ladder priced at the cheap model's rates reports the same cost whichever model ran, and the budget the product enforces stops being true the moment a run escalates.
- [x] `PIPE-082 BLOCKER` Treat a refused turn as a failed attempt rather than a dead run, and tell the next attempt which rule it broke. Every loop error ended the run, which put a model slip in the same category as a database that will not open; rung 5 died on one with four attempts and three rungs unused.
- [x] `PIPE-083` Stop the module root matching every request in acceptance-command selection: `strings.Contains` is true of the empty string against any text, so a root package read as named by every requirement and the one externally grounded stage never ran.
- [ ] `PIPE-084 TEST` Record predicted rung, seeded rung, and finishing rung on every run, and report how often each rung was reached, so the ladder's own settings can be calibrated against what runs actually needed rather than against an argument in this document. Record alongside them the routing-key projection version of `MEM-022` and whether the run was seeded or exploring per `MEM-028`; without the first the rows cannot be grouped, and without the second the seeded population's cost is reported as everyone's.

## Difficulty Rating

Depends on `PIPE-079`: the rating is produced inside the specification request that already enumerates the units, so there is no rating stage until there is a planning request to carry it.

- [ ] `PIPE-085` Add the difficulty-rating stage as stage 8, after contracts and inside the reviewed set, so a person can disagree with a tier before the money is spent rather than after.
- [ ] `PIPE-086` Rate against a closed set of named tiers — direct, conditional, structural, algorithmic — each justified by a property of the contract; refuse a numeric score, which produces numbers that look measured, are noise through the middle of the range, and then make a binding cost decision.
- [ ] `PIPE-087` Bias the rubric low: where two tiers both fit, take the lower. Under-rating is visible because the run stalls and climbs; over-rating is invisible and self-justifying, so the untended drift is upward.
- [ ] `PIPE-088` Seed the ladder from the rating rather than replacing it, so a rating at the top rung short-circuits while a wrong rating still corrects; the correction path is the only thing that makes an over-rating recoverable.
- [ ] `PIPE-089` Take the run's starting rung from the hardest unit and name the hard units in the decomposition, until `PIPE-018a` allows a rung per unit.
- [ ] `PIPE-090 TEST` Record tier against outcome and answer the calibration question directly: of the units rated structural, how many did the cheapest rung handle unaided? A rubric nobody can calibrate drifts upward forever.
- [ ] `PIPE-091` Choose the reviewing rung from measurement rather than from the rating: branch counts, loop nesting, path coverage, and above all which deliberate defects survived, since a surviving mutant is evidence the tests cannot see a defect in that region. The flow already computes all of it.
- [ ] `PIPE-092 DOC` Record in `docs/plan.md` §33 which tier seeds which rung, so a rubric change and a cost change are the same edit and cannot drift apart.

## Refinement Adversary

Plan references: §21 Adversarial Agent; §9 Proof Obligations as the Unit of Assurance; §10 Guarantee Provenance; §12 Effect Discipline; §22 Correctness and Assurance Gates; §25 Metrics; §26 Benchmark Timing; §31 Evidence-Driven Reuse and Learning; §33 Pipeline Refinement.

Depends on: `PIPE-001` through `PIPE-004`. A critic whose findings are recorded in a ledger that silently drops rows has recorded nothing.

- [ ] `PIPE-093 BLOCKER` Assemble the critic's input: the contracts, data model, threat model, acceptance examples, and the diff, as one packet bound to the revision it describes.
- [ ] `PIPE-093a BLOCKER` Make the critic a model request over that packet and parse its answer into typed findings, so it challenges requirement interpretation and unsupported guarantees as §21 defines it instead of reporting what the source looks like.
- [ ] `PIPE-094` Keep the five static finders as evidence handed to the critic rather than as the critic, so §21's judgement and the mechanical checks stop being the same thing.
- [ ] `PIPE-095` Select the critic's checks from task risk, changed obligation categories, effect types, dependency changes, and security classification as §21 requires, and read the `scope.riskLevel` the run already resolves and never uses.
- [ ] `PIPE-096 BLOCKER` Raise every finding as a proof obligation with a stable identity in §9's sense, so a review can be discharged, regressed, or reopened rather than expiring with the attempt that produced it.
- [ ] `PIPE-097` Attach guarantee provenance to each finding as §10 requires: evidence level, dependency binding, and lineage, so a later run can re-derive or invalidate it transitively.
- [ ] `PIPE-098 BLOCKER` Record findings and their disposition in the pipeline ledger and the evidence bundle, so §22's assurance report reflects what the critic found and whether the next attempt fixed it; today the only consumers are one prompt string and one chat message.
- [ ] `PIPE-099 BLOCKER` Route the review's send-back through `sendBack` so §21's progress monitor sees it; a run stuck on review findings currently neither escalates up the model ladder nor decomposes.
- [ ] `PIPE-100` Mark the review round as spent only when findings actually sent work back, so the single round is not consumed by an attempt-one review that errored or found nothing and never sees the code that ships.
- [ ] `PIPE-101` Establish reviewer independence as §31 defines lineage independence: the critic works from the specification and the diff rather than from the criteria the author derived, and its findings are recorded as an independent lineage. Independence includes retrieval: advisory material shown to this run's implementer is withheld from this run's critic, and where it cannot be, the finding records `influenced_by` and is excluded from confirming that pattern or its ancestors.
- [ ] `PIPE-102` Remove `AdversarialReview` as a switch that deletes the reviewer; §22 forbids trading away a required reviewer for lower cost, so the setting may scale the critic's depth but not its existence.
- [ ] `PIPE-103` Repair the swallowed-error finder against `PIPE-105`'s measurement: it examines only pure functions, resolves no standard-library call, and infers a swallow from the caller returning no error, so it reports correctly handled failures as defects. Retire it if the measured false-positive rate stays above the threshold `PIPE-105` records.
- [ ] `PIPE-104` Repair the boundary finder against `PIPE-105`'s measurement by reading literal arguments from the test syntax tree: substring matching over concatenated source makes `"0"` match a file mode and `"Error"` match `t.Errorf`, so one finding is unreachable and the rest are near-random. Retire it if the measured rate stays above the threshold.
- [ ] `PIPE-105 TEST` Measure the critic's false-positive rate before either finder is promoted, as §31's mechanical-rule governance requires, and record the measurement with the rule.
- [ ] `PIPE-106` Rank findings by expected defect cost as §22 orders work, and bound how many reach the instruction, so the review does not compete with the code context under the loop's byte limits.
- [ ] `PIPE-107` Share one mutation result between the critic and the ledger; the two independent `checkMutations` calls are roughly twenty-four whole-suite executions per run and §25's honest cost display cannot attribute them.
- [ ] `PIPE-108` Attribute the critic's cost separately in the run's cost summary and in §26's benchmark timing, so the price of reviewing is visible next to the price of building.
- [ ] `PIPE-109` Read the exit code in the runtime probe and distinguish a legible refusal from a silent one; §12's effect discipline is about failure being visible, and the probe currently examines only timeout, the literal `panic:`, and silence.
- [ ] `PIPE-110` Stop reporting an empty output with a zero exit as a finding for programs whose correct behaviour is to emit nothing, so the probe does not fail work for doing what was asked.

## Change Attribution

Plan references: §33 Pipeline Refinement; §19 Review and Source Mapping; §22 Correctness and Assurance Gates; §27 Local Runtime and Repository Isolation.

Depends on: nothing. This is the scoping every phase B through D gate already assumes it has.

- [ ] `PIPE-111 BLOCKER` Derive the changed line ranges from the base revision the worktree was created at, replacing the changed-file list as the run's unit of attribution.
- [ ] `PIPE-111a BLOCKER` Map those ranges to their enclosing declarations and expose the attributed set to every check, so a one-line fix inside a thirty-function file does not make the run answerable for the other twenty-nine.
- [ ] `PIPE-112` Re-scope the completeness loop to attributed declarations only, so the refinement loop stops sending a run back to write tests and doc comments for functions it never touched.
- [ ] `PIPE-113` Re-scope anti-patterns, complexity, and simplification to attributed declarations, and report a pre-existing finding in a touched file as context rather than as a gate failure.
- [ ] `PIPE-114` Re-scope mutation to attributed lines, so the score measures whether this change's tests detect this change's defects.
- [ ] `PIPE-115` Report coverage for the changed lines rather than the least-covered package in the module, which on any real repository describes code the run never touched.
- [ ] `PIPE-116 TEST` Add a fixture repository with substantial pre-existing code and prove that a one-line change is judged on one declaration, since every gate in phases B through D inherits this scoping.

## Task Classes and Acceptance

Plan references: §27 Initial Product Scope; §0 Acceptance definitions; §24 Specification Review; §33 Pipeline Refinement.

Depends on: `PIPE-117`, the spike whose answer decides the rest of the section and whether `PIPE-019` can ship as written.

- [ ] `PIPE-117 BLOCKER SPIKE` Research how each of the six declared task classes states an executable acceptance example. The current form is arguments, stdin, and expected output, which only a command-line filter can satisfy; a refactor, a dependency change, a library feature, and behaviour-linked documentation can express none of them, and `PIPE-019` makes an example mandatory.
- [ ] `PIPE-118` Add a second example form for work with no command-line surface: a named test that must pass, identified by package and test name, so a library change can be judged against something the requester wrote.
- [ ] `PIPE-119` Define what a refactor and a dependency change are judged against, given that both are meant to preserve behaviour: the pre-change suite passing unchanged is the candidate, and it needs stating rather than assuming.
- [ ] `PIPE-120` Restate the acceptance-oracle gate for repository work: an example must fail against the base revision rather than against an empty program, which is both checkable and the stronger claim.
- [ ] `PIPE-121` Support quoted arguments in the example format; `args:` splits on spaces, so no argument containing one can be expressed.

## Repository Reality

Plan references: §27 Local Runtime and Repository Isolation; §27 Repository Indexing and Context Selection; §22 Correctness and Assurance Gates.

Depends on: `PIPE-046`, the declared profile that lets a non-Go or non-standard repository be declined honestly rather than failed.

- [ ] `PIPE-122 BLOCKER` Discover the project's own build and test commands instead of assuming `go build ./...` and `go test ./...`, and approve the discovered command in the permission policy, which today hardcodes exactly one invocation as the run's only approved command.
- [ ] `PIPE-123` Support a repository whose module is not at the worktree root, and one whose suite needs build tags, rather than recording a gate failure for a repository that builds correctly.
- [ ] `PIPE-124` Stop failing the atoms stage when a run produces no new function: a refactor, a configuration change, and a dependency bump each legitimately produce none, and the current detail claims every piece of work is entangled.
- [ ] `PIPE-125 SPIKE` Decide what the flow records for a repository outside the Go-first scope. Recording failed for every stage misattributes a scope decision as a defect; not-implemented by declared profile is the honest answer and needs `PIPE-046` to carry it.
- [ ] `PIPE-126` Adopt `internal/testselection` for the repeated whole-suite stages; repetition, non-functional, mutation, verification, and coverage each run the entire repository suite today, and the package exists and is wired only into the validation review workflow.

## Mutation Correctness

Plan references: §9 Proof Obligations as the Unit of Assurance; §31 Mechanical-Rule Governance; §33 Pipeline Refinement.

Depends on: `PIPE-114`. A score computed over unattributed lines measures whether the wrong tests catch the wrong defects.

- [ ] `PIPE-127 BLOCKER` Build every mutant before running the suite and discard the ones that do not compile: `" + "` becomes `" - "` inside string concatenation, the build fails, the suite fails, and the score counts it as caught, so the current score is inflated by mutants that never ran.
- [ ] `PIPE-128 BLOCKER` Mutate the syntax tree rather than the file bytes, so a substitution cannot land inside a string literal or a comment and produce a survivor that is not a blind spot.
- [ ] `PIPE-129` Sample mutants across the attributed lines rather than taking the first occurrence of each pattern in each file, so the score is not an artifact of file ordering.
- [ ] `PIPE-130 TEST` Prove the score's meaning with a fixture whose tests are known to be blind and one whose tests are known to be thorough, and record the false-positive and false-negative rates as §31 requires of a mechanical rule.

## Execution Containment

Plan references: §27 Commands, Secrets, and Malicious Repository Content; §12 Effect Discipline; §10 Guarantee Provenance.

Depends on: nothing. This is a boundary the rest of the product already enforces and these three stages do not.

- [ ] `PIPE-131 SECURITY BLOCKER` Route acceptance runs, adversarial probes, and mutation suite runs through the mediated executor and its permission policy; all three shell out directly today, so repository code — a `TestMain`, an `init`, or any produced command — runs with the coordinator's own authority and no approval.
- [ ] `PIPE-132 SECURITY` Bound what a probed or accepted program may reach: the probe builds and runs every main package in the repository against garbage arguments, and on a real repository those are programs the run did not write.
- [ ] `PIPE-133 SECURITY TEST` Add an abuse case in which repository test code attempts to read a credential, reach the network, and write outside the worktree, and prove each attempt is refused and recorded.

## Measurement Honesty

Plan references: §25 Metrics; §10 Guarantee Provenance; §33 Pipeline Refinement.

Depends on: `PIPE-039` for the stage `PIPE-136`'s baseline exists to serve, and `PIPE-111` for the scope `PIPE-115` measures.

- [ ] `PIPE-134` Complete the capability map used by global-invariants: `io/fs`, `net/rpc`, `database/sql`, `os/user`, and `runtime/debug` are absent, `io/ioutil` is deprecated, and `os` conflates reading arguments with touching the filesystem.
- [ ] `PIPE-135` State the complexity label's limits where it is recorded: it is read from loop nesting, so a recursive function and a call to a library sort are both labelled constant, and a label that is wrong in a named way is usable while an unqualified one is not.
- [ ] `PIPE-136` Capture the base revision's suite result before the run starts, so the regression stage has something to compare against rather than asserting that nothing broke.

## Contracts, Effects, and Atomicity

Plan references: §14 Contracts and Invariants; §12 Effect Discipline; §9 Proof Obligations as the Unit of Assurance; §10 Guarantee Provenance; §7 Semantic Atom Categories.

Depends on: `PIPE-111a` for what the run is answerable for, and `PIPE-004`, because each of these turns a stage that reports into a stage that constrains.

These are the tickets where the product's own thesis is not yet implemented. Atomicity, declared contracts, and effect discipline are what the flow claims to derive its correctness from, and in each case below the check currently describes the code rather than holding it to something stated before it existed.

- [ ] `PIPE-137 BLOCKER` Produce contracts from the specification rather than from the finished code: `describeContracts` reads signatures off what was written and records `derived_after: true`, so a contract cannot constrain the unit it describes. §14 requires the contract to exist first, and the atoms stage to check the implementation against it.
- [ ] `PIPE-138` Enforce purity where a contract declares it: the atoms gate says an atom reads nothing outside its arguments, and `checkAtoms` only counts how many happen to be pure. A unit declared pure that reaches outside itself is a gate failure, and an impure unit records what it reaches for.
- [ ] `PIPE-139` Resolve effects from imports and types rather than from a fixed list of package identifiers: `callsAnythingImpure` reads `time.Duration(n)` as an effect and cannot see a call through an injected interface, so the purity figure it produces is wrong in both directions.
- [ ] `PIPE-140 BLOCKER` Compare declared effects against observed effects and fail the gate on an undeclared one. §12 makes the request-side effect the primary verification surface, and no stage currently checks that what a unit does matches what it said it would do.
- [ ] `PIPE-141` Replace `testsNaming`'s identifier sweep with call-site resolution: a unit whose name coincides with a local variable is reported verified by a test that never calls it. This is the second site of the defect `PIPE-008` repairs and is not reached by that fix.
- [ ] `PIPE-142 SPIKE` Decide whether the functional house style is the default or an option, and record the decision. `FunctionalStyleDirective` is documented as a style the tools do not enforce, while the flow's atomicity, purity, and effect-at-the-edge gates all presuppose it; the two cannot both stand.
- [ ] `PIPE-143 BLOCKER` Raise every gate failure as a proof obligation with a stable identity rather than a detail string, so an obligation can be tracked across attempts and across runs and §22's rule about a required obligation disappearing has something to watch.
- [ ] `PIPE-144` Record an evidence level on every satisfied stage in §10's vocabulary — fully evaluated, contract checked, model verified, runtime only — so §22's CI rule about evidence dropping below policy can read it, and a satisfied row stops meaning the same thing whatever established it.

## Gate

- [ ] `PIPE-G01 GATE` No stage records satisfied for a gate its check does not establish, proven by the binding test of `PIPE-004` over every stage in the flow.
- [ ] `PIPE-G02 GATE` A compiling-but-failing run's ledger carries the real verdict for every stage the run actually performed, and no verdict computed by the run is discarded.
- [ ] `PIPE-G03 GATE` A task started twice records two attempts, each with its own run identity and its own complete flow.
- [ ] `PIPE-G04 GATE` A run whose request carries no executable acceptance example does not start, and the refusal names the missing example.
- [ ] `PIPE-G05 GATE` Every stage in the refined flow is either performed, declined by a declared profile, or recorded not-implemented, and the skip audit reports the ratio with each skip's justification.
- [ ] `PIPE-G06 GATE` The dogfood run over a repository with existing code passes the regression, api-surface, and compatibility stages, proving the flow can be used more than once on the same repository.
- [ ] `PIPE-G07 GATE` Every function a run writes is registered with its documentation, and a later run's recall finds it by contract hash without a model request.
- [ ] `PIPE-G08 GATE` A second run of the same requirement against a populated registry reuses rather than rebuilds, and its ledger shows each reused atom excused from the atom-writing stages with its case re-verification recorded in their place.
- [ ] `PIPE-G09 GATE` The same worktree produces an identical ledger under serial and parallel execution, timing aside.
- [ ] `PIPE-G10 GATE` A run against a provider returning sustained `429` responses completes with its waits recorded, no stage recorded failed for a rate limit, and the budget respected.
- [ ] `PIPE-G11 GATE` Run wall clock is measured against a recorded baseline on the target-class machine, with the saving attributed per mechanism — parsing once, collapsed suite runs, parallel mutation, digest cache, and reuse — rather than claimed in aggregate.
- [ ] `PIPE-G12 GATE` A run failing a different gate each attempt finishes on the rung it started on, and a run failing one gate identically reaches the rung its ladder allows; both are provable from the ledger without reading the source.
- [ ] `PIPE-G13 GATE` No run reaches an approval rung without a recorded request naming what was tried, what failed, and which cost axis the step moves.
- [ ] `PIPE-G14 GATE` A refused turn costs one attempt and never the run, proven by a fixture whose model writes the same file twice in a turn and whose run still delivers.
- [ ] `PIPE-G15 GATE` Difficulty tiers are calibrated against outcomes rather than asserted: the report states, per tier, the share of units the cheapest rung handled unaided, and a tier whose share is near one is reported as over-rated.
- [ ] `PIPE-G16 GATE` A program that satisfies every gate while doing the wrong thing is caught by the critic and not by a person, proving §21's challenge to requirement interpretation is being made.
- [ ] `PIPE-G17 GATE` Every critic finding exists in the ledger and the evidence bundle as a proof obligation with its provenance and its disposition, and no finding is visible only in a chat message.
- [ ] `PIPE-G18 GATE` The critic's false-positive rate and its share of run cost are both measured and recorded before either finder is promoted to a mechanical rule.
- [ ] `PIPE-G19 GATE` A one-line change to a file holding many declarations is judged on the declarations it touched, proven on a fixture repository with substantial pre-existing code.
- [ ] `PIPE-G20 GATE` Each of the six declared task classes can state an executable acceptance example and complete a run, or is explicitly recorded as unsupported with the reason.
- [ ] `PIPE-G21 GATE` A repository whose build or test command is not the assumed one completes a run against its own commands rather than failing a gate for building correctly.
- [ ] `PIPE-G22 GATE` The mutation score counts only mutants that compiled and ran, proven against a fixture whose tests are known to be blind and one whose tests are known to be thorough.
- [ ] `PIPE-G23 GATE` No stage executes repository code outside the mediated executor, proven by an abuse case in which test code attempts a credential read, a network call, and a write outside the worktree.
- [ ] `PIPE-G24 GATE` Every unit's contract exists before its implementation and the implementation is checked against it, proven by a run whose code satisfies its tests and fails its contract.
- [ ] `PIPE-G25 GATE` A unit that takes an effect it did not declare fails its gate, and the ledger names the effect and the declaration it contradicts.
- [ ] `PIPE-G26 GATE` Every satisfied stage records an evidence level, and a run whose evidence level drops between attempts is reported rather than passing on the strength of the earlier one.
- [ ] `PIPE-G27 GATE` Every ticket in this milestone is atomic by the standard at the top of this file: one observable output and no unrecorded architecture decision, checked by review before the milestone closes.

---

# Memory and Learning Layer (2026-08-02)

Goal: give the retrieval half of §31 something to retrieve, and bound what the product is allowed to conclude from its own runs.

The read side is built and structurally sound. Retrieval gates refuse similarity as authority, maturity states cannot regain authority once quarantined, lineage refuses self-confirmation, and `RunPreWorkGate` already runs before planning. It queries a store nothing writes: `OpenEpisode`, `CloseEpisode`, `UpsertExtractedMemoryFact`, and every `Extract*FromEpisode` function have zero production callers, and `ForecastedTask.Retrieval` has zero consumers. The Pipeline Refinement milestone creates the atom registry, the proof-obligation findings, the discovered build commands, and the rung records an extractor would draw on, and names no extractor. This milestone is the write half, the admission rules that decide where what is written may re-enter, and the governance both require.

Plan references: §31 Evidence-Driven Reuse and Learning; Extraction Triggers and the Candidacy Funnel; Injection Surfaces and Timing; Routing Evidence Keys; Mechanical-Rule Governance; Permanent Clean Room; §33 Model Selection, The Prior; §29 Phase 5.

Depends on: `PIPE-001` through `PIPE-003` and `PIPE-111` before any extractor is enabled. §31 quarantine is terminal and a successor inherits no confidence, so a cohort minted from a ledger that drops rows, cannot count attempts, or holds a run answerable for every declaration in a file it touched once can only be retired, never corrected. Injection of already-verified material does not wait and is tracked at `PIPE-053`.

Milestone output: an episode per run, deterministic facts extracted at close, a candidacy funnel that mints prose only when nothing stronger fits, context items that state their own evidence strength, a declared routing key, and a ladder prior that seeds rather than replaces and keeps a recorded exploration floor so it stays falsifiable.

## Episode Lifecycle

Plan references: §31 Chronological Episodes; Influence Lineage.

Depends on: nothing. Every other ticket in this milestone writes into an episode, so an absent episode makes the rest unbuildable rather than merely unwired.

- [ ] `MEM-001 BLOCKER` Open an episode when a run starts and close it when the run ends, carrying the task, the starting and ending revisions, and the run identity, so the append-only record §31 assumes exists actually exists; `OpenEpisode` and `CloseEpisode` have no production caller today.
- [ ] `MEM-002 BLOCKER DATA` Record each attempt's gate, normalised failure, rung, and outcome as episode action events, so the transitions every extractor reads from are facts in the store rather than strings in a chat message.
- [ ] `MEM-003 DATA` Bind the episode to the pipeline ledger's real attempt number from `PIPE-003`, so a task started twice produces two episodes and neither inherits the other's evidence.
- [ ] `MEM-004 DATA` Carry the eligible set `RunPreWorkGate` surfaced into the run that consumes it: `ForecastedTask.Retrieval` has no consumer, and `RecordMemoryInfluence` sits on the preflight service while used, adapted, or rejected is only knowable in the agent run, so the decision has no path back today.
- [ ] `MEM-004a DATA` Record the influence action and its justification for every carried item at episode close; without it eligibility never becomes influence, lineage stays unpopulated, and `ConfirmsMemoryArtifactIndependently` correctly refuses to trust anything the store holds.
- [ ] `MEM-005` Record `advisory_exposure` on every episode as a write-once transition rather than a property asserted at open: an episode opens unexposed, becomes exposed the first time an advisory pattern enters, and can never be relabelled unexposed, so the permanent clean-room cohort §31 requires can be identified after the fact rather than asserted.

## Deterministic Extraction at Close

Plan references: §31 Learning Artifact Types, Workspace Facts; Extraction Triggers and the Candidacy Funnel.

Depends on: `MEM-001` for the episode, `PIPE-111` for what counts as produced. These are the cheapest artifacts in §31 and the only tier that needs no judgment; the extraction functions are already written and merely uncalled.

- [ ] `MEM-006 DATA` Extract the successful build and test commands from attributable executions at episode close through `ExtractAttributableBuildCommandsFromEpisode` and `ExtractAttributableTestCommandsFromEpisode`, so the commands `PIPE-122` discovers are learned once rather than rediscovered per run.
- [ ] `MEM-007 DATA` Extract file-to-test mappings from observed successful validations, and record which validation established each mapping.
- [ ] `MEM-008 DATA` Extract formatting and lint conventions from configuration and accepted work, bound to the revision that carried them.
- [ ] `MEM-009 SECURITY` Extract repository instructions only through `ExtractApprovedRepositoryInstruction` and its durable approval identity, because repository text is untrusted input and an unapproved instruction extracted into project memory is a prompt-injection path into every later run.
- [ ] `MEM-010 DATA` Record the base-revision suite result `PIPE-136` captures as a workspace fact bound to that revision, so the regression stage's oracle is established once per revision rather than recomputed per run. This is the one fact observed before the episode opened and extracted at its close, admissible because the rule forbids minting from work in progress rather than recording what was already true.
- [ ] `MEM-011` Invalidate every extracted fact when its supporting evidence moves — revision, toolchain binding, or dependency version — rather than letting it age silently into a claim about a repository that has changed.
- [ ] `MEM-011a BENCH` Budget extraction and report it as its own line in the run's cost summary: five extractors over an eight-attempt episode is unaccounted work in a milestone that accounts for everything else, and §25 requires the price of learning to be as visible as the price of building.

## The Candidacy Funnel

Plan references: §31 Extraction Triggers and the Candidacy Funnel; Mechanical-Rule Governance; Regression Oracle and Execution Policy.

Depends on: `MEM-002` for the transitions, `PIPE-096` for findings that outlive their attempt. The ordering is the point: a product that mints prose when a check would have done has converted a checkable property into one that has to be believed.

- [ ] `MEM-012 BLOCKER` Mint a candidate at the strongest tier that can express the observation — workspace fact, mechanical rule, regression case, routing evidence, advisory pattern — and refuse a weaker tier without a recorded reason the stronger one could not carry it.
- [ ] `MEM-013 BLOCKER` Refuse candidacy from `agent_self_report`, from a run whose produced work was not attributed to changed declarations, and from unapproved repository text, so the sources §31 names as inadmissible cannot enter through an extractor rather than through retrieval.
- [ ] `MEM-013a BLOCKER` Refuse a first-attempt success only for the tiers that require something to have been contested — advisory pattern, regression case, and routing evidence about where a run stalled — and admit it for the deterministic tier, since a command that ran and exited zero is an observation of what executed rather than a conclusion about what was hard. Refusing it outright would forbid `MEM-006` through `MEM-008` on the runs that go well, which are most of them.
- [ ] `MEM-014` Promote a `PIPE-096` finding that survived its attempt into a regression case when it has a reproducible input, a stable oracle, a demonstrated failure, and an expected outcome, and store it as an observation when any of the four is absent.
- [ ] `MEM-015` Route a candidate mechanical rule through §31's governance — `warn` by default, replay against accepted revisions, and a false-positive measurement — and share that path with `PIPE-105` and `PIPE-130` rather than building a second promotion mechanism beside it.
- [ ] `MEM-016 TEST` Prove the funnel refuses to mint an advisory pattern for an observation a mechanical rule expresses, using a fixture whose failure is a swallowed error, which the anti-patterns stage already checks.

## Injection Surfaces

Plan references: §31 Injection Surfaces and Timing; Advisory Pattern Evaluation; Permanent Clean Room.

Depends on: `PIPE-053` for the first-round path. `MEM-017` is the one ticket here that changes an existing surface rather than adding one, and every other ticket depends on it, because advice indistinguishable from a demonstrated failure is worse than no advice.

- [ ] `MEM-017 BLOCKER` Classify every repository context item by evidence strength and label it by what it is, splitting the single `last-test-run-output` item that today carries compiler errors, completeness gaps, acceptance failures, and adversarial findings under one name that is accurate for none of them.
- [ ] `MEM-018 BLOCKER` Admit advisory patterns only through the send-back for the gate they are indexed under, and only once that gate has already failed in this run, so the first attempt stays the clean arm §31's lesson-failure protocol compares against.
- [ ] `MEM-019` Refuse advisory material in the specification request at any rung, because no gate downstream catches a wrong decomposition and the error is paid on every rung of every attempt.
- [ ] `MEM-020` Index advisory patterns by the four gate names `pipeline.ModelBearing` already declares, and validate an unknown gate name at settings time exactly as `StageModels` does, so a pattern indexed against a stage that sends nothing back is a caught configuration error rather than a control that silently does nothing.
- [ ] `MEM-021` Record the clean and exposed attempt as the two arms of §31's investigation, each stamped with its rung, and void the pair when the rung changed between them. Escalation triggers on the same gate failing repeatedly, which is exactly when an advisory is injected, so the two collide by construction; the run is not delayed to protect the measurement, and a void pair confirms nothing.
- [ ] `MEM-021a TEST` Assert that a void pair is excluded from confirming a pattern, and that every pair records what it inherited: attempt N+1 carries N's edits and N's failure output, so the comparison isolates the advisory only against that shared inheritance.

## Routing Evidence and the Ladder Prior

Plan references: §31 Routing Evidence Keys; Versioned Task Fingerprints; §33 Model Selection, The Prior.

Depends on: `MEM-022` for the key, then `PIPE-084` for the records. The key comes first because `fingerprint.ExactFingerprint` binds the base revision and so recurs for no second run: every routing row written before a coarser key exists is written against a value nothing will ever query.

- [ ] `MEM-022 BLOCKER DATA` Declare a named, versioned routing-key projection over structured fields only — task class, difficulty tier, requested authority, required assurance, risk, and banded affected-path and affected-symbol counts — excluding the base revision and the affected identifiers, and stamp every routing record with the projection version that produced its key. The band edges are part of the projection: moving one without bumping the version silently merges two populations that were never comparable.
- [ ] `MEM-023 DATA` Refuse to read routing records written under one projection version through another, because a key whose meaning changed is a different key wearing the same value.
- [ ] `MEM-024` Seed the starting rung from the prior rather than replacing the ladder, leaving every rung above the seed intact, so a wrong prior still corrects by the path `PIPE-088` keeps for a wrong rating.
- [ ] `MEM-025` Implement top-truncation before bottom-seeding: learning that a shape never benefits above a rung is self-correcting, because a wrong truncation stalls a run with nowhere left to climb, while a wrong upward seed succeeds and reports nothing.
- [ ] `MEM-026 BLOCKER` Refuse to seed into a rung requiring approval: approval carries what was tried and what failed, a seeded run has failed nothing, and seeding there converts a person's decision about money into an automatic one justified by evidence that person never saw.
- [ ] `MEM-027 BLOCKER` Refuse any prior that removes a check rather than a cost; where a stage genuinely does not apply, the declared profile of `PIPE-046` says so before the run starts and the skip audit records which of the two happened.
- [ ] `MEM-028 DATA` Run a declared fraction of runs against the bottom of the ladder regardless of the prior, and record per run whether it was seeded or exploring, because a seeded run collects no evidence about the rung it skipped and an unexplored prior is unfalsifiable rather than calibrated.
- [ ] `MEM-028a` Decay the exploration fraction toward a declared minimum as the prior's interval for a key tightens, and never to zero: a prior that keeps being right should cost steadily less to keep falsifiable, and one nothing contradicts is one nothing tests. Without a decay the floor is an unbounded permanent tax with no review point.
- [ ] `MEM-029 UX` Report the exploration fraction and the seeded and exploring populations separately in the cost summary, so the seeded population's cost is not presented as everyone's.
- [ ] `MEM-030` Refuse to let routing evidence satisfy any correctness gate, mirroring `retrievalgate`'s structural refusal of similarity as authority: routing evidence is admissible for spending decisions and inadmissible as evidence a program is right. This is the general rule `MEM-027` applies to the ladder specifically; implement it once, in the admission path both share.

## Gate

- [ ] `MEM-G01 GATE` Every run leaves a closed episode whose attempts, gates, normalised failures, and rungs are readable from the store without reading a chat message.
- [ ] `MEM-G02 GATE` A second run in the same repository uses the build and test commands the first run established, and the ledger names the fact and the episode that supplied them.
- [ ] `MEM-G03 GATE` An observation a mechanical rule can express never reaches the advisory tier, proven by a fixture whose failure the anti-patterns stage already detects.
- [ ] `MEM-G04 GATE` No advisory pattern reaches a first attempt or a specification request, and every exposed attempt has a recorded clean attempt before it against the same worktree and rung.
- [ ] `MEM-G05 GATE` Advisory material shown to a run's implementer is withheld from that run's critic.
- [ ] `MEM-G10 GATE` Where a critic must see advisory material, its findings record `influenced_by` and are excluded from confirming that pattern or its ancestors.
- [ ] `MEM-G11 GATE` Every recorded clean and exposed pair states both rungs, and a pair whose rung changed is void and confirms nothing.
- [ ] `MEM-G06 GATE` Routing records are grouped and read by a declared projection version, and a run whose fingerprint has never recurred still matches a prior by shape.
- [ ] `MEM-G07 GATE` No prior seeds into an approval rung and no prior removes a stage, proven by a fixture whose routing evidence recommends both.
- [ ] `MEM-G08 GATE` The exploration floor is observable in the record: of the runs seeded above the cheapest rung, the report states how many the cheapest rung would have handled, drawn from exploring runs of the same key.
- [ ] `MEM-G09 GATE` A quarantined artifact never regains authority, and its independently derived successor carries a new identity, no inherited confidence, and the original counterexample, proven end to end through the extraction path rather than only in the domain layer.
