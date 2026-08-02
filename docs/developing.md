# Developing CodeFlux

This is the working reference for people and agents changing this repository.
It covers where failures leave evidence, how to replay a session, how to look
at the database safely, how to profile, and the golden path for each kind of
vertical change.

Authority order, unchanged by this document: [`docs/plan.md`](plan.md) decides
what is built, [`TODOS.md`](../TODOS.md) decides in what order, and
[`AGENTS.md`](../AGENTS.md) decides how. This file only explains how to operate
the tooling those documents assume.

## Failure artifacts (`M22-128`)

Everything disposable lives under `.artifacts/`. Nothing else in the repository
may be written by a test or a tool, and `codeflux-dev artifact-check` fails the
build if something escapes.

| Artifact | Location | Written when |
| --- | --- | --- |
| Browser scenario failures | `.artifacts/<run>/…failure` | only on a failing scenario |
| Mounted browser evidence | `.artifacts/m16-mounted-render-isolation/` | when the mounted suite runs |
| Benchmark output | `.artifacts/bench/` | on `codeflux-dev benchmark` |
| Coverage | `.artifacts/coverage/` | on `codeflux-dev test-coverage` |
| CI failure bundles | `.artifacts/ci-*/` | on a failing CI step |

Two rules govern them:

- **A passing run writes nothing.** An artifact directory full of successes is
  noise nobody reads, so an artifact is evidence that something failed.
- **Artifacts are scanned for credentials.** `artifact-check` reads every
  retained text artifact and fails if any fixture credential material appears
  in one. Binary profiles are not scanned, because a substring search over a
  heap dump yields neither confidence nor an actionable finding; they are
  instead never committed.

Preserving artifacts is explicit. Harnesses take a `PreserveOnCleanup` /
`PreserveArtifacts` flag; without it they delete their own directory, and the
deletion goes through `testfixtures.ValidateCleanupTarget`, which refuses any
path outside the OS temporary directory.

## Replaying a session (`M22-128`)

A replay fixture is a redacted JSON recording of one session:
`internal/testharness.ReplayFixture`. It carries the starting snapshot and a
contiguous event stream, and it is validated on load — an unredacted export
cannot be saved, and a file containing fixture credential material cannot be
loaded.

```go
fixture, err := testharness.LoadReplayFixture(path, testfixtures.FixtureCredentialShapes())
result, err := testharness.Replay(fixture, testharness.ReplayControls{...}, consumer)
```

The controls reproduce one real transport condition each:

| Control | Reproduces |
| --- | --- |
| `StopAtSequence` | inspecting state partway through a session |
| `StepEvent` | advancing one event at a time |
| `DuplicateSequences` | an event delivered twice |
| `GapSequences` | an event lost in transit |
| `ReconnectAfterSequence` | transport loss and re-subscription |
| `SnapshotRepairAtSequence` | the repair a client must request after a gap |

Two comparisons decide whether a replay proved anything:

- `CompareProjections` reports every disagreement between the server's view and
  the client's. A key present on one side and absent on the other is a
  difference, not a match against empty.
- `CompareGraphRevisions` rebuilds the graph from the replayed events and
  compares it with the original. The graph is a projection of events, so a
  mismatch means the projection depends on something outside the event stream.

## Safe database inspection (`M22-128`)

Use `storage.Repositories.Inspect`. It is read-only by construction: every
entity maps to a fixed parameterised statement, and there is no free-text SQL
door. The plan forbids manual database mutation as a way of making a flow work,
and this surface is what makes that restriction livable.

```go
result, err := repositories.Inspect(ctx, storage.InspectionQuery{
    Entity: storage.InspectEvent,
    TaskID: taskID.String(),
    FromSequence: 100, ToSequence: 200,
    Limit: 50,
})
```

Inspectable entities: `task`, `run`, `event`, `approval`, `checkpoint`, `plan`,
`memory-artifact`, `graph-revision`.

- A limit is **required** and capped at 500. `Truncated` reports when the limit
  was reached, so a clipped answer is never mistaken for a complete one.
- A `NULL` renders as `(null)`, never as an empty string, so "not recorded" is
  never read as "empty".
