// Package observe is Ecumene's tracer/observer (the internal design spec component A4). It watches
// a launched sandbox container and reports what it actually did. P0 (Tier 1) is
// "does it start": the process launches and does not crash-loop. The eBPF-backed
// capability / filesystem-write / syscall tracer is deferred until the tracer
// substrate (D-5) is chosen; those fields stay empty until then.
package observe

import (
	"context"

	"github.com/ranklancer/ecumene/internal/sandbox"
)

// Result is what the tracer observed about a container's startup.
type Result struct {
	Started    bool     // Tier 1: process launched and did not crash-loop
	Healthy    bool     // liveness/healthcheck held through the soak window
	Caps       []string // capabilities actually used (empty until the tracer lands)
	WritePaths []string // filesystem paths written (empty until the tracer lands)
	Syscalls   []string // syscalls observed (empty until the tracer lands)
}

// Tracer observes a launched sandbox container.
type Tracer interface {
	Trace(ctx context.Context, h sandbox.Handle) (Result, error)
}
