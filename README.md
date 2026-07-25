<p align="center">
  <img src="assets/logo-dark.svg" alt="Ecumene — the Domain (Forerunner mark)" width="200" height="200">
</p>

# Ecumene

**Observation-driven hardened-deployment generator** — the *forge* in the suite:
**forge (Ecumene) → verify ([bulwark](https://github.com/ranklancer/bulwark)) → act ([juridical](https://github.com/ranklancer/juridical))**.

Ecumene runs a container image, watches what it actually needs, and tightens the
deployment to the minimal working set — emitting a hardened Compose fragment plus
the evidence for every retained privilege. It never loosens a control to force a
pass: if it cannot reach a healthy, doctrine-passing state, it **fails closed**.

> Status: **P0 founding scaffold.** The architecture and the fail-closed
> convergence engine are in place; the sandbox runtime and eBPF tracer land next.

## What P0 does (SPEC-G, D-3 narrow scope)

Does-it-start convergence on **one stateless image**, **Compose output only**. No
full seccomp/cap/fs observation loop, no multi-emitter, no resource tuning yet —
those layer on after P0 proves out.

```
ecumene forge <image>     # author a hardened Compose fragment + evidence
ecumene doctrine          # print the shipped reference/v1 hardening doctrine
ecumene version
```

`forge` emits the static-hardened baseline with observation-derived controls
(capabilities, tmpfs) held at their **fail-closed defaults** until the sandbox
runtime + tracer (D-5) tighten them to observed use.

## The doctrine model (D-4)

The enforcement substrate is **Rego/OPA**, but authors interact through a simple
**YAML doctrine profile** (`internal/doctrine/reference-v1.yaml`) that drives the
Rego — YAML ergonomics on top, OPA rigor underneath, sharing a policy language
with bulwark's `compose-policy-gate`. The shipped `reference/v1` is the 14-point
compose-hardening doctrine; absent an operator profile every control is
`required` and can never be silently downgraded to `off`.

## Architecture (the internal design spec components)

| Package | Component | P0 |
|---|---|---|
| `internal/doctrine` | A1 policy engine (profile load + validate; Rego seam) | ✅ |
| `internal/emit` | A2 generator/emitter (hardened Compose + evidence) | ✅ Compose-only |
| `internal/sandbox` | A3 sandbox launcher (ephemeral, rootless, isolated) | seam (runtime deferred) |
| `internal/observe` | A4 tracer/observer (Tier-1 does-it-start) | seam (eBPF tracer D-5) |
| `internal/converge` | A5 convergence tightener (tighten↔re-verify, fail-closed) | ✅ engine |
| `internal/version` | build metadata | ✅ |

## Building

Go 1.22, `CGO_ENABLED=0` static binary (the design notes). `make gate-full` runs the full
gate: fmt · vet · build · test · coverage · golangci-lint · gosec · gitleaks ·
PII scan · smoke. Release: reproducible GoReleaser build + CycloneDX SBOM +
keyless cosign; OpenSSF Scorecard + SLSA provenance staged in CI.

## License

MIT. Public-repo hygiene: no host IPs, no secrets, no PII in code, fixtures, or docs.
