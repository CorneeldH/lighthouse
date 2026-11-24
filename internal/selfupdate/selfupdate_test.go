package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

const asset = "lighthouse_linux_amd64"

type fixture struct {
	srv       *httptest.Server
	target    string
	dir       string
	downloads atomic.Int32
	calls     atomic.Int32
	auth      atomic.Value // string, Authorization of the API call
	sumsAuth  atomic.Value // string
	assetAuth atomic.Value // string
	tag       string
	sums      string // SHA256SUMS body; "" = computed from newBody
	assetCode int
	newBody   string
	pages     [][]rel // /releases pages; page 1 is pages[0]
	latestHit atomic.Int32
	listHit   atomic.Int32
	listQuery atomic.Value // string, RawQuery of the last /releases call
}

type rel struct {
	Tag   string `json:"tag_name"`
	Draft bool   `json:"draft"`
}

func sumOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func newFixture(t *testing.T, tag string) *fixture {
	t.Helper()
	f := &fixture{tag: tag, newBody: "new", assetCode: 200}
	f.dir = t.TempDir()
	f.target = filepath.Join(f.dir, "lighthouse")
	if err := os.WriteFile(f.target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.auth.Store("")
	f.sumsAuth.Store("")
	f.assetAuth.Store("")
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/CorneeldH/lighthouse/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.latestHit.Add(1)
		f.auth.Store(r.Header.Get("Authorization"))
		_, _ = fmt.Fprintf(w, `{"tag_name": %q}`, f.tag)
	})
	mux.HandleFunc("/repos/CorneeldH/lighthouse/releases", func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.listHit.Add(1)
		f.auth.Store(r.Header.Get("Authorization"))
		f.listQuery.Store(r.URL.RawQuery)
		page := 1
		if p := r.URL.Query().Get("page"); p != "" {
			page, _ = strconv.Atoi(p)
		}
		if page < 1 || page > len(f.pages) {
			_, _ = fmt.Fprint(w, "[]")
			return
		}
		if page < len(f.pages) {
			next := url.Values{"per_page": {"100"}, "page": {strconv.Itoa(page + 1)}}
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/CorneeldH/lighthouse/releases?%s>; rel="next"`, f.srv.URL, next.Encode()))
		}
		_ = json.NewEncoder(w).Encode(f.pages[page-1])
	})
	base := "/CorneeldH/lighthouse/releases/download/" + tag + "/"
	mux.HandleFunc(base+asset, func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.downloads.Add(1)
		f.assetAuth.Store(r.Header.Get("Authorization"))
		if f.assetCode != 200 {
			http.Error(w, "boom", f.assetCode)
			return
		}
		_, _ = fmt.Fprint(w, f.newBody)
	})
	mux.HandleFunc(base+"SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.downloads.Add(1)
		f.sumsAuth.Store(r.Header.Get("Authorization"))
		if f.sums != "" {
			_, _ = fmt.Fprint(w, f.sums)
			return
		}
		_, _ = fmt.Fprintf(w, "%s  %s\n", sumOf(f.newBody), asset)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fixture) opts(current string) Options {
	return Options{
		Current: current, Target: f.target, GOOS: "linux", GOARCH: "amd64",
		Repo: "CorneeldH/lighthouse", APIBase: f.srv.URL, DownloadBase: f.srv.URL,
	}
}

func (f *fixture) assertOld(t *testing.T) {
	t.Helper()
	b, err := os.ReadFile(f.target)
	if err != nil || string(b) != "old" {
		t.Fatalf("target = %q, %v; want old", b, err)
	}
	f.assertOnlyTarget(t)
}

func (f *fixture) assertOnlyTarget(t *testing.T) {
	t.Helper()
	es, _ := os.ReadDir(f.dir)
	if len(es) != 1 || es[0].Name() != "lighthouse" {
		var n []string
		for _, e := range es {
			n = append(n, e.Name())
		}
		t.Fatalf("dir holds %v; want only lighthouse", n)
	}
}

func TestRunReplacesOnNewerTagAndGoodChecksum(t *testing.T) {
	f := newFixture(t, "v0.11.3")
	res, err := Run(context.Background(), f.opts("v0.11.2"))
	if err != nil {
		t.Fatal(err)
	}
	if res != (Result{Replaced, "v0.11.2", "v0.11.3"}) {
		t.Fatalf("result = %+v", res)
	}
	b, _ := os.ReadFile(f.target)
	if string(b) != "new" {
		t.Fatalf("target = %q", b)
	}
	f.assertOnlyTarget(t)
	if runtime.GOOS == "windows" {
		t.Skip("NTFS has no POSIX mode bits; see host-assumptions.md")
	}
	st, _ := os.Stat(f.target)
	if st.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", st.Mode())
	}
}

func TestRunKeepsBinaryOnBadChecksum(t *testing.T) {
	f := newFixture(t, "v0.11.3")
	f.sums = strings.Repeat("0", 64) + "  " + asset + "\n"
	_, err := Run(context.Background(), f.opts("v0.11.2"))
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v; want checksum error", err)
	}
	f.assertOld(t)
}

func TestRunKeepsBinaryWhenAssetMissingFromSums(t *testing.T) {
	f := newFixture(t, "v0.11.3")
	f.sums = sumOf("new") + "  lighthouse_darwin_arm64\n"
	if _, err := Run(context.Background(), f.opts("v0.11.2")); err == nil {
		t.Fatal("want error")
	}
	f.assertOld(t)
}

func TestRunUpToDate(t *testing.T) {
	for _, c := range []struct{ cur, tag string }{
		{"v0.11.3", "v0.11.3"}, {"v0.11.10", "v0.11.9"}, {"v1.0.0", "v0.99.99"},
	} {
		t.Run(c.cur+"_"+c.tag, func(t *testing.T) {
			f := newFixture(t, c.tag)
			res, err := Run(context.Background(), f.opts(c.cur))
			if err != nil {
				t.Fatal(err)
			}
			if res.Outcome != UpToDate {
				t.Fatalf("outcome = %v", res.Outcome)
			}
			if n := f.downloads.Load(); n != 0 {
				t.Fatalf("downloads = %d", n)
			}
			f.assertOld(t)
		})
	}
}

func TestRunNewerComparesNumerically(t *testing.T) {
	f := newFixture(t, "v0.11.10")
	res, err := Run(context.Background(), f.opts("v0.11.9"))
	if err != nil || res.Outcome != Replaced {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestRunSkipsDevBuild(t *testing.T) {
	f := newFixture(t, "v0.11.3")
	res, err := Run(context.Background(), f.opts("dev"))
	if err != nil || res.Outcome != SkippedDev {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if n := f.calls.Load(); n != 0 {
		t.Fatalf("server calls = %d", n)
	}
	f.assertOld(t)
}

func TestRunRejectsWindows(t *testing.T) {
	f := newFixture(t, "v0.11.3")
	o := f.opts("v0.11.2")
	o.GOOS = "windows"
	if _, err := Run(context.Background(), o); err == nil {
		t.Fatal("want error")
	}
	f.assertOld(t)
}

func TestRunRejectsMalformedTag(t *testing.T) {
	for _, tag := range []string{"v0.12.0-beta.1", "0.12.0", ""} {
		t.Run(tag, func(t *testing.T) {
			f := newFixture(t, tag)
			if _, err := Run(context.Background(), f.opts("v0.11.2")); err == nil {
				t.Fatal("want error")
			}
			f.assertOld(t)
		})
	}
}

func TestRunKeepsBinaryOnDownloadError(t *testing.T) {
	f := newFixture(t, "v0.11.3")
	f.assetCode = 500
	_, err := Run(context.Background(), f.opts("v0.11.2"))
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v", err)
	}
	f.assertOld(t)
}

func TestRunAcceptsBinaryModeChecksumLine(t *testing.T) {
	f := newFixture(t, "v0.11.3")
	f.sums = strings.Repeat("a", 64) + "  lighthouse_darwin_arm64\n" + sumOf("new") + " *" + asset + "\n"
	res, err := Run(context.Background(), f.opts("v0.11.2"))
	if err != nil || res.Outcome != Replaced {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestRunReplacesSymlinkTarget(t *testing.T) {
	f := newFixture(t, "v0.11.3")
	root := t.TempDir()
	real := filepath.Join(root, "real")
	bin := filepath.Join(root, "bin")
	for _, d := range []string{real, bin} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(real, "lighthouse"), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(bin, "lighthouse")
	if err := os.Symlink(filepath.Join("..", "real", "lighthouse"), link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	o := f.opts("v0.11.2")
	o.Target = resolved
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link not kept: %v %v", st, err)
	}
	b, _ := os.ReadFile(filepath.Join(real, "lighthouse"))
	if string(b) != "new" {
		t.Fatalf("real = %q", b)
	}
}

func TestRunRestartOnlyAfterReplace(t *testing.T) {
	var n int
	restart := func() error { n++; return nil }

	f := newFixture(t, "v0.11.3")
	o := f.opts("v0.11.2")
	o.Restart = restart
	if _, err := Run(context.Background(), o); err != nil || n != 1 {
		t.Fatalf("after replace: n = %d, err = %v", n, err)
	}

	n = 0
	f = newFixture(t, "v0.11.3")
	o = f.opts("v0.11.3")
	o.Restart = restart
	if _, err := Run(context.Background(), o); err != nil || n != 0 {
		t.Fatalf("after up to date: n = %d, err = %v", n, err)
	}

	f = newFixture(t, "v0.11.3")
	o = f.opts("dev")
	o.Restart = restart
	if _, err := Run(context.Background(), o); err != nil || n != 0 {
		t.Fatalf("after dev: n = %d, err = %v", n, err)
	}
}

func TestRunRestartFailureKeepsNewBinary(t *testing.T) {
	f := newFixture(t, "v0.11.3")
	boom := errors.New("boom")
	o := f.opts("v0.11.2")
	o.Restart = func() error { return boom }
	_, err := Run(context.Background(), o)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; want wrapping boom", err)
	}
	if !strings.Contains(err.Error(), "v0.11.2") || !strings.Contains(err.Error(), "v0.11.3") {
		t.Fatalf("err = %v; want both versions", err)
	}
	b, _ := os.ReadFile(f.target)
	if string(b) != "new" {
		t.Fatalf("target = %q", b)
	}
}

func TestRunSendsTokenOnlyWhenSet(t *testing.T) {
	f := newFixture(t, "v0.11.3")
	o := f.opts("v0.11.3")
	o.Token = "abc"
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if got := f.auth.Load().(string); got != "Bearer abc" {
		t.Fatalf("auth = %q", got)
	}
	f = newFixture(t, "v0.11.3")
	if _, err := Run(context.Background(), f.opts("v0.11.3")); err != nil {
		t.Fatal(err)
	}
	if got := f.auth.Load().(string); got != "" {
		t.Fatalf("auth = %q; want none", got)
	}
}

func TestRunSendsTokenToAPIOnlyNotToDownloads(t *testing.T) {
	f := newFixture(t, "v0.11.3")
	o := f.opts("v0.11.2")
	o.Token = "abc"
	res, err := Run(context.Background(), o)
	if err != nil || res.Outcome != Replaced {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if got := f.auth.Load().(string); got != "Bearer abc" {
		t.Fatalf("api auth = %q", got)
	}
	for name, v := range map[string]*atomic.Value{"SHA256SUMS": &f.sumsAuth, "asset": &f.assetAuth} {
		if got := v.Load().(string); got != "" {
			t.Fatalf("%s auth = %q; want none", name, got)
		}
	}
}

func TestRunRejectsMalformedCurrentBeforeNetwork(t *testing.T) {
	f := newFixture(t, "v0.11.3")
	if _, err := Run(context.Background(), f.opts("v0.12.0-beta.1")); err == nil {
		t.Fatal("want error")
	}
	if n := f.calls.Load(); n != 0 {
		t.Fatalf("server calls = %d; want 0", n)
	}
	f.assertOld(t)
}

func devOpts(f *fixture, current string) Options {
	o := f.opts(current)
	o.Channel = ChannelDev
	return o
}

func TestRunDevChannelPicksHighestIncludingPrerelease(t *testing.T) {
	f := newFixture(t, "v0.28.0-rc.1")
	f.pages = [][]rel{{{Tag: "v0.27.0"}, {Tag: "v0.28.0-rc.1"}, {Tag: "v0.28.0-dev.5"}}}
	res, err := Run(context.Background(), devOpts(f, "v0.27.0"))
	if err != nil {
		t.Fatal(err)
	}
	if res != (Result{Replaced, "v0.27.0", "v0.28.0-rc.1"}) {
		t.Fatalf("result = %+v", res)
	}
	if f.latestHit.Load() != 0 {
		t.Fatal("dev channel must not call /releases/latest")
	}
	if q := f.listQuery.Load().(string); !strings.Contains(q, "per_page=100") {
		t.Fatalf("query = %q; want per_page=100", q)
	}
}

func TestRunDevChannelIgnoresDrafts(t *testing.T) {
	f := newFixture(t, "v0.28.0-dev.5")
	f.pages = [][]rel{{{Tag: "v0.27.0"}, {Tag: "v0.28.0-rc.1", Draft: true}, {Tag: "v0.28.0-dev.5"}}}
	res, err := Run(context.Background(), devOpts(f, "v0.27.0"))
	if err != nil || res.To != "v0.28.0-dev.5" || res.Outcome != Replaced {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestRunDevChannelIgnoresTestTags(t *testing.T) {
	f := newFixture(t, "v0.28.0-rc.2")
	f.pages = [][]rel{{{Tag: "v0.28.0-test.9"}, {Tag: "v0.28.0-rc.2"}}}
	res, err := Run(context.Background(), devOpts(f, "v0.27.0"))
	if err != nil || res.To != "v0.28.0-rc.2" || res.Outcome != Replaced {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestRunDevChannelFollowsPagination(t *testing.T) {
	f := newFixture(t, "v0.30.0")
	f.pages = [][]rel{{{Tag: "v0.27.0"}}, {{Tag: "v0.28.0"}}, {{Tag: "v0.30.0"}}}
	res, err := Run(context.Background(), devOpts(f, "v0.27.0"))
	if err != nil || res.To != "v0.30.0" || res.Outcome != Replaced {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if n := f.listHit.Load(); n != 3 {
		t.Fatalf("list calls = %d; want 3", n)
	}
}

func TestRunReleaseChannelStaysOnLatest(t *testing.T) {
	f := newFixture(t, "v0.28.0")
	f.pages = [][]rel{{{Tag: "v0.29.0-rc.1"}}}
	for _, ch := range []Channel{"", ChannelRelease} {
		o := f.opts("v0.27.0")
		o.Channel = ch
		res, err := Run(context.Background(), o)
		if err != nil || res.To != "v0.28.0" {
			t.Fatalf("channel %q: res = %+v, err = %v", ch, res, err)
		}
		if err := os.WriteFile(f.target, []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if f.listHit.Load() != 0 || f.latestHit.Load() != 2 {
		t.Fatalf("list = %d, latest = %d", f.listHit.Load(), f.latestHit.Load())
	}
}

func TestRunNeverDowngrades(t *testing.T) {
	f := newFixture(t, "v0.28.0")
	res, err := Run(context.Background(), f.opts("v0.29.0-dev.3"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != UpToDate || res.To != "v0.28.0" {
		t.Fatalf("res = %+v", res)
	}
	f.assertOld(t)
}

func TestRunDevBinaryTagIsNotSkipped(t *testing.T) {
	f := newFixture(t, "v0.28.0")
	res, err := Run(context.Background(), f.opts("v0.29.0-dev.3"))
	if err != nil || res.Outcome == SkippedDev {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if f.calls.Load() == 0 {
		t.Fatal("want network calls")
	}
}

func TestRunDevChannelWithoutValidTagFails(t *testing.T) {
	f := newFixture(t, "v0.28.0")
	f.pages = [][]rel{{{Tag: "v0.28.0-test.1"}, {Tag: "v0.29.0", Draft: true}, {Tag: "junk"}}}
	if _, err := Run(context.Background(), devOpts(f, "v0.27.0")); err == nil {
		t.Fatal("want error")
	}
	f.assertOld(t)
}

func TestParseChannelRejectsUnknown(t *testing.T) {
	for _, s := range []string{"stable", "", "Dev"} {
		if _, err := ParseChannel(s); err == nil {
			t.Fatalf("ParseChannel(%q): want error", s)
		}
	}
	for s, want := range map[string]Channel{"release": ChannelRelease, "dev": ChannelDev} {
		if got, err := ParseChannel(s); err != nil || got != want {
			t.Fatalf("ParseChannel(%q) = %q, %v", s, got, err)
		}
	}
}
