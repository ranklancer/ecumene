package converge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ranklancer/ecumene/internal/doctrine"
	"github.com/ranklancer/ecumene/internal/emit"
	"github.com/ranklancer/ecumene/internal/observe"
	"github.com/ranklancer/ecumene/internal/sandbox"
)

type fakeLauncher struct {
	launches, stops int
	// specs records every RunSpec handed to Launch, in call order, so tests
	// can assert what the sandbox was actually asked to verify.
	specs []sandbox.RunSpec
}

func (f *fakeLauncher) Launch(_ context.Context, spec sandbox.RunSpec) (sandbox.Handle, error) {
	f.launches++
	f.specs = append(f.specs, spec)
	return sandbox.Handle{ID: "h"}, nil
}
func (f *fakeLauncher) Stop(_ context.Context, _ sandbox.Handle) error { f.stops++; return nil }

// errInfraLaunch and errInfraTrace are the sentinel infra errors used by the
// scripted-failure launcher/tracer below. They stand in for a Launcher.Launch
// or Tracer.Trace failure unrelated to the candidate's health (e.g. a Docker
// daemon hiccup), as opposed to ErrDidNotStart/ErrDidNotConverge which are
// this package's own fail-closed verdicts.
var errInfraLaunch = errors.New("infra: launch failed")
var errInfraTrace = errors.New("infra: trace failed")

// scriptedFailLauncher succeeds like fakeLauncher except on the failAt'th
// (1-indexed) call to Launch, where it returns errInfraLaunch. failAt == 0
// means never fail.
type scriptedFailLauncher struct {
	failAt          int
	launches, stops int
}

func (f *scriptedFailLauncher) Launch(_ context.Context, _ sandbox.RunSpec) (sandbox.Handle, error) {
	f.launches++
	if f.failAt != 0 && f.launches == f.failAt {
		return sandbox.Handle{}, errInfraLaunch
	}
	return sandbox.Handle{ID: "h"}, nil
}
func (f *scriptedFailLauncher) Stop(_ context.Context, _ sandbox.Handle) error {
	f.stops++
	return nil
}

// scriptedTracer returns a preset Result per Trace call (last repeats). If
// failAt is nonzero, the failAt'th (1-indexed) call returns errInfraTrace
// instead of a scripted Result.
type scriptedTracer struct {
	seq    []observe.Result
	i      int
	failAt int
}

