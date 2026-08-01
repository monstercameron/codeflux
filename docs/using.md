# Using CodeFlux

This is the user guide: installing, connecting a provider, running a first
task, and understanding what CodeFlux will and will not do. It ends with the
limitations, which are as much a part of the guide as anything before them.

## Installing

CodeFlux is one executable. It installs per user and never needs administrator
rights: a coding agent asking for elevation is asking for far more trust than
it needs.

1. Download the artifact for your platform and its `SHA256SUMS` file.
2. Check the checksum:
   - Windows: `Get-FileHash codeflux-windows-amd64.exe -Algorithm SHA256`
   - macOS / Linux: `shasum -a 256 codeflux-darwin-arm64`
3. Verify the signature against the published release key.
4. Put the executable somewhere on your `PATH`.

Everything CodeFlux stores lives in one directory:

| Platform | Data directory |
| --- | --- |
| Windows | `%LOCALAPPDATA%\codeflux` |
| macOS | `~/Library/Application Support/codeflux` |
| Linux | `~/.local/share/codeflux` |

Paths with spaces and non-ASCII characters are supported. A path component
with a leading or trailing space is refused, because some filesystems keep it
and others silently drop it, which makes the same path mean two things.

Check the install with `codeflux doctor`. It reports every prerequisite as ok,
missing, degraded, failed, or unknown, and gives a next step for anything that
is not ok. It exits `3` when something required is absent, so a script can tell
"not set up yet" from "broken".

## Connecting a provider

CodeFlux needs one model provider before it can do any work.

```
codeflux provider set --name anthropic
```

The credential is read from standard input, never from a command-line
argument: an argument is visible to every process on the machine and is written
into your shell history. It is stored in your operating system's credential
store, not in the CodeFlux database and not in any file in your repository.

`codeflux provider test --name anthropic` confirms a credential is stored and
readable. It deliberately does **not** contact the provider: a local check that
reached the network would fail for reasons that have nothing to do with your
key — no connectivity, a proxy, an outage — and would teach you to distrust the
result.

`codeflux provider delete --name anthropic` removes it. Deleting something that
was not there is still reported as success, so cleanup scripts are not fragile.

## Your first task

```
codeflux start
```

This starts the coordinator on a loopback address and opens your browser. The
URL is printed; the session secret is not, because a secret printed to a
terminal survives in scrollback and in shell history. Your browser receives it
as an HttpOnly, same-site cookie without you ever typing it.

Pass `--no-browser` to print the URL and open it yourself.

Then:

1. **Choose a repository.** It must be a Git repository with a clean working
   tree. Uncommitted changes would make it impossible to tell your edits from
   the agent's.
2. **Describe what you want.** One outcome, in your own words. You do not need
   to write a specification.
3. **Read the plan.** CodeFlux proposes a plan before doing anything, listing
   its scope, its steps, the checks it intends to run, and the authority it
   will need. Approve it or ask for changes.
4. **Watch it work.** Every command it wants to run outside the task worktree
   stops and asks you.
5. **Review the diff.** Nothing reaches your branch until you accept it.

## Worktrees, acceptance, repair, rollback, and cleanup

**Worktrees.** CodeFlux never edits your checkout. Each task gets its own Git
worktree, so you can keep working while a task runs, and nothing the agent does
appears in the files you have open.

**Acceptance.** A finished task produces a diff and the evidence behind it:
what was changed, which checks ran, what they said. Accepting merges the change
into your branch. Until then nothing has moved.

**Repair.** When a validation fails, CodeFlux proposes a bounded repair rather
than retrying blindly. A repair resets the approval attached to the plan it
changes, because evidence gathered against the old plan no longer describes
what will run.

**Rollback.** Rejecting a task preserves its patch rather than discarding it.
You can inspect the work even after deciding not to take it.

**Cleanup.** Task worktrees are removed when the task reaches a terminal state.
A worktree whose task ended ambiguously is kept until you resolve it — deleting
it would destroy the only record of what happened.

## Costs, forecasts, and budgets

CodeFlux distinguishes three things that are easy to confuse:

- **A forecast** is an estimate made before the work. It is a range, not a
  promise, and it is never presented as one.
- **An actual cost** is what the provider charged. It appears when the provider
  reports it.
- **Unknown** is what you see when the provider has not reported a price. It
  stays "unknown" and is never rendered as zero: a total that quietly counted
  unknown as nothing would understate your spend exactly when it matters.

A **hard budget** stops new paid work when it is reached. Work already in
flight is allowed to settle, and the task is left resumable. You raise the
limit, finish, or stop — CodeFlux does not decide for you.

All money is exact integer minor units. There is no floating point anywhere in
a cost.

## Permissions

CodeFlux does some things without asking and always asks about the rest.

**Without asking:**

- reading files and searching your repository
- writing inside the task worktree

**Always asks:**

- running any command
- installing any dependency
- any network access
- anything touching files outside the task worktree
- anything reading a credential

Authority is derived from what an action *is* — the tool, its ordered
arguments, and its declared effects — never from what the agent says it needs.
A denied action is not retried through a different tool: a denial is recorded
against the capability, not against the one tool you happened to see.

