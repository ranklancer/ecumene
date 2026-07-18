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

// scriptedTracer returns a preset Result per Trace call (last repeats).
type scriptedTracer struct {
	seq []observe.Result
	i   int
}

func (t *scriptedTracer) Trace(_ context.Context, _ sandbox.Handle) (observe.Result, error) {
	r := t.seq[min(t.i, len(t.seq)-1)]
	t.i++
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
	// iter1: reveals a needed cap (grows). iter2: same set, stable+healthy → converge.
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
	// tracer keeps revealing a new cap every iteration → never stabilises.
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