func (t *scriptedTracer) Trace(_ context.Context, _ sandbox.Handle) (observe.Result, error) {
	t.i++
	if t.failAt != 0 && t.i == t.failAt {
		return observe.Result{}, errInfraTrace
	}
	r := t.seq[min(t.i-1, len(t.seq)-1)]
	return r, nil
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func newLoop(l sandbox.Launcher, tr observe.Tracer) Loop {
	return Loop{Launcher: l, Tracer: tr, Profile: doctrine.Reference(), Service: "app", MaxIter: 5}
}

func TestConverge_HealthyStable_ConvergesFirstIteration(t *testing.T) {
	fl := &fakeLauncher{}
	res, err := newLoop(fl, &scriptedTracer{seq: []observe.Result{{Started: true, Healthy: true}}}).Converge(context.Background(), "img")
	if err != nil {
		t.Fatalf("should converge: %v", err)
	}
	if !res.Converged || res.Iterations != 1 {
		t.Fatalf("converged=%v iters=%d", res.Converged, res.Iterations)
	}
	if fl.stops != fl.launches {
		t.Errorf("every launch must be stopped: launches=%d stops=%d", fl.launches, fl.stops)
	}
}

func TestConverge_TightenThenConverge(t *testing.T) {
	// iter1: reveals a needed cap (grows). iter2: same set, stable+healthy -> converge.
	tr := &scriptedTracer{seq: []observe.Result{
		{Started: true, Healthy: true, Caps: []string{"NET_BIND_SERVICE"}},
		{Started: true, Healthy: true, Caps: []string{"NET_BIND_SERVICE"}},
	}}
	res, err := newLoop(&fakeLauncher{}, tr).Converge(context.Background(), "img")
	if err != nil {
		t.Fatalf("should converge after tighten: %v", err)
	}
	if res.Iterations != 2 {
		t.Fatalf("expected 2 iterations, got %d", res.Iterations)
	}
	if !strings.Contains(string(res.Compose), "NET_BIND_SERVICE") {
		t.Errorf("converged compose must carry the observed cap:\n%s", res.Compose)
	}
}

func TestConverge_NeverStarts_FailsClosed(t *testing.T) {
	res, err := newLoop(&fakeLauncher{}, &scriptedTracer{seq: []observe.Result{{Started: false}}}).Converge(context.Background(), "img")
	if !errors.Is(err, ErrDidNotStart) {
		t.Fatalf("must fail closed as did-not-start, got %v", err)
	}
	if res.Converged {
		t.Error("must not report converged")
	}
	if len(res.Compose) == 0 {
		t.Error("fail-closed result must still carry the last-good candidate + evidence")
	}
}

func TestConverge_BudgetExhausted_FailsClosed(t *testing.T) {
	// tracer keeps revealing a new cap every iteration -> never stabilises.
	seq := make([]observe.Result, 6)
	for i := range seq {
		seq[i] = observe.Result{Started: true, Healthy: true, Caps: []string{"CAP_" + string(rune('A'+i))}}
	}
	res, err := Loop{Launcher: &fakeLauncher{}, Tracer: &scriptedTracer{seq: seq}, Profile: doctrine.Reference(), MaxIter: 3}.Converge(context.Background(), "img")
	if !errors.Is(err, ErrDidNotConverge) {
		t.Fatalf("exhausted budget must fail closed, got %v", err)
	}
	if res.Iterations != 3 {
		t.Errorf("should stop at the iteration budget, got %d", res.Iterations)
	}
}

// TestConverge_MidLoopLaunchInfraError_EmitsLastVerifiedCandidate proves the
// fail-closed hardening from PR#1's Opus review: on a mid-loop Launcher.Launch
// infra error, Converge must emit the last candidate that completed a full
// start+observe verify cycle -- never the current iteration's candidate,
// whose caps were just grown from the prior observation but never re-verified.
//
// iter1: starts healthy but reveals CAP_A is needed -> tighten (grow to
//
// [CAP_A]). iter1's rendered candidate (which does NOT grant CAP_A) is
// now the last-verified candidate.
//
// iter2: rendered WITH CAP_A granted; starts healthy but reveals CAP_B is
//
// also needed -> tighten (grow to [CAP_A, CAP_B]). iter2's rendered
// candidate (which grants CAP_A but not CAP_B) becomes the new
// last-verified candidate, since it completed a full verify cycle.
//
// iter3: rendered WITH CAP_A+CAP_B granted -- this candidate is NEVER
//
// launched successfully: Launcher.Launch fails with an infra error.
//
// Under the old behaviour, Converge would return iter3's un-re-verified
// candidate (which carries CAP_B despite CAP_B never having been proven to
// start+run healthy). Under the fixed behaviour, it must return iter2's
// candidate instead: Iterations == 2, and the emitted Compose carries CAP_A
// (proven) but not CAP_B (never re-verified).
func TestConverge_MidLoopLaunchInfraError_EmitsLastVerifiedCandidate(t *testing.T) {
	tr := &scriptedTracer{seq: []observe.Result{
		{Started: true, Healthy: true, Caps: []string{"CAP_A"}},
		{Started: true, Healthy: true, Caps: []string{"CAP_A", "CAP_B"}},
	}}
	fl := &scriptedFailLauncher{failAt: 3} // iter3's Launch call fails
	res, err := newLoop(fl, tr).Converge(context.Background(), "img")

	if !errors.Is(err, errInfraLaunch) {
		t.Fatalf("expected the infra launch error to propagate, got %v", err)
	}
	if res.Converged {
		t.Error("must not report converged on a mid-loop infra error")
	}
	if res.Iterations != 2 {
		t.Fatalf("must emit the last FULLY-VERIFIED candidate (iteration 2), got Iterations=%d", res.Iterations)
	}
	if !strings.Contains(string(res.Compose), "CAP_A") {
		t.Errorf("emitted candidate must carry CAP_A, which was proven in a completed verify cycle:\n%s", res.Compose)
	}
	if strings.Contains(string(res.Compose), "CAP_B") {
		t.Errorf("emitted candidate must NOT carry CAP_B: it was only ever granted to the iter3 candidate, which never completed a start+observe cycle (this is exactly the un-re-verified candidate the old code emitted):\n%s", res.Compose)
	}
	// Under the pre-fix code this assertion is where the test would actually
	// fail: `last` at the point of the Launch error is iter3's Result, whose
	// Iterations is 3 and whose Compose contains CAP_B.
	if fl.launches != 3 {
		t.Fatalf("expected exactly 3 Launch attempts before the infra failure, got %d", fl.launches)
	}
}

// TestConverge_MidLoopTraceInfraError_EmitsLastVerifiedCandidate is the same
// property as above, but for a mid-loop Tracer.Trace infra error instead of a
// Launcher.Launch one -- the loop's other infra-error exit point.
//
// iter1: rendered with no extra caps; starts healthy but reveals CAP_A is
// needed -> tighten. iter1's candidate becomes last-verified.
// iter2: rendered WITH CAP_A granted; Tracer.Trace itself fails with an infra
// error before any health/observation result is available for this
// candidate, so it never completes a verify cycle.
//
// The emitted result must be iter1's candidate (Iterations == 1, no CAP_A in
// the compose), never iter2's un-re-verified, CAP_A-granting candidate.
func TestConverge_MidLoopTraceInfraError_EmitsLastVerifiedCandidate(t *testing.T) {
	tr := &scriptedTracer{
		seq:    []observe.Result{{Started: true, Healthy: true, Caps: []string{"CAP_A"}}},
		failAt: 2, // iter2's Trace call fails
	}
	fl := &fakeLauncher{}
	res, err := newLoop(fl, tr).Converge(context.Background(), "img")

	if !errors.Is(err, errInfraTrace) {
		t.Fatalf("expected the infra trace error to propagate, got %v", err)
	}
	if res.Converged {
		t.Error("must not report converged on a mid-loop infra error")
	}
	if res.Iterations != 1 {
		t.Fatalf("must emit the last FULLY-VERIFIED candidate (iteration 1), got Iterations=%d", res.Iterations)
	}
	if strings.Contains(string(res.Compose), "CAP_A") {
		t.Errorf("emitted candidate must NOT carry CAP_A: it was only ever granted to the iter2 candidate, which never completed a start+observe cycle:\n%s", res.Compose)
	}
	// Every launch this loop performs must still be stopped, including the
	// one whose Trace call failed.
	if fl.stops != fl.launches {
		t.Errorf("every launch must be stopped, even when Trace errors: launches=%d stops=%d", fl.launches, fl.stops)
	}
}

// TestConverge_RunSpecMirrorsEmittedDoctrine pins the invariant raised in the
// PR#5 Opus review: the RunSpec handed to the Launcher must mirror exactly
// what emit.Harden would write for the same doctrine profile and
// observation, so the sandbox launch verifies the config that will actually
// ship. For the shipped reference profile, read-only-root-tmpfs and
// cap-drop-all-min-add are both required, so the final iteration's RunSpec
// must have ReadOnly==true and CapAdd/Tmpfs carrying exactly the observed
// set -- the same set emit.Harden would put in cap_add/tmpfs.
func TestConverge_RunSpecMirrorsEmittedDoctrine(t *testing.T) {
	fl := &fakeLauncher{}
	tr := &scriptedTracer{seq: []observe.Result{
		{Started: true, Healthy: true, Caps: []string{"NET_BIND_SERVICE"}, WritePaths: []string{"/var/run/app"}},
		{Started: true, Healthy: true, Caps: []string{"NET_BIND_SERVICE"}, WritePaths: []string{"/var/run/app"}},
	}}
	_, err := newLoop(fl, tr).Converge(context.Background(), "img")
	if err != nil {
		t.Fatalf("should converge: %v", err)
	}
	if len(fl.specs) != 2 {
		t.Fatalf("expected 2 launches, got %d", len(fl.specs))
	}
	final := fl.specs[len(fl.specs)-1]
	if !final.ReadOnly {
		t.Error("RunSpec.ReadOnly must be true: the reference profile requires read-only-root-tmpfs, which is exactly the condition under which emit.Harden writes read_only: true -- a false here would mean the sandbox verified a WEAKER config than what ships")
	}
	if len(final.Tmpfs) != 1 || final.Tmpfs[0] != "/var/run/app" {
		t.Errorf("RunSpec.Tmpfs must carry the observed write path emit.Harden would tmpfs-mount, got %v", final.Tmpfs)
	}
	if len(final.CapAdd) != 1 || final.CapAdd[0] != "NET_BIND_SERVICE" {
		t.Errorf("RunSpec.CapAdd must carry the observed capability emit.Harden would cap_add, got %v", final.CapAdd)
	}
}

// TestConverge_RunSpecOmitsUngatedFieldsWhenControlNotRequired proves the
// RunSpec derives ReadOnly/Tmpfs/CapAdd from the SAME doctrine.Profile
// predicate emit.Harden uses (p.Required("read-only-root-tmpfs") and
// p.Required("cap-drop-all-min-add")) rather than hardcoding them on. With a
// profile where those two controls are explicitly "off", emit.Harden would
// leave ReadOnly=false and Tmpfs/CapAdd nil (see internal/emit/emit.go); the
// RunSpec handed to the Launcher must match, or the sandbox would verify a
// STRICTER environment than the one that ships -- also a divergence from
// "verifies the config that ships", just in the safe direction. If a future
// change re-hardcodes ReadOnly: true or unconditionally forwards
// caps/writes regardless of the profile, this test fails.
func TestConverge_RunSpecOmitsUngatedFieldsWhenControlNotRequired(t *testing.T) {
	raw := []byte(`
name: no-ro-no-cap
version: v1
controls:
  no-new-privileges:      { status: required, kind: static }
  cap-drop-all-min-add:   { status: off, kind: observation }
  read-only-root-tmpfs:   { status: off, kind: observation }
  no-privileged:          { status: required, kind: static }
  no-docker-socket:       { status: required, kind: static }
  minimal-ro-mounts:      { status: required, kind: observation }
  secrets-by-reference:   { status: required, kind: static }
  restart-unless-stopped: { status: required, kind: static }
  lan-bound-ports:        { status: required, kind: static }
  resource-limits:        { status: warn, kind: observation }
  log-driver-limits:      { status: required, kind: static }
  healthchecks:           { status: required, kind: static }
  pin-image-digest:       { status: required, kind: static }
  dedicated-bridge-net:   { status: required, kind: static }
`)
	p, err := doctrine.Load(raw)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}

	fl := &fakeLauncher{}
	tr := &scriptedTracer{seq: []observe.Result{
		{Started: true, Healthy: true, Caps: []string{"NET_BIND_SERVICE"}, WritePaths: []string{"/var/run/app"}},
		{Started: true, Healthy: true, Caps: []string{"NET_BIND_SERVICE"}, WritePaths: []string{"/var/run/app"}},
	}}
	loop := Loop{Launcher: fl, Tracer: tr, Profile: p, Service: "app", MaxIter: 5}
	_, err = loop.Converge(context.Background(), "img")
	if err != nil {
		t.Fatalf("should converge: %v", err)
	}
	if len(fl.specs) == 0 {
		t.Fatal("expected at least one launch")
	}
	final := fl.specs[len(fl.specs)-1]
	if final.ReadOnly {
		t.Error("RunSpec.ReadOnly must be false: read-only-root-tmpfs is not required by this profile, so emit.Harden would never write read_only: true for this candidate")
	}
	if len(final.Tmpfs) != 0 {
		t.Errorf("RunSpec.Tmpfs must be empty: emit.Harden only sets tmpfs inside its read-only-root-tmpfs required branch, got %v", final.Tmpfs)
	}
	if len(final.CapAdd) != 0 {
		t.Errorf("RunSpec.CapAdd must be empty: emit.Harden only sets cap_add inside its cap-drop-all-min-add required branch, got %v", final.CapAdd)
	}
}

