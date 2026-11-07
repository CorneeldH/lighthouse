package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	auth      atomic.Value // string
	tag       string
	sums      string // SHA256SUMS body; "" = computed from newBody
	assetCode int
	newBody   string
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
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/CorneeldH/lighthouse/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.auth.Store(r.Header.Get("Authorization"))
		_, _ = fmt.Fprintf(w, `{"tag_name": %q}`, f.tag)
	})
	base := "/CorneeldH/lighthouse/releases/download/" + tag + "/"
	mux.HandleFunc(base+asset, func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.downloads.Add(1)
		if f.assetCode != 200 {
			http.Error(w, "boom", f.assetCode)
			return
		}
		_, _ = fmt.Fprint(w, f.newBody)
	})
	mux.HandleFunc(base+"SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.downloads.Add(1)
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
	st, _ := os.Stat(f.target)
	if st.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", st.Mode())
	}
	f.assertOnlyTarget(t)
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
	for _, tag := range []string{"v0.12.0-rc.1", "0.12.0", ""} {
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
