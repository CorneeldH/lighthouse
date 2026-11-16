package pins

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a semantic version: MAJOR.MINOR.PATCH with optional prerelease and build.
type Version struct {
	Major, Minor, Patch uint64
	Pre                 string
	Build               string
}

const (
	numIdent  = `(?:0|[1-9][0-9]*)`
	preIdent  = `(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)`
	versionRe = numIdent + `\.` + numIdent + `\.` + numIdent +
		`(?:-` + preIdent + `(?:\.` + preIdent + `)*)?` +
		`(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?`
)

var (
	exactRe  = regexp.MustCompile(`^` + versionRe + `$`)
	searchRe = regexp.MustCompile(versionRe)
)

// ParseVersion parses MAJOR.MINOR.PATCH[-pre][+build]. A leading `v` is not accepted.
func ParseVersion(s string) (Version, error) {
	if !exactRe.MatchString(s) {
		return Version{}, fmt.Errorf("not a semantic version: %q", s)
	}
	rest, build, _ := strings.Cut(s, "+")
	core, pre, _ := strings.Cut(rest, "-")
	parts := strings.Split(core, ".")
	var nums [3]uint64
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return Version{}, fmt.Errorf("not a semantic version: %q: %w", s, err)
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2], Pre: pre, Build: build}, nil
}

// String returns the complete version, prerelease and build included.
func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	if v.Build != "" {
		s += "+" + v.Build
	}
	return s
}

// Compare orders by semver precedence: -1, 0 or 1. A prerelease sorts before its release;
// build metadata is ignored.
func (v Version) Compare(o Version) int {
	for _, p := range [3][2]uint64{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if c := cmpUint(p[0], p[1]); c != 0 {
			return c
		}
	}
	switch {
	case v.Pre == o.Pre:
		return 0
	case v.Pre == "":
		return 1
	case o.Pre == "":
		return -1
	}
	a, b := strings.Split(v.Pre, "."), strings.Split(o.Pre, ".")
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := cmpIdent(a[i], b[i]); c != 0 {
			return c
		}
	}
	return cmpUint(uint64(len(a)), uint64(len(b)))
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// cmpIdent compares two prerelease identifiers: numeric ones numerically and below
// alphanumeric ones, alphanumeric ones lexically.
func cmpIdent(a, b string) int {
	an, aerr := strconv.ParseUint(a, 10, 64)
	bn, berr := strconv.ParseUint(b, 10, 64)
	switch {
	case aerr == nil && berr == nil:
		return cmpUint(an, bn)
	case aerr == nil:
		return -1
	case berr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

// FirstVersionIn returns the first semantic version found in the output of a version command.
func FirstVersionIn(out string) (Version, bool) {
	for _, m := range searchRe.FindAllString(out, -1) {
		if v, err := ParseVersion(m); err == nil {
			return v, true
		}
	}
	return Version{}, false
}
