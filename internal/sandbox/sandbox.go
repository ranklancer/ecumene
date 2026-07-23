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
//
// User, Memory and Network are per-profile axes: the caller (internal/converge)
// derives them from the SAME doctrine.Profile predicates/values that
// internal/emit.Harden uses (doctrine.Profile.Required("non-root-user"),
// Required("memory-limit"), and Profile.NetworkMode()), so the sandbox launch
// verifies exactly what the emitted compose ships on every axis. An empty
// User/Memory means the corresponding control is not required by the active
// profile — buildRunArgs omits the flag rather than substituting a hidden
// default, so the launcher never diverges from what emit.Harden would write
// for the same profile. Network is fail-closed: any value other than "serve"
// (including empty/absent) launches fully isolated (--network none).
type RunSpec struct {
	Image      string
	CapAdd     []string      // capabilities granted for this run (tighten narrows)
	ReadOnly   bool          // read-only root filesystem
	Tmpfs      []string      // writable tmpfs paths
	User       string        // UID:GID to run as; empty omits --user entirely
	Memory     string        // memory ceiling (e.g. "256m"); empty omits --memory entirely
	Network    string        // "serve" joins a reachable network; anything else is isolated (--network none)
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
