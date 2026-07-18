//go:build integration_runtime

// This file exercises execLauncher against a REAL container runtime. It is
// gated behind the integration_runtime build tag specifically so it is
// excluded from `make gate-full` (and any default `go test ./...`), which
// must stay green in environments — like this sandbox — that have no
// podman/docker available. Run it explicitly with:
//
// go test -tags integration_runtime ./internal/sandbox/...
//
// It skips cleanly (not fails) when no runtime is found on PATH.
package sandbox

import (
	"context"
	"testing"
	"time"
)

func TestExecLauncher_RealRuntime_LaunchAndStop(t *testing.T) {
	runtime := DefaultRuntime()
	if runtime == "" {
		t.Skip("no container runtime (podman or docker) on PATH; skipping integration test")
	}

	l := NewExecLauncher(nil, runtime)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	spec := RunSpec{
		Image:    "docker.io/library/busybox:1.36",
		ReadOnly: true,
		Tmpfs:    []string{"/tmp"},
	}

	h, err := l.Launch(ctx, spec)
	if err != nil {
		t.Fatalf("Launch against real runtime %q: %v", runtime, err)
	}
	if h.ID == "" {
		t.Fatal("Launch: expected a non-empty container ID from a real runtime")
	}

	if err := l.Stop(context.Background(), h); err != nil {
		t.Fatalf("Stop against real runtime %q: %v", runtime, err)
	}
}
