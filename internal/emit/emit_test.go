package emit

import (
	"strings"
	"testing"

	"github.com/ranklancer/ecumene/internal/doctrine"
	"github.com/ranklancer/ecumene/internal/observe"
)

func TestHarden_StaticControlsAndFailClosedObservation(t *testing.T) {
	c, ev, err := Harden("app", "nginx@sha256:abc", doctrine.Reference(), observe.Result{})
	if err != nil {
		t.Fatal(err)
	}
	svc := c.Services["app"]
	if len(svc.SecurityOpt) == 0 || svc.SecurityOpt[0] != "no-new-privileges:true" {
		t.Errorf("no-new-privileges not applied: %+v", svc.SecurityOpt)
	}
	if len(svc.CapDrop) != 1 || svc.CapDrop[0] != "ALL" {
		t.Errorf("cap_drop ALL not applied: %+v", svc.CapDrop)
	}
	if len(svc.CapAdd) != 0 {
		t.Errorf("with no observation, cap_add must be empty (fail-closed), got %+v", svc.CapAdd)
	}
	if !svc.ReadOnly || len(svc.Tmpfs) != 0 {
		t.Errorf("read_only with no tmpfs expected (fail-closed), got ro=%v tmpfs=%+v", svc.ReadOnly, svc.Tmpfs)
	}
	if svc.Restart != "unless-stopped" || svc.Logging == nil || svc.HealthCheck == nil {
		t.Error("static controls (restart/logging/healthcheck) not applied")
	}
	out, err := Render(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "no-new-privileges:true") {
		t.Errorf("rendered compose missing hardening:\n%s", out)
	}
	if len(ev.Controls) == 0 {
		t.Error("evidence must record applied controls")
	}
}

func TestHarden_ObservationDerivedCapsAndTmpfs(t *testing.T) {
	obs := observe.Result{Caps: []string{"NET_BIND_SERVICE", "NET_BIND_SERVICE"}, WritePaths: []string{"/tmp", "/run"}}
	c, _, err := Harden("web", "img", doctrine.Reference(), obs)
	if err != nil {
		t.Fatal(err)
	}
	svc := c.Services["web"]
	if len(svc.CapAdd) != 1 || svc.CapAdd[0] != "NET_BIND_SERVICE" {
		t.Errorf("cap_add should be the deduped observed set, got %+v", svc.CapAdd)
	}
	if len(svc.Tmpfs) != 2 {
		t.Errorf("tmpfs should be the observed write paths, got %+v", svc.Tmpfs)
	}
}

func TestHarden_RejectsEmptyArgs(t *testing.T) {
	if _, _, err := Harden("", "img", doctrine.Reference(), observe.Result{}); err == nil {
		t.Error("empty service must error")
	}
}

// TestHarden_NonRootUserAndMemoryLimit pins doctrine controls 15/16: when
// required, Harden must write the SAME values internal/sandbox (via
// internal/converge) derives for the launcher (doctrine.NonRootUser /
// doctrine.MemoryLimit) -- this is the single source of truth both sides
// share.
func TestHarden_NonRootUserAndMemoryLimit(t *testing.T) {
	c, ev, err := Harden("app", "img", doctrine.Reference(), observe.Result{})
	if err != nil {
		t.Fatal(err)
	}
	svc := c.Services["app"]
	if svc.User != doctrine.NonRootUser {
		t.Errorf("svc.User = %q, want doctrine.NonRootUser %q", svc.User, doctrine.NonRootUser)
	}
	if svc.MemLimit != doctrine.MemoryLimit {
		t.Errorf("svc.MemLimit = %q, want doctrine.MemoryLimit %q", svc.MemLimit, doctrine.MemoryLimit)
	}
	if !controlApplied(ev, "non-root-user") || !controlApplied(ev, "memory-limit") {
		t.Errorf("evidence must record non-root-user and memory-limit as applied: %+v", ev.Controls)
	}
}

// TestHarden_NonRootUserAndMemoryLimit_OffOmitsFields proves the off/absent
// path never forces a value: emit must match a profile that explicitly
// turns these controls off, or the sandbox and the compose could disagree
// on why a launch is stricter than what ships.
func TestHarden_NonRootUserAndMemoryLimit_OffOmitsFields(t *testing.T) {
	raw := []byte(`
name: no-user-no-mem
version: v1
controls:
  no-new-privileges: { status: required, kind: static }
  non-root-user:     { status: off, kind: static }
  memory-limit:      { status: off, kind: static }
`)
	p, err := doctrine.Load(raw)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	c, _, err := Harden("app", "img", p, observe.Result{})
	if err != nil {
		t.Fatal(err)
	}
	svc := c.Services["app"]
	if svc.User != "" {
		t.Errorf("svc.User must be empty when non-root-user is off, got %q", svc.User)
	}
	if svc.MemLimit != "" {
		t.Errorf("svc.MemLimit must be empty when memory-limit is off, got %q", svc.MemLimit)
	}
}

