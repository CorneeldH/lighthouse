// Package selfupdate replaces the running lighthouse binary with the latest
// GitHub release, after verifying its SHA-256 checksum. It prints nothing.
package selfupdate

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Outcome says what Run did to the binary.
type Outcome int

const (
	SkippedDev Outcome = iota
	UpToDate
	Replaced
)

// Options configures Run.
type Options struct {
	Current      string       // version.String()
	Target       string       // resolved path of the binary to replace
	GOOS, GOARCH string       //
	Repo         string       // "CorneeldH/lighthouse"
	APIBase      string       // "https://api.github.com"
	DownloadBase string       // "https://github.com"
	Token        string       // optional, sent to the API call only
	Client       *http.Client // nil = http.DefaultClient
	Restart      func() error // nil = no restart; called only after a replace
	Channel      Channel      // "" = release
}

// Result reports the outcome and the versions involved.
type Result struct {
	Outcome Outcome
	From    string
	To      string
}

// Channel selects which releases Run may update to.
type Channel string

const (
	// ChannelRelease follows the latest published release (vX.Y.Z only).
	ChannelRelease Channel = "release"
	// ChannelDev follows the highest tag, including -dev.N and -rc.N.
	ChannelDev Channel = "dev"
)

// ParseChannel accepts exactly "release" and "dev".
func ParseChannel(s string) (Channel, error) {
	switch Channel(s) {
	case ChannelRelease, ChannelDev:
		return Channel(s), nil
	}
	return "", fmt.Errorf("unknown channel %q; want release or dev", s)
}

// Run replaces o.Target with the latest release when it is strictly newer
// than o.Current. On any error before the rename the target is unchanged.
func Run(ctx context.Context, o Options) (Result, error) {
	res := Result{Outcome: SkippedDev, From: o.Current}
	if o.Current == "dev" {
		return res, nil
	}
	if o.GOOS == "windows" {
		return res, errors.New("self-update is not supported on windows")
	}
	if _, err := ParseTag(o.Current); err != nil {
		return res, fmt.Errorf("current: %w", err)
	}
	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}

	tag, err := latestTag(ctx, client, o)
	if err != nil {
		return res, err
	}
	if _, err := ParseTag(tag); err != nil {
		return res, fmt.Errorf("latest release: %w", err)
	}
	res.To = tag
	if cmp, err := CompareTags(tag, o.Current); err != nil {
		return res, fmt.Errorf("compare %s with %s: %w", tag, o.Current, err)
	} else if cmp <= 0 {
		res.Outcome = UpToDate
		return res, nil
	}

	asset := "lighthouse_" + o.GOOS + "_" + o.GOARCH
	base := strings.TrimRight(o.DownloadBase, "/") + "/" + o.Repo + "/releases/download/" + tag + "/"

	want, err := expectedSum(ctx, client, base+"SHA256SUMS", asset)
	if err != nil {
		return res, err
	}
	if err := install(ctx, client, base+asset, want, o.Target); err != nil {
		return res, err
	}
	res.Outcome = Replaced

	if o.Restart != nil {
		if err := o.Restart(); err != nil {
			return res, fmt.Errorf("restart after update (replaced %s with %s): %w", res.From, res.To, err)
		}
	}
	return res, nil
}

func get(ctx context.Context, c *http.Client, url, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return resp, nil
}

func latestTag(ctx context.Context, c *http.Client, o Options) (string, error) {
	if o.Channel == ChannelDev {
		return highestReleaseTag(ctx, c, o)
	}
	url := strings.TrimRight(o.APIBase, "/") + "/repos/" + o.Repo + "/releases/latest"
	resp, err := get(ctx, c, url, o.Token)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode %s: %w", url, err)
	}
	return body.TagName, nil
}

// maxReleasePages bounds the pagination loop of the dev channel.
const maxReleasePages = 100

// highestReleaseTag lists every release over all pages, drops drafts and
// returns the highest valid tag.
func highestReleaseTag(ctx context.Context, c *http.Client, o Options) (string, error) {
	base := strings.TrimRight(o.APIBase, "/")
	next := base + "/repos/" + o.Repo + "/releases?per_page=100"
	var tags []string
	for page := 0; next != "" && page < maxReleasePages; page++ {
		if !strings.HasPrefix(next, base+"/") {
			return "", fmt.Errorf("release list: next page %s is outside %s", next, base)
		}
		resp, err := get(ctx, c, next, o.Token)
		if err != nil {
			return "", err
		}
		var list []struct {
			TagName string `json:"tag_name"`
			Draft   bool   `json:"draft"`
		}
		err = json.NewDecoder(resp.Body).Decode(&list)
		link := resp.Header.Get("Link")
		_ = resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("decode %s: %w", next, err)
		}
		for _, r := range list {
			if !r.Draft {
				tags = append(tags, r.TagName)
			}
		}
		next = nextLink(link)
	}
	tag, err := HighestTag(tags)
	if err != nil {
		return "", fmt.Errorf("dev channel: %w", err)
	}
	return tag, nil
}

// nextLink returns the rel="next" target of a Link header, or "".
func nextLink(h string) string {
	for _, part := range strings.Split(h, ",") {
		seg := strings.Split(part, ";")
		if len(seg) < 2 {
			continue
		}
		u := strings.TrimSpace(seg[0])
		if !strings.HasPrefix(u, "<") || !strings.HasSuffix(u, ">") {
			continue
		}
		for _, p := range seg[1:] {
			if strings.ReplaceAll(strings.TrimSpace(p), " ", "") == `rel="next"` {
				return u[1 : len(u)-1]
			}
		}
	}
	return ""
}

// expectedSum returns the hex digest listed for asset in the SHA256SUMS file.
func expectedSum(ctx context.Context, c *http.Client, url, asset string) (string, error) {
	resp, err := get(ctx, c, url, "")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset {
			return strings.ToLower(f[0]), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("read %s: %w", url, err)
	}
	return "", fmt.Errorf("no checksum for %s in %s", asset, url)
}

// install streams url into a temp file next to target, verifies it against
// want, and renames it over target. The temp file never outlives a failure.
func install(ctx context.Context, c *http.Client, url, want, target string) (err error) {
	resp, err := get(ctx, c, url, "")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	tmp, err := os.CreateTemp(filepath.Dir(target), ".lighthouse-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	renamed := false
	defer func() {
		_ = tmp.Close()
		if !renamed {
			_ = os.Remove(tmp.Name())
		}
	}()

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", url, got, want)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("replace %s: %w", target, err)
	}
	renamed = true
	return nil
}
