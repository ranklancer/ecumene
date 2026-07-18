// Package sandbox is Ecumene's sandbox launcher (the internal design spec component A3): it
// stands up an ephemeral, rootless, network-isolated run environment and treats
// the image as untrusted — no host Docker socket, no persistent host mounts.
//
// The P0 scaffold defines the seam and a fake for tests; wiring a real rootless
// runtime (Podman/Docker, user-namespaced) is a reviewed follow-up (tracer
// substrate D-5 is still pending).
package sandbox

import (
	"context"
	"time"
)

// RunSpec is one ephemeral run request. The image is untrusted; the runtime
// must isolate the network, refuse a host Docker socket, and mount nothing
// persistent from the host.
type RunSpec struct {
	Image      string
	CapAdd     []string      // capabilities granted for this run (tighten narrows)
	ReadOnly   bool          // read-only root filesystem
	Tmpfs      []string      // writable tmpfs paths
	SoakWindow time.Duration // how long to let the container run before judging
}

// Handle refers to a launched sandbox container.
type Handle struct {
	ID string
}

// Launcher stands up and tears down the ephemeral sandbox.
type Launcher interface {
	Launch(ctx context.Context, spec RunSpec) (Handle, error)
	Stop(ctx context.Context, h Handle) error
}
