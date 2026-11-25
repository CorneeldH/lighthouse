package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func set(d ...string) map[string]bool {
	m := map[string]bool{"web": false, "pack-python": false, "go": false}
	for _, x := range d {
		m[x] = true
	}
	return m
}

func TestClassify(t *testing.T) {
	tests := []struct {
		paths []string
		want  map[string]bool
	}{
		{[]string{"web/src/x.tsx"}, set("web")},
		{[]string{"internal/a.go"}, set("go", "pack-python")},
		{[]string{"web/openapi.json"}, set("web", "go")},
		{[]string{"web/go.mod"}, set("web", "go")},
		{[]string{"docs/x.md"}, set()},
		{[]string{"changelog.d/x.md"}, set()},
		{[]string{"ci/literal-denylist.txt"}, set("go")},
		{[]string{"ci/literal-allowlist.txt"}, set("go")},
		{[]string{"ci/literal-denylist-go.txt"}, set("go")},
		{[]string{"ci/x.sh"}, set("go")},
		{[]string{"docsx/x.md"}, set("go")},
		{[]string{"docs/x.md", "internal/a.go"}, set("go", "pack-python")},
		{[]string{".github/workflows/ci.yml"}, set("web", "pack-python", "go")},
		{[]string{"go.mod"}, set("pack-python", "go")},
		{[]string{"go.sum"}, set("pack-python", "go")},
		{[]string{"ci/domains/main.go"}, set("web", "go")},
		{[]string{"ci/domains/domains.go"}, set("web", "go")},
		{[]string{"lighthouse.toml"}, set("web", "go")},
		{[]string{"webapp/x.ts"}, set("go")},
		{[]string{"tests/test_x.py"}, set()},
		{[]string{"README.md"}, set()},
		{[]string{"CHANGELOG.md"}, set()},
		{[]string{"CONTRIBUTING.md"}, set()},
		{[]string{"SECURITY.md"}, set()},
		{[]string{"CLA.md"}, set()},
		{[]string{"CONTRIBUTING.md", "internal/a.go"}, set("go", "pack-python")},
		{[]string{"AGENTS.md"}, set("go")},
		{[]string{"tests/test_x.py", "README.md"}, set()},
		{[]string{"tests/pack/fixtures/protocol/headers.json"}, set("go")},
		{[]string{"tests/pack/python/testdata/ruff-format-check/E722.py"}, set("go", "pack-python")},
		{[]string{"pack/core/README.md"}, set("go", "pack-python")},
		{[]string{"e2e/e2e_test.go"}, set("go", "pack-python")},
		{[]string{"pack/python/scripts/a.py"}, set("go", "pack-python")},
		{[]string{"tests/pack/python/t.py"}, set("pack-python")},
		{[]string{"pack/core/x"}, set("go", "pack-python")},
		{[]string{"pack/go/hooks/go-imports-gate.sh"}, set("go", "pack-python")},
		{[]string{"pack/node/hooks/node-imports-gate.sh"}, set("go", "pack-python")},
		{[]string{"cmd/lighthouse/main.go"}, set("go", "pack-python")},
		{[]string{"pyproject.toml"}, set("go", "pack-python")},
		{[]string{"uv.lock"}, set("go", "pack-python")},
		{[]string{"web/src/a.ts"}, set("web")},
		{[]string{"docs/a.md"}, set()},
		{[]string{"pack/shell/x"}, set("go")},
		{[]string{"pack/powershell/x"}, set("go")},
		{[]string{"ci/literal-grep.sh"}, set("go")},
		{[]string{"tests/pack/fixtures/a.json"}, set("go")},
		{[]string{"docs/install.md"}, set("go")},
		{[]string{"docs/other.md"}, set()},
		{[]string{"docs/getting-started.md"}, set("go")},
		{[]string{"docs/site/agents/install.md"}, set("go")},
		{[]string{"docs/site/install.html"}, set("go")},
		{[]string{"docs/site/index.html"}, set("go")},
		{[]string{"docs/site/download.js"}, set("go")},
		{[]string{"docs/site/other.html"}, set()},
		{[]string{"docs/developer/strategie.md"}, set("go")},
		{[]string{"docs/developer/uitgangspunten.md"}, set("go")},
		{[]string{"docs/developer/labels.md"}, set("go")},
		{[]string{"docs/developer/areas/core.md"}, set("go")},
		{[]string{"docs/developer/other.md"}, set()},
		{[]string{"docs/developer/areas-extra.md"}, set()},
		{[]string{"testsx/a.py"}, set("go")},
		{nil, set()},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.paths, ","), func(t *testing.T) {
			if got := Classify(tc.paths); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Classify(%q) = %v, want %v", tc.paths, got, tc.want)
			}
		})
	}
}

