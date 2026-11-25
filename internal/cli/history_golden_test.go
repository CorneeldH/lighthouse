package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

var historySets = []struct{ name, level string }{
	{"second-user", "light"},
	{"chain", "standard"},
	{"chain-codex", "strict"},
}

const historyTestdata = "../history/testdata"

// copyTree copies the directory tree src into dst.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if e.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// fixtureRig is a rig whose repo has the origin acme/widget and whose projects directory is
// a copy of the fixture set.
func fixtureRig(t *testing.T, set string) (*histRig, string) {
	t.Helper()
	r := newHistRig(t)
	mrGit(t, r.repo, nil, "remote", "add", "origin", "https://github.com/Acme/Widget.git")
	projects := filepath.Join(r.home, ".claude", "projects")
	copyTree(t, filepath.Join(historyTestdata, set, "projects"), projects)
	return r, projects
}

func TestHistory_Golden(t *testing.T) {
	for _, s := range historySets {
		t.Run(s.name, func(t *testing.T) {
			r, _ := fixtureRig(t, s.name)
			code, out, errOut := r.run(r.repo)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			if got := histDecode(t, out)["level"].(map[string]any)["recommended"]; got != s.level {
				t.Errorf("level = %v, want %s", got, s.level)
			}
			golden := filepath.Join(historyTestdata, "golden", s.name+".json")
			if *update {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, []byte(out), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update)", err)
			}
			if out != string(want) {
				t.Errorf("output differs from %s (run with -update and review):\n%s", golden, out)
			}
		})
	}
}

func TestHistory_NoSecrets(t *testing.T) {
	for _, s := range historySets {
		for _, args := range [][]string{nil, {"--sources"}} {
			t.Run(fmt.Sprintf("%s%v", s.name, args), func(t *testing.T) {
				r, _ := fixtureRig(t, s.name)
				code, out, errOut := r.run(r.repo, args...)
				if code != 0 {
					t.Fatalf("exit %d: %s", code, errOut)
				}
				// The sources hold file paths, which carry the project directory name and so the
				// user name of the fixture; the report itself never does.
				bad := []string{"SECRET-", "alice"}
				if len(args) > 0 {
					bad = bad[:1]
				}
				for _, bad := range bad {
					if strings.Contains(out, bad) {
						t.Errorf("output holds %q:\n%s", bad, out)
					}
				}
			})
		}
	}
}

func TestHistory_Sources(t *testing.T) {
	r := newHistRig(t)
	mrGit(t, r.repo, nil, "remote", "add", "origin", "https://github.com/Acme/Widget.git")
	cwd := "/home/u/src/acme/widget"
	for f := 1; f <= 7; f++ {
		var lines []string
		for l := 1; l <= 12; l++ {
			lines = append(lines, histBashLine(cwd, "main", fmt.Sprintf("2026-11-%02dT10:%02d:00Z", f, l), "git push origin main"))
		}
		r.transcript("-p", fmt.Sprintf("s%d", f), lines...)
	}
	_, withoutOut, _ := r.run(r.repo)
	if f := histFinding(t, histDecode(t, withoutOut), "trunk"); f["sources"] != nil {
		t.Errorf("sources without the flag: %v", f["sources"])
	}
	code, out, errOut := r.run(r.repo, "--sources")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	f := histFinding(t, histDecode(t, out), "trunk")
	if f["status"] != "found" {
		t.Fatalf("status = %v", f["status"])
	}
	srcs := f["sources"].([]any)
	if len(srcs) != 5 {
		t.Fatalf("%d source files, want 5", len(srcs))
	}
	for i, s := range srcs {
		sm := s.(map[string]any)
		lines := sm["lines"].([]any)
		if len(lines) != 10 {
			t.Errorf("%v: %d lines, want 10", sm["file"], len(lines))
		}
		if lines[0].(float64) != 12 {
			t.Errorf("%v: first line %v, want most recent 12", sm["file"], lines[0])
		}
		if want := fmt.Sprintf("s%d.jsonl", 7-i); !strings.HasSuffix(sm["file"].(string), want) {
			t.Errorf("source %d is %v, want most recent first (%s)", i, sm["file"], want)
		}
	}
}

func TestHistory_Unreadable(t *testing.T) {
	r := newHistRig(t)
	mrGit(t, r.repo, nil, "remote", "add", "origin", "https://github.com/Acme/Widget.git")
	r.transcript("-p", "s1",
		histBashLine("/home/u/src/acme/widget", "feature", "2026-11-01T10:00:00Z", "go test ./..."),
		`{"type":"assistant", broken`)
	code, out, errOut := r.run(r.repo)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	m := histDecode(t, out)
	if m["skipped_lines"].(float64) != 1 || m["sessions"].(float64) != 1 {
		t.Errorf("skipped_lines %v, sessions %v; want 1 and 1", m["skipped_lines"], m["sessions"])
	}
}

type treeEntry struct {
	mode  fs.FileMode
	mtime int64
	sum   string
}

// treeState is path, mode, mtime and content hash of everything under the roots.
func treeState(t *testing.T, roots ...string) map[string]treeEntry {
	t.Helper()
	out := map[string]treeEntry{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := e.Info()
			if err != nil {
				return err
			}
			te := treeEntry{mode: info.Mode(), mtime: info.ModTime().UnixNano()}
			if info.Mode().IsRegular() {
				b, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				h := sha256.Sum256(b)
				te.sum = hex.EncodeToString(h[:])
			}
			out[p] = te
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestHistory_WritesNothing(t *testing.T) {
	r, _ := fixtureRig(t, "chain-codex")
	before := treeState(t, r.home, r.repo)
	time.Sleep(20 * time.Millisecond)
	for _, args := range [][]string{nil, {"--sources"}} {
		if code, _, errOut := r.run(r.repo, args...); code != 0 {
			t.Fatalf("exit %d: %s", code, errOut)
		}
	}
	after := treeState(t, r.home, r.repo)
	var diff []string
	for p, b := range before {
		if a, ok := after[p]; !ok || a != b {
			diff = append(diff, p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			diff = append(diff, p)
		}
	}
	sort.Strings(diff)
	if len(diff) > 0 {
		t.Errorf("the command changed the tree: %v", diff)
	}
}

func TestHistory_NoNetwork(t *testing.T) {
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddr := closed.Addr().String()
	_ = closed.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	conns := make(chan struct{}, 8)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns <- struct{}{}
			_ = c.Close()
		}
	}()
	t.Setenv("HTTP_PROXY", "http://"+closedAddr)
	t.Setenv("http_proxy", "http://"+closedAddr)
	t.Setenv("HTTPS_PROXY", "http://"+ln.Addr().String())
	t.Setenv("https_proxy", "http://"+ln.Addr().String())
	t.Setenv("ALL_PROXY", "http://"+ln.Addr().String())

	r, _ := fixtureRig(t, "chain-codex")
	code, out, errOut := r.run(r.repo, "--sources")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if _, err := io.WriteString(io.Discard, out); err != nil {
		t.Fatal(err)
	}
	select {
	case <-conns:
		t.Error("the command opened a network connection")
	case <-time.After(100 * time.Millisecond):
	}
}
