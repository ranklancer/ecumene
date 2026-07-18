package converge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ranklancer/ecumene/internal/doctrine"
	"github.com/ranklancer/ecumene/internal/observe"
	"github.com/ranklancer/ecumene/internal/sandbox"
)

type fakeLauncher struct{ launches, stops int }

func (f *fakeLauncher) Launch(_ context.Context, _ sandbox.RunSpec) (sandbox.Handle, error) {
	f.launches++
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
