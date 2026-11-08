package gate

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoPackagesFixtures(t *testing.T) {
	data, err := os.ReadFile("../../tests/pack/fixtures/gate_go/select.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name    string
		Listing []map[string]json.RawMessage
		Files   []string
		Want    []string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			var listing []GoPackage
			for _, entry := range c.Listing {
				var rel string
				_ = json.Unmarshal(entry["rel"], &rel)
				var pkg GoPackage
				raw, _ := json.Marshal(entry)
				if err := json.Unmarshal(raw, &pkg); err != nil {
					t.Fatal(err)
				}
				pkg.Dir = filepath.Join("/repo", rel)
				pkg.ImportPath = strings.TrimSuffix("example.com/m/"+rel, "/")
				listing = append(listing, pkg)
			}
			got := GoPackages(c.Files, listing, "/repo")
			if strings.Join(got, "|") != strings.Join(c.Want, "|") {
				t.Errorf("GoPackages = %q, want %q", got, c.Want)
			}
		})
	}
}

// goModule is a module example.com/m with packages a and b.
func goModule(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	dir := t.TempDir()
	write := func(name, body string) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/m\n\ngo 1.21\n")
	write("a/a.go", "package a\n")
	write("b/b.go", "package b\n")
	return dir
}

func runGo(t *testing.T, dir, files string, args ...string) (rc int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	rc = Go(args, Env{Dir: dir, FilesPath: files, Stdout: &out, Stderr: &errOut})
	return rc, out.String(), errOut.String()
}

func filesList(t *testing.T, dir string, lines ...string) string {
	t.Helper()
	path := filepath.Join(dir, "files.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGoWithoutFilesRunsAll(t *testing.T) {
	rc, out, _ := runGo(t, t.TempDir(), "", "--", "echo")
	if rc != 0 || out != "./...\n" {
		t.Errorf("rc=%d out=%q, want 0 and ./...", rc, out)
	}
}

func TestGoTouchedPackageIsSelected(t *testing.T) {
	dir := goModule(t)
	rc, out, errOut := runGo(t, dir, filesList(t, dir, "a/a.go"), "--", "echo")
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errOut)
	}
	if !strings.Contains(out, "gate-go: example.com/m/a\n") || !strings.Contains(out, "example.com/m/a\n") {
		t.Errorf("stdout = %q", out)
	}
	if strings.Contains(out, "example.com/m/b") {
		t.Errorf("untouched package b selected: %q", out)
	}
}

func TestGoNothingTouchedIsSkipped(t *testing.T) {
	dir := goModule(t)
	rc, out, _ := runGo(t, dir, filesList(t, dir, "README.md"), "--", "false")
	if rc != 0 || out != "gate-go: skipped — the commit touches no Go package\n" {
		t.Errorf("rc=%d out=%q", rc, out)
	}
}

func TestGoCommandExitCodePassesThrough(t *testing.T) {
	dir := goModule(t)
	rc, _, _ := runGo(t, dir, filesList(t, dir, "a/a.go"), "--", "sh", "-c", "exit 7")
	if rc != 7 {
		t.Errorf("rc = %d, want 7", rc)
	}
}

func TestGoSignalledCommandIsRed(t *testing.T) {
	rc, _, _ := runGo(t, t.TempDir(), "", "--", "sh", "-c", "kill -TERM $$")
	if rc != 143 {
		t.Errorf("rc = %d, want 143", rc)
	}
}

func TestGoListFailureKeepsItsStderr(t *testing.T) {
	dir := goModule(t)
	if err := os.WriteFile(filepath.Join(dir, "a", "a.go"), []byte("package a\nimport (\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, out, errOut := runGo(t, dir, filesList(t, dir, "a/a.go"), "--", "echo")
	if rc != Unchecked {
		t.Errorf("rc = %d, want 3", rc)
	}
	for _, want := range []string{"gate-go: COULD NOT CHECK — go list failed", "a.go"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr %q lacks %q", errOut, want)
		}
	}
	if strings.Contains(out, "gate-go: example.com") {
		t.Errorf("stdout = %q", out)
	}
}

func TestGoUsageErrorIsUnchecked(t *testing.T) {
	for _, args := range [][]string{{"echo"}, {"--"}, {}} {
		rc, _, errOut := runGo(t, t.TempDir(), "", args...)
		if rc != Unchecked || !strings.Contains(errOut, "gate-go: COULD NOT CHECK — usage: lighthouse gate go -- CMD...") {
			t.Errorf("args %q: rc=%d stderr=%q", args, rc, errOut)
		}
	}
}

func TestGoMissingFilesListIsUnchecked(t *testing.T) {
	dir := goModule(t)
	rc, _, errOut := runGo(t, dir, filepath.Join(dir, "nope.txt"), "--", "echo")
	if rc != Unchecked || !strings.Contains(errOut, "cannot read the changed files") {
		t.Errorf("rc=%d stderr=%q", rc, errOut)
	}
}

func TestGoMissingGoIsUnchecked(t *testing.T) {
	dir := t.TempDir()
	files := filesList(t, dir, "a/a.go")
	t.Setenv("PATH", t.TempDir())
	rc, _, errOut := runGo(t, dir, files, "--", "echo")
	if rc != Unchecked || !strings.Contains(errOut, "go not on PATH") {
		t.Errorf("rc=%d stderr=%q", rc, errOut)
	}
	if strings.Contains(errOut, "changed files") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestGoCommandThatCannotStartIsUnchecked(t *testing.T) {
	dir := goModule(t)
	for name, files := range map[string]string{"with files": filesList(t, dir, "a/a.go"), "without files": ""} {
		t.Run(name, func(t *testing.T) {
			rc, _, errOut := runGo(t, dir, files, "--", "lighthouse-no-such-cmd")
			if rc != Unchecked || !strings.Contains(errOut, "lighthouse-no-such-cmd cannot start") {
				t.Errorf("rc=%d stderr=%q", rc, errOut)
			}
		})
	}
}

func TestGoSymlinkedCwdSelectsPackage(t *testing.T) {
	dir := goModule(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skip("no symlinks: ", err)
	}
	rc, out, errOut := runGo(t, link, filesList(t, dir, "a/a.go"), "--", "echo")
	if rc != 0 || !strings.Contains(out, "gate-go: example.com/m/a") {
		t.Errorf("rc=%d stdout=%q stderr=%q", rc, out, errOut)
	}
}

func TestGoPackagesResolvesSymlinkedRoot(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks: ", err)
	}
	listing := []GoPackage{{ImportPath: "example.com/m/a", Dir: filepath.Join(real, "a"), GoFiles: []string{"a.go"}}}
	got := GoPackages([]string{"a/a.go"}, listing, link)
	if len(got) != 1 || got[0] != "example.com/m/a" {
		t.Errorf("GoPackages = %q, want example.com/m/a", got)
	}
}
