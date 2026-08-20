package version

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

func canonicalSemver(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return semver.Canonical(v)
}

// SameRelease reports whether a and b refer to the same semver release.
func SameRelease(a, b string) bool {
	ca, cb := canonicalSemver(a), canonicalSemver(b)
	if ca == "" || cb == "" {
		return a == b
	}
	return ca == cb
}

// VersionMismatchLines returns plain-text warning lines when CLI and runtime versions differ.
func VersionMismatchLines(cliVersion, runtimeVersion string) []string {
	return []string{
		fmt.Sprintf("warning: CLI version (%s) does not match runtime version (%s).", cliVersion, runtimeVersion),
		"The operator CLI and runtime are released together; version skew can cause gRPC errors, missing commands, or subtle publish/deploy/session failures.",
		"Upgrade the CLI: phrony upgrade",
		"Upgrade the runtime: go install github.com/phrony-platform/runtime/cmd/phrony-runtime@latest (or update your Docker stack)",
	}
}
