package importcheck

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseTidyDiff(t *testing.T) {
	hdr := "diff current/go.mod tidied/go.mod\n--- current/go.mod\n+++ tidied/go.mod\n"
	tests := []struct {
		name        string
		diff        string
		wantShifted []string
		wantMissing []string
	}{
		{"indirect becomes direct", hdr + "@@ -5,4 +5,4 @@\n require (\n-\tgithub.com/x/y v1.0.0 // indirect\n+\tgithub.com/x/y v1.0.0\n )\n",
			[]string{"github.com/x/y"}, nil},
		{"single-line require form", hdr + "@@ -3 +3 @@\n-require github.com/x/y v1.0.0 // indirect\n+require github.com/x/y v1.0.0\n",
			[]string{"github.com/x/y"}, nil},
		{"missing direct require", hdr + "@@ -5,3 +5,4 @@\n require (\n+\tgithub.com/x/y v1.0.0\n )\n",
			nil, []string{"github.com/x/y"}},
		{"superfluous require", hdr + "@@ -5,4 +5,3 @@\n require (\n-\tgithub.com/x/y v1.0.0\n )\n", nil, nil},
		{"missing indirect require", hdr + "@@ -5,3 +5,4 @@\n require (\n+\tgithub.com/x/y v1.0.0 // indirect\n )\n", nil, nil},
		{"direct became indirect", hdr + "@@ -5,3 +5,3 @@\n-\tgithub.com/x/y v1.0.0\n+\tgithub.com/x/y v1.0.0 // indirect\n", nil, nil},
		{"version change only", hdr + "@@ -5,3 +5,3 @@\n-\tgithub.com/x/y v1.0.0\n+\tgithub.com/x/y v1.1.0\n", nil, nil},
		{"go.sum hunk", "diff current/go.sum tidied/go.sum\n--- current/go.sum\n+++ tidied/go.sum\n@@ -1,1 +1,2 @@\n+github.com/x/y v1.0.0 h1:abc=\n+github.com/x/y v1.0.0/go.mod h1:def=\n", nil, nil},
		{"replace and go lines", hdr + "@@ -1,5 +1,5 @@\n-go 1.22\n+go 1.23\n-replace github.com/x/y v1.0.0 => ../y\n+\tgithub.com/x/z v1.0.0 => ../z\n+toolchain go1.23.1\n", nil, nil},
		{"exclude block", hdr + "@@ -1,5 +1,6 @@\n exclude (\n+\tgithub.com/x/y v1.0.0\n )\n", nil, nil},
		{"shifted across hunks with other version", hdr + "@@ -5,3 +5,2 @@\n-\tgithub.com/x/y v1.0.0 // indirect\n@@ -20,2 +19,3 @@\n+\tgithub.com/x/y v1.2.0\n",
			[]string{"github.com/x/y"}, nil},
		{"shifted and missing together", hdr + "@@ -5,3 +5,4 @@\n-\tgithub.com/a/a v1.0.0 // indirect\n+\tgithub.com/a/a v1.0.0\n+\tgithub.com/b/b v1.0.0\n",
			[]string{"github.com/a/a"}, []string{"github.com/b/b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shifted, missing := parseTidyDiff(tt.diff)
			if !reflect.DeepEqual(shifted, tt.wantShifted) || !reflect.DeepEqual(missing, tt.wantMissing) {
				t.Errorf("shifted=%v missing=%v, want %v and %v", shifted, missing, tt.wantShifted, tt.wantMissing)
			}
		})
	}
}

type goExec struct {
	calls  int
	stdout string
	stderr string
	code   int
	err    error
}

func (g *goExec) exec(dir string, argv []string, extraEnv []string) ([]byte, []byte, int, error) {
	g.calls++
	return []byte(g.stdout), []byte(g.stderr), g.code, g.err
}

func goEnv(root, version string, g *goExec) Env {
	return Env{
		Root:      root,
		Exec:      g.exec,
		LookPath:  func(string) (string, error) { return "/usr/bin/go", nil },
		GoVersion: func() (string, error) { return version, nil },
	}
}

func TestGoCheckerUncheckedCases(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		lookErr   error
		g         goExec
		want      string
		wantCalls int
	}{
		{"go older than 1.23", "go1.22.5", nil, goExec{}, "go1.22.5", 0},
		{"exit 1 without diff", "go1.23.1", nil, goExec{code: 1, stderr: "module not in cache"}, "module not in cache", 1},
		{"exit 2", "go1.24.0", nil, goExec{code: 2, stderr: "go: boom"}, "go: boom", 1},
		{"exit 0 with a diff", "go1.24.0", nil, goExec{stdout: "--- x"}, "exit 0", 1},
		{"go not on PATH", "go1.24.0", errors.New("not found"), goExec{}, "not found", 0},
		{"go does not start", "go1.24.0", nil, goExec{err: errors.New("exec failed")}, "exec failed", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := goEnv(t.TempDir(), tt.version, &tt.g)
			env.LookPath = func(string) (string, error) { return "", tt.lookErr }
			findings, err := GoChecker{}.Check(Project{Lang: "go", Dir: "svc"}, env)
			var u UncheckedError
			if !errors.As(err, &u) {
				t.Fatalf("err = %v (findings %v), want UncheckedError", err, findings)
			}
			if !strings.Contains(string(u), tt.want) || !strings.Contains(string(u), "svc") {
				t.Errorf("reason %q lacks %q or the project dir", u, tt.want)
			}
			if tt.g.calls != tt.wantCalls {
				t.Errorf("exec calls = %d, want %d", tt.g.calls, tt.wantCalls)
			}
		})
	}
}

