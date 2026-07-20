# CLAUDE.md

**The canonical agent instructions for this repository live in
[`AGENTS.md`](AGENTS.md). Read that file.**

This pointer exists only because Claude Code looks for `CLAUDE.md` by
convention. `AGENTS.md` is the cross-tool standard and the single source of
truth — the engineered dev-loop, the local-first worktree workflow, the full
gate (`make gate-full`), PR/branch discipline, the security doctrine, and this
repo's structure and build/test commands all live there.

Do not duplicate that content here. If a convention changes, change it in
`AGENTS.md` only — this file and `.openhands/microagents/repo.md` must stay thin
pointers so the two lanes can never drift apart.
