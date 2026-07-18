// Command ecumene forges a hardened deployment manifest from a container image
// by observing what the image actually needs and tightening to the minimal
// working set (SPEC-G "forge" in the suite: forge (ecumene) -> verify (bulwark)
// -> act (juridical)).
//
// P0 (SPEC-G, D-3): does-it-start convergence on ONE stateless image, Compose
// output only. The sandbox runtime + eBPF tracer (D-5) are a reviewed follow-up;
// until they land, `forge` emits the STATIC-hardened compose + evidence, with
// observation-derived controls (caps/tmpfs) held at their fail-closed defaults.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ranklancer/ecumene/internal/doctrine"
	"github.com/ranklancer/ecumene/internal/emit"
	"github.com/ranklancer/ecumene/internal/observe"
	"github.com/ranklancer/ecumene/internal/version"
)

const usage = `ecumene - observation-driven hardened-deployment generator (forge).

Usage:
  ecumene <command> [flags]

Commands:
  forge <image>   author a hardened Compose fragment + evidence for an image
  doctrine        print the shipped reference/v1 hardening doctrine
  version         print build metadata

Global flags:
  -version   print build metadata and exit
  -help      print this message and exit
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ecumene", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print build metadata and exit")
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, version.Get().String())
		return 0
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}
	switch rest[0] {
	case "version":
		fmt.Fprintln(stdout, version.Get().String())
		return 0
	case "doctrine":
		return runDoctrine(stdout)
	case "forge":
		return runForge(rest[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "ecumene: unknown command %q\n\n%s", rest[0], usage)
		return 2
	}
}

func runDoctrine(stdout io.Writer) int {
	p := doctrine.Reference()
	fmt.Fprintf(stdout, "doctrine %s/%s (%d controls)\n", p.Name, p.Version, len(p.Controls))
	for _, name := range sortedControls(p) {
		c := p.Controls[name]
		fmt.Fprintf(stdout, "  %-24s %-9s %s\n", name, c.Status, c.Kind)
	}
	return 0
}

func runForge(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("forge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	profilePath := fs.String("profile", "", "path to a doctrine profile YAML (default: reference/v1)")
	service := fs.String("service", "app", "compose service name")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, "ecumene forge: exactly one <image> argument is required\n")
		return 2
	}
	image := fs.Arg(0)

	prof := doctrine.Reference()
	if *profilePath != "" {
		raw, err := os.ReadFile(*profilePath) // #nosec G304 -- operator-supplied profile path
		if err != nil {
			fmt.Fprintf(stderr, "ecumene forge: read profile: %v\n", err)
			return 1
		}
		prof, err = doctrine.Load(raw)
		if err != nil {
			fmt.Fprintf(stderr, "ecumene forge: %v\n", err)
			return 1
		}
	}

	// P0 scaffold: static-hardened baseline with fail-closed observation-derived
	// controls (empty observation). The convergence loop wires a real sandbox +
	// tracer in a follow-up (D-5).
	c, ev, err := emit.Harden(*service, image, prof, observe.Result{})
	if err != nil {
		fmt.Fprintf(stderr, "ecumene forge: %v\n", err)
		return 1
	}
	out, err := emit.Render(c)
	if err != nil {
		fmt.Fprintf(stderr, "ecumene forge: render: %v\n", err)
		return 1
	}
	if _, err := stdout.Write(out); err != nil {
		return 1
	}
	fmt.Fprintf(stderr, "\n# evidence: %s/%s, tier %d, %d controls applied\n", ev.ProfileName, ev.ProfileVersion, ev.Tier, len(ev.Controls))
	fmt.Fprintln(stderr, "# NOTE: observation-derived controls (caps/tmpfs) are at fail-closed defaults;")
	fmt.Fprintln(stderr, "#       the sandbox runtime + tracer (D-5) will tighten these to observed use.")
	return 0
}

func sortedControls(p doctrine.Profile) []string {
	out := make([]string, 0, len(p.Controls))
	for k := range p.Controls {
		out = append(out, k)
	}
	// simple insertion sort to avoid pulling sort into main for a small set
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
