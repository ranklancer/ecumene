// Package converge is Ecumene's convergence tightener (the internal design spec component A5): the
// does-it-start loop (the internal design spec). It authors a candidate, launches it in the
// sandbox, observes it, tightens to the minimal observed working set, and
// re-verifies -- under a bounded iteration budget. It NEVER loosens a control to
// force a pass: if it cannot reach a healthy, doctrine-passing state within the
// budget it fails CLOSED, emitting the last-good candidate plus the evidence of
// what blocked convergence.
package converge

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/ranklancer/ecumene/internal/doctrine"
	"github.com/ranklancer/ecumene/internal/emit"
	"github.com/ranklancer/ecumene/internal/observe"
	"github.com/ranklancer/ecumene/internal/sandbox"
)

// Defaults for the P0 iteration budget (policy-overridable; O-G3-2 open).
const (
	DefaultMaxIter = 5
	DefaultSoak    = 10 * time.Second
)

// ErrDidNotStart means the image never launched, even after incorporating every
// observed need -- a fail-closed outcome, not a loosened pass.
var ErrDidNotStart = errors.New("converge: image did not start within the budget (fail-closed)")

// ErrDidNotConverge means the loop never reached a stable, healthy state within
// the iteration budget -- fail-closed.
var ErrDidNotConverge = errors.New("converge: did not reach a stable healthy state within the iteration budget (fail-closed)")

// Loop is the configured convergence engine.
type Loop struct {
	Launcher sandbox.Launcher
	Tracer   observe.Tracer
	Profile  doctrine.Profile
	Service  string
	MaxIter  int
	Soak     time.Duration
}

// Result is a convergence outcome. Compose/Evidence are always the last-good
// candidate -- populated even on a fail-closed error so the operator sees what
// blocked convergence.
type Result struct {
	Compose    []byte
	Evidence   emit.Evidence
	Converged  bool
	Iterations int
}

// Converge runs the tighten<->re-verify loop for a single stateless image.
func (l Loop) Converge(ctx context.Context, image string) (Result, error) {
	maxIter := l.MaxIter
	if maxIter <= 0 {
		maxIter = DefaultMaxIter
	}
	soak := l.Soak
	if soak <= 0 {
		soak = DefaultSoak
	}
	svc := l.Service
	if svc == "" {
		svc = "app"
	}

	var caps, writes []string // the granted working set, grown from observation
	var last Result
	// lastVerified is the most recent candidate that completed a full
	// start+observe cycle (Launcher.Launch and Tracer.Trace both succeeded)
	// without an infrastructure error -- regardless of whether it was healthy
	// or still needed further tightening. On a mid-loop infra error we must
	// emit lastVerified, never `last`: `last`'s caps/tmpfs may have just been
	// grown from the prior iteration's observation but never re-verified in a
	// completed start+healthy cycle (PR#1 Opus review, fail-closed hardening).
	var lastVerified Result

	for i := 1; i <= maxIter; i++ {
		obs := observe.Result{Caps: caps, WritePaths: writes}
		c, ev, err := emit.Harden(svc, image, l.Profile, obs)
		if err != nil {
			return last, err
		}
		rendered, err := emit.Render(c)
		if err != nil {
			return last, err
		}
		last = Result{Compose: rendered, Evidence: ev, Iterations: i}

		h, err := l.Launcher.Launch(ctx, sandbox.RunSpec{
			Image: image, CapAdd: caps, ReadOnly: true, Tmpfs: writes, SoakWindow: soak,
		})
		if err != nil {
			// Infra error before this candidate ever launched: emit the last
			// candidate that actually completed a verify cycle, not the
			// un-re-verified `last`.
			return lastVerified, err
		}
		res, terr := l.Tracer.Trace(ctx, h)
		_ = l.Launcher.Stop(ctx, h)
		if terr != nil {
			// Infra error mid-trace: same rationale -- this candidate never
			// completed a full start+observe cycle.
			return lastVerified, terr
		}

		// This candidate completed a full start+observe cycle without an
		// infra error: it becomes the last-verified candidate, whether or
		// not it turned out healthy or still needs further tightening.
		lastVerified = last

		newCaps := union(caps, res.Caps)
		newWrites := union(writes, res.WritePaths)
		grew := !equal(newCaps, caps) || !equal(newWrites, writes)

		switch {
		case res.Started && res.Healthy && !grew:
			// stable: healthy with no new observed needs -> converged.
			last.Converged = true
			return last, nil
		case !grew:
			// no progress possible: either never started, or unhealthy with
			// nothing more to grant. Fail closed.
			if !res.Started {
				return last, ErrDidNotStart
			}
			return last, ErrDidNotConverge
		default:
			// incorporate the newly observed needs and re-verify (tighten loop).
			caps, writes = newCaps, newWrites
		}
	}
	return last, ErrDidNotConverge
}

func union(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string{}, a...), b...) {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
