# Ecumene architecture (P0)

Ecumene forges a hardened deployment by *observing* an image rather than guessing.
The data spine (the internal design spec): `image ref + doctrine` → **doctrine** classifies each
control static vs. observation-derived → **emit** authors a candidate → the
**converge** loop launches it in the **sandbox**, **observe**s it, tightens to the
minimal working set, and re-verifies → **emit** renders the final Compose + evidence.

## The convergence loop (the internal design spec, P0 Tier-1)

`internal/converge` runs a bounded tighten↔re-verify loop:

1. Author a candidate from the doctrine + the current observed working set
   (initially empty → the tightest possible: `cap_drop: [ALL]`, read-only root,
   no tmpfs).
2. Launch it in the ephemeral, rootless, network-isolated sandbox.
3. Observe: did it start (Tier-1), and did it stay healthy through the soak window?
4. If new capabilities / write paths were observed, incorporate them and re-verify
   (tighten to the minimal *observed* set, each privilege annotated with its
   justifying observation).
5. Converge when healthy **and** stable (no new observed needs). Otherwise, when
   the iteration budget is exhausted or the image never starts, **fail closed** —
   emit the last-good candidate + the evidence of what blocked convergence, never
   a loosened pass.

## Fail-closed invariants

- Absent an operator profile, `reference/v1` applies at `required` for every control.
- An observation-derived control with no observation keeps its safest value
  (`cap_add` empty; read-only root with no tmpfs).
- A doctrine profile with an unknown/renamed field fails to load (strict decode).
- The loop never loosens a control to make a container pass.

## Deferred past P0

A4 eBPF tracer (D-5 substrate pending), A6 resource profiler (P3 load harness),
A7 verify/pin handoff to bulwark + juridical (P4 suite integration), Podman
Quadlet emission (D-6), and vendoring bulwark's `capture.Source` (D-7).
