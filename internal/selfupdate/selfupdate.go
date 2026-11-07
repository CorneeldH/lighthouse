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
	"regexp"
	"strconv"
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
}

// Result reports the outcome and the versions involved.
type Result struct {
	Outcome Outcome
	From    string
	To      string
}

var tagRe = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

func parseTag(s string) ([3]int, error) {
	var v [3]int
	m := tagRe.FindStringSubmatch(s)
	if m == nil {
		return v, fmt.Errorf("version %q is not of the form vX.Y.Z", s)
	}
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return v, fmt.Errorf("version %q: %w", s, err)
		}
		v[i] = n
	}
	return v, nil
}

func newer(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
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
	cur, err := parseTag(o.Current)
	if err != nil {
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
	latest, err := parseTag(tag)
	if err != nil {
		return res, fmt.Errorf("latest release: %w", err)
	}
	res.To = tag
	if !newer(latest, cur) {
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
