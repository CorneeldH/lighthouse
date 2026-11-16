package lane

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/pins"
)

// isolateGit keeps the user's and the system's git config out of every git call of a test,
// including the ones the code under test makes, and keeps init away from ~/.config.
func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("LIGHTHOUSE_HOST_TOML", filepath.Join(t.TempDir(), "host.toml"))
}

// gitT runs git as a test author and returns its trimmed stdout.
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, stderr)
	}
	return strings.TrimSpace(string(out))
}

func writeT(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// upstream is a local bare repository standing in for github.com/o/r.
type upstream struct {
	t          *testing.T
	root, work string
}

func newUpstream(t *testing.T) *upstream {
	t.Helper()
	u := &upstream{t: t, root: t.TempDir(), work: t.TempDir()}
	bare := filepath.Join(u.root, "o", "r")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, bare, "init", "-q", "--bare", "-b", "main")
	gitT(t, bare, "config", "uploadpack.allowFilter", "true")
	gitT(t, bare, "config", "uploadpack.allowAnySHA1InWant", "true")
	gitT(t, u.work, "init", "-q", "-b", "main")
	gitT(t, u.work, "remote", "add", "origin", bare)
	return u
}

// commit writes files (an empty content removes the file), commits and pushes; it returns the
// commit sha.
func (u *upstream) commit(files map[string]string) string {
	u.t.Helper()
	for rel, content := range files {
		if content == "" {
			gitT(u.t, u.work, "rm", "-q", rel)
			continue
		}
		writeT(u.t, u.work, rel, content)
	}
	gitT(u.t, u.work, "add", "-A")
	gitT(u.t, u.work, "commit", "-q", "-m", "upstream change")
	gitT(u.t, u.work, "push", "-q", "origin", "HEAD")
	return gitT(u.t, u.work, "rev-parse", "HEAD")
}

func (u *upstream) clone(t *testing.T) pins.Clone {
	t.Helper()
	src := pins.NewGitSourceAt(func(repo string) string {
		return "file://" + filepath.Join(u.root, filepath.FromSlash(repo))
	})
	c, err := src.Clone(context.Background(), "o/r")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func skillPin(key, commit, path string, files ...string) pins.SourcePin {
	return pins.SourcePin{Key: key, Repo: "o/r", Commit: commit, Path: path, Files: files}
}

func filePin(key, commit, path string) pins.SourcePin {
	return pins.SourcePin{Key: key, Repo: "o/r", Commit: commit, Path: path, Files: []string{key}}
}

// localSkill is a SKILL.md as lighthouse keeps it: own frontmatter, the upstream body below.
func localSkill(commit, body string) string {
	return "---\nname: lighthouse-demo\nmetadata:\n  origin: adopted\n  upstream: https://github.com/o/r\n" +
		"  upstream-commit: " + commit + "\n  upstream-path: skills/demo\n---\n" + body
}

const (
	upSkillBase = "---\nname: demo\n---\n\nintro\none\ntwo\nthree\nfour\nfive\n"
	upSkillHead = "---\nname: demo\n---\n\nintro\none\ntwo\nthree\nfour\nfive upstream\n"
	bodyBase    = "\nintro\none\ntwo\nthree\nfour\nfive\n"
)

func strs(files map[string][]byte) map[string]string {
	out := map[string]string{}
	for k, v := range files {
		out[k] = string(v)
	}
	return out
}

// Fails when a clean upstream change is not merged with the local change, when a directory is
// not merged per file, or when a file pin is not merged at all.
func TestMergeSource_cleanFileAndDir(t *testing.T) {
	isolateGit(t)
	u := newUpstream(t)
	pin := u.commit(map[string]string{
		"skills/demo/SKILL.md": upSkillBase,
		"skills/demo/ref.md":   "a\nb\nc\n",
		"skills/demo/same.md":  "unchanged\n",
		"lib/tool.py":          "x = 1\ny = 2\nz = 3\nv = 4\nw = 5\n",
	})
	u.commit(map[string]string{
		"skills/demo/SKILL.md": upSkillHead,
		"skills/demo/ref.md":   "a\nb\nc upstream\n",
		"lib/tool.py":          "x = 10\ny = 2\nz = 3\nv = 4\nw = 5\n",
	})
	c := u.clone(t)
	root := t.TempDir()
	skillMD := "pack/core/skills/demo/SKILL.md"
	writeT(t, root, skillMD, localSkill(pin, "\nintro local\none\ntwo\nthree\nfour\nfive\n"))
	writeT(t, root, "pack/core/skills/demo/ref.md", "a\nb\nc\n")
	writeT(t, root, "pack/core/skills/demo/same.md", "unchanged\n")
	writeT(t, root, "pack/core/scripts/tool.py", "x = 1\ny = 2\nz = 3\nv = 4\nw = 5 local\n")

	got, err := MergeSource(context.Background(), root, skillPin("core/skills/demo", pin, "skills/demo", skillMD), c)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		skillMD:                        localSkill(pin, "\nintro local\none\ntwo\nthree\nfour\nfive upstream\n"),
		"pack/core/skills/demo/ref.md": "a\nb\nc upstream\n",
	}
	if !got.Clean || len(got.Conflicts) != 0 || got.TouchesWorkflows || len(got.Removed) != 0 {
		t.Errorf("skill: clean %v conflicts %v workflows %v removed %v", got.Clean, got.Conflicts, got.TouchesWorkflows, got.Removed)
	}
	if !reflect.DeepEqual(strs(got.Files), want) {
		t.Errorf("skill files:\n got %q\nwant %q", strs(got.Files), want)
	}

	got, err = MergeSource(context.Background(), root, filePin("pack/core/scripts/tool.py", pin, "lib/tool.py"), c)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Clean {
		t.Errorf("file: not clean: %v", got.Conflicts)
	}
	if w := map[string]string{"pack/core/scripts/tool.py": "x = 10\ny = 2\nz = 3\nv = 4\nw = 5 local\n"}; !reflect.DeepEqual(strs(got.Files), w) {
		t.Errorf("file:\n got %q\nwant %q", strs(got.Files), w)
	}
}

