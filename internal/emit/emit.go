// Package emit is Ecumene's generator/emitter (the internal design spec component A2): it authors
// a hardened Docker Compose fragment from an image reference, the doctrine
// profile (static controls), and the convergence observation (observation-derived
// controls), and packages the evidence bundle. P0 emits Compose only (D-3).
package emit

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/ranklancer/ecumene/internal/doctrine"
	"github.com/ranklancer/ecumene/internal/observe"
)

// Compose is the minimal hardened compose model Ecumene emits.
type Compose struct {
	Services map[string]Service  `yaml:"services"`
	Networks map[string]struct{} `yaml:"networks,omitempty"`
}

// Service is one hardened service.
type Service struct {
	Image       string       `yaml:"image"`
	Restart     string       `yaml:"restart,omitempty"`
	ReadOnly    bool         `yaml:"read_only,omitempty"`
	SecurityOpt []string     `yaml:"security_opt,omitempty"`
	CapDrop     []string     `yaml:"cap_drop,omitempty"`
	CapAdd      []string     `yaml:"cap_add,omitempty"`
	Tmpfs       []string     `yaml:"tmpfs,omitempty"`
	Networks    []string     `yaml:"networks,omitempty"`
	Logging     *Logging     `yaml:"logging,omitempty"`
	HealthCheck *HealthCheck `yaml:"healthcheck,omitempty"`
}

// Logging caps log growth (control 11).
type Logging struct {
	Driver  string            `yaml:"driver"`
	Options map[string]string `yaml:"options"`
}

// HealthCheck is the liveness contract (control 12); shape only in P0.
type HealthCheck struct {
	Test     []string `yaml:"test"`
	Interval string   `yaml:"interval"`
	Timeout  string   `yaml:"timeout"`
	Retries  int      `yaml:"retries"`
}

// ControlStatus is a per-control outcome in the evidence bundle.
type ControlStatus struct {
	Control       string `yaml:"control"`
	Applied       bool   `yaml:"applied"`
	Justification string `yaml:"justification,omitempty"`
}

// Evidence records what was applied and why (the ✓/✗/⚠ doctrine checklist).
type Evidence struct {
	Image          string          `yaml:"image"`
	ProfileName    string          `yaml:"profile_name"`
	ProfileVersion string          `yaml:"profile_version"`
	Tier           int             `yaml:"tier"`
	Controls       []ControlStatus `yaml:"controls"`
}

// Harden authors the hardened compose for a single service. Static controls come
// from the profile; observation-derived controls (caps, tmpfs) come from obs.
// A required observation-derived control with no observation yet keeps the
// safest value (cap_drop ALL with no add; read-only with no tmpfs) — fail-closed,
// never loosened.
func Harden(serviceName, image string, p doctrine.Profile, obs observe.Result) (Compose, Evidence, error) {
	if serviceName == "" || image == "" {
		return Compose{}, Evidence{}, fmt.Errorf("emit: service name and image are required")
	}
	svc := Service{Image: image}
	ev := Evidence{Image: image, ProfileName: p.Name, ProfileVersion: p.Version, Tier: 1}
	netName := serviceName + "_net"

	mark := func(control string, applied bool, why string) {
		ev.Controls = append(ev.Controls, ControlStatus{Control: control, Applied: applied, Justification: why})
	}

	if p.Required("no-new-privileges") {
		svc.SecurityOpt = append(svc.SecurityOpt, "no-new-privileges:true")
		mark("no-new-privileges", true, "static")
	}
	if p.Required("cap-drop-all-min-add") {
		svc.CapDrop = []string{"ALL"}
		svc.CapAdd = dedupeSorted(obs.Caps) // only observed caps; empty if none seen
		mark("cap-drop-all-min-add", true, capJustification(svc.CapAdd))
	}
	if p.Required("read-only-root-tmpfs") {
		svc.ReadOnly = true
		svc.Tmpfs = dedupeSorted(obs.WritePaths) // tmpfs only where writes observed
		mark("read-only-root-tmpfs", true, tmpfsJustification(svc.Tmpfs))
	}
	if p.Required("restart-unless-stopped") {
		svc.Restart = "unless-stopped"
		mark("restart-unless-stopped", true, "static")
	}
	if p.Required("log-driver-limits") {
		svc.Logging = &Logging{Driver: "json-file", Options: map[string]string{"max-size": "10m", "max-file": "3"}}
		mark("log-driver-limits", true, "static")
	}
	if p.Required("healthchecks") {
		svc.HealthCheck = &HealthCheck{Test: []string{"CMD-SHELL", "exit 0"}, Interval: "30s", Timeout: "5s", Retries: 3}
		mark("healthchecks", true, "static shape; liveness confirmed by convergence")
	}
	if p.Required("dedicated-bridge-net") {
		svc.Networks = []string{netName}
		mark("dedicated-bridge-net", true, "static")
	}

	c := Compose{Services: map[string]Service{serviceName: svc}}
	if len(svc.Networks) > 0 {
		c.Networks = map[string]struct{}{netName: {}}
	}
	return c, ev, nil
}

// Render serialises a Compose model to YAML.
func Render(c Compose) ([]byte, error) { return yaml.Marshal(c) }

func dedupeSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func capJustification(caps []string) string {
	if len(caps) == 0 {
		return "no capabilities observed in use; none added (fail-closed)"
	}
	return fmt.Sprintf("added only the %d capabilities observed in use", len(caps))
}

func tmpfsJustification(paths []string) string {
	if len(paths) == 0 {
		return "no writes observed; read-only root with no tmpfs (fail-closed)"
	}
	return fmt.Sprintf("tmpfs for the %d write paths observed", len(paths))
}