// TestConverge_RunSpecMirrorsEmitHarden_Serve is the structural mirror test
// requested by the sandbox-fidelity review: rather than hand-asserting
// expected RunSpec values, it runs the REAL converge.Loop (fakeLauncher
// captures the RunSpec it was handed) and separately calls the REAL
// emit.Harden with the same profile and the observation the loop converged
// on, then compares the two independently-produced outputs on EVERY axis --
// ReadOnly, Tmpfs, CapAdd, User, Memory and Network. If a future change lets
// the launcher and the emitter drift on any axis (e.g. a hardcoded default
// that stops matching what a profile requires), this test fails.
//
// This profile requires non-root-user, memory-limit, dedicated-bridge-net
// and network-mode: serve -- a service that must be reachable. The launch
// must therefore NOT be network-isolated (never --network none for a
// serving profile: that would make a port-serving candidate unreachable and
// produce a false FAIL, exactly the over-reject bug this change fixes).
func TestConverge_RunSpecMirrorsEmitHarden_Serve(t *testing.T) {
	raw := []byte(`
name: serves
version: v1
controls:
  no-new-privileges:      { status: required, kind: static }
  cap-drop-all-min-add:   { status: required, kind: observation }
  read-only-root-tmpfs:   { status: required, kind: observation }
  non-root-user:          { status: required, kind: static }
  memory-limit:           { status: required, kind: static }
  network-mode:           { status: required, kind: static, value: serve }
  dedicated-bridge-net:   { status: required, kind: static }
`)
	p, err := doctrine.Load(raw)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}

	fl := &fakeLauncher{}
	tr := &scriptedTracer{seq: []observe.Result{
		{Started: true, Healthy: true, Caps: []string{"NET_BIND_SERVICE"}, WritePaths: []string{"/var/run/app"}},
		{Started: true, Healthy: true, Caps: []string{"NET_BIND_SERVICE"}, WritePaths: []string{"/var/run/app"}},
	}}
	loop := Loop{Launcher: fl, Tracer: tr, Profile: p, Service: "app", MaxIter: 5}
	if _, err := loop.Converge(context.Background(), "img"); err != nil {
		t.Fatalf("should converge: %v", err)
	}
	if len(fl.specs) == 0 {
		t.Fatal("expected at least one launch")
	}
	final := fl.specs[len(fl.specs)-1]

	obs := observe.Result{Caps: []string{"NET_BIND_SERVICE"}, WritePaths: []string{"/var/run/app"}}
	c, _, err := emit.Harden("app", "img", p, obs)
	if err != nil {
		t.Fatalf("emit.Harden: %v", err)
	}
	svc := c.Services["app"]

	if final.ReadOnly != svc.ReadOnly {
		t.Errorf("RunSpec.ReadOnly = %v, emit.Harden svc.ReadOnly = %v -- must match", final.ReadOnly, svc.ReadOnly)
	}
	if !equal(final.Tmpfs, svc.Tmpfs) {
		t.Errorf("RunSpec.Tmpfs = %v, emit.Harden svc.Tmpfs = %v -- must match", final.Tmpfs, svc.Tmpfs)
	}
	if !equal(final.CapAdd, svc.CapAdd) {
		t.Errorf("RunSpec.CapAdd = %v, emit.Harden svc.CapAdd = %v -- must match", final.CapAdd, svc.CapAdd)
	}
	if final.User != svc.User {
		t.Errorf("RunSpec.User = %q, emit.Harden svc.User = %q -- must match", final.User, svc.User)
	}
	if final.Memory != svc.MemLimit {
		t.Errorf("RunSpec.Memory = %q, emit.Harden svc.MemLimit = %q -- must match", final.Memory, svc.MemLimit)
	}
	if final.Network != "serve" {
		t.Errorf("RunSpec.Network = %q, want %q for a serving profile -- a serving profile must never be sandboxed under --network none", final.Network, "serve")
	}
	if svc.NetworkMode != "" || len(svc.Networks) == 0 {
		t.Errorf("emit.Harden for a serving profile must leave network_mode unset and assign a bridge network, got network_mode=%q networks=%v", svc.NetworkMode, svc.Networks)
	}
}