An approval is an exact action identity. Approving `curl https://example.com`
does not approve `curl https://elsewhere`, and does not approve the same URL
through a different tool.

**Container isolation** is optional and not enabled in the prototype. Without
it, a command you approve runs with your user's privileges. Approve accordingly.

## Your data

Everything is in one SQLite file in the data directory above.

- **Back up** with `codeflux backup --output <path>` while CodeFlux is running.
  It writes a consistent snapshot and never overwrites an existing file.
- **Check** with `codeflux integrity-check`. A pass means the file is
  structurally sound, not that its contents are correct.
- **Inspect** from the interface, or through the read-only inspection surface
  described in [`developing.md`](developing.md). There is no path that lets an
  inspection change anything.
- **Export** a diagnostic bundle with `codeflux diagnostics export --output
  <path>`. It carries versions, counts, and statuses — no requirement text, no
  file contents, no model output — and is scanned before it is written.
- **Delete** by removing the data directory. Your repository is not part of it
  and is not affected.

Clearing the application log clears the log only. Task evidence — events,
plans, approvals, validations, diffs — is stored separately and is untouched.

## The graph

The execution graph shows how a task's work fits together: what depended on
what, which step produced which change, where a decision was made.

**It is an explanation, not a proof.** The graph is projected from the events
the system recorded. It shows what happened; it does not establish that what
happened was correct. Nothing in CodeFlux treats a graph node as evidence, and
neither should you.

## Project memory

CodeFlux remembers things about your project between tasks: repository facts,
reviewed commands, file-to-test mappings, conventions.

**Eligibility.** A memory item is used only when it applies: same project, same
toolchain, same dependencies, and evidence that has not been invalidated.
Similarity is never treated as applicability — an item that *looks* relevant is
not thereby eligible.

**Lineage.** Items record what they were derived from and what merely
influenced them. The distinction matters: invalidating an item automatically
quarantines everything *derived from* it, because those conclusions depended on
it. Items that were only *influenced by* it are flagged for review instead.

**Invalidation.** When evidence stops holding, the item built on it is
invalidated rather than quietly kept. A quarantined item can never regain
authority; a new item must be established instead.

**Vector candidate discovery** is off unless measured retrieval recall
justifies it. When enabled, a vector search proposes *candidates* only. It
never confers eligibility or authority, and every candidate still passes the
same applicability checks as any other item.

## Crash recovery

If CodeFlux, a worker, or your machine stops mid-task, the next start finds the
task and offers only choices that are safe given what it can actually
determine.

It tells you three things separately:

- **what is known**: the last durable checkpoint and what it contains
- **what is ambiguous**: for example, whether a command that reached an
  external system completed
- **what it recommends**: the safest of the available options

When an external effect's outcome cannot be determined, CodeFlux requires you
to reconcile it before anything else proceeds. It will not retry an ambiguous
external effect on your behalf — a duplicate charge, a duplicate deployment, or
a duplicate message is not something it can take back.

## Known limitations

These are real. Read them before deciding what to use CodeFlux for.

**It is not a security sandbox.** A command you approve runs as you, with your
privileges, with access to your files and your network. CodeFlux controls *what
gets proposed* and *what you are asked about*; it does not contain what runs
after you say yes. Container isolation is designed but not enabled in the
prototype. Do not use CodeFlux as a boundary against code you do not trust.

**Prompt injection is mitigated, not solved.** Repository content is untrusted
input, and CodeFlux derives authority structurally rather than from anything a
model or a file says. That closes the direct path. It does not make a model
immune to being misled about *what to propose* — which is why the approval step
exists and why it shows you the exact action.

**External systems may violate their contracts.** A provider may report usage
late, inconsistently, or not at all. An API may accept a request, act on it,
and then fail to respond. A webhook receiver may process a delivery twice.
CodeFlux is built to stay honest when this happens — it will tell you an
outcome is unknown rather than guess — but it cannot make an external system
behave.

**Evidence is bounded by what was checked.** A passing validation means the
commands that ran passed. It does not mean the change is correct, and CodeFlux
never claims it does.

**Forecasts are estimates.** They are frequently wrong. They are shown as
ranges and are never treated as commitments.

**The graph is not proof.** See above.

**Single machine, single user.** There is no multi-user model, no shared state,
and no server deployment. The coordinator binds loopback only.

## Deferred work

Named here so their absence is a decision rather than an oversight:

- **Container and VM isolation** for tool execution — designed, not enabled.
- **Multi-user and team features**: shared projects, roles, audit trails across
  people.
- **Deep verification**: formal methods, property inference, semantic diffing.
  The prototype checks what you tell it to check.
- **Hosted or remote operation.** CodeFlux is local-only by design; making it
  remote is a different product with a different threat model.
- **Automatic updates.** Manual only. An agent that can change your repository
  and hold your credentials must not also replace its own executable without
  being asked.
- **Provider fallback and routing.** One provider at a time.
- **Atom reuse at scale.** The mechanism exists; whether reuse actually pays
  for itself is an open question the prototype exists to answer, with a stated
  kill criterion.
