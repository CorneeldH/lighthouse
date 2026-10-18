package manifest

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/packs"
)

var sum = strings.Repeat("a", 64)

func sample() Manifest {
	return Manifest{
		Version:  "dev",
		Packages: []string{"core"},
		Units: []Entry{
			{Kind: packs.KindHook, Path: ".claude/settings.json", ID: "PreToolUse|Bash|x", Package: "core", Version: "dev", SHA256: sum},
			{Kind: packs.KindFile, Path: ".claude/hooks/lighthouse/g.sh", Package: "core", Version: "dev", SHA256: sum},
			{Kind: packs.KindBlock, Path: "AGENTS.md", ID: "core", Package: "core", Version: "dev", SHA256: sum},
		},
	}
}

func writeManifest(t *testing.T, root string, data []byte) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(Path))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRoundTrip(t *testing.T) {
	root := t.TempDir()
	m := sample()
	writeManifest(t, root, Encode(m))
	got, err := Read(root)
	if err != nil {
		t.Fatal(err)
	}
	want := m
	want.Units = []Entry{m.Units[1], m.Units[0], m.Units[2]} // sorted by (Path, ID)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if !strings.HasPrefix(string(Encode(m)), "# Managed by lighthouse init. Do not edit by hand.\n") {
		t.Fatalf("header missing:\n%s", Encode(m))
	}
}

func TestEncodeIsDeterministic(t *testing.T) {
	a := sample()
	b := sample()
	b.Units = []Entry{b.Units[2], b.Units[0], b.Units[1]}
	if string(Encode(a)) != string(Encode(b)) {
		t.Fatal("order of units changes the bytes")
	}
}

func TestReadMissing(t *testing.T) {
	if _, err := Read(t.TempDir()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v", err)
	}
}

func TestReadRejects(t *testing.T) {
	cases := map[string]string{
		"unknown kind":  "version = \"dev\"\npackages = [\"core\"]\n[[unit]]\nkind = \"dir\"\npath = \"x\"\npackage = \"core\"\nversion = \"dev\"\nsha256 = \"" + sum + "\"\n",
		"bad sha":       "version = \"dev\"\npackages = [\"core\"]\n[[unit]]\nkind = \"file\"\npath = \"x\"\npackage = \"core\"\nversion = \"dev\"\nsha256 = \"abc\"\n",
		"duplicate key": "version = \"dev\"\npackages = [\"core\"]\n" + strings.Repeat("[[unit]]\nkind = \"file\"\npath = \"x\"\npackage = \"core\"\nversion = \"dev\"\nsha256 = \""+sum+"\"\n", 2),
		"invalid toml":  "version = \n",
		"no path":       "version = \"dev\"\npackages = [\"core\"]\n[[unit]]\nkind = \"file\"\npackage = \"core\"\nversion = \"dev\"\nsha256 = \"" + sum + "\"\n",
		"unknown key":   "version = \"dev\"\npackages = [\"core\"]\nextra = 1\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeManifest(t, root, []byte(text))
			_, err := Read(root)
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), ".lighthouse/manifest.toml") {
				t.Fatalf("error does not name the file: %v", err)
			}
		})
	}
}

func TestLookup(t *testing.T) {
	m := sample()
	e, ok := m.Lookup(packs.Key{Kind: packs.KindBlock, Path: "AGENTS.md", ID: "core"})
	if !ok || e.Package != "core" {
		t.Fatalf("lookup = %+v %v", e, ok)
	}
	if _, ok := m.Lookup(packs.Key{Kind: packs.KindFile, Path: "AGENTS.md"}); ok {
		t.Fatal("kind ignored")
	}
}

func TestSum(t *testing.T) {
	if got := Sum([]byte("abc")); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("sum = %s", got)
	}
}

func unitText(kind, path, id string) string {
	s := "version = \"dev\"\npackages = [\"core\"]\n[[unit]]\nkind = \"" + kind + "\"\npath = \"" + path + "\"\n"
	if id != "" {
		s += "id = \"" + id + "\"\n"
	}
	return s + "package = \"core\"\nversion = \"dev\"\nsha256 = \"" + sum + "\"\n"
}

func TestReadRejectsPathsOutsideLighthouse(t *testing.T) {
	cases := map[string]string{
		"parent":          unitText("file", "../outside.txt", ""),
		"absolute":        unitText("file", "/etc/passwd", ""),
		"dotdot inside":   unitText("file", ".claude/hooks/lighthouse/../../../README.md", ""),
		"not clean":       unitText("file", ".claude/hooks/lighthouse//g.sh", ""),
		"project file":    unitText("file", "README.md", ""),
		"git config":      unitText("file", ".git/config", ""),
		"own hook dir":    unitText("file", ".claude/hooks/own.sh", ""),
		"block elsewhere": unitText("block", "README.md", "core"),
		"hook elsewhere":  unitText("hook", ".claude/settings.local.json", "x"),
		"perm elsewhere":  unitText("permission", ".claude/settings.local.json", "Bash(x)"),
		"backslash":       unitText("file", ".claude/hooks/lighthouse/..\\\\x", ""),
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeManifest(t, root, []byte(text))
			if _, err := Read(root); err == nil || !strings.Contains(err.Error(), "path") {
				t.Fatalf("err = %v", err)
			}
		})
	}
	for _, ok := range []string{
		unitText("file", ".claude/hooks/lighthouse/g.sh", ""),
		unitText("file", ".lighthouse/lib/lighthouse_lib/a.py", ""),
		unitText("file", ".lighthouse/scripts/s.py", ""),
		unitText("file", ".claude/agents/lighthouse-scout.md", ""),
		unitText("block", "AGENTS.md", "core"),
		unitText("hook", ".claude/settings.json", "PreToolUse|Bash|x"),
	} {
		root := t.TempDir()
		writeManifest(t, root, []byte(ok))
		if _, err := Read(root); err != nil {
			t.Errorf("valid unit rejected: %v\n%s", err, ok)
		}
	}
}

func TestReadAcceptsPermission(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, []byte(unitText("permission", ".claude/settings.json", "Bash(x:*)")))
	m, err := Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Lookup(packs.Key{Kind: packs.KindPermission, Path: ".claude/settings.json", ID: "Bash(x:*)"}); !ok {
		t.Fatal("permission not read")
	}
}

func TestReadRejectsPermissionWithoutID(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, []byte(unitText("permission", ".claude/settings.json", "")))
	if _, err := Read(root); err == nil {
		t.Fatal("no error")
	}
}