// TestConverge_RunSpecMirrorsEmitHarden_Batch is the batch/one-shot
// counterpart: network-mode: none. The launch must be fully isolated
// (--network none), exactly mirroring emit.Harden's network_mode: none.
func TestConverge_RunSpecMirrorsEmitHarden_Batch(t *testing.T) {
	raw := []byte(`
name: batch
version: v1
controls:
  no-new-privileges:      { status: required, kind: static }
  cap-drop-all-min-add:   { status: required, kind: observation }
  read-only-root-tmpfs:   { status: required, kind: observation }
  non-root-user:          { status: required, kind: static }
  memory-limit:           { status: required, kind: static }
  network-mode:           { status: required, kind: static, value: none }
  dedicated-bridge-net:   { status: required, kind: static }
`)
	p, err := doctrine.Load(raw)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}

	fl := &fakeLauncher{}
	tr := &scriptedTracer{seq: []observe.Result{{Started: true, Healthy: true}}}
	loop := Loop{Launcher: fl, Tracer: tr, Profile: p, Service: "job", MaxIter: 5}
	if _, err := loop.Converge(context.Background(), "img"); err != nil {
		t.Fatalf("should converge: %v", err)
	}
	if len(fl.specs) == 0 {
		t.Fatal("expected at least one launch")
	}
	final := fl.specs[len(fl.specs)-1]

	c, _, err := emit.Harden("job", "img", p, observe.Result{})
	if err != nil {
		t.Fatalf("emit.Harden: %v", err)
	}
	svc := c.Services["job"]

	if final.Network != "none" {
		t.Errorf("RunSpec.Network = %q, want %q for a batch/one-shot profile", final.Network, "none")
	}
	if svc.NetworkMode != "none" {
		t.Errorf("emit.Harden for a batch profile must set network_mode: none, got %q", svc.NetworkMode)
	}
	if len(svc.Networks) != 0 {
		t.Errorf("emit.Harden for a batch profile must NOT also assign a bridge network (mutually exclusive with network_mode), got %v", svc.Networks)
	}
	if final.User != svc.User || final.Memory != svc.MemLimit {
		t.Errorf("RunSpec user/memory must still match emit.Harden: RunSpec.User=%q svc.User=%q RunSpec.Memory=%q svc.MemLimit=%q",
			final.User, svc.User, final.Memory, svc.MemLimit)
	}
}

