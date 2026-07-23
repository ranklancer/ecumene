// Package sandbox: this file is the concrete rootless-container Launcher
// (the internal design spec component A3). It shells out to a container runtime (podman
// preferred for rootless operation, docker as a fallback) via an injected
// CmdRunner so the launcher is unit-testable without a real runtime on
// PATH. The `run` argv it builds mirrors the SAME hardening doctrine
// (internal/doctrine/reference-v1.yaml) that internal/emit applies to
// generated compose files, so "does the candidate start" genuinely
// verifies the hardened configuration and not some looser stand-in.
//
// User, memory and network used to be hardcoded here regardless of
// doctrine — stricter than (and sometimes disagreeing with) what the
// emitter actually shipped, which could over-reject a candidate the
// emitted compose would have run fine (e.g. a port-serving service can't
// be reached under a hardcoded --network none; a heavier image OOMs at a
// hardcoded 256m). Those axes are now carried on RunSpec, derived
// per-profile by internal/converge from the SAME doctrine.Profile
// predicates emit.Harden gates on — see sandbox.go's RunSpec doc comment.
// buildRunArgs itself does no policy derivation: it only translates
// whatever RunSpec it is handed into argv.
//
// eBPF / the Tracer (A4 / D-5) are explicitly out of scope here; they
// remain a seam for a follow-up change.
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// defaultPids is a fail-closed process-count ceiling applied to every
// launch, independent of RunSpec or doctrine. Unlike user/memory/network,
// it has no corresponding doctrine control or emitted compose field: it is
// deliberately extra-only sandbox strictness (defence in depth for an
// untrusted candidate image) that does not over-reject typical
// single-process workloads, so it stays unconditional like cap-drop ALL
// and no-new-privileges below.
const defaultPids = "256"

// CmdRunner runs an external command and captures its output. It is the
// injection seam that lets execLauncher be unit-tested without a real
// container runtime on PATH.
type CmdRunner interface {
	Run(ctx context.Context, name string, args ...string) (stdout []byte, stderr []byte, err error)
}

// execRunner is the real CmdRunner, backed by os/exec. Arguments are always
// passed as discrete argv elements (exec.CommandContext), never joined into
// a shell string, so a hostile RunSpec.Image or capability name cannot
// break out of its own argument.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// execLauncher is a concrete rootless-container Launcher. It builds a
// hardened `run` argv from a RunSpec plus the doctrine defaults above and
// executes it through an injected CmdRunner.
type execLauncher struct {
	runner  CmdRunner
	runtime string // e.g. "podman" or "docker"; "" means no runtime found
}

var _ Launcher = (*execLauncher)(nil)

// DefaultRuntime resolves the container runtime binary to use, preferring
// podman (rootless-friendly) and falling back to docker. It returns "" if
// neither is found on PATH — callers must treat that as fail-closed (no
// launch is attempted).
func DefaultRuntime() string {
	if _, err := exec.LookPath("podman"); err == nil {
		return "podman"
	}
	if _, err := exec.LookPath("docker"); err == nil {
		return "docker"
	}
	return ""
}

// NewExecLauncher builds a Launcher backed by runner, invoking the given
// container runtime binary. Pass runtime == "" to resolve it via
// DefaultRuntime at construction time. Pass runner == nil to use the real
// os/exec-backed CmdRunner.
func NewExecLauncher(runner CmdRunner, runtime string) *execLauncher {
	if runner == nil {
		runner = execRunner{}
	}
	if runtime == "" {
		runtime = DefaultRuntime()
	}
	return &execLauncher{runner: runner, runtime: runtime}
}