// Fails when overlapping local and upstream changes (the usual outcome of an extended skill)
// come back as a clean merge or carry conflict markers into Files.
func TestMergeSource_extendedConflictIsConflict(t *testing.T) {
	isolateGit(t)
	u := newUpstream(t)
	pin := u.commit(map[string]string{"skills/demo/SKILL.md": upSkillBase, "skills/demo/ref.md": "a\n"})
	u.commit(map[string]string{"skills/demo/SKILL.md": upSkillHead, "skills/demo/ref.md": "a upstream\n"})
	root := t.TempDir()
	skillMD := "pack/core/skills/demo/SKILL.md"
	writeT(t, root, skillMD, localSkill(pin, "\nintro\none\ntwo\nthree\nfour\nfive extended\n"))
	writeT(t, root, "pack/core/skills/demo/ref.md", "a\n")

	got, err := MergeSource(context.Background(), root, skillPin("core/skills/demo", pin, "skills/demo", skillMD), u.clone(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.Clean || !reflect.DeepEqual(got.Conflicts, []string{skillMD}) {
		t.Fatalf("clean %v conflicts %v, want conflict on %s", got.Clean, got.Conflicts, skillMD)
	}
	if _, ok := got.Files[skillMD]; ok {
		t.Errorf("conflicting file in Files: %q", got.Files[skillMD])
	}
}

// Fails when a new upstream file is left out, or when a file that vanished upstream is
// dropped silently instead of being a conflict.
func TestMergeSource_newAndRemovedUpstreamFile(t *testing.T) {
	isolateGit(t)
	u := newUpstream(t)
	pin := u.commit(map[string]string{"skills/demo/SKILL.md": upSkillBase, "skills/demo/old.md": "old\n"})
	u.commit(map[string]string{"skills/demo/old.md": "", "skills/demo/new.md": "new\n"})
	c := u.clone(t)
	skillMD := "pack/core/skills/demo/SKILL.md"
	p := skillPin("core/skills/demo", pin, "skills/demo", skillMD)

	t.Run("removed file still local", func(t *testing.T) {
		root := t.TempDir()
		writeT(t, root, skillMD, localSkill(pin, bodyBase))
		writeT(t, root, "pack/core/skills/demo/old.md", "old\n")
		got, err := MergeSource(context.Background(), root, p, c)
		if err != nil {
			t.Fatal(err)
		}
		old := "pack/core/skills/demo/old.md"
		if got.Clean || !reflect.DeepEqual(got.Conflicts, []string{old}) || !reflect.DeepEqual(got.Removed, []string{old}) {
			t.Errorf("clean %v conflicts %v removed %v, want conflict and removal of %s", got.Clean, got.Conflicts, got.Removed, old)
		}
		if w := map[string]string{"pack/core/skills/demo/new.md": "new\n"}; !reflect.DeepEqual(strs(got.Files), w) {
			t.Errorf("files %q, want %q", strs(got.Files), w)
		}
	})
	t.Run("removed file already gone locally", func(t *testing.T) {
		root := t.TempDir()
		writeT(t, root, skillMD, localSkill(pin, bodyBase))
		got, err := MergeSource(context.Background(), root, p, c)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Clean || len(got.Removed) != 0 {
			t.Errorf("clean %v conflicts %v removed %v, want clean", got.Clean, got.Conflicts, got.Removed)
		}
		if w := map[string]string{"pack/core/skills/demo/new.md": "new\n"}; !reflect.DeepEqual(strs(got.Files), w) {
			t.Errorf("files %q, want %q", strs(got.Files), w)
		}
	})
}

// Fails when the upstream SKILL.md is merged into only one of the claude/ and agents/
// variants, or when one variant's own text leaks into the other.
func TestMergeSource_variantsBothMerged(t *testing.T) {
	isolateGit(t)
	u := newUpstream(t)
	pin := u.commit(map[string]string{"skills/demo/SKILL.md": upSkillBase})
	u.commit(map[string]string{"skills/demo/SKILL.md": upSkillHead})
	root := t.TempDir()
	claude := "pack/core/skills/demo/claude/SKILL.md"
	agents := "pack/core/skills/demo/agents/SKILL.md"
	writeT(t, root, claude, localSkill(pin, "\nintro claude\none\ntwo\nthree\nfour\nfive\n"))
	writeT(t, root, agents, localSkill(pin, "\nintro agents\none\ntwo\nthree\nfour\nfive\n"))

	got, err := MergeSource(context.Background(), root, skillPin("core/skills/demo", pin, "skills/demo", agents, claude), u.clone(t))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		claude: localSkill(pin, "\nintro claude\none\ntwo\nthree\nfour\nfive upstream\n"),
		agents: localSkill(pin, "\nintro agents\none\ntwo\nthree\nfour\nfive upstream\n"),
	}
	if !got.Clean || !reflect.DeepEqual(strs(got.Files), want) {
		t.Errorf("clean %v conflicts %v files:\n got %q\nwant %q", got.Clean, got.Conflicts, strs(got.Files), want)
	}
}

// Fails when a changed binary file is merged or copied instead of being a conflict, and when a
// file without a final newline comes out broken.
func TestMergeSource_binaryIsConflict(t *testing.T) {
	isolateGit(t)
	// git merge-file only looks for NUL in the first 8000 bytes; this one sits beyond them.
	late := strings.Repeat("x\n", 4500)
	u := newUpstream(t)
	pin := u.commit(map[string]string{
		"skills/demo/SKILL.md":  upSkillBase,
		"skills/demo/img.bin":   "PNG\x00\x01old",
		"skills/demo/late.bin":  "local\n" + late + "\x00old",
		"skills/demo/noeol.txt": "a\nb\nc\nd\ne",
	})
	u.commit(map[string]string{
		"skills/demo/img.bin":   "PNG\x00\x02new",
		"skills/demo/late.bin":  "local\n" + late + "\x00new",
		"skills/demo/new.bin":   "\x00\x00new",
		"skills/demo/noeol.txt": "a\nb\nc\nd\ne\nf",
	})
	root := t.TempDir()
	skillMD := "pack/core/skills/demo/SKILL.md"
	writeT(t, root, skillMD, localSkill(pin, bodyBase))
	writeT(t, root, "pack/core/skills/demo/img.bin", "PNG\x00\x01old")
	writeT(t, root, "pack/core/skills/demo/late.bin", "LOCAL\n"+late+"\x00old")
	writeT(t, root, "pack/core/skills/demo/noeol.txt", "a local\nb\nc\nd\ne")

	got, err := MergeSource(context.Background(), root, skillPin("core/skills/demo", pin, "skills/demo", skillMD), u.clone(t))
	if err != nil {
		t.Fatal(err)
	}
	wantConflicts := []string{"pack/core/skills/demo/img.bin", "pack/core/skills/demo/late.bin", "pack/core/skills/demo/new.bin"}
	if got.Clean || !reflect.DeepEqual(got.Conflicts, wantConflicts) {
		t.Errorf("clean %v conflicts %v, want %v", got.Clean, got.Conflicts, wantConflicts)
	}
	if w := map[string]string{"pack/core/skills/demo/noeol.txt": "a local\nb\nc\nd\ne\nf"}; !reflect.DeepEqual(strs(got.Files), w) {
		t.Errorf("files %q, want %q", strs(got.Files), w)
	}
}

// Fails when a merge result under .github/workflows/ is not flagged.
func TestMergeSource_workflowsPathIsFlagged(t *testing.T) {
	isolateGit(t)
	u := newUpstream(t)
	pin := u.commit(map[string]string{"ci.yml": "on: push\n"})
	u.commit(map[string]string{"ci.yml": "on: pull_request\n"})
	root := t.TempDir()
	writeT(t, root, ".github/workflows/ci.yml", "on: push\n")

	got, err := MergeSource(context.Background(), root, filePin(".github/workflows/ci.yml", pin, "ci.yml"), u.clone(t))
	if err != nil {
		t.Fatal(err)
	}
	if !got.TouchesWorkflows {
		t.Errorf("TouchesWorkflows false for %v", strs(got.Files))
	}
}

// commitTree commits a tree built with git mktree on top of the upstream main and moves main
// there; tree maps an entry name to "<mode> <type> <object>" or, for a subtree, to another map.
func (u *upstream) commitTree(t *testing.T, tree map[string]any) string {
	t.Helper()
	bare := filepath.Join(u.root, "o", "r")
	var mk func(m map[string]any) string
	mk = func(m map[string]any) string {
		var lines []string
		for name, v := range m {
			switch v := v.(type) {
			case string:
				lines = append(lines, v+"\t"+name)
			case map[string]any:
				lines = append(lines, "040000 tree "+mk(v)+"\t"+name)
			}
		}
		cmd := exec.Command("git", "mktree")
		cmd.Dir = bare
		cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("mktree %v: %v", lines, err)
		}
		return strings.TrimSpace(string(out))
	}
	sha := gitT(t, bare, "commit-tree", mk(tree), "-p", "main", "-m", "crafted")
	gitT(t, bare, "update-ref", "refs/heads/main", sha)
	return sha
}