// TestConverge_RunSpecOmitsUserMemoryNetworkWhenControlsNotRequired extends
// the existing off/absent-controls coverage to the three sandbox-fidelity
// axes: a profile that never declares non-root-user/memory-limit/
// network-mode must produce a RunSpec with User=="" Memory=="" and
// Network=="none" -- never falling back to a hardcoded default the way the
// pre-fix launcher always did. "none" is the correct fail-closed floor for
// Network specifically (see doctrine.Profile.NetworkMode's doc comment): an
// absent control must not widen network exposure.
func TestConverge_RunSpecOmitsUserMemoryNetworkWhenControlsNotRequired(t *testing.T) {
	raw := []byte(`
name: no-sandbox-fidelity-controls
version: v1
controls:
  no-new-privileges:      { status: required, kind: static }
  cap-drop-all-min-add:   { status: required, kind: observation }
  read-only-root-tmpfs:   { status: required, kind: observation }
`)
	p, err := doctrine.Load(raw)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}

	fl := &fakeLauncher{}
	tr := &scriptedTracer{seq: []observe.Result{{Started: true, Healthy: true}}}
	loop := Loop{Launcher: fl, Tracer: tr, Profile: p, Service: "app", MaxIter: 5}
	if _, err := loop.Converge(context.Background(), "img"); err != nil {
		t.Fatalf("should converge: %v", err)
	}
	if len(fl.specs) == 0 {
		t.Fatal("expected at least one launch")
	}
	final := fl.specs[len(fl.specs)-1]

	if final.User != "" {
		t.Errorf("RunSpec.User must be empty when non-root-user is not required, got %q", final.User)
	}
	if final.Memory != "" {
		t.Errorf("RunSpec.Memory must be empty when memory-limit is not required, got %q", final.Memory)
	}
	if final.Network != "none" {
		t.Errorf("RunSpec.Network must fail closed to %q when network-mode is not declared, got %q", "none", final.Network)
	}
}

