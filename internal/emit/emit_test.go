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
