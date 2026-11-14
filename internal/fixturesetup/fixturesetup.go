// Package fixturesetup builds the directory tree of a shared-fixture `setup` (tests/pack/fixtures)
// and fills its placeholders. It mirrors build_fixture_setup and fill_fixture of
// tests/pack/_testkit.py, so the Python and the Go runner read one format.
//
// It is for tests only: it takes a *testing.T and fails it on any error.
package fixturesetup

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	realPlaceholder = regexp.MustCompile(`\{real:([^}]+)\}`)
	homePlaceholder = regexp.MustCompile(`\{home:([^}]+)\}`)
	revPlaceholder  = regexp.MustCompile(`\{rev:([^:}]+):([^}]+)\}`)
)

// emptyTree is the tree git hashes for an empty repository.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Fill replaces {root}, {real:<path>} (the realpath of {root}/<path>), {home:<name>} (the home
// directory of user <name>), {rev:<path>:<rev>} (`git rev-parse <rev>` in {root}/<path>) and
// {empty_tree} in value. Call it after the last step of the case. The keys of a map are the
// caller's to fill, as fill_fixture does.
func Fill(t *testing.T, value, root string) string {
	t.Helper()
	value = revPlaceholder.ReplaceAllStringFunc(value, func(m string) string {
		parts := revPlaceholder.FindStringSubmatch(m)
		out, err := exec.Command("git", "-C", filepath.Join(root, parts[1]), "rev-parse", parts[2]).Output()
		if err != nil {
			t.Fatalf("rev-parse %s in %s: %v", parts[2], parts[1], err)
		}
		return strings.TrimSpace(string(out))
	})
	value = strings.ReplaceAll(value, "{empty_tree}", emptyTree)
	value = realPlaceholder.ReplaceAllStringFunc(value, func(m string) string {
		path := filepath.Join(root, realPlaceholder.FindStringSubmatch(m)[1])
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatalf("realpath %s: %v", path, err)
		}
		return real
	})
	value = strings.ReplaceAll(value, "{root}", root)
	return homePlaceholder.ReplaceAllStringFunc(value, func(m string) string {
		u, err := user.Lookup(homePlaceholder.FindStringSubmatch(m)[1])
		if err != nil {
			t.Fatalf("lookup user: %v", err)
		}
		return u.HomeDir
	})
}

// Git runs git in dir with a fixed identity and fails t on an error.
func Git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func str(t *testing.T, step map[string]any, key string) string {
	t.Helper()
	s, ok := step[key].(string)
	if !ok {
		t.Fatalf("setup step %v: %q is not a string", step, key)
	}
	return s
}

// Build creates the tree of steps under root. A step is one of repo (+branch), dir, file (+text),
// symlink (+to), overwrite (+text), worktree (+of, +branch: a linked worktree of repo `of`) and
// origin (+url: remote origin of that repo), unborn (a repo on main without a commit), commit
// (+message: `add -A` and commit in that repo) and mv (+from, +to: `git mv` in that repo).
func Build(t *testing.T, root string, steps []map[string]any) {
	t.Helper()
	write := func(rel, text string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, step := range steps {
		switch {
		case step["repo"] != nil:
			name := str(t, step, "repo")
			dir := filepath.Join(root, name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			Git(t, dir, "init", "-q", "-b", str(t, step, "branch"))
			write(filepath.Join(name, "README.md"), "# repo\n")
			Git(t, dir, "add", "README.md")
			Git(t, dir, "commit", "-qm", "init")
		case step["dir"] != nil:
			if err := os.MkdirAll(filepath.Join(root, str(t, step, "dir")), 0o755); err != nil {
				t.Fatal(err)
			}
		case step["file"] != nil:
			write(str(t, step, "file"), str(t, step, "text"))
		case step["symlink"] != nil:
			if err := os.Symlink(filepath.Join(root, str(t, step, "to")), filepath.Join(root, str(t, step, "symlink"))); err != nil {
				t.Fatal(err)
			}
		case step["overwrite"] != nil:
			write(str(t, step, "overwrite"), str(t, step, "text"))
		case step["worktree"] != nil:
			Git(t, filepath.Join(root, str(t, step, "of")), "worktree", "add", "-q",
				filepath.Join(root, str(t, step, "worktree")), "-b", str(t, step, "branch"))
		case step["origin"] != nil:
			Git(t, filepath.Join(root, str(t, step, "origin")), "remote", "add", "origin", str(t, step, "url"))
		case step["unborn"] != nil:
			dir := filepath.Join(root, str(t, step, "unborn"))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			Git(t, dir, "init", "-q", "-b", "main")
		case step["commit"] != nil:
			dir := filepath.Join(root, str(t, step, "commit"))
			Git(t, dir, "add", "-A")
			Git(t, dir, "commit", "-qm", str(t, step, "message"))
		case step["mv"] != nil:
			Git(t, filepath.Join(root, str(t, step, "mv")), "mv", str(t, step, "from"), str(t, step, "to"))
		default:
			t.Fatalf("unknown setup step %v", step)
		}
	}
}
