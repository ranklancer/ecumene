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
	if len(p.Controls) != 14 {
		t.Fatalf("reference must declare 14 controls, got %d", len(p.Controls))
	}
	if !p.Required("no-new-privileges") || !p.Required("cap-drop-all-min-add") {
		t.Fatal("core controls must be required by default")
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
