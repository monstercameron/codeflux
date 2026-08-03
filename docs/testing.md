# Testing CodeFlux

Four surfaces, three of which cost nothing and one of which spends real money.
Know which one you are running before you run it.

| Surface | Command | Cost | Runs a model |
|---|---|---|---|
| Unit and package tests | `go test ./...` | seconds | no |
| The pipeline flow | `go test ./internal/pipeline/ ./internal/coordinator/` | ~1 min | no |
| Browser checks | `go test ./internal/frontendtest/` | minutes | no |
| The ladder | see below | **real money** | **yes, repeatedly** |

## The ordinary suites

```
go build ./...
go vet ./...
gofmt -l internal/          # prints nothing when the tree is formatted
go test ./... -count=1
```

`-count=1` defeats the result cache. Without it a passing package stays passing
in the output after you have broken it, which is the one lie a test run can
tell.

### The suites worth knowing by name

- `internal/executor` — the tools a run may use. The write guard, the patch
  parser and applier, path escaping, authority classification. About 13
  seconds, most of it subprocess tests.
- `internal/agent` — the loop: plan-step contracts, turn validation, tool
  attribution. Fast.
- `internal/coordinator` — the flow, the gates, the checkpoint, the circuit
  breaker, memory. The largest suite and the one most changes touch.
- `internal/pipeline` — the stage list, the requirement graph, the profiles.
  Fast, and it fails loudly when a dependency edge points forward.
- `internal/openaimodel` — the request shape and the observation document.
- `internal/storage` — schema, migrations, repositories.

### Tests that guard a relationship rather than a function

These exist because a defect lived in the space between two files that were
each individually correct. They are worth running after any change to plans,
tools, or scope:

```
go test ./internal/coordinator/ -run 'TestEveryPlanStepIsAcceptedByTheLoop' -count=1
go test ./internal/coordinator/ -run 'TestTheStepKindFollowsTheFilesystem' -count=1
go test ./internal/coordinator/ -run 'TestAFileBeingPatchedIsShownInFull' -count=1
go test ./internal/executor/   -run 'TestTheToolSaysWhatItTakes' -count=1
```

The first asserts that the plan builder and the loop's validator agree about
which tool completes which kind of step. They disagreed once; every plan was
refused before the first prompt and the run reported it as a model failure.

## The ladder

`internal/coordinator/engine_produces_program_test.go` asks a real model to
write a real program, 250 times over, in rungs of rising difficulty. It is
gated behind an environment variable because **it spends money on every run**.

```
CODEFLUX_LADDER=isolated  # each rung gets its own project
CODEFLUX_LADDER=shared    # every rung runs against one project
```

Shared is the interesting one: atoms, lessons and registry rows accumulate
across rungs, which is the only way to exercise reuse. Isolated is for a clean
baseline.

### Running one rung

Always one rung at a time, always in a visible terminal, always with tracing on:

```powershell
$env:CODEFLUX_LADDER='shared'
$env:CODEFLUX_TRACE='1'
$env:CODEFLUX_ENGINE_ROOT='C:\...\codeflux\.artifacts\rung7'
go test ./internal/coordinator/ `
  -run 'TestTheEngineProducesProgramsThatBuildAndRun/7_summarises_tabular_input_by_column' `
  -count=1 -v -timeout=25m
```

The subtest name is the rung's name with spaces replaced by underscores. The
names are in the table at the top of the test file.

`CODEFLUX_ENGINE_ROOT` must point at a directory that does not exist or is
empty — the harness refuses a dirty root so that what a run finds is what that
run put there. Do not write the log into it; the log counts as an entry.

### Reading the trace

`CODEFLUX_TRACE=1` narrates to stderr as the run happens, because Go buffers a
subtest's own log output until the subtest ends, which is exactly when somebody
watching a slow run stops needing it.

| Category | What it says |
|---|---|
| `phase` | a phase begins; totals and the unaccounted remainder at the end |
| `stage` | number, name, verdict, severity, elapsed, detail |
| `gate` | what the stage wanted, printed when it did not hold |
| `prompt` | what was sent to the model, head and tail, with the omitted count |
| `reply` | tool calls and their arguments, and anything the model said |
| `tool` | each tool: succeeded, failed, unchanged, no-op, out-of-scope |
| `sendback` | which gate refused the work and why |
| `infra` | a provider ruling: disposition and remaining allowance |
| `checkpoint` | captured, restored, discarded |
| `memory` | preflight candidates, what was presented, what was refused |
| `final` | how the run ended |

The line worth reading first is the phase total's last entry, `unaccounted`.
On a four-minute run whose stages measured nine seconds, no stage is the
answer.

### What a healthy rung looks like

- two `apply-edit` calls, creating the two files
- at least one later refinement done with `apply-patch`
- no `apply-edit` against a file that already exists
- a checkpoint captured inside about thirty seconds
- no more than two patches before a test
- a terminal state: `awaiting-review`, `completed`, or an honest bounded failure
- never `recovery-required` caused by this build's own metadata

The rung fails if the program is correct and the pipeline did not converge.
That is deliberate: a green result meaning "the program was right anyway"
cannot measure whether the platform works.

### Reading a run afterwards

The engine root is kept. The database is `codeflux.sqlite3` inside it, and the
produced code is under `worktrees/<repository>/<task>/`.

```
go run ./.artifacts/atomdump  .artifacts/rung7/codeflux.sqlite3
go run ./.artifacts/recheck   .artifacts/rung7/worktrees/<repo>/<task>
```

`atomdump` prints row counts, the stage ledger by attempt, and the lessons
memory holds. `recheck` re-decides the fifteen worktree-only stages against
code on disk with no model calls at all, which is how to tell a gate defect
from a model defect without paying for another run.

Both live under `.artifacts/`, which is disposable by policy: anything there
can be deleted without losing anything that matters.

## Cost discipline

- Never set `CODEFLUX_LADDER` casually. Unscoped, the test runs 250 rungs
  against a paid model.
- Always scope with `-run` to one rung.
- Kill a run by PID when its trace shows it grinding against a platform defect
  rather than a code defect. Find it with the command line, not the process
  name:
  ```powershell
  Get-CimInstance Win32_Process -Filter "Name='coordinator.test.exe'" |
    Where-Object { $_.CommandLine -like '*7_summarises*' }
  ```
- `run-live` is denied in `.claude/settings.json`. It is the only command that
  reaches a provider outside the ladder.

## When a rung fails

Ask which of three things failed, in this order, because the answers are
different work:

1. **The generated program.** The trace shows a compile error or a failing
   assertion in `cmd/generated/`. That is the model's, and the pipeline
   caught it — which is the pipeline working.
2. **A gate.** A stage refused something no correct program could satisfy.
   `recheck` against the worktree confirms it without another model call. Four
   such defects were the same mistake in four places: a synthesised test case
   matched by the literal the synthesiser invented rather than by the property
   it names.
3. **This build's own metadata.** `recovery-required`, a refused plan, a tool
   nobody was offered. Nothing about the model or the work; a defect between
   two files that are each individually correct. The relationship tests above
   exist to catch these before a run pays for them.
