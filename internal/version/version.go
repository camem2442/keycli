package version

import "fmt"

// Version and Commit are set at link time via -ldflags.
var (
	Version = "dev"
	Commit  = "none"
)

// String returns a human-readable version label.
func String() string {
	if Commit == "none" || Commit == "" {
		return Version
	}
	return fmt.Sprintf("%s (%s)", Version, Commit)
}
