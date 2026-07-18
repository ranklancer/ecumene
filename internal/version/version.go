// Package version carries build metadata injected via -ldflags at build time.
package version

import "fmt"

var (
	// Version is the semantic version (git tag) or "dev".
	Version = "dev"
	// Commit is the short git SHA.
	Commit = "none"
	// Date is the build/commit date (RFC3339).
	Date = "unknown"
)

// Info is the resolved build metadata.
type Info struct {
	Version string
	Commit  string
	Date    string
}

// Get returns the current build metadata.
func Get() Info { return Info{Version: Version, Commit: Commit, Date: Date} }

// String renders the metadata on one line.
func (i Info) String() string {
	return fmt.Sprintf("ecumene %s (commit %s, built %s)", i.Version, i.Commit, i.Date)
}
