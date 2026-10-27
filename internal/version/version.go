// Package version reports the version of the lighthouse binary.
//
// The version of every package is the version of the binary (spec, decision 2).
// A local build has no tag; it reports "dev".
package version

import (
	"regexp"
	"runtime/debug"
	"strings"
)

// A Go pseudo-version ends in a 14-digit timestamp and a 12-character commit hash.
var pseudo = regexp.MustCompile(`\d{14}-[0-9a-f]{12}$`)

// String returns the tag of the binary, or "dev" for a build without one.
func String() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	return normalize(info.Main.Version)
}

func normalize(v string) string {
	if v == "" || v == "(devel)" || strings.Contains(v, "+") || pseudo.MatchString(v) {
		return "dev"
	}
	return v
}
