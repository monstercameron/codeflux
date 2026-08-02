# Contributing to CodeFlux

Thank you for looking. Before you spend time on a change, please read this
page — CodeFlux is governed more tightly than most repositories its size, and a
patch that ignores the governance is rejected no matter how good the code is.

## The short version

1. Open an issue first. Unsolicited pull requests are frequently declined for
   reasons that have nothing to do with quality.
2. Branch from `dev`, and open your pull request against `dev`. Never against
   `main`.
3. `docs/plan.md` decides **what** gets built. `TODOS.md` decides **in what
   order**. [`AGENTS.md`](../AGENTS.md) decides **how**. Source, protobuf
   definitions, migrations, and tests are authoritative for what already
   exists.
4. Run `go run ./cmd/codeflux-dev lint` and `test-fast` before you push.
5. Every commit adds one `CHANGELOG` entry and one `DEVLOG` entry, and carries
   the matching trailers.

## Branches

```
  your branch  ──PR──>  dev  ──PR──>  main
   one change            integration    released
                         red CI ok      all checks green
```

**`dev` is where work lands.** It is the default branch, so a pull request
targets it unless you deliberately change that. Every contribution arrives as
its own pull request into `dev` — one coherent change per pull request, the same
rule commits follow.

CI runs on `dev`, and it is **allowed to be red there**. That is the point of
having it: `dev` is where a half-finished integration is allowed to exist and be
looked at. A red `dev` is information, not an emergency.

**`main` is the released branch.** It only ever receives `dev`, and only through
a pull request whose checks are all green:

| Check | Runner |
| --- | --- |
| Quality | Windows 11 ARM64 |
| Build and test | Windows 11 ARM64 |
| Build and test | Windows Server 2025 AMD64 |
| Build and test | macOS 15 ARM64 |
| Build and test | Ubuntu 24.04 AMD64 |
| Race tests | Ubuntu 24.04 AMD64 |
| CodeQL | Analyze Go |

Nobody pushes to `main` directly, force-pushes it, or deletes it — the ruleset
refuses, and there is no administrator bypass. If `main` cannot take a change,
the answer is to make the checks pass, not to route around them.

Dependency bumps are contributions like any other: Dependabot targets `dev`.

## Why an issue first

The plan is not a wish list. It is a sequenced argument in which each milestone
depends on the evidence produced by the one before it, and several capabilities
are deliberately deferred with a stated reason — container isolation, deep
verification, multi-user, hosted operation, automatic updates, provider
fallback. A pull request implementing a deferred capability is not an early
gift; it is work that the plan says must not exist yet.

So the useful first message is: *what outcome do you want, and which milestone
does it belong to?* That conversation costs one issue. Discovering the answer
after writing the code costs your weekend.

**Always welcome without a preceding discussion:**

- a reproducible bug report with the smallest fixture that shows it;
- a failing test that demonstrates a real defect;
- a documentation correction where the docs describe behavior the code does not
  have;
- a security report — but privately, via [`SECURITY.md`](SECURITY.md).

## Setting up

You need **Go 1.26.0 or newer** and Git. Nothing else is required to build.

```
git clone https://github.com/monstercameron/codeflux.git
cd codeflux
go run ./cmd/codeflux-dev bootstrap    # verify and pin the development tools
go run ./cmd/codeflux-dev test-fast    # the default suite
go run ./cmd/codeflux-dev build
```

To run the same lint gate Git-side that CI runs:

```
git config core.hooksPath .githooks
```

The browser suite additionally needs Playwright browsers; the harness reports
what is missing rather than failing obscurely.

CI is authoritative and runs on Windows 11 ARM64, Windows Server 2025 AMD64,
macOS 15 ARM64, and Ubuntu 24.04 AMD64. It invokes the same
`codeflux-dev` commands by the same names, and
`TestM22_124_LocalAndCIShareTheSameCommandGraph` enforces that correspondence —
a gate that existed only in the workflow would be a gate nobody could run
before pushing.

## The commands

```
go run ./cmd/codeflux-dev lint              # gofmt + vet + staticcheck + secret scan
go run ./cmd/codeflux-dev generate-check    # generated output is current
go run ./cmd/codeflux-dev test-fast         # default suite
go run ./cmd/codeflux-dev test-integration  # SQLite integration
go run ./cmd/codeflux-dev test-security     # abuse suites
go run ./cmd/codeflux-dev test-race         # race detector
go run ./cmd/codeflux-dev test-coverage     # coverage report
go run ./cmd/codeflux-dev test-browser      # mounted browser harness
go run ./cmd/codeflux-dev artifact-check    # artifact boundary + credential scan
go run ./cmd/codeflux-dev migration-check   # migration catalog is consistent
go run ./cmd/codeflux-dev benchmark performance
go run ./cmd/codeflux-dev build
```

