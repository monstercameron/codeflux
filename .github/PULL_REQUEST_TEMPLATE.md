<!--
Please read .github/CONTRIBUTING.md before opening this. In particular:
open an issue first, target `dev` and never `main`, and do not add new
Markdown files.
-->

## What changed

<!-- The observable outcome, not the implementation tour. One or two sentences. -->

## Why

<!-- Link the governing issue, and the milestone/task ID from TODOS.md if there
     is one. If this implements something docs/plan.md defers, say so and say
     why it should be un-deferred. -->

Closes #

Governing task ID:

## Verification

<!-- State what you actually ran. Tick only what you ran yourself, and delete
     nothing — an unticked box is information, a deleted one is not. -->

- [ ] `codeflux-dev lint`
- [ ] `codeflux-dev test-fast`
- [ ] `codeflux-dev test-integration`
- [ ] `codeflux-dev test-race`
- [ ] `codeflux-dev test-security`
- [ ] `codeflux-dev test-browser`
- [ ] `codeflux-dev generate-check`
- [ ] `codeflux-dev migration-check`
- [ ] `codeflux-dev artifact-check`
- [ ] Other:

**What I did not run, and why:**

<!-- This section is the point of the list above. "Tests pass" that turns out
     to mean the fast suite only costs the reviewer their trust in every other
     claim here. -->

## Evidence

<!-- What proves this works? Name the new or changed test and what it fails on
     if the fix is reverted. A test that also passes against the unfixed code
     proves nothing. -->

## Limitations

<!-- What this does not cover, what is still ambiguous, what a reviewer should
     be skeptical about. Overstating your own evidence argues against the thing
     you are contributing to. -->

## Checklist

- [ ] One coherent change with one reason to exist; explicit paths staged.
- [ ] `CHANGELOG` entry added with a `CL-YYYYMMDD-NNN` identifier.
- [ ] `DEVLOG` entry added with a `DL-YYYYMMDD-NNN` identifier.
- [ ] Commit messages carry the matching `Change-Log:` and `Dev-Log:` trailers.
- [ ] No new Markdown files.
- [ ] No handwritten JavaScript, TypeScript, HTML, or CSS.
- [ ] Nothing outside `.artifacts/` is written by a tool or a test.
- [ ] No credential reaches SQLite, a log, an artifact, or a worker process.
- [ ] Currency is exact integer minor units; unknown stays unknown, never zero.
- [ ] `internal/domain` still imports nothing infrastructural.
- [ ] Existing migrations are unmodified; new schema is a new migration.
- [ ] Interactive controls have accessible names; no state conveyed by color
      alone.

## Atom naming review

<!-- Delete this section only if the PR declares, renames, or retires no
     `//codeflux:atom`. A name is the retrieval key: a later run finds this atom
     by it or rebuilds the work. `AGENTS.md` holds the full grammar. -->

Atoms declared, renamed, or retired:

- [ ] A reviewer can tell what domain action each name performs, without
      reopening the implementation.
- [ ] Each name is distinguishable from the nearest plausible atom.
- [ ] No name promises more than its contract and evidence support.
- [ ] Each name still reads correctly as a graph-node label and a retrieval
      candidate.
- [ ] Every qualifier is semantically important, not implementation trivia.
- [ ] Any naming exception carries a neighbouring
      `//codeflux:atom-name-exception <kind>: <reason>` with a real reason.
- [ ] A rename kept the stable atom ID, recorded the prior name as an alias, and
      regenerated the affected embeddings.

## Agent involvement

<!-- If a coding agent wrote part of this, say so and confirm you reviewed the
     diff yourself. This is normal here and is not held against a PR — an
     unreviewed one is. -->