func (u *upstream) blob(t *testing.T, content string) string {
	t.Helper()
	cmd := exec.Command("git", "hash-object", "-w", "--stdin")
	cmd.Dir = filepath.Join(u.root, "o", "r")
	cmd.Stdin = strings.NewReader(content)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return "100644 blob " + strings.TrimSpace(string(out))
}

// Fails when an upstream tree entry named `..` maps to a local path outside the skill
// directory instead of failing the pin.
func TestMergeSource_dotDotTreeEntryFailsPin(t *testing.T) {
	isolateGit(t)
	u := newUpstream(t)
	pin := u.commit(map[string]string{"skills/demo/SKILL.md": upSkillBase})
	// skills/demo/../../../../../evil: path.Join of it under pack/core/skills/demo leaves the repo.
	escape := map[string]any{"evil": u.blob(t, "pwned\n")}
	for range 5 {
		escape = map[string]any{"..": escape}
	}
	demo := map[string]any{"SKILL.md": u.blob(t, upSkillHead), "..": escape[".."]}
	u.commitTree(t, map[string]any{"skills": map[string]any{"demo": demo}})
	root := t.TempDir()
	writeT(t, root, "pack/core/skills/demo/SKILL.md", localSkill(pin, bodyBase))

	got, err := MergeSource(context.Background(), root, skillPin("core/skills/demo", pin, "skills/demo", "pack/core/skills/demo/SKILL.md"), u.clone(t))
	if err == nil || !strings.Contains(err.Error(), "not a plain path") {
		t.Fatalf("err %v, files %v; want the pin to fail on the .. entry", err, strs(got.Files))
	}
}

