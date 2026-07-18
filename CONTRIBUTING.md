# Contributing to Juridical

Thanks for your interest. Juridical is an authorization- and audit-focused
control plane, so contributions are held to a security-first bar.

## Ground rules

- **Every change lands as a pull request.** Nothing is pushed straight to
  `main`, and nothing is self-merged. Each PR gets an independent review pass
  before merge.
- **The gate is the contract.** `make gate-full` must be green locally before
  you open a PR; CI runs the identical target. It covers formatting, `go vet`,
  build, race-enabled tests with a coverage floor, `golangci-lint`, `gosec`,
  `gitleaks`, a PII/infra scan, a smoke check, and fuzzing of untrusted-input
  parsers.
- **No secrets, no host specifics.** Never commit credentials, private IPs,
  internal hostnames, or personal data. The `gitleaks` and PII gates are
  blocking; a real secret must be rotated, never allowlisted.
- **Commit identity.** Commits are authored under the project identity, not a
  personal account. CI enforces this.

## Workflow

1. Branch from `main`.
2. Make the change with tests. New untrusted-input parsers ship with a fuzz
   target.
3. Run `make gate-full` until green.
4. Open a PR with a clear description and a DOCUMENT section (docs/tables where
   useful). Link the relevant SPEC/ADR.
5. Address review; a maintainer merges once the review is clean.

## Design decisions

Significant decisions are captured as ADRs under `adr/`. If your change alters
an architectural boundary (the action set, blast-radius model, approval flow,
gate, or audit chain), add or update an ADR in the same PR.