func TestGoCheckerCleanIsExit0EmptyStdout(t *testing.T) {
	g := &goExec{}
	findings, err := GoChecker{}.Check(Project{Lang: "go", Dir: "."}, goEnv(t.TempDir(), "go1.24.0", g))
	if err != nil || len(findings) != 0 {
		t.Fatalf("findings=%v err=%v, want clean", findings, err)
	}
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const tidyHdr = "diff current/go.mod tidied/go.mod\n--- current/go.mod\n+++ tidied/go.mod\n@@ -1 +1 @@\n"

func TestGoCheckerLocatesImports(t *testing.T) {
	gomod := "module example.com/app\n\ngo 1.23\n\nrequire (\n\tgithub.com/x/y v1.0.0 // indirect\n)\n"
	files := map[string]string{
		"svc/go.mod":            gomod,
		"svc/a.go":              "package a\n\nimport (\n\t\"fmt\"\n\t\"github.com/x/y/sub\"\n)\n",
		"svc/pkg/b_test.go":     "package pkg\n\nimport \"github.com/x/y\"\n",
		"svc/nested/go.mod":     "module example.com/nested\n",
		"svc/nested/c.go":       "package c\n\nimport \"github.com/x/y\"\n",
		"svc/vendor/v/v.go":     "package v\n\nimport \"github.com/x/y\"\n",
		"svc/other.go":          "package a\n\nimport \"github.com/x/yy\"\n",
		"svc/broken.go":         "this is not go",
		"svc/pkg/other_arch.go": "//go:build windows\n\npackage pkg\n\nimport _ \"github.com/x/y\"\n",
	}
	t.Run("shifted lists every importing file", func(t *testing.T) {
		root := t.TempDir()
		writeTree(t, root, files)
		g := &goExec{code: 1, stdout: tidyHdr + "-\tgithub.com/x/y v1.0.0 // indirect\n+\tgithub.com/x/y v1.0.0\n"}
		got, err := GoChecker{}.Check(Project{Lang: "go", Dir: "svc"}, goEnv(root, "go1.24.0", g))
		if err != nil {
			t.Fatal(err)
		}
		want := []Finding{
			{File: "svc/a.go", Line: 5, ID: "indirect-import", Import: "github.com/x/y/sub"},
			{File: "svc/pkg/b_test.go", Line: 3, ID: "indirect-import", Import: "github.com/x/y"},
			{File: "svc/pkg/other_arch.go", Line: 5, ID: "indirect-import", Import: "github.com/x/y"},
		}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %d findings", got, len(want))
		}
		for i, w := range want {
			if got[i].File != w.File || got[i].Line != w.Line || got[i].ID != w.ID || got[i].Import != w.Import || got[i].Reason == "" {
				t.Errorf("finding %d = %+v, want %+v with a reason", i, got[i], w)
			}
		}
	})
	t.Run("shifted without import points at go.mod", func(t *testing.T) {
		root := t.TempDir()
		writeTree(t, root, map[string]string{"svc/go.mod": gomod, "svc/a.go": "package a\n"})
		g := &goExec{code: 1, stdout: tidyHdr + "-\tgithub.com/x/y v1.0.0 // indirect\n+\tgithub.com/x/y v1.0.0\n"}
		got, err := GoChecker{}.Check(Project{Lang: "go", Dir: "svc"}, goEnv(root, "go1.24.0", g))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].File != "svc/go.mod" || got[0].Line != 6 || got[0].ID != "indirect-import" {
			t.Fatalf("got %+v, want one indirect-import at svc/go.mod:6", got)
		}
	})
	t.Run("missing require with import", func(t *testing.T) {
		root := t.TempDir()
		writeTree(t, root, map[string]string{"go.mod": "module example.com/app\n", "a.go": "package a\n\nimport \"github.com/q/r\"\n"})
		g := &goExec{code: 1, stdout: tidyHdr + "+\tgithub.com/q/r v1.0.0\n"}
		got, err := GoChecker{}.Check(Project{Lang: "go", Dir: "."}, goEnv(root, "go1.24.0", g))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].File != "a.go" || got[0].Line != 3 || got[0].ID != "missing-require" {
			t.Fatalf("got %+v, want one missing-require at a.go:3", got)
		}
	})
	t.Run("missing require without import is drift", func(t *testing.T) {
		root := t.TempDir()
		writeTree(t, root, map[string]string{"go.mod": "module example.com/app\n", "a.go": "package a\n"})
		g := &goExec{code: 1, stdout: tidyHdr + "+\tgithub.com/q/r v1.0.0\n"}
		got, err := GoChecker{}.Check(Project{Lang: "go", Dir: "."}, goEnv(root, "go1.24.0", g))
		if err != nil || len(got) != 0 {
			t.Fatalf("got %v err %v, want no findings", got, err)
		}
	})
	t.Run("only non-blocking drift", func(t *testing.T) {
		g := &goExec{code: 1, stdout: tidyHdr + "-\tgithub.com/q/r v1.0.0\n"}
		got, err := GoChecker{}.Check(Project{Lang: "go", Dir: "."}, goEnv(t.TempDir(), "go1.24.0", g))
		if err != nil || len(got) != 0 {
			t.Fatalf("got %v err %v, want no findings", got, err)
		}
	})
}
