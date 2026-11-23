package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// Release is one release of a repository as the forge lists it.
type Release struct {
	Tag, Name, Body, URL string
	Draft, Prerelease    bool
}

// ReleaseReader lists the releases of any repository. It is not part of Forge, which is one
// repository: the releases read here are those of an upstream.
type ReleaseReader interface {
	Releases(ctx context.Context, repo string) ([]Release, error)
}

// ReleaseUntilReader is a ReleaseReader that can stop listing early. Releases come newest first,
// so a caller that needs only the releases down to one known release stops there: a repository
// with more than 1000 releases cannot be listed whole (GitHub answers 422 past the first 1000).
type ReleaseUntilReader interface {
	ReleaseReader
	// ReleasesUntil lists releases like Releases, but stops after the page that holds the first
	// release for which stop returns true. That page is returned whole.
	ReleasesUntil(ctx context.Context, repo string, stop func(Release) bool) ([]Release, error)
}

var (
	_ ReleaseReader      = (*GitHub)(nil)
	_ ReleaseUntilReader = (*GitHub)(nil)
)

// releasesPerPage is 30, not 100: openai/codex answers HTTP 504 at per_page=100.
const releasesPerPage = 30

// maxReleasePages bounds the pages Releases follows, so a bad Link header cannot loop it.
const maxReleasePages = 100

// Releases lists all releases of repo ("owner/name"), following every page, newest first as the
// forge orders them. Without a token the request is anonymous: public repositories only, with the
// lower rate limit. Drafts and prereleases are included, with their flags. A 5xx answer is
// tried once more.
func (c *GitHub) Releases(ctx context.Context, repo string) ([]Release, error) {
	return c.ReleasesUntil(ctx, repo, nil)
}

// ReleasesUntil is Releases that stops after the page holding the first release for which stop
// returns true. A nil stop follows every page.
func (c *GitHub) ReleasesUntil(ctx context.Context, repo string, stop func(Release) bool) ([]Release, error) {
	if repo == "" {
		return nil, errors.New("forge releases: empty repo")
	}
	type item struct {
		Tag        string `json:"tag_name"`
		Name       string `json:"name"`
		Body       string `json:"body"`
		URL        string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	var out []Release
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=%d", c.BaseURL, repo, releasesPerPage)
	seen := map[string]bool{}
	for url != "" {
		if seen[url] {
			return nil, fmt.Errorf("forge releases %s: next page repeats %s", repo, url)
		}
		if len(seen) >= maxReleasePages {
			return nil, fmt.Errorf("forge releases %s: more than %d pages", repo, maxReleasePages)
		}
		seen[url] = true
		var page []item
		next, err := c.doRetry5xx(ctx, url, &page)
		if err != nil {
			return nil, err
		}
		stopped := false
		for _, it := range page {
			r := Release(it)
			out = append(out, r)
			if stop != nil && stop(r) {
				stopped = true
			}
		}
		if stopped {
			break
		}
		url = next
	}
	return out, nil
}

// doRetry5xx is a GET through do that repeats once after a 5xx status.
func (c *GitHub) doRetry5xx(ctx context.Context, url string, into any) (string, error) {
	next, err := c.do(ctx, http.MethodGet, url, nil, into)
	var se *httpStatusError
	if errors.As(err, &se) && se.Code >= 500 && ctx.Err() == nil {
		return c.do(ctx, http.MethodGet, url, nil, into)
	}
	return next, err
}
