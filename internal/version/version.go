// Package version holds build-time version information.
package version

import "fmt"

var (
	// Version is the Beeket version string, set at build time via -ldflags.
	Version = "0.1.0-dev"

	// Commit is the git commit SHA, set at build time.
	Commit = "unknown"

	// BuildDate is the build date, set at build time.
	BuildDate = "unknown"
)

// String returns a human-readable version string.
func String() string {
	return fmt.Sprintf("beeket %s (commit %s, built %s)", Version, Commit, BuildDate)
}
