---
name: repo
type: repo
agent: CodeActAgent
---

# Ecumene — repo agent (always loaded)

You are operating inside **ecumene**, the observation-driven hardened-deployment
generator — the **forge** in the suite:
`forge (ecumene) → verify (bulwark) → act (juridical)`.

Ecumene runs a container image, watches what it actually needs, and tightens the
deployment to the minimal working set — emitting a hardened Compose fragment
plus the **evidence** for every retained privilege. It never loosens a control
to force a pass: if it cannot reach a healthy, doctrine-passing state, it
**fails closed**.

> Status: **P0 founding scaffold.** Architecture + fail-closed convergence engine
> are in place; the sandbox runtime and eBPF tracer land next. P0 scope: does-it-
> start convergence on one stateless image, **Compose output only**.

## Your role in the loop

OpenHands is the **routine-tier producer only** — author a focused change and
open a PR. You do not review and do not merge. The engineered dev-loop:

`pull → plan → build → FULL GATE → document → open PR → independent adversarial review → auto-merge on clean`

You own `pull → … → open PR`; the independent review-loop owns the rest.

## PR & branch discipline

- Branch from the default branch; prefix work branches `openhands/…`.
- **Never** push to the default branch (`main`). **Never** self-merge — the
  PR-only credential enforces this; do not try to bypass it.
- One logical change per PR; every PR enters the independent review-loop.
  Conventional Commits.

## The full gate — MUST pass before you call anything done

`make gate-full` is the **single source of truth**; `ci.yml` runs the identical
target so local and CI stay in lockstep. Do not mark work done without proof.

`gate-full: fmt vet build test cover lint gosec gitleaks pii smoke`

- `fmt` — `go fmt` + `gofmt -l` check over `cmd internal` (fails on drift).
- `vet` — `go vet ./...`.
- `build` — `CGO_ENABLED=0 go build -trimpath` → `./ecumene`.
- `test` — `go test -race -count=1 ./...`.
- `cover` — total-coverage floor `COVER_FLOOR=74` (atomic profile).
- `lint` — `golangci-lint run` (required in CI).
- `gosec` — `gosec -quiet ./...` (required in CI).
- `gitleaks` — `gitleaks detect --redact --exit-code 1` (required in CI).
- `pii` — `./tools/pii_scan.sh` (no host IPs, real emails, or domains).
- `smoke` — build + `./ecumene -version` + `./ecumene doctrine` +
  `./ecumene forge nginx@sha256:<digest>`.
- **Fuzz:** no targets in the scaffold yet. Add `FuzzXxx` targets over
  untrusted-input parsers as they land (the internal design spec); keep them advisory (seed
  corpus runs under `make test`), not a blocking `gate-full` dependency.
- **Release/CI:** reproducible GoReleaser build + CycloneDX SBOM + keyless
  cosign; OpenSSF Scorecard + SLSA provenance staged in CI.

## Security doctrine (design constraint, not a phase)

- **Fail-closed is the product.** Observation-derived controls (capabilities,
  tmpfs, …) are held at their fail-closed **defaults** until the sandbox
  runtime + tracer (D-5) tighten them to *observed* use. Never downgrade a
  control to force convergence.
- **Doctrine, enforced.** The substrate is **Rego/OPA**; authors interact via a
  YAML doctrine profile (`internal/doctrine/reference-v1.yaml`) — YAML
  ergonomics on top, OPA rigor underneath, sharing a policy language with
  bulwark's compose-policy-gate. The shipped `reference/v1` is the 14-point
  compose-hardening doctrine; absent an operator profile every control is
  `required` and can never be silently downgraded to `off`.
- **Evidence for every retained privilege** — emit the justification, not just
  the result.
- **Least privilege. Zero-leak. Root cause, not band-aid. Minimal surface.**
  Never commit or echo a secret (refer by shape/fingerprint). Public-repo
  hygiene: no host IPs, no secrets, no PII in code, fixtures, or docs — use
  RFC-5737 / RFC-1918, `example.com`, `noreply@…`.

## Repo context

- **Language:** Go 1.22, module `github.com/ranklancer/ecumene`. Single static
  binary, `CGO_ENABLED=0` (the design notes). Minimal deps (`gopkg.in/yaml.v3`).
- **Commands:** `ecumene forge <image>` (author hardened Compose fragment +
  evidence), `ecumene doctrine` (print the shipped reference/v1 doctrine),
  `ecumene version`.
- **Directories / components:** `cmd/ecumene`; `internal/doctrine` (A1 policy
  engine, profile load/validate + Rego seam), `internal/emit` (A2
  generator/emitter, Compose-only in P0), `internal/sandbox` (A3 launcher —
  runtime deferred), `internal/observe` (A4 tracer — eBPF at D-5),
  `internal/converge` (A5 tighten↔re-verify engine, fail-closed),
  `internal/version`; `docs/`, `tools/` (incl. `pii_scan.sh`), `wiki/`.
- **Build/dev:** `make build`; `make tools` installs `golangci-lint`, `gosec`,
  `govulncheck`.

## Tier note

The routine tier runs on **local Devstral**. Keep changes small, focused, and
verifier-friendly — narrow diffs, tests alongside, clear commit messages. Stay
inside the declared P0 scope; if a change wants to reach past it (seccomp/cap/fs
observation, multi-emitter, resource tuning), split it and let the review-loop
weigh the expansion.