// TestConverge_NeverLooserThanEmitted_PerAxis is the explicit
// never-looser-than-emitted assertion the sandbox-fidelity review asked
// for: across a small matrix of profiles, on every one of the six
// mirrored axes (ReadOnly, Tmpfs, CapAdd, User, Memory, Network) the
// RunSpec the Launcher receives must be at least as strict as -- for these
// controls, exactly equal to -- what emit.Harden would ship. A launch
// weaker than what ships on any axis is the fail-closed violation this
// suite exists to catch.
func TestConverge_NeverLooserThanEmitted_PerAxis(t *testing.T) {
	profiles := map[string][]byte{
		"reference-default": []byte(`
name: reference-like
version: v1
controls:
  no-new-privileges:      { status: required, kind: static }
  cap-drop-all-min-add:   { status: required, kind: observation }
  read-only-root-tmpfs:   { status: required, kind: observation }
  non-root-user:          { status: required, kind: static }
  memory-limit:           { status: required, kind: static }
  network-mode:           { status: required, kind: static, value: none }
  dedicated-bridge-net:   { status: required, kind: static }
`),
		"serving": []byte(`
name: serving
version: v1
controls:
  no-new-privileges:      { status: required, kind: static }
  cap-drop-all-min-add:   { status: required, kind: observation }
  read-only-root-tmpfs:   { status: required, kind: observation }
  non-root-user:          { status: required, kind: static }
  memory-limit:           { status: required, kind: static }
  network-mode:           { status: required, kind: static, value: serve }
  dedicated-bridge-net:   { status: required, kind: static }
`),
		"minimal": []byte(`
name: minimal
version: v1
controls:
  no-new-privileges: { status: required, kind: static }
`),
	}

	for name, raw := range profiles {
		t.Run(name, func(t *testing.T) {
			p, err := doctrine.Load(raw)
			if err != nil {
				t.Fatalf("load profile: %v", err)
			}
			fl := &fakeLauncher{}
			tr := &scriptedTracer{seq: []observe.Result{
				{Started: true, Healthy: true, Caps: []string{"NET_BIND_SERVICE"}, WritePaths: []string{"/var/run/app"}},
				{Started: true, Healthy: true, Caps: []string{"NET_BIND_SERVICE"}, WritePaths: []string{"/var/run/app"}},
			}}
			loop := Loop{Launcher: fl, Tracer: tr, Profile: p, Service: "app", MaxIter: 5}
			if _, err := loop.Converge(context.Background(), "img"); err != nil {
				t.Fatalf("should converge: %v", err)
			}
			final := fl.specs[len(fl.specs)-1]

			obs := observe.Result{Caps: []string{"NET_BIND_SERVICE"}, WritePaths: []string{"/var/run/app"}}
			c, _, err := emit.Harden("app", "img", p, obs)
			if err != nil {
				t.Fatalf("emit.Harden: %v", err)
			}
			svc := c.Services["app"]

			// ReadOnly: launch must be read-only whenever the compose is.
			if svc.ReadOnly && !final.ReadOnly {
				t.Error("axis ReadOnly: launch is looser than emitted (compose read-only, launch is not)")
			}
			// Tmpfs: every tmpfs path emitted must be present in the launch.
			for _, p := range svc.Tmpfs {
				if !containsStr(final.Tmpfs, p) {
					t.Errorf("axis Tmpfs: emitted tmpfs %q missing from launch %v", p, final.Tmpfs)
				}
			}
			// CapAdd: every capability emitted must be granted in the launch.
			for _, capName := range svc.CapAdd {
				if !containsStr(final.CapAdd, capName) {
					t.Errorf("axis CapAdd: emitted cap_add %q missing from launch %v", capName, final.CapAdd)
				}
			}
			// User: if the compose pins a user, the launch must pin the SAME user
			// (a different or absent user would not verify what ships).
			if svc.User != "" && final.User != svc.User {
				t.Errorf("axis User: emitted user %q, launch user %q -- must match", svc.User, final.User)
			}
			// Memory: the launch's ceiling must be present whenever the compose
			// sets one, and must match exactly (a looser/absent launch ceiling
			// would let a candidate pass that would be memory-capped in prod).
			if svc.MemLimit != "" && final.Memory != svc.MemLimit {
				t.Errorf("axis Memory: emitted mem_limit %q, launch memory %q -- must match", svc.MemLimit, final.Memory)
			}
			// Network: a compose service assigned a network (svc.NetworkMode
			// != "none") must not be sandboxed under full isolation, or a
			// port-serving candidate would be falsely rejected as unreachable.
			emittedIsolated := svc.NetworkMode == "none"
			launchIsolated := final.Network != "serve"
			// The over-reject check only applies once a profile explicitly
			// declares network-mode: a profile that predates this control (no
			// network-mode key at all) keeps the launcher's historical
			// conservative "none" default even if dedicated-bridge-net alone
			// gives it a compose network -- that is extra strictness, not a
			// violation of the never-looser invariant (see doctrine.Profile.
			// NetworkMode's fail-closed-absent doc comment).
			if p.Required("network-mode") && !emittedIsolated && launchIsolated {
				t.Error("axis Network: launch is STRICTER than emitted in a way that over-rejects (compose is reachable, launch is isolated) -- this is the over-reject bug this change fixes")
			}
			if emittedIsolated && !launchIsolated {
				t.Error("axis Network: launch is LOOSER than emitted (compose is isolated, launch is reachable) -- fail-closed violation")
			}
		})
	}
}

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
