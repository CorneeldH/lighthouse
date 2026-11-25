package stubbin_test

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// allowed lists the test files whose "/usr/bin" or "/bin" is not a POSIX PATH the test depends
// on (an environment value that is only compared or passed along, or an argument of another
// tool). The key is the file path relative to the repo root; the value is the substring that
// identifies the line, with the reason after it.
var allowed = map[string]struct{ line, reason string }{
	"internal/cli/answer_classifier_test.go": {`"PATH": "/bin"`, "a fixture value of the classifier environment; no process looks up a program through it"},
	"internal/cli/pushworkflows_test.go":     {`"PATH": "/usr/bin"`, "an env-value fixture that is only compared, never used to start a program"},
	"internal/commitgate/gate_test.go":       {"/usr/bin:/bin", "the PATH value the gate passes on is compared as a string; no program is started with it"},
	"internal/dispatch/apply_test.go":        {`"PATH": "/usr/bin"`, "an env-value fixture that is only compared, never used to start a program"},
}

// Classification: guard over the source. It fails when a test builds a PATH from a literal
// "/usr/bin" or "/bin", which does not exist on Windows.
func TestNoHardcodedUnixPathInTests(t *testing.T) {
	root := repoRoot(t)
	var hits []string
	for _, dir := range packageDirs(t, root) {
		files, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
		for _, f := range files {
			if filepath.Base(f) == "guard_test.go" && filepath.Base(dir) == "stubbin" {
				continue
			}
			rel, _ := filepath.Rel(root, f)
			rel = filepath.ToSlash(rel)
			hits = append(hits, scan(t, f, rel)...)
		}
	}
	if len(hits) > 0 {
		t.Errorf("a test builds PATH from a literal /usr/bin or /bin; use stubbin or os.PathListSeparator, "+
			"or mark the line with `// stubbin-guard: system dirs needed (<why>)`:\n%s", strings.Join(hits, "\n"))
	}
}

func scan(t *testing.T, path, rel string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	prev := ""
	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		n++
		line := sc.Text()
		before := prev
		prev = line
		if !(strings.Contains(line, "PATH") || strings.Contains(line, "PathListSeparator")) {
			continue
		}
		if !(strings.Contains(line, `"/usr/bin"`) || strings.Contains(line, `"/bin"`) ||
			strings.Contains(line, `/usr/bin:`) || strings.Contains(line, `:/bin`)) {
			continue
		}
		const marker = "stubbin-guard: system dirs needed"
		if strings.Contains(line, marker) || strings.Contains(before, marker) {
			continue
		}
		if a, ok := allowed[rel]; ok && strings.Contains(line, a.line) {
			continue
		}
		out = append(out, rel+":"+strconv.Itoa(n)+": "+strings.TrimSpace(line))
	}
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-f", "{{.Module.Dir}}", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func packageDirs(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	var dirs []string
	for _, tags := range []string{"", "e2e,canary"} {
		args := []string{"list"}
		if tags != "" {
			args = append(args, "-tags", tags)
		}
		args = append(args, "-f", "{{.Dir}}", "./...")
		cmd := exec.Command("go", args...)
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go %v: %v", args, err)
		}
		for _, d := range strings.Fields(string(out)) {
			if !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		}
	}
	return dirs
}
