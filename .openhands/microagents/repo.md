---
name: repo
type: repo
agent: CodeActAgent
---

# Ecumene — OpenHands repo agent

## Read `AGENTS.md` first

The canonical agent instructions for this repository are in **`AGENTS.md` at the
repo root**. Read it at the start of every task. It is the single source of
truth for:

- what Ecumene is (observation-driven hardened-deployment generator; the
  **forge** in forge → verify → act)
- the engineered dev-loop and the **local-first worktree workflow**
- the **full gate** that must pass (`make gate-full`, mirrored by `ci.yml`)
- PR & branch discipline
- the security doctrine (fail-closed convergence, Rego/OPA doctrine profile,
  evidence for every retained privilege)
- repo structure, the internal design spec component map, and build/test commands

Everything below is **OpenHands-lane specific only**. Shared conventions are not
repeated here — if this file and `AGENTS.md` ever disagree, `AGENTS.md` wins.

## OpenHands-specific

- **You are the routine-tier producer only.** Author a focused change and open a
  pull request. You do not review and you do not merge; the independent
  adversarial review-loop owns everything after the PR is opened.
- **Branch prefix:** this lane prefixes work branches **`openhands/…`** (the
  Claude lane uses its own prefixes). Branch from `main`.
- **Never push to `main`; never self-merge.** The PR-only credential enforces
  this — treat a permission error there as correct behavior, not an obstacle to
  route around.
- **Work in a git worktree and get `make gate-full` green locally before opening
  the PR.** Do not use CI as your iteration loop. Full sequence in `AGENTS.md`.
- **Tier:** this lane runs on **local Devstral**. Keep changes small, focused,
  and verifier-friendly — narrow diffs, tests alongside, clear commit messages.
- **Stay inside P0 scope** (does-it-start convergence, one stateless image,
  Compose output only). If a change reaches past it — seccomp/cap/fs
  observation, multi-emitter, resource tuning — split it out and let the
  review-loop weigh the expansion rather than quietly widening scope.
- **Never downgrade a control to make convergence pass.** Fail closed and report
  it; that behavior is the product, not a blocker to work around.
