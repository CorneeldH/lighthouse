//go:build canary

package importcheck

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// canaryCase is the canary.json of a fixture: what the turn touched, what the hook must answer
// and which fake packages the environment of the copy needs.
type canaryCase struct {
	Hook        string     `json:"hook"`
	Touch       string     `json:"touch"`
	Exit        int        `json:"exit"`
	ID          string     `json:"id"`
	Dists       []fakeDist `json:"dists"`
	NodeModules []string   `json:"node_modules"`
	GoDeps      []string   `json:"go_deps"`
}

// fakeDist is an installed Python distribution that holds only its metadata.
type fakeDist struct {
	Name     string   `json:"name"`
	Requires []string `json:"requires"`
}

func loadCanaryCase(t *testing.T, dir string) canaryCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "canary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c canaryCase
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("%s/canary.json: %v", dir, err)
	}
	return c
}

// needTool fails the test when the tool is missing and CI is set, else skips: a canary that
// skips on CI would read as green.
func needTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err == nil {
		return
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("%s is not on PATH, and the canary must run on CI", name)
	}
	t.Skipf("%s is not on PATH", name)
}

func mustRun(t *testing.T, dir string, env []string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

// copyFixture copies a fixture into dst, canary.json left out.
func copyFixture(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if rel == "canary.json" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeGoDeps makes each module a local directory in the copy and points a `replace` at it, so
// `go mod tidy -diff` resolves it offline.
func fakeGoDeps(t *testing.T, dir string, mods []string) {
	t.Helper()
	if len(mods) == 0 {
		return
	}
	var replaces strings.Builder
	for _, mod := range mods {
		rel := filepath.ToSlash(filepath.Join(".fakedep", filepath.Base(mod)))
		writeFile(t, filepath.Join(dir, rel, "go.mod"), "module "+mod+"\n\ngo 1.23\n")
		writeFile(t, filepath.Join(dir, rel, "dep.go"), "package "+filepath.Base(mod)+"\n\nfunc F() {}\n")
		fmt.Fprintf(&replaces, "\nreplace %s => ./%s\n", mod, rel)
	}
	f, err := os.OpenFile(filepath.Join(dir, "go.mod"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(replaces.String()); err != nil {
		t.Fatal(err)
	}
}

// fakePython builds the venv of the copy with `uv venv` and puts a fake .dist-info in it per
// distribution: a METADATA whose Requires-Dist lines make one the dependency of another, and a
// top_level.txt, which is what deptry reads to map an import to its distribution. Offline.
func fakePython(t *testing.T, dir string, dists []fakeDist) {
	t.Helper()
	needTool(t, "uv")
	mustRun(t, dir, nil, "uv", "venv", "--quiet", ".venv")
	matches, err := filepath.Glob(filepath.Join(dir, ".venv", "lib", "python*", "site-packages"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("site-packages of the venv: %v %v", matches, err)
	}
	for _, d := range dists {
		info := filepath.Join(matches[0], d.Name+"-1.0.dist-info")
		meta := "Metadata-Version: 2.1\nName: " + d.Name + "\nVersion: 1.0\n"
		for _, r := range d.Requires {
			meta += "Requires-Dist: " + r + "\n"
		}
		writeFile(t, filepath.Join(info, "METADATA"), meta)
		writeFile(t, filepath.Join(info, "top_level.txt"), d.Name+"\n")
	}
}

// fakeNodeModules makes a node_modules at the root of the copy that holds only a package.json
// per package, hoisted like an installed workspace.
func fakeNodeModules(t *testing.T, dir string, names []string) {
	t.Helper()
	needTool(t, "npx")
	for _, n := range names {
		writeFile(t, filepath.Join(dir, "node_modules", n, "package.json"),
			`{"name": "`+n+`", "version": "1.0.0"}`+"\n")
	}
}