None of these reach the network. The one command that does — `run-live` — is
deliberately excluded from every suite, so an ordinary test run can never spend
your money or depend on a provider being up.

## Rules that will actually block your pull request

These are the ones contributors trip over. The complete set is in
[`AGENTS.md`](../AGENTS.md).

**No new Markdown files.** Not a README, not a design note, not a summary, not
a nested `AGENTS.md`. A Markdown file is created only when the repository owner
explicitly asks for that specific file. If your change produces knowledge with
nowhere to go, put it in the pull request description or in `DEVLOG`.

**No handwritten JavaScript, TypeScript, HTML, or CSS.** The entire frontend is
Go compiled to WebAssembly through GoWebComponents v5, using the repository's
own primitives and design tokens. This is not a stylistic preference; it is the
product boundary.

**`.artifacts/` is the only place a tool may write.** Binaries, WASM output,
coverage, profiles, benchmark output, failure captures, scratch databases — all
of it. `artifact-check` fails the build when something escapes, and a passing
run is expected to write nothing at all, because an artifact directory full of
successes is noise nobody reads.

**SQLite is the only store for runtime state.** Do not add a JSON, YAML, or
Markdown sidecar for anything CodeFlux manages at runtime.

**Credentials never enter the database, a log, an artifact, or a worker
process.** They live in the operating-system credential store.

**Money is exact integer minor units.** No floating point in a cost, ever. An
unknown price stays unknown and is never rendered as zero.

**`internal/domain` imports nothing infrastructural** — no SQLite, no provider,
no gRPC, no browser, no Git packages.

**Migrations are immutable once merged.** Add a new one.

**Accessibility is a gate, not a polish pass.** Every interactive control needs
an accessible name, every landmark needs a label, and no state may be conveyed
by color alone.

## Tests

A change ships with the test layer that proves it, and
[`docs/developing.md`](../docs/developing.md) names the layer for each kind of
change: storage tests for the transaction, coordinator tests for the flow,
transport tests for the boundary, events tests for validation and replay,
mounted browser checks for rendered behavior, graph replay tests for
projections.

Write the test so that it fails for the reason you claim. A test that passes
against the unfixed code proves nothing and will be sent back.

## Commits

Each commit is one coherent, independently reviewable change with one reason to
exist. Stage explicit paths — never `git add -A` — so unrelated working-tree
changes are not swept in. Separate refactors, formatting sweeps, dependency
bumps, and behavior changes from each other.

Every commit adds:

- a `CHANGELOG` entry with a stable `CL-YYYYMMDD-NNN` identifier, recording the
  outcome, the affected behavior, compatibility impact, and the verification
  that was run;
- a `DEVLOG` entry with a stable `DL-YYYYMMDD-NNN` identifier, recording the
  goal, assumptions, decisions, files and schemas touched, validation, failures
  and discarded approaches, remaining limitations, and the next safe step;
- the matching trailers, so the entry can be resolved back to the commit:

```
Add the durable ledger for pipeline stage transitions

Change-Log: CL-20260802-007
Dev-Log: DL-20260802-007
```

Subjects are imperative and describe the observable change. `updates`, `misc`,
`cleanup`, `fixes`, and `WIP` are rejected. A released `CHANGELOG` entry is
never rewritten or deleted — correct it with a later entry that references the
superseded identifier.

## Pull requests

Fill in the template. In particular, state the verification you actually ran,
and say plainly what you did not run. A blanket "tests pass" that turns out to
mean the fast suite only costs the reviewer their trust in every other claim in
the description.

Describe limitations you know about. This project's central claim is that it
reports what is known, what is ambiguous, and what it recommends as three
separate things — a pull request that overstates its own evidence is arguing
against the thing it is contributing to.

## Working with an agent

Much of this repository was written by a coding agent, and that is expected to
continue. If you are using one, point it at `AGENTS.md` first; the rules above
are not discoverable from the code alone, and a capable agent that has not read
them will produce a confident, well-tested, unmergeable patch.

You remain the author. Review the diff yourself before you open the pull
request — "the agent wrote it" is not a defense for anything in it.

## Conduct and licence

Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md).
Contributions are accepted under the [MIT License](../LICENSE), the same terms
that cover the rest of the project.
