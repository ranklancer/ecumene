// Package doctrine is Ecumene's policy engine (the internal design spec component A1). It loads a
// versioned hardening profile (the internal design spec), classifies each control as static or
// observation-derived, and validates a candidate manifest against the doctrine.
//
// D-4 (ratified): the enforcement substrate is Rego/OPA, but authors interact
// through this simple YAML doctrine profile which drives the Rego. The Rego
// evaluator is wired behind the Validator seam (see validator.go); the shipped
// P0 validator checks control presence/shape statically until the OPA policy
// module lands.
package doctrine

import (
	_ "embed"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed reference-v1.yaml
var referenceV1 []byte

// Status is a control's enforcement level.
type Status string

const (
	// StatusRequired means the control MUST hold; a violation fails closed.
	StatusRequired Status = "required"
	// StatusWarn means observe-and-report, never block.
	StatusWarn Status = "warn"
	// StatusOff means the control is disabled — only valid via an explicit,
	// recorded exception (never a silent default).
	StatusOff Status = "off"
)

// Kind classifies how a control's value is determined.
type Kind string

const (
	// KindStatic means the policy fully determines the emitted value.
	KindStatic Kind = "static"
	// KindObservation means the policy declares intent and the convergence
	// loop fills the concrete value from observed behaviour.
	KindObservation Kind = "observation"
)

// Sandbox/compose hardened values for the static per-profile controls
// non-root-user and memory-limit (added alongside network-mode to close the
// A3 sandbox fidelity gap: the launcher used to hardcode these regardless of
// doctrine). internal/emit and internal/sandbox (via internal/converge) both
// derive from these SAME constants — never duplicated as separate literals —
// so a compose service and its sandbox verification agree exactly on the
// run-as user and memory ceiling whenever the corresponding control is
// required.
const (
	// NonRootUser is the UID:GID applied when the "non-root-user" control is
	// required (nobody:nobody).
	NonRootUser = "65534:65534"
	// MemoryLimit is the memory ceiling applied when the "memory-limit"
	// control is required.
	MemoryLimit = "256m"
)

// Control is one doctrine control. Value is an optional, control-specific
// parameter; today only the "network-mode" control interprets it (see
// Profile.NetworkMode).
type Control struct {
	Status Status `yaml:"status"`
	Kind   Kind   `yaml:"kind"`
	Value  string `yaml:"value,omitempty"`
}

// Profile is a versioned hardening doctrine: a named, versioned set of controls.
type Profile struct {
	Name     string             `yaml:"name"`
	Version  string             `yaml:"version"`
	Controls map[string]Control `yaml:"controls"`
}

// ErrEmptyProfile is returned when a profile declares no controls.
var ErrEmptyProfile = errors.New("doctrine: profile declares no controls")

// Reference returns the shipped reference/v1 doctrine. It always parses; a
// panic here would be a build defect in the embedded file.
func Reference() Profile {
	p, err := parse(referenceV1)
	if err != nil {
		panic("doctrine: embedded reference-v1.yaml is invalid: " + err.Error())
	}
	return p
}

// Load parses a doctrine profile from raw YAML with strict decoding: an unknown
// or misspelled key fails closed rather than being silently ignored (a dropped
// control must never pass as "absent"). An empty profile is rejected.
func Load(raw []byte) (Profile, error) {
	p, err := parse(raw)
	if err != nil {
		return Profile{}, err
	}
	if len(p.Controls) == 0 {
		return Profile{}, ErrEmptyProfile
	}
	for name, c := range p.Controls {
		if err := c.validate(name); err != nil {
			return Profile{}, fmt.Errorf("doctrine: control %q: %w", name, err)
		}
	}
	return p, nil
}

func parse(raw []byte) (Profile, error) {
	dec := yaml.NewDecoder(bytesReader(raw))
	dec.KnownFields(true) // strict: reject unknown fields (tamper/typo-evident)
	var p Profile
	if err := dec.Decode(&p); err != nil {
		return Profile{}, fmt.Errorf("doctrine: parse: %w", err)
	}
	return p, nil
}

func (c Control) validate(name string) error {
	switch c.Status {
	case StatusRequired, StatusWarn, StatusOff:
	default:
		return fmt.Errorf("invalid status %q (want required|warn|off)", c.Status)
	}
	switch c.Kind {
	case KindStatic, KindObservation:
	default:
		return fmt.Errorf("invalid kind %q (want static|observation)", c.Kind)
	}
	// Value is a control-specific parameter; only "network-mode" interprets
	// it today. Rejecting it elsewhere keeps a typo'd/misplaced value from
	// being silently ignored on a control that never reads it.
	if name == "network-mode" {
		switch c.Value {
		case "", "none", "serve":
		default:
			return fmt.Errorf("invalid value %q (want none|serve)", c.Value)
		}
	} else if c.Value != "" {
		return fmt.Errorf("value is only valid on the network-mode control")
	}
	return nil
}

// Required reports whether the named control is present and required.
func (p Profile) Required(name string) bool {
	c, ok := p.Controls[name]
	return ok && c.Status == StatusRequired
}

// NetworkMode returns the doctrine-mandated network axis for the
// "network-mode" control: "serve" only when the control is required AND
// explicitly declares value: serve — a profile stating this service must
// listen/be reachable, so it gets a network. Every other case — the control
// absent, off, warn, or required with an empty/unrecognised value — resolves
// to "none", the fail-closed default (fully isolated, batch/one-shot shape,
// matching the sandbox's historical hardcoded default). An absent or
// omitted control must never widen network exposure.
//
// internal/emit and internal/sandbox (via internal/converge) both call this
// SAME method — never a duplicated rule — so the emitted compose and the
// sandbox launch agree on the network axis exactly.
func (p Profile) NetworkMode() string {
	c, ok := p.Controls["network-mode"]
	if !ok || c.Status != StatusRequired || c.Value != "serve" {
		return "none"
	}
	return "serve"
}
