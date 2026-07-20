# AGENTS.md — Ecumene

**Canonical agent instructions for this repository.** Every coding agent — Claude
Code, OpenHands, or a human following the same loop — reads *this* file.
`CLAUDE.md` and `.openhands/microagents/repo.md` are thin pointers here; do not
duplicate content into them.

## What this repo is

**Ecumene** is an observation-driven hardened-deployment generator — the **forge**
in the suite:
**forge (ecumene)** → verify ([bulwark](https://github.com/ranklancer/bulwark)) →
act ([juridical](https://github.com/ranklancer/juridical)).

Ecumene runs a container image, watches what it *actually* needs, and tightens
the deployment to the minimal working set — emitting a hardened Compose fragment
plus the **evidence** for every retained privilege. It never loosens a control to
force a pass: if it cannot reach a healthy, doctrine-passing state, it **fails
closed**.

> **Status: P0 founding scaffold.** The architecture and the fail-closed
> convergence engine are in place; the sandbox runtime and eBPF tracer land next.
> P0 scope (SPEC-G, D-3 narrow): does-it-start convergence on **one stateless
> image**, **Compose output only**. No full seccomp/cap/fs observation loop, no
> multi-emitter, no resource tuning yet — those layer on after P0 proves out.

CLI surface:

```
ecumene forge <image>     # author a hardened Compose fragment + evidence
ecumene doctrine          # print the shipped reference/v1 hardening doctrine
ecumene version
```

`forge` emits the static-hardened baseline with observation-derived controls
(capabilities, tmpfs) held at their **fail-closed defaults** until the sandbox
runtime + tracer (D-5) tighten them to observed use.

## The engineered dev-loop

`pull → plan → build → FULL GATE → document → open PR → independent adversarial review → auto-merge on clean`

Agents are the **routine-tier producer only**. You author focused changes and
open a pull request. You are *not* the reviewer and *not* the merger. The
independent adversarial review-loop owns everything after the PR is opened.

### Local-first workflow

Work in a real checkout; do not iterate through CI.

1. **Pull** the default branch and create a **git worktree** for the change:
   `git worktree add ../ecumene-<slug> -b <branch> origin/main`
   A worktree keeps the change isolated and leaves your primary checkout clean.
2. **Plan** before code: approach, files touched, risks, definition of done. Any
   task of three or more steps, or any architectural decision, gets a plan first.
   If execution diverges from the plan, stop and replan. Architectural decisions
   are recorded as ADRs.
3. **Build**, then **run `make gate-full` locally** and **iterate until green**.
   A red gate is not a PR.
4. **Document** the change (code comments where non-obvious, `docs/` or an ADR
   where the decision is architectural).
5. **Open the PR** only once the gate is green locally.
6. Remove the worktree when the PR merges: `git worktree remove ../ecumene-<slug>`

## PR & branch discipline

- Branch from the default branch (`main`). The **OpenHands lane** prefixes work
  branches `openhands/…`.
- **Never push to `main`.** **Never self-merge** — the PR-only credential
  enforces this; do not attempt to work around it.
- One logical change per PR. Every PR enters the independent review-loop like
  any other change.
- Conventional Commits (`feat(converge): …`, `fix(emit): …`, `chore(ci): …`).

## The full gate — MUST pass before anything is "done"

**`make gate-full` is the single source of truth for the quality bar**, and
`ci.yml` runs the identical target so local and CI stay in lockstep. Never mark
work complete without proof it is green — unverified means unfinished.

`gate-full: fmt vet build test cover lint gosec gitleaks pii smoke`

- `fmt` — `go fmt` plus a `gofmt -l` check over `cmd internal` (fails on drift).
- `vet` — `go vet ./...`.
- `build` — `CGO_ENABLED=0 go build -trimpath` → `./ecumene`.
- `test` — `go test -race -count=1 ./...`.
- `cover` — total-coverage floor `COVER_FLOOR=74` (atomic profile).
- `lint` — `golangci-lint run` (required in CI).
- `gosec` — `gosec -quiet ./...` (required in CI).
- `gitleaks` — `gitleaks detect --redact --exit-code 1` (required in CI).
- `pii` — `./tools/pii_scan.sh`.
- `smoke` — build, then `./ecumene -version`, `./ecumene doctrine`, and
  `./ecumene forge` against a digest-pinned image.

**Fuzzing:** no targets exist in the founding scaffold. **Add `FuzzXxx` targets
over untrusted-input parsers as they land (the internal design spec)** — keep them advisory (seed
corpus runs under `make test`), not a blocking `gate-full` dependency.

**Release / CI:** reproducible GoReleaser build + CycloneDX SBOM + keyless
cosign; OpenSSF Scorecard and SLSA provenance staged in CI.

## Security doctrine

Security is a **design constraint from the first line**, never a later phase.
For Ecumene the doctrine *is* the product — the tool exists to produce hardened
output, so it must never model laxity.

- **Fail-closed convergence.** Observation-derived controls (capabilities,
  tmpfs, …) are held at their fail-closed **defaults** until the sandbox runtime
  and tracer (D-5) tighten them to *observed* use. **Never downgrade a control
  to force convergence.** If a healthy doctrine-passing state is unreachable,
  fail closed and say so.
- **Doctrine, enforced.** The enforcement substrate is **Rego/OPA**; authors
  interact through a YAML doctrine profile
  (`internal/doctrine/reference-v1.yaml`) that drives the Rego — YAML ergonomics
  on top, OPA rigor underneath, sharing a policy language with bulwark's
  `compose-policy-gate`. The shipped `reference/v1` is the 14-point
  compose-hardening doctrine; **absent an operator profile every control is
  `required` and can never be silently downgraded to `off`.**
- **Evidence for every retained privilege.** Emit the justification, not just
  the result. A privilege without evidence is a bug.
- **Least privilege.** Minimum scope for every capability, mount, and credential
  — in generated output and in this codebase alike.
- **Zero-leak.** Never commit, echo, or log a secret — in chat, logs, command
  lines, or files; not even rotated ones. Refer to secrets by shape or
  fingerprint only.
- **PII-clean.** Public-repo hygiene: **no host IPs, no secrets, no PII** in
  code, fixtures, or docs — use RFC-5737 / RFC-1918 ranges, `example.com`, and
  `noreply@…`. Enforced by `./tools/pii_scan.sh`.
- **Root cause, not band-aid.** Fix the actual defect; no patches that mask
  symptoms.
- **Minimal impact / minimal surface.** Touch the least code that achieves the
  goal; leave unrelated code alone.

## Repo context

- **Language:** Go 1.22, module `github.com/ranklancer/ecumene`. Single static
  binary, `CGO_ENABLED=0` (the design notes). Minimal dependency surface
  (`gopkg.in/yaml.v3`).
- **Layout / the internal design spec components:**

  | Package | Component | P0 |
  |---|---|---|
  | `internal/doctrine` | A1 policy engine (profile load + validate; Rego seam) | done |
  | `internal/emit` | A2 generator/emitter (hardened Compose + evidence) | Compose-only |
  | `internal/sandbox` | A3 sandbox launcher (ephemeral, rootless, isolated) | seam (runtime deferred) |
  | `internal/observe` | A4 tracer/observer (Tier-1 does-it-start) | seam (eBPF tracer D-5) |
  | `internal/converge` | A5 convergence tightener (tighten↔re-verify, fail-closed) | engine done |
  | `internal/version` | build metadata | done |

  Plus `cmd/ecumene` (entrypoint), `docs/`, `wiki/`, and `tools/` (incl.
  `pii_scan.sh`).
- **Build:** `make build`. `make tools` installs `golangci-lint`, `gosec`,
  `govulncheck`.

## Tier note

The routine tier runs on **local Devstral**. Keep changes small, focused, and
verifier-friendly — narrow diffs, tests alongside, clear commit messages. **Stay
inside the declared P0 scope**; if a change wants to reach past it
(seccomp/cap/fs observation, multi-emitter, resource tuning), split it out and
let the review-loop weigh the expansion.
