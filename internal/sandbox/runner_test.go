package sandbox

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}

// fakeRunner is the injected CmdRunner used to unit-test execLauncher
// without any real container runtime.
type fakeRunner struct {
	calls  []call
	stdout []byte
	stderr []byte
	err    error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, call{name: name, args: append([]string(nil), args...)})
	return f.stdout, f.stderr, f.err
}

func containsSeq(args []string, seq ...string) bool {
	if len(seq) == 0 || len(args) < len(seq) {
		return false
	}
	for i := 0; i+len(seq) <= len(args); i++ {
		match := true
		for j, s := range seq {
			if args[i+j] != s {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func containsFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// TestBuildRunArgs_HardenedDefaults asserts the EXACT argv built for a
// representative RunSpec whose User/Memory/Network are set (as
// internal/converge would set them for a profile requiring non-root-user,
// memory-limit and network-mode: serve). This test fails if any hardening
// flag is dropped or reordered, or if an undeclared capability sneaks in.
func TestBuildRunArgs_HardenedDefaults(t *testing.T) {
	spec := RunSpec{
		Image:    "example.com/app:1.2.3",
		CapAdd:   []string{"NET_BIND_SERVICE", "CHOWN"},
		ReadOnly: true,
		Tmpfs:    []string{"/tmp", "/run"},
		User:     "65534:65534",
		Memory:   "256m",
		Network:  "serve",
	}
	got, err := buildRunArgs(spec)
	if err != nil {
		t.Fatalf("buildRunArgs: unexpected error: %v", err)
	}
	want := []string{
		"run", "--rm", "-d",
		"--security-opt", "no-new-privileges:true",
		"--cap-drop", "ALL",
		"--cap-add", "NET_BIND_SERVICE",
		"--cap-add", "CHOWN",
		"--read-only",
		"--tmpfs", "/tmp",
		"--tmpfs", "/run",
		"--user", "65534:65534",
		"--memory", "256m",
		"--pids-limit", defaultPids,
		"--network", "bridge",
		"example.com/app:1.2.3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv mismatch:\n got:  %#v\nwant: %#v", got, want)
	}
}

// TestBuildRunArgs_EmptyCapAdd_NoAddFlags proves empty CapAdd fails closed:
// no --cap-add at all, while cap-drop ALL and no-new-privileges remain.
func TestBuildRunArgs_EmptyCapAdd_NoAddFlags(t *testing.T) {
	spec := RunSpec{Image: "example.com/app:1.0"}
	got, err := buildRunArgs(spec)
	if err != nil {
		t.Fatalf("buildRunArgs: unexpected error: %v", err)
	}
	if containsFlag(got, "--cap-add") {
		t.Fatalf("expected no --cap-add for empty CapAdd, got argv: %#v", got)
	}
	if !containsSeq(got, "--cap-drop", "ALL") {
		t.Fatalf("expected --cap-drop ALL always present, got argv: %#v", got)
	}
	if !containsSeq(got, "--security-opt", "no-new-privileges:true") {
		t.Fatalf("expected no-new-privileges always present, got argv: %#v", got)
	}
}

// TestBuildRunArgs_UndeclaredCap_NeverAdded proves only declared caps are
// added — no other capability appears anywhere in the argv.
func TestBuildRunArgs_UndeclaredCap_NeverAdded(t *testing.T) {
	spec := RunSpec{Image: "example.com/app:1.0", CapAdd: []string{"CHOWN"}}
	got, err := buildRunArgs(spec)
	if err != nil {
		t.Fatalf("buildRunArgs: unexpected error: %v", err)
	}
	if !containsSeq(got, "--cap-add", "CHOWN") {
		t.Fatalf("expected --cap-add CHOWN, got argv: %#v", got)
	}
	for _, undeclared := range []string{"SYS_ADMIN", "NET_ADMIN", "SYS_PTRACE"} {
		if containsFlag(got, undeclared) {
			t.Fatalf("undeclared capability %q leaked into argv: %#v", undeclared, got)
		}
	}
	// exactly one --cap-add flag/value pair
	n := 0
	for _, a := range got {
		if a == "--cap-add" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 --cap-add flag, got %d in argv: %#v", n, got)
	}
}

// TestBuildRunArgs_ReadOnlyFalse_StillHardened proves the ReadOnly=false
// path never produces a fully-unhardened launch: cap-drop ALL and
// no-new-privileges remain even without --read-only, and an empty
// RunSpec (no profile axes set) still fails closed to --network none.
func TestBuildRunArgs_ReadOnlyFalse_StillHardened(t *testing.T) {
	spec := RunSpec{Image: "example.com/app:1.0", ReadOnly: false}
	got, err := buildRunArgs(spec)
	if err != nil {
		t.Fatalf("buildRunArgs: unexpected error: %v", err)
	}
	if containsFlag(got, "--read-only") {
		t.Fatalf("did not expect --read-only when ReadOnly=false, got argv: %#v", got)
	}
	if !containsSeq(got, "--cap-drop", "ALL") {
		t.Fatalf("expected --cap-drop ALL even when ReadOnly=false, got argv: %#v", got)
	}
	if !containsSeq(got, "--security-opt", "no-new-privileges:true") {
		t.Fatalf("expected no-new-privileges even when ReadOnly=false, got argv: %#v", got)
	}
	if !containsSeq(got, "--network", "none") {
		t.Fatalf("expected --network none even when ReadOnly=false, got argv: %#v", got)
	}
}

// TestBuildRunArgs_EmptyUserAndMemory_OmitsFlags proves the empty-string
// fail-closed default for User/Memory: buildRunArgs performs no policy
// substitution of its own — an empty RunSpec.User/Memory (meaning the
// active profile did not require non-root-user/memory-limit) must produce
// argv with NO --user/--memory flag at all, exactly matching what
// emit.Harden would have left unset in the compose for the same profile.
func TestBuildRunArgs_EmptyUserAndMemory_OmitsFlags(t *testing.T) {
	spec := RunSpec{Image: "example.com/app:1.0"}
	got, err := buildRunArgs(spec)
	if err != nil {
		t.Fatalf("buildRunArgs: unexpected error: %v", err)
	}
	if containsFlag(got, "--user") {
		t.Fatalf("expected no --user when RunSpec.User is empty, got argv: %#v", got)
	}
	if containsFlag(got, "--memory") {
		t.Fatalf("expected no --memory when RunSpec.Memory is empty, got argv: %#v", got)
	}
	if !containsSeq(got, "--pids-limit", defaultPids) {
		t.Fatalf("expected --pids-limit always present (unconditional, defence in depth), got argv: %#v", got)
	}
}

// TestBuildRunArgs_NetworkServe_JoinsBridge_NotNone proves the core fix
// this change makes: a profile that declares this service serves must NOT
// get --network none (which would make a port-serving candidate
// unreachable and cause a false FAIL). It must join a reachable network.
func TestBuildRunArgs_NetworkServe_JoinsBridge_NotNone(t *testing.T) {
	spec := RunSpec{Image: "example.com/app:1.0", Network: "serve"}
	got, err := buildRunArgs(spec)
	if err != nil {
		t.Fatalf("buildRunArgs: unexpected error: %v", err)
	}
	if containsSeq(got, "--network", "none") {
		t.Fatalf("a serving profile must NOT get --network none, got argv: %#v", got)
	}
	if !containsSeq(got, "--network", "bridge") {
		t.Fatalf("a serving profile must get --network bridge, got argv: %#v", got)
	}
}

// TestBuildRunArgs_NetworkNotServe_IsolatesToNone proves every non-"serve"
// value (including empty/absent, and an explicit "none") fails closed to
// full isolation -- the batch/one-shot default.
func TestBuildRunArgs_NetworkNotServe_IsolatesToNone(t *testing.T) {
	for _, network := range []string{"", "none", "bogus"} {
		spec := RunSpec{Image: "example.com/app:1.0", Network: network}
		got, err := buildRunArgs(spec)
		if err != nil {
			t.Fatalf("buildRunArgs: unexpected error: %v", err)
		}
		if !containsSeq(got, "--network", "none") {
			t.Fatalf("Network=%q must isolate to --network none, got argv: %#v", network, got)
		}
	}
}

// TestBuildRunArgs_HostileImage_NoShellInterpolation proves argv-injection
// safety: a hostile image string is a single, unmodified, discrete argv
// element — never split into multiple arguments.
func TestBuildRunArgs_HostileImage_NoShellInterpolation(t *testing.T) {
	hostile := "example.com/x;rm -rf"
	spec := RunSpec{Image: hostile}
	got, err := buildRunArgs(spec)
	if err != nil {
		t.Fatalf("buildRunArgs: unexpected error: %v", err)
	}
	last := got[len(got)-1]
	if last != hostile {
		t.Fatalf("expected hostile image as the final, unmodified argv element; got %q (full argv: %#v)", last, got)
	}
	for _, a := range got[:len(got)-1] {
		if strings.Contains(a, "rm -rf") {
			t.Fatalf("hostile image content leaked into another argv element: %q", a)
		}
	}
}

// TestBuildRunArgs_ImageAlwaysLast proves the image is the final argv
// element regardless of spec contents.
func TestBuildRunArgs_ImageAlwaysLast(t *testing.T) {
	spec := RunSpec{
		Image:    "example.com/app:2.0",
		CapAdd:   []string{"CHOWN"},
		ReadOnly: true,
		Tmpfs:    []string{"/tmp"},
		User:     "65534:65534",
		Memory:   "256m",
		Network:  "serve",
	}
	got, err := buildRunArgs(spec)
	if err != nil {
		t.Fatalf("buildRunArgs: unexpected error: %v", err)
	}
	if got[len(got)-1] != spec.Image {
		t.Fatalf("expected image last, got argv: %#v", got)
	}
}

// TestBuildRunArgs_MissingImage_Errors proves a missing image fails closed
// at argv-build time rather than producing a malformed command.
func TestBuildRunArgs_MissingImage_Errors(t *testing.T) {
	if _, err := buildRunArgs(RunSpec{}); err == nil {
		t.Fatal("expected error for empty RunSpec.Image")
	}
}

func TestLaunch_ParsesContainerID(t *testing.T) {
	fr := &fakeRunner{stdout: []byte("abcdef0123456789\n")}
	l := NewExecLauncher(fr, "podman")

	h, err := l.Launch(context.Background(), RunSpec{Image: "example.com/app:1.0"})
	if err != nil {
		t.Fatalf("Launch: unexpected error: %v", err)
	}
	if h.ID != "abcdef0123456789" {
		t.Fatalf("Handle.ID = %q, want %q", h.ID, "abcdef0123456789")
	}
	if len(fr.calls) != 1 || fr.calls[0].name != "podman" {
		t.Fatalf("expected exactly one call to podman, got %#v", fr.calls)
	}
}

// TestLaunch_HostileImage_PassedAsSingleArgvElement proves the same
// argv-injection safety property end-to-end through Launch, against the
// exact call recorded by the fake CmdRunner.
func TestLaunch_HostileImage_PassedAsSingleArgvElement(t *testing.T) {
	hostile := "example.com/x;rm -rf"
	fr := &fakeRunner{stdout: []byte("abcdef012345\n")}
	l := NewExecLauncher(fr, "podman")

	if _, err := l.Launch(context.Background(), RunSpec{Image: hostile}); err != nil {
		t.Fatalf("Launch: unexpected error: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("expected exactly one call, got %d", len(fr.calls))
	}
	args := fr.calls[0].args
	if args[len(args)-1] != hostile {
		t.Fatalf("expected hostile image as final discrete argv element; got %#v", args)
	}
}

func TestLaunch_NonZeroExit_ReturnsErrorNoHandle(t *testing.T) {
	fr := &fakeRunner{err: errors.New("exit status 1"), stderr: []byte("boom")}
	l := NewExecLauncher(fr, "podman")

	h, err := l.Launch(context.Background(), RunSpec{Image: "example.com/app:1.0"})
	if err == nil {
		t.Fatal("expected error on non-zero exit")
	}
	if h.ID != "" {
		t.Fatalf("expected empty Handle on error, got %+v", h)
	}
}

func TestLaunch_EmptyStdout_Errors(t *testing.T) {
	fr := &fakeRunner{stdout: []byte("")}
	l := NewExecLauncher(fr, "podman")

	h, err := l.Launch(context.Background(), RunSpec{Image: "example.com/app:1.0"})
	if err == nil {
		t.Fatal("expected error for empty stdout")
	}
	if h.ID != "" {
		t.Fatalf("expected empty Handle on error, got %+v", h)
	}
}

func TestLaunch_GarbageStdout_Errors(t *testing.T) {
	fr := &fakeRunner{stdout: []byte("not-a-container-id!!\n")}
	l := NewExecLauncher(fr, "podman")

	h, err := l.Launch(context.Background(), RunSpec{Image: "example.com/app:1.0"})
	if err == nil {
		t.Fatal("expected error for garbage stdout")
	}
	if h.ID != "" {
		t.Fatalf("expected empty Handle on error, got %+v", h)
	}
}

func TestLaunch_NoRuntimeAvailable_Errors(t *testing.T) {
	fr := &fakeRunner{stdout: []byte("abcdef012345\n")}
	l := NewExecLauncher(fr, "")
	l.runtime = "" // force "no runtime found"

	if _, err := l.Launch(context.Background(), RunSpec{Image: "example.com/app:1.0"}); err == nil {
		t.Fatal("expected error when no container runtime is available")
	}
	if len(fr.calls) != 0 {
		t.Fatalf("expected no runner calls when runtime is unavailable, got %#v", fr.calls)
	}
}

func TestStop_IssuesRmArgv(t *testing.T) {
	fr := &fakeRunner{}
	l := NewExecLauncher(fr, "podman")

	if err := l.Stop(context.Background(), Handle{ID: "abc123"}); err != nil {
		t.Fatalf("Stop: unexpected error: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("expected exactly one call, got %d", len(fr.calls))
	}
	c := fr.calls[0]
	if c.name != "podman" {
		t.Fatalf("expected runtime %q, got %q", "podman", c.name)
	}
	want := []string{"rm", "-f", "abc123"}
	if !reflect.DeepEqual(c.args, want) {
		t.Fatalf("Stop argv = %#v, want %#v", c.args, want)
	}
}

func TestStop_RunnerError_Surfaces(t *testing.T) {
	fr := &fakeRunner{err: errors.New("boom"), stderr: []byte("some real runtime error")}
	l := NewExecLauncher(fr, "podman")

	if err := l.Stop(context.Background(), Handle{ID: "abc123"}); err == nil {
		t.Fatal("expected Stop to surface a real runner error")
	}
}

func TestStop_AlreadyGone_Tolerated(t *testing.T) {
	fr := &fakeRunner{err: errors.New("exit status 1"), stderr: []byte("Error: no such container abc123")}
	l := NewExecLauncher(fr, "podman")

	if err := l.Stop(context.Background(), Handle{ID: "abc123"}); err != nil {
		t.Fatalf("expected already-gone container to be tolerated, got error: %v", err)
	}
}

func TestStop_EmptyHandleID_Errors(t *testing.T) {
	fr := &fakeRunner{}
	l := NewExecLauncher(fr, "podman")

	if err := l.Stop(context.Background(), Handle{}); err == nil {
		t.Fatal("expected error for empty Handle.ID")
	}
	if len(fr.calls) != 0 {
		t.Fatalf("expected no runner calls for empty Handle.ID, got %#v", fr.calls)
	}
}