// buildRunArgs constructs the hardened `run` argv for spec. It mirrors the
// doctrine internal/emit applies to compose:
//
//   - --security-opt no-new-privileges:true   (doctrine #1, always —
//     unconditional regardless of profile, defence in depth)
//   - --cap-drop ALL                          (doctrine #2 baseline,
//     always — unconditional regardless of profile, defence in depth)
//   - --cap-add <c> for each spec.CapAdd       (doctrine #2, minimal add;
//     empty CapAdd emits no --cap-add at all — fail-closed tightest)
//   - --read-only                             (doctrine #3, when spec.ReadOnly)
//   - --tmpfs <p> for each spec.Tmpfs entry    (doctrine #3)
//   - --user <spec.User>                       (doctrine #15, only when
//     spec.User is set — mirrors emit's non-root-user gate)
//   - --memory <spec.Memory>                    (doctrine #16, only when
//     spec.Memory is set — mirrors emit's memory-limit gate)
//   - --pids-limit                              (fail-closed process
//     ceiling, always — see defaultPids doc comment)
//   - --network bridge | none                  (doctrine #17: "serve"
//     joins a reachable network; anything else is fully isolated — mirrors
//     emit's network-mode gate)
//   - --rm, -d                                  (so Stop can tear down by ID)
//
// The image is always the last argv element, and every value — including
// the image — is passed as its own discrete argument, never interpolated
// into a shell string.
func buildRunArgs(spec RunSpec) ([]string, error) {
	if spec.Image == "" {
		return nil, fmt.Errorf("sandbox: RunSpec.Image is required")
	}

	args := []string{
		"run",
		"--rm",
		"-d",
		"--security-opt", "no-new-privileges:true",
		"--cap-drop", "ALL",
	}
	for _, c := range spec.CapAdd {
		if c == "" {
			continue
		}
		args = append(args, "--cap-add", c)
	}

	if spec.ReadOnly {
		args = append(args, "--read-only")
	}
	for _, p := range spec.Tmpfs {
		if p == "" {
			continue
		}
		args = append(args, "--tmpfs", p)
	}

	if spec.User != "" {
		args = append(args, "--user", spec.User)
	}
	if spec.Memory != "" {
		args = append(args, "--memory", spec.Memory)
	}
	args = append(args, "--pids-limit", defaultPids)

	if spec.Network == "serve" {
		args = append(args, "--network", "bridge")
	} else {
		args = append(args, "--network", "none")
	}

	args = append(args, spec.Image)
	return args, nil
}

// isPlausibleContainerID is a defensive sanity check on runtime stdout: a
// real container ID is a short hex string. Anything else (empty output, a
// stray warning line, garbage) is treated as a fail-closed error rather
// than handed back as a Handle.
func isPlausibleContainerID(s string) bool {
	if len(s) < 12 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !isHex {
			return false
		}
	}
	return true
}

// Launch runs the hardened argv for spec. On any failure — non-zero exit,
// empty stdout, or stdout that doesn't look like a container ID — it
// returns an error and a zero Handle; it never hands back a half-open
// handle.
func (l *execLauncher) Launch(ctx context.Context, spec RunSpec) (Handle, error) {
	if l.runtime == "" {
		return Handle{}, fmt.Errorf("sandbox: no container runtime available (checked podman, docker)")
	}
	args, err := buildRunArgs(spec)
	if err != nil {
		return Handle{}, err
	}

	stdout, stderr, err := l.runner.Run(ctx, l.runtime, args...)
	if err != nil {
		return Handle{}, fmt.Errorf("sandbox: launch failed: %w (stderr: %s)", err, strings.TrimSpace(string(stderr)))
	}

	id := strings.TrimSpace(string(stdout))
	if idx := strings.LastIndexByte(id, '\n'); idx >= 0 {
		id = strings.TrimSpace(id[idx+1:])
	}
	if id == "" {
		return Handle{}, fmt.Errorf("sandbox: launch produced no container ID (stderr: %s)", strings.TrimSpace(string(stderr)))
	}
	if !isPlausibleContainerID(id) {
		return Handle{}, fmt.Errorf("sandbox: launch produced an unexpected container ID output: %q", id)
	}

	return Handle{ID: id}, nil
}

// Stop tears the sandbox down by ID. It tolerates "already gone" (the
// runtime reports no such container, e.g. because --rm already reaped a
// short-lived process) but surfaces any other runtime error.
func (l *execLauncher) Stop(ctx context.Context, h Handle) error {
	if h.ID == "" {
		return fmt.Errorf("sandbox: Stop requires a non-empty Handle.ID")
	}
	if l.runtime == "" {
		return fmt.Errorf("sandbox: no container runtime available (checked podman, docker)")
	}

	_, stderr, err := l.runner.Run(ctx, l.runtime, "rm", "-f", h.ID)
	if err != nil {
		msg := strings.ToLower(strings.TrimSpace(string(stderr)))
		if strings.Contains(msg, "no such container") || strings.Contains(msg, "no such object") {
			return nil
		}
		return fmt.Errorf("sandbox: stop failed for %s: %w (stderr: %s)", h.ID, err, strings.TrimSpace(string(stderr)))
	}
	return nil
}
