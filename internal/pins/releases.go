package pins

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// isPinRelease reports whether r is the non-draft, non-prerelease release of the pinned version.
func isPinRelease(r forge.Release, p Pin, pinVer Version) bool {
	if r.Draft || r.Prerelease {
		return false
	}
	raw, ok := strings.CutPrefix(r.Tag, p.TagPrefix)
	if !ok {
		return false
	}
	v, err := ParseVersion(raw)
	return err == nil && v.Compare(pinVer) == 0
}

type taggedRelease struct {
	rel forge.Release
	ver Version
}

// MeasureRelease compares the pinned version of a plugin or tool with the releases of its
// upstream repository. Only non-draft, non-prerelease releases whose tag is the pin's tag_prefix
// followed by a semantic version count. Releases lists the ones above the pin, newest first;
// Target is the newest version. Whatever cannot be measured is an error.
func MeasureRelease(ctx context.Context, rr forge.ReleaseReader, p Pin) Result {
	if p.Err != nil {
		return errResult("%v", p.Err)
	}
	pinVer, err := ParseVersion(p.Version)
	if err != nil {
		return errResult("pin version: %v", err)
	}
	var all []forge.Release
	if ur, ok := rr.(forge.ReleaseUntilReader); ok {
		// Newest first: nothing below the pin's own release matters, and a repository with more
		// than 1000 releases cannot be listed whole.
		all, err = ur.ReleasesUntil(ctx, p.Repo, func(r forge.Release) bool { return isPinRelease(r, p, pinVer) })
	} else {
		all, err = rr.Releases(ctx, p.Repo)
	}
	if err != nil {
		return errResult("releases of %s: %v", p.Repo, err)
	}
	var usable []taggedRelease
	for _, r := range all {
		if r.Draft || r.Prerelease {
			continue
		}
		raw, ok := strings.CutPrefix(r.Tag, p.TagPrefix)
		if !ok {
			continue
		}
		v, err := ParseVersion(raw)
		// A semver prerelease tag (-rc.1) is not a release to move to, unless it is the pin itself.
		if err != nil || (v.Pre != "" && v.Compare(pinVer) != 0) {
			continue
		}
		usable = append(usable, taggedRelease{r, v})
	}
	if len(usable) == 0 {
		return errResult("no release of %s has a tag %s<version>", p.Repo, p.TagPrefix)
	}
	sort.SliceStable(usable, func(i, j int) bool { return usable[i].ver.Compare(usable[j].ver) > 0 })
	newest := usable[0].ver
	found := false
	var above []forge.Release
	for _, u := range usable {
		switch c := u.ver.Compare(pinVer); {
		case c > 0:
			above = append(above, u.rel)
		case c == 0:
			found = true
		}
	}
	switch {
	case !found:
		return errResult("no release %s%s in %s", p.TagPrefix, p.Version, p.Repo)
	case newest.Compare(pinVer) < 0:
		return errResult("newest release %s of %s is below the pin %s", newest, p.Repo, p.Version)
	}
	target := newest.String()
	if len(above) == 0 {
		return Result{Status: StatusCurrent, Target: target}
	}
	return Result{Status: StatusBehind, Target: target, Releases: above,
		Detail: fmt.Sprintf("%d release(s) behind, newest %s", len(above), target)}
}
