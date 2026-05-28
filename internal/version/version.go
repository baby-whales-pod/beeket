// Package version holds build-time version information for beeketd.
// These variables are set via -ldflags at build time.
package version

var (
	// Version is the semantic version string (e.g. "0.1.0").
	Version = "0.1.0-dev"
	// Commit is the short git commit hash.
	Commit = "unknown"
	// BuildDate is the RFC3339 build timestamp.
	BuildDate = "unknown"
)
