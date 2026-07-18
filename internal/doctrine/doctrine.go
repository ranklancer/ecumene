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

// Control is one doctrine control.
type Control struct {
	Status Status `yaml:"status"`
	Kind   Kind   `yaml:"kind"`
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
		if err := c.validate(); err != nil {
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

func (c Control) validate() error {
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
	return nil
}

// Required reports whether the named control is present and required.
func (p Profile) Required(name string) bool {
	c, ok := p.Controls[name]
	return ok && c.Status == StatusRequired
}
