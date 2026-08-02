# Security Policy

CodeFlux is a coding agent. It reads your source, proposes changes, asks to run
commands, and holds a provider credential. That is a large amount of trust for
one executable, so this policy states plainly what the project defends, what it
does not defend, and how to report a problem.

## Supported versions

CodeFlux is a prototype and has not reached a stable release. Only the tip of
`main` receives fixes. There are no backports, and there is no long-term
support branch to report against.

| Version | Supported |
| --- | --- |
| `main` | Yes |
| Anything older | No |

## Reporting a vulnerability

**Report privately. Do not open a public issue.**

Use GitHub's private vulnerability reporting:
[**Report a vulnerability**](https://github.com/monstercameron/codeflux/security/advisories/new).
It creates a draft advisory visible only to you and the maintainer.

Please include:

- what an attacker can do, stated as an outcome rather than as a code smell;
- the smallest reproduction you have, ideally a repository fixture plus the
  exact task text;
- the commit you tested, your platform, and your provider;
- whether the issue requires the user to approve something, and if so, what the
  approval prompt showed them.

That last point matters more than it looks. CodeFlux's entire safety model is
that a dangerous action is *shown accurately before it runs*. A finding that an
approval prompt misrepresents what will execute is a serious vulnerability even
when the action itself is mundane.

**What to expect.** This is a single-maintainer project, not a vendor with an
on-call rotation. Expect an acknowledgement within 7 days and an assessment
within 30. If you have not heard anything in 14 days, comment on the advisory —
a missed notification is far more likely than a decision to ignore you.

Please give a fix a reasonable window before publishing. There is no bounty
program, and none is implied. Credit in the advisory and the `CHANGELOG` is
offered unless you decline it.

## What is in scope

- Any path by which CodeFlux performs an action the user did not authorize:
  a command, a network request, a write outside the task worktree, a credential
  read, an install, a provider switch, a budget increase, or a skipped
  validation.
- Any mismatch between what an approval prompt displays and what actually
  executes, including approval reuse across a different tool, a different
  argument order, or a different target.
- Credential disclosure: a provider key reaching the SQLite database, a log, a
  diagnostic bundle, a test artifact, a worker process, an error message, or a
  crash dump.
- Escape from the artifact boundary: anything writing outside `.artifacts/`
  that claims to be disposable output.
- Loopback assumptions that turn out not to hold: the coordinator, the
  profiler, or the frontend server becoming reachable from another machine.
- Session-secret handling: the cookie, its scope, its lifetime, or any path
  that prints it where scrollback or shell history would keep it.
- Repository content that is treated as instruction rather than as data — see
  the prompt-injection note below for how such a report is triaged.

## What is out of scope

These are documented design limits, not undiscovered bugs. See
[`docs/using.md`](../docs/using.md#known-limitations) for the full statement.

- **CodeFlux is not a security sandbox.** A command you approve runs as your
  user, with your files and your network. Container isolation is designed but
  not enabled. "An approved command did something bad" is the system working as
  documented; "an unapproved command ran" is a vulnerability.
- **Prompt injection is mitigated, not solved.** Authority is derived
  structurally from the action — the tool, its ordered arguments, its declared
  effects — never from what a model or a file asserts. So a report showing that
  a poisoned `README` made the agent *propose* something hostile is expected
  behavior and is why the approval step exists. A report showing that poisoned
  content caused something to *execute without the exact authority required*,
  or caused the approval prompt to under-describe it, is in scope and serious.
- **Model output quality.** A wrong patch, a bad plan, or a hallucinated API is
  a correctness bug — please file it as a regular issue, not a security report.
- **Provider-side issues.** Report those to the provider.
- **Findings from a scanner with no demonstrated impact**, including
  dependency-advisory output for a code path CodeFlux does not reach. Show the
  reachable path.
- **Denial of service against your own machine.** Everything runs locally,
  single-user, on loopback.

## What the project does to keep this honest

- CI runs `codeflux-dev artifact-check`, which scans every retained text
  artifact for fixture credential material and fails the build when it finds
  any.
- Diagnostic bundles carry versions, counts, and statuses only — no requirement
  text, no file contents, no model output — and are scanned before they are
  written.
- Credentials live in the operating-system credential store. Never in SQLite,
  never in a repository file, and never handed to a worker process.
- The profiler requires both loopback *and* a token of at least 32 bytes,
  because a profile is a dump of process memory.
- Provider transport refuses a remote endpoint unless it has been explicitly
  approved. The ordinary test suites never reach the network.

## Reporting a non-security problem

Open a [regular issue](https://github.com/monstercameron/codeflux/issues/new/choose).
If you are unsure which a finding is, report it privately first — that mistake
is easy to undo, and the other one is not.
