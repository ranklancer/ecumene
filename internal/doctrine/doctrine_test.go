package doctrine

import (
	"errors"
	"testing"
)

func TestReference_LoadsAndIsRequiredByDefault(t *testing.T) {
	p := Reference()
	if p.Name != "reference" || p.Version != "v1" {
		t.Fatalf("reference id = %s/%s", p.Name, p.Version)
	}
	if len(p.Controls) != 17 {
		t.Fatalf("reference must declare 17 controls, got %d", len(p.Controls))
	}
	if !p.Required("no-new-privileges") || !p.Required("cap-drop-all-min-add") {
		t.Fatal("core controls must be required by default")
	}
	if !p.Required("non-root-user") || !p.Required("memory-limit") || !p.Required("network-mode") {
		t.Fatal("the sandbox-fidelity controls (non-root-user, memory-limit, network-mode) must be required by default")
	}
}

func TestReference_NetworkModeDefaultsToNone(t *testing.T) {
	// The shipped reference profile is the fail-closed batch/one-shot
	// default: it must not silently open a network. A profile authored for
	// a service that must listen sets value: serve explicitly.
	if got := Reference().NetworkMode(); got != "none" {
		t.Fatalf("reference NetworkMode() = %q, want %q", got, "none")
	}
}

func TestLoad_StrictRejectsUnknownField(t *testing.T) {
	// A misspelled/unknown key must fail closed, never be silently ignored — a
	// dropped control must not pass as "absent".
	_, err := Load([]byte("name: x\nversion: v1\ncontrols:\n  a: {status: required, kind: static}\nbogus: true\n"))
	if err == nil {
		t.Fatal("unknown top-level field must fail closed")
	}
}

func TestLoad_RejectsEmptyAndInvalid(t *testing.T) {
	if _, err := Load([]byte("name: x\nversion: v1\ncontrols: {}\n")); !errors.Is(err, ErrEmptyProfile) {
		t.Fatalf("empty controls must be rejected, got %v", err)
	}
	if _, err := Load([]byte("name: x\nversion: v1\ncontrols:\n  a: {status: bogus, kind: static}\n")); err == nil {
		t.Fatal("invalid status must be rejected")
	}
	if _, err := Load([]byte("name: x\nversion: v1\ncontrols:\n  a: {status: required, kind: bogus}\n")); err == nil {
		t.Fatal("invalid kind must be rejected")
	}
}

func TestLoad_ValidRoundTrips(t *testing.T) {
	p, err := Load([]byte("name: custom\nversion: v2\ncontrols:\n  no-new-privileges: {status: required, kind: static}\n  x: {status: off, kind: observation}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Required("no-new-privileges") || p.Required("x") {
		t.Fatalf("required lookup wrong: %+v", p.Controls)
	}
}

// TestLoad_NetworkModeValue exercises the network-mode control's Value
// field: only "none"/"serve" (or empty) are valid, and only the
// network-mode control may carry a value at all.
func TestLoad_NetworkModeValue(t *testing.T) {
	if _, err := Load([]byte("name: x\nversion: v1\ncontrols:\n  network-mode: {status: required, kind: static, value: serve}\n")); err != nil {
		t.Fatalf("value: serve must be accepted, got %v", err)
	}
	if _, err := Load([]byte("name: x\nversion: v1\ncontrols:\n  network-mode: {status: required, kind: static, value: bogus}\n")); err == nil {
		t.Fatal("invalid network-mode value must be rejected")
	}
	if _, err := Load([]byte("name: x\nversion: v1\ncontrols:\n  no-new-privileges: {status: required, kind: static, value: serve}\n")); err == nil {
		t.Fatal("a value on a control other than network-mode must be rejected")
	}
}

// TestProfile_NetworkMode pins the fail-closed semantics: "serve" is
// returned ONLY when network-mode is required AND explicitly value: serve.
// Every other combination -- absent, off, warn, required with an empty or
// unrecognised value -- resolves to "none". An absent/omitted control must
// never widen network exposure.
func TestProfile_NetworkMode(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "absent",
			yaml: "name: x\nversion: v1\ncontrols:\n  no-new-privileges: {status: required, kind: static}\n",
			want: "none",
		},
		{
			name: "off",
			yaml: "name: x\nversion: v1\ncontrols:\n  network-mode: {status: off, kind: static, value: serve}\n",
			want: "none",
		},
		{
			name: "warn",
			yaml: "name: x\nversion: v1\ncontrols:\n  network-mode: {status: warn, kind: static, value: serve}\n",
			want: "none",
		},
		{
			name: "required-empty-value",
			yaml: "name: x\nversion: v1\ncontrols:\n  network-mode: {status: required, kind: static}\n",
			want: "none",
		},
		{
			name: "required-none",
			yaml: "name: x\nversion: v1\ncontrols:\n  network-mode: {status: required, kind: static, value: none}\n",
			want: "none",
		},
		{
			name: "required-serve",
			yaml: "name: x\nversion: v1\ncontrols:\n  network-mode: {status: required, kind: static, value: serve}\n",
			want: "serve",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Load([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := p.NetworkMode(); got != tc.want {
				t.Fatalf("NetworkMode() = %q, want %q", got, tc.want)
			}
		})
	}
}