// Fails when a changed symlink's target is merged as file content, an unchanged one conflicts,
// or a submodule fails the whole pin.
func TestMergeSource_symlinkAndSubmoduleAreConflicts(t *testing.T) {
	isolateGit(t)
	u := newUpstream(t)
	// An unchanged symlink is left alone.
	if err := os.MkdirAll(filepath.Join(u.work, "skills", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("SKILL.md", filepath.Join(u.work, "skills", "demo", "same")); err != nil {
		t.Fatal(err)
	}
	pin := u.commit(map[string]string{"skills/demo/SKILL.md": upSkillBase})
	link := u.blob(t, "../../etc/passwd")
	u.commitTree(t, map[string]any{"skills": map[string]any{"demo": map[string]any{
		"same":     "120000 blob " + gitT(t, u.work, "rev-parse", pin+":skills/demo/same"),
		"SKILL.md": u.blob(t, upSkillHead),
		"link":     "120000 blob " + strings.Fields(link)[2],
		"sub":      "160000 commit " + pin,
	}}})
	root := t.TempDir()
	skillMD := "pack/core/skills/demo/SKILL.md"
	writeT(t, root, skillMD, localSkill(pin, bodyBase))

	got, err := MergeSource(context.Background(), root, skillPin("core/skills/demo", pin, "skills/demo", skillMD), u.clone(t))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pack/core/skills/demo/link", "pack/core/skills/demo/sub"}; got.Clean || !reflect.DeepEqual(got.Conflicts, want) {
		t.Errorf("clean %v conflicts %v, want %v", got.Clean, got.Conflicts, want)
	}
	if w := map[string]string{skillMD: localSkill(pin, "\nintro\none\ntwo\nthree\nfour\nfive upstream\n")}; !reflect.DeepEqual(strs(got.Files), w) {
		t.Errorf("files %q, want %q", strs(got.Files), w)
	}
}
