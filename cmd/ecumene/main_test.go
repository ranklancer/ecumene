package main

import (
	"bytes"
	"strings"
	"testing"
)

func runCap(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestCLI_VersionAndHelp(t *testing.T) {
	if code, out, _ := runCap("-version"); code != 0 || !strings.Contains(out, "ecumene") {
		t.Errorf("-version: code=%d out=%q", code, out)
	}
	if code, out, _ := runCap(); code != 0 || !strings.Contains(out, "Usage") {
		t.Errorf("no-args help: code=%d", code)
	}
	if code, out, _ := runCap("version"); code != 0 || !strings.Contains(out, "ecumene") {
		t.Errorf("version subcommand: code=%d out=%q", code, out)
	}
}

func TestCLI_Doctrine(t *testing.T) {
	code, out, _ := runCap("doctrine")
	if code != 0 || !strings.Contains(out, "reference/v1") || !strings.Contains(out, "no-new-privileges") {
		t.Errorf("doctrine: code=%d out=%q", code, out)
	}
}

func TestCLI_Forge(t *testing.T) {
	code, out, errb := runCap("forge", "nginx@sha256:abc")
	if code != 0 {
		t.Fatalf("forge failed: code=%d err=%s", code, errb)
	}
	if !strings.Contains(out, "no-new-privileges:true") || !strings.Contains(out, "read_only") {
		t.Errorf("forge output missing hardening:\n%s", out)
	}
	if !strings.Contains(errb, "fail-closed defaults") {
		t.Errorf("forge should note fail-closed observation defaults, got %q", errb)
	}
}

func TestCLI_Errors(t *testing.T) {
	if code, _, _ := runCap("forge"); code != 2 {
		t.Errorf("forge with no image must exit 2, got %d", code)
	}
	if code, _, _ := runCap("bogus"); code != 2 {
		t.Errorf("unknown command must exit 2, got %d", code)
	}
}