- Invalidated memory revisions are excluded unless `IncludeInvalidated` is set.
  A stale fact presented beside live ones is how a stale fact gets believed.
- `IncludeLineage` follows `derived_from` — the semantic dependency edge — so
  the descendants an invalidated ancestor would quarantine are visible.

## Profiling and diagnostics (`M22-128`)

All of `internal/devdiag` is **off unless explicitly enabled**. Diagnostics
that default to on become production behaviour nobody chose.

**Structured timing** (`devdiag.Recorder`) covers ten stages: transaction,
event append, event publish, worker lease, provider request, tool execution,
frontend reducer, frontend render, graph projection, and memory retrieval. Each
sample carries the event sequence that caused it, which is what lets a slow
render be traced to the event that triggered it. Attributes are scanned against
forbidden credential material before anything is logged.

**Profiling** (`devdiag.Profiler`) serves CPU, heap, goroutine, mutex, and block
profiles. It requires **both** loopback and a token of at least 32 bytes: a
profile is a dump of process memory, so it must not be reachable from another
machine even by someone holding the token. Mutex and block sampling rates are
restored on `Close`.

**Browser marks** (`devdiag.MarkLedger`) correlate a durable event sequence with
its reducer and render duration, and record which render boundaries actually
re-rendered. `UnexpectedBoundaries` reports an event re-rendering a boundary it
does not own, which is the signature of a render-isolation regression.

## Golden paths (`M22-129`)

Each path names the plan section that governs it, the test layer that proves
it, the event it produces, and the transaction it commits in. Follow the order
given; the order is the point.

### Adding a backend use case

1. Find the governing section in `docs/plan.md` and the atomic task in
   `TODOS.md`. If neither exists, stop and add the task first.
2. Model the domain type in `internal/domain`. It must not import SQLite,
   provider, gRPC, browser, or Git packages — that dependency is prohibited.
3. Add the durable shape: a migration in `migrations/`, then the repository
   method in `internal/storage`. One transaction per user-visible outcome.
4. Add the coordinator service in `internal/coordinator`.
5. Expose it in `api/proto/…`, regenerate, and implement the handler in
   `internal/transport`.
6. Test layer: a `storage` integration test for the transaction, a
   `coordinator` test for the flow, a `transport` test for the boundary.

### Adding an event and its card

1. Declare the `Kind` and payload in `internal/events/session.go`, and handle
   it in the payload validator — an unvalidated kind is unstorable.
2. Add it to the session projection's `EventKinds()`, or the browser drops it.
3. Add the reducer case and the timeline card in `web/frontend`.
4. Test layer: an `events` test for validation and replay, a
   `sessionprojection` test for the reducer, a mounted browser check for the
   card.

### Adding a frontend component

1. Build it in `web/frontend` using `primitives` and the design tokens. No
   handwritten JS, TS, HTML, or CSS.
2. Every interactive control needs an accessible name; every landmark region
   needs a label; no state may be conveyed by colour alone.
3. Test layer: a Go test for the pure projection, **and** a mounted browser
   check in `internal/frontendtest` that drives the component with real clicks
   and real keystrokes. Both are required. The projection test proves the
   projection; only the driven check proves the interface works.
4. The driven check must assert **the event, the resulting state, and the
   render** — those are three separate failures with one symptom — walk the
   keyboard path including focus restoration after an overlay closes, and cover
   the loading, empty, error, and truncated states rather than only the one you
   built in.
5. Screenshot it and **look at the screenshot**. Legibility at rendered scale,
   overlap, clipping, spacing, focus ring inside the viewport. A capture nobody
   opened is not evidence, and UI quality is part of the deliverable.

`AGENTS.md`, under "Frontend changes must be driven in a browser", is the
authoritative statement of this rule. Run the checks with
`codeflux-dev test-browser`; headless rendering is slow, so the existing checks
use 15s–120s timeouts on purpose.

### Adding a graph projection

1. Extend the model in `internal/graph`, respecting the declared bounds.
2. Project it in `internal/coordinator`'s graph projection service, which
   publishes only after SQLite commit.
3. Test layer: a `graph` model test for the bounds, and a replay test proving
   the rebuilt revision matches the original.

