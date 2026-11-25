package selfupdate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/pins"
)

// channelTagRe accepts vX.Y.Z with an optional -dev.N or -rc.N suffix; N has no leading zero.
var channelTagRe = regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:dev|rc)\.(?:0|[1-9][0-9]*))?$`)

// ParseTag parses a release tag: vX.Y.Z, vX.Y.Z-dev.N or vX.Y.Z-rc.N.
func ParseTag(s string) (pins.Version, error) {
	if !channelTagRe.MatchString(s) {
		return pins.Version{}, fmt.Errorf("not a release tag: %q", s)
	}
	return pins.ParseVersion(strings.TrimPrefix(s, "v"))
}

// CompareTags orders two release tags by semver precedence: -1, 0 or 1.
func CompareTags(a, b string) (int, error) {
	va, err := ParseTag(a)
	if err != nil {
		return 0, err
	}
	vb, err := ParseTag(b)
	if err != nil {
		return 0, err
	}
	return va.Compare(vb), nil
}

// HighestTag returns the highest valid tag; invalid tags are skipped.
func HighestTag(tags []string) (string, error) {
	best := ""
	var bestV pins.Version
	for _, t := range tags {
		v, err := ParseTag(t)
		if err != nil {
			continue
		}
		if best == "" || v.Compare(bestV) > 0 {
			best, bestV = t, v
		}
	}
	if best == "" {
		return "", errors.New("no valid release tag")
	}
	return best, nil
}
