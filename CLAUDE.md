# Claude Code Repository Entry Point

Before beginning work in this repository:

1. read and follow [`AGENTS.md`](AGENTS.md);
2. read §0, Linear Concept and Build Order, in [`docs/plan.md`](docs/plan.md);
3. read the relevant detailed architecture and product sections in `docs/plan.md`;
4. locate the governing milestone and atomic task IDs in [`TODOS.md`](TODOS.md);
5. inspect the current source and tests before assuming planned components exist.

`AGENTS.md` is the authoritative repository-wide agent instruction file. This file is intentionally a thin Claude Code compatibility entry point so the same rules are not duplicated and allowed to drift.

Everything a Claude Code session needs beyond that lives in `AGENTS.md` and is not repeated here: the branch model, the tracked configuration under `.claude/`, the subagents, and the vendored skills.
