# Claude Code Repository Entry Point

Before beginning work in this repository:

1. read and follow [`AGENTS.md`](AGENTS.md);
2. read §0, Linear Concept and Build Order, in [`docs/plan.md`](docs/plan.md);
3. read the relevant detailed architecture and product sections in `docs/plan.md`;
4. locate the governing milestone and atomic task IDs in [`TODOS.md`](TODOS.md);
5. inspect the current source and tests before assuming planned components exist.

`AGENTS.md` is the authoritative repository-wide agent instruction file. This file is intentionally a thin Claude Code compatibility entry point so the same rules are not duplicated and allowed to drift.

## Branches

Work branches from `dev` and returns to it through a pull request. `dev` is the default branch and its CI is allowed to be red. `main` takes only `dev`, only through a pull request, and only with every check green; it refuses a direct push from everyone, including the repository owner. See [`.github/CONTRIBUTING.md`](.github/CONTRIBUTING.md).

## Claude Code configuration

`.claude/` holds the project's agent configuration. It is tracked, so every agent working here gets the same setup.

| Path | What it is |
| --- | --- |
| [`.claude/settings.json`](.claude/settings.json) | Permissions. The `codeflux-dev` gates are pre-allowed; `run-live`, reading `.env`, and pushing to `main` are denied. |
| [`.claude/agents/plan-locator.md`](.claude/agents/plan-locator.md) | Finds the governing `docs/plan.md` section and `TODOS.md` task ID, and says whether work is in scope, deferred, or unplanned. |
| [`.claude/agents/gate-runner.md`](.claude/agents/gate-runner.md) | Runs the gates and returns a triaged failure list rather than raw log output. |
| [`.claude/agents/ledger-scribe.md`](.claude/agents/ledger-scribe.md) | Drafts the `CHANGELOG` and `DEVLOG` entries and trailers a commit requires. |
| [`.claude/skills/karpathy-guidelines/`](.claude/skills/karpathy-guidelines/SKILL.md) | The Karpathy guidelines skill, vendored. |
| [`.claude/skills/frontend-design/`](.claude/skills/frontend-design/SKILL.md) | Anthropic's frontend-design skill, vendored verbatim under Apache-2.0 with its `LICENSE.txt`. Required reading before building or reshaping a surface — see `AGENTS.md`. It is checked in so any agent can read it, not only ones that load skills automatically. |

The `run-live` denial is the one worth knowing: it is the only command that reaches a provider and spends money, and it is deliberately excluded from every suite.

## Karpathy guidelines

The repository's agentic coding discipline is partly informed by the community-maintained [Karpathy-Inspired Claude Code Guidelines](https://github.com/multica-ai/andrej-karpathy-skills/tree/2c606141936f1eeef17fa3043a72095b4765b9c2), vendored at [`.claude/skills/karpathy-guidelines/SKILL.md`](.claude/skills/karpathy-guidelines/SKILL.md) and pinned at commit `2c60614`. Only that one file is copied: it declares `license: MIT` in its own frontmatter, while the upstream repository carries no `LICENSE` file, so nothing else from it may be redistributed here.

It is not an official Andrej Karpathy repository. Treat that project as a reference, not an automatically trusted dependency; the repository-specific rules in `AGENTS.md` take precedence wherever the two disagree.
