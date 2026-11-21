package checkinstr

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// tree is a repo as files: path under the repo root -> content. A path that ends in "/" is an
// empty directory.
type tree map[string]string

// with is t plus the files of the others; a later one wins.
func (t tree) with(others ...tree) tree {
	merged := tree{}
	for _, m := range append([]tree{t}, others...) {
		for k, v := range m {
			merged[k] = v
		}
	}
	return merged
}

// emptySkillRoots is a repo with both skill roots present and empty.
func emptySkillRoots() tree {
	return tree{".claude/skills/": "", ".agents/skills/": ""}
}

// skillText is a minimal SKILL.md: name, description, optional metadata keys (each a
// "key: value" line) and the body after the frontmatter.
func skillText(name, description string, metadata []string, body string) string {
	text := "---\nname: " + name + "\ndescription: " + description + "\n"
	if len(metadata) > 0 {
		text += "metadata:\n"
		for _, m := range metadata {
			text += "  " + m + "\n"
		}
	}
	return text + "---\n\n" + body
}

// mirrored writes text as SKILL.md of skill dir in both skill roots.
func mirrored(dir, text string) tree {
	return tree{
		".claude/skills/" + dir + "/SKILL.md": text,
		".agents/skills/" + dir + "/SKILL.md": text,
	}
}

// runOptions are the knobs of one run besides the tree.
type runOptions struct {
	toml     string
	env      map[string]string // set after the defaults
	personal string            // "" = an existing, empty personal directory
}

// result is what one Run printed and returned. Paths are rewritten: {root} for the repo root,
// {personal} for the personal skills directory.
type result struct {
	code           int
	stdout, stderr string
	root, personal string
}

// check writes files under a fresh repo root and runs every check on it, with a lighthouse.toml
// outside the tree (so the config of the machine is never read) and the personal directory empty.
func check(t *testing.T, files tree, opts runOptions) result {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	personal := filepath.Join(base, "personal", "skills")
	if opts.personal != "" {
		personal = opts.personal
	} else if err := os.MkdirAll(personal, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range files {
		if strings.HasSuffix(path, "/") {
			if err := os.MkdirAll(filepath.Join(root, path), 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		writeFile(t, filepath.Join(root, path), content)
	}
	t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", "")
	toml := filepath.Join(base, "lighthouse.toml")
	writeFile(t, toml, opts.toml)
	t.Setenv("LIGHTHOUSE_TOML", toml)
	t.Setenv("LIGHTHOUSE_REPO_ROOT", root)
	keys := make([]string, 0, len(opts.env))
	for k := range opts.env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Setenv(k, opts.env[k])
	}
	var out, errOut bytes.Buffer
	code := Run(root, personal, &out, &errOut)
	return result{code: code, stdout: out.String(), stderr: errOut.String(), root: root, personal: personal}
}

// expand fills {root} and {personal} in s.
func (r result) expand(s string) string {
	return strings.NewReplacer("{root}", r.root, "{personal}", r.personal).Replace(s)
}

// verdict is what one case expects: the exit code, whole lines that must be in stdout, and whole
// lines that must not be.
type verdict struct {
	code   int
	lines  []string // each without its newline
	absent []string
}

// assertVerdict compares one run with v. A line is a whole line of stdout, not a substring.
func assertVerdict(t *testing.T, r result, v verdict) {
	t.Helper()
	if r.code != v.code {
		t.Errorf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", r.code, v.code, r.stdout, r.stderr)
	}
	have := map[string]bool{}
	for _, l := range strings.Split(r.stdout, "\n") {
		have[l] = true
	}
	for _, want := range v.lines {
		if !have[r.expand(want)] {
			t.Errorf("no line %q in stdout:\n%s", r.expand(want), r.stdout)
		}
	}
	for _, no := range v.absent {
		if have[r.expand(no)] {
			t.Errorf("unexpected line %q in stdout:\n%s", r.expand(no), r.stdout)
		}
	}
}

// noFAIL fails the test when stdout carries a FAIL line.
func noFAIL(t *testing.T, r result) {
	t.Helper()
	for _, l := range strings.Split(r.stdout, "\n") {
		if strings.HasPrefix(l, "FAIL") {
			t.Errorf("unexpected %q in stdout:\n%s", l, r.stdout)
		}
	}
}