### Adding an atom

1. Declare it with a `//codeflux:atom` comment and its documentation fields.
2. Name it through `internal/atomname`; the canonical name is versioned.
3. Test layer: `atomdoc` round-trip, plus the atom's own behaviour tests. An
   atom's value is that it is verified once and reused, so its tests are the
   deliverable, not an afterthought.

### Adding a migration

1. Write `migrations/NNNNNN_name.sql`. Migrations are immutable once merged.
2. Run `codeflux-dev generate` to update the catalog, then `migration-check`.
3. Test layer: `migration-check` plus a storage test exercising the new shape.

### Adding a provider

1. Implement the adapter under `internal/providers/<name>`.
2. Route everything through `internal/providers`' transport, which requires
   explicit endpoint approval — a remote endpoint is refused by default.
3. Pricing is integer minor units. Never floating point for currency.
4. Test layer: adapter tests against `httptest`, and a scripted-provider
   scenario. Ordinary tests never reach the network.

## Atom review reference (`M01-071`)

### Reviewed atom comment example

The example below was not written for this document. It is
`Repositories.AttributeSpend` in `internal/storage/spend_attribution_repository.go`,
the first atom admitted to the repository, quoted after review. The plan blocks
inventing an example earlier on purpose: a contract nobody implemented reads as
precedent once it is in the tree.

```go
// AttributeSpend slices one window's recorded provider spend by flow phase,
// stage, and model.
//
// Codeflux atom documentation (schema v1):
//
//	Purpose:
//	  Answer "what did the atoms cost, what did the molecules cost, what did
//	  the program cost" from the recorded provider ledger, so a person can see
//	  where a task's money went rather than only how much of it went.
//	Use when:
//	  Reporting spend for a closed time window from the local database.
//	Do not use when:
//	  A caller needs live per-request cost during a run; subscribe to session
//	  cost events instead. Do not use it to enforce a budget: the budget ledger
//	  reserves and commits against the authoritative limit, and this is a
//	  read-only summary that lags a call by the width of its own window.
//	Semantics:
//	  Each physical attempt contributes once, through its highest-sequence
//	  accounting row. Stage attribution is approximate and documented on
//	  SpendAttribution. Totals are independent of attribution.
//	...
//
//codeflux:atom
func (repositories *Repositories) AttributeSpend(
	ctx context.Context,
	window MetricsWindow,
) (SpendAttribution, error)
```

The full field set is in the source; `AGENTS.md` lists every required field and
`internal/atomdoc` parses them. What review looked for, and what a new atom is
held to:

1. **Purpose states the domain outcome, not the signature.** "Answer what the
   atoms cost" is a question a person has; "slice a struct by a key" is not.
2. **"Do not use when" names the near-matches.** Both exclusions are real ways a
   caller could pick this atom and be wrong: live cost, and budget enforcement.
   An exclusion list that names no plausible mistake is decoration.
3. **The claims are bounded by what the code establishes.** "Stage attribution
   is approximate" is in the comment because it is approximate. A comment
   promising exact attribution would be the failure this obligation exists to
   prevent, and no test would have caught it.
4. **Verification names the test file**, so the comment's claims are traceable
   to something that runs.

## Running things

```
go run ./cmd/codeflux-dev lint             # staticcheck + vet + format
go run ./cmd/codeflux-dev generate-check   # generated output is current
go run ./cmd/codeflux-dev test-fast        # the whole default suite
go run ./cmd/codeflux-dev test-integration # SQLite integration
go run ./cmd/codeflux-dev test-security    # abuse suites
go run ./cmd/codeflux-dev test-browser     # browser harness
go run ./cmd/codeflux-dev benchmark performance
go run ./cmd/codeflux-dev artifact-check   # artifact + credential scan
go run ./cmd/codeflux-dev migration-check
```

CI runs the same commands by the same names. A gate that only existed in the
workflow would be a gate nobody could run before pushing, so
`TestM22_124_LocalAndCIShareTheSameCommandGraph` enforces the correspondence.

None of these reach the network. The one command that does —
`codeflux-dev run-live` — is deliberately not part of any suite.