// TestHarden_NetworkMode_Serve proves a profile that declares this service
// serves gets a reachable network: no network_mode: none override, and
// dedicated-bridge-net still assigns its bridge network.
func TestHarden_NetworkMode_Serve(t *testing.T) {
	raw := []byte(`
name: serves
version: v1
controls:
  dedicated-bridge-net: { status: required, kind: static }
  network-mode:         { status: required, kind: static, value: serve }
`)
	p, err := doctrine.Load(raw)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	c, ev, err := Harden("web", "img", p, observe.Result{})
	if err != nil {
		t.Fatal(err)
	}
	svc := c.Services["web"]
	if svc.NetworkMode != "" {
		t.Errorf("a serving profile must not set network_mode, got %q", svc.NetworkMode)
	}
	if len(svc.Networks) != 1 || svc.Networks[0] != "web_net" {
		t.Errorf("a serving profile must still get its dedicated bridge network, got %+v", svc.Networks)
	}
	if !controlApplied(ev, "network-mode") || !controlApplied(ev, "dedicated-bridge-net") {
		t.Errorf("evidence must record both network-mode and dedicated-bridge-net as applied: %+v", ev.Controls)
	}
}

// TestHarden_NetworkMode_None proves a batch/one-shot profile is fully
// isolated in the emitted compose (network_mode: none), and that the
// mutually-exclusive dedicated-bridge-net control is suppressed -- never
// silently producing an invalid compose file that sets both network_mode
// and networks.
func TestHarden_NetworkMode_None(t *testing.T) {
	raw := []byte(`
name: batch
version: v1
controls:
  dedicated-bridge-net: { status: required, kind: static }
  network-mode:         { status: required, kind: static, value: none }
`)
	p, err := doctrine.Load(raw)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	c, ev, err := Harden("job", "img", p, observe.Result{})
	if err != nil {
		t.Fatal(err)
	}
	svc := c.Services["job"]
	if svc.NetworkMode != "none" {
		t.Errorf("a batch profile must set network_mode: none, got %q", svc.NetworkMode)
	}
	if len(svc.Networks) != 0 {
		t.Errorf("network_mode: none must suppress dedicated-bridge-net's networks assignment, got %+v", svc.Networks)
	}
	if controlApplied(ev, "dedicated-bridge-net") {
		t.Errorf("dedicated-bridge-net must be recorded as NOT applied (suppressed) when network-mode is none: %+v", ev.Controls)
	}
	out, err := Render(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "networks:") {
		t.Errorf("rendered compose must not carry a networks: key alongside network_mode: none:\n%s", out)
	}
}

// TestHarden_NetworkMode_AbsentPreservesLegacyBehaviour proves that when a
// profile doesn't declare network-mode at all (e.g. a profile authored
// before this control existed), dedicated-bridge-net alone continues to
// govern networking exactly as before -- no silent behaviour change for
// profiles that predate the sandbox-fidelity controls.
func TestHarden_NetworkMode_AbsentPreservesLegacyBehaviour(t *testing.T) {
	raw := []byte(`
name: legacy
version: v1
controls:
  dedicated-bridge-net: { status: required, kind: static }
`)
	p, err := doctrine.Load(raw)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	c, _, err := Harden("legacy-svc", "img", p, observe.Result{})
	if err != nil {
		t.Fatal(err)
	}
	svc := c.Services["legacy-svc"]
	if svc.NetworkMode != "" {
		t.Errorf("network_mode must stay unset when network-mode isn't declared, got %q", svc.NetworkMode)
	}
	if len(svc.Networks) != 1 || svc.Networks[0] != "legacy-svc_net" {
		t.Errorf("dedicated-bridge-net must still apply unconditionally when network-mode is absent, got %+v", svc.Networks)
	}
}

func controlApplied(ev Evidence, name string) bool {
	for _, cs := range ev.Controls {
		if cs.Control == name {
			return cs.Applied
		}
	}
	return false
}
