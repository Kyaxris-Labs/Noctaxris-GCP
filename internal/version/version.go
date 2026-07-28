// Package version holds the product semver string.
package version

// Version is the Noctaxris-GCP release semver.
// Override at link time:
//
//	-ldflags "-X github.com/Kyaxris-Labs/Noctaxris-GCP/internal/version.Version=0.5.0"
var Version = "0.5.0"
