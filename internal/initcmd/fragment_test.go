package initcmd

import (
	"io/fs"
	"os/exec"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/CorneeldH/lighthouse/pack"
)

func TestFragmentInInitDiff(t *testing.T) {
	root := newRepo(t)
	write(t, root, "pyproject.toml", "")
	r := run(t, root, "y\n", withPython)
	q := strings.Index(r.out, "Apply these changes?")
	if r.code != 0 || q < 0 || !strings.Contains(r.out[:q], "+[test_gate]\n") || !strings.Contains(r.out[:q], "+command = \"uv run pytest -q\"\n") {
		t.Fatalf("rc %d, fragment not in the diff:\n%s", r.code, r.out)
	}
}

func TestFragmentAppendsTable(t *testing.T) {
	root := initWithPython(t)
	want := "[project]\nlanguage = \"en\"\n\n[test_gate]\ncommand = \"uv run pytest -q\"\nunchecked_exit_codes = [3, 4, 5]\n"
	if got := read(t, root, "lighthouse.toml"); got != want {
		t.Fatalf("lighthouse.toml = %q", got)
	}
}

func TestFragmentKeepsProjectKeys(t *testing.T) {
	root := newRepo(t)
	write(t, root, "pyproject.toml", "")
	write(t, root, "lighthouse.toml", "[test_gate]\ncommand = \"make test\"  # own\n")
	if r := run(t, root, "", all(yes, withPython)); r.code != 0 {
		t.Fatalf("rc %d\n%s", r.code, r.err)
	}
	got := read(t, root, "lighthouse.toml")
	if !strings.HasPrefix(got, "[test_gate]\ncommand = \"make test\"  # own\nunchecked_exit_codes = [3, 4, 5]\n") || strings.Count(got, "[test_gate]") != 1 {
		t.Fatalf("lighthouse.toml = %q", got)
	}
}

func TestFragmentStaysAfterDrop(t *testing.T) {
	root := initWithPython(t)
	if r := run(t, root, "", all(upgrade, yes, withPython, dropPack("python"))); r.code != 0 {
		t.Fatalf("rc %d\n%s", r.code, r.err)
	}
	if !strings.Contains(read(t, root, "lighthouse.toml"), "[test_gate]\ncommand = \"uv run pytest -q\"\n") {
		t.Fatalf("lighthouse.toml = %q", read(t, root, "lighthouse.toml"))
	}
}

func TestFragmentKeyInTwoPacks(t *testing.T) {
	root := newRepo(t)
	write(t, root, "pyproject.toml", "")
	before := snapshot(t, root)
	r := run(t, root, "", all(yes, withPython, func(o *Options) {
		m := o.Packs.(fstest.MapFS)
		m["tox/lighthouse.toml.fragment"] = file("[test_gate]\ncommand = \"tox\"\n")
		m["tox/lib/t.py"] = file("x = 1\n")
	}, addPack("tox")))
	if r.code != 1 || !strings.Contains(r.err, "test_gate.command comes from both package python and package tox") {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
	if !sameSnapshot(before, snapshot(t, root)) || strings.Contains(r.out, "Apply") {
		t.Fatalf("written or asked:\n%s", r.out)
	}
}

func TestFragmentKeyAlsoInCore(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "", all(yes, func(o *Options) {
		o.Packs.(fstest.MapFS)["nl/lighthouse.toml.fragment"] = file("[project]\nlanguage = \"nl\"\n")
	}, addPack("nl")))
	if r.code != 1 || !strings.Contains(r.err, "project.language comes from both package core and package nl") {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
}

func TestUpgradeRestoresFragmentKey(t *testing.T) {
	root := initWithPython(t)
	write(t, root, "lighthouse.toml", strings.Replace(read(t, root, "lighthouse.toml"), "unchecked_exit_codes = [3, 4, 5]\n", "", 1))
	r := run(t, root, "", all(upgrade, withPython))
	if r.code != 1 || !strings.Contains(r.out, "+unchecked_exit_codes = [3, 4, 5]\n") {
		t.Fatalf("rc %d out:\n%s", r.code, r.out)
	}
}

func TestDefaultLookupSeesFragment(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	root := newRepo(t)
	write(t, root, "pyproject.toml", "")
	r := run(t, root, "", all(yes, withPython, func(o *Options) {
		m := o.Packs.(fstest.MapFS)
		for _, f := range []string{"__init__.py", "config.py"} {
			b, err := fs.ReadFile(pack.Files, "core/lib/"+f)
			if err != nil {
				t.Fatal(err)
			}
			m["core/lib/"+f] = &fstest.MapFile{Data: b}
		}
		m["core/workflows/x.yml"] = file("cmd: @@test_gate.command@@\n")
	}))
	if r.code != 0 {
		t.Fatalf("rc %d\n%s", r.code, r.err)
	}
	if got := read(t, root, ".github/workflows/lighthouse-x.yml"); got != "# lighthouse: core@dev\ncmd: uv run pytest -q\n" {
		t.Fatalf("workflow = %q", got)
	}
}