func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func TestRunCLI(t *testing.T) {
	tests := []struct{ name, stdin, want string }{
		{"two_paths", "a\x00b\x00", "web=false\npack-python=false\ngo=true\n"},
		{"go_paths", "internal/a.go\x00e2e/b_test.go\x00", "web=false\npack-python=true\ngo=true\n"},
		{"docs_only", "changelog.d/fix-labels.md\x00docs/developer/other.md\x00", "web=false\npack-python=false\ngo=false\n"},
		{"tests_only", "tests/test_site_pages.py\x00README.md\x00changelog.d/x.md\x00", "web=false\npack-python=false\ngo=false\n"},
		{"web_only", "web/x.tsx\x00", "web=true\npack-python=false\ngo=false\n"},
		{"empty", "", "web=false\npack-python=false\ngo=false\n"},
		{"only_nuls", "\x00\x00\x00", "web=false\npack-python=false\ngo=false\n"},
		{"non_utf8", "docs/\xff.md\x00", "web=false\npack-python=false\ngo=false\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errb := runCLI(t, tc.stdin, "classify")
			if code != 0 || out != tc.want || errb != "" {
				t.Errorf("code=%d out=%q err=%q, want 0 %q empty", code, out, errb, tc.want)
			}
		})
	}
}

func TestRunGithubOutputAppends(t *testing.T) {
	f := filepath.Join(t.TempDir(), "github_output")
	if err := os.WriteFile(f, []byte("earlier=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runCLI(t, "web/x.tsx\x00internal/y.go\x00", "classify", "--github-output", f)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	got, _ := os.ReadFile(f)
	want := "earlier=1\nweb=true\npack-python=true\ngo=true\n"
	if string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
	if out != "web=true\npack-python=true\ngo=true\n" {
		t.Errorf("stdout = %q", out)
	}
}

func TestRunGithubOutputCreates(t *testing.T) {
	f := filepath.Join(t.TempDir(), "new")
	if code, _, _ := runCLI(t, "", "classify", "--github-output", f); code != 0 {
		t.Fatalf("code=%d", code)
	}
	got, _ := os.ReadFile(f)
	if string(got) != "web=false\npack-python=false\ngo=false\n" {
		t.Errorf("file = %q", got)
	}
}

func TestRunGithubOutputUnwritable(t *testing.T) {
	f := filepath.Join(t.TempDir(), "missing-dir", "out")
	code, out, errb := runCLI(t, "a\x00", "classify", "--github-output", f)
	if code != 2 || out != "" || !strings.Contains(errb, "COULD NOT CHECK") {
		t.Errorf("code=%d out=%q err=%q", code, out, errb)
	}
}

func TestRunNoSubcommand(t *testing.T) {
	for _, args := range [][]string{nil, {"gate"}, {"classify", "extra"}, {"classify", "--bogus"}} {
		code, out, errb := runCLI(t, "web/x\x00", args...)
		if code != 2 || out != "" || errb == "" {
			t.Errorf("args %q: code=%d out=%q err=%q", args, code, out, errb)
		}
	}
}
