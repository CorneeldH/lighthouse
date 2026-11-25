package automemory

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScan(t *testing.T) {
	cfg := t.TempDir()
	p := filepath.Join(cfg, "projects")
	writeFile(t, filepath.Join(p, "D--x-repo", "memory", "feedback_a.md"),
		"---\nname: fa\ntype: feedback\n---\nbody\n")
	writeFile(t, filepath.Join(p, "D--x-repo", "memory", "proj.md"),
		"---\nname: pj\nmetadata:\n  type: project\n---\nbody\n")
	writeFile(t, filepath.Join(p, "D--x-repo", "memory", "MEMORY.md"), "- index\n")
	writeFile(t, filepath.Join(p, "d--x-repo", "memory", "ref_host.md"),
		"---\nname: rh\ntype: reference\n---\nsee /srv/data for it\n")
	writeFile(t, filepath.Join(p, "d--x-repo", "memory", "ref_doc.md"),
		"---\nname: rd\ntype: reference\n---\nsee D:\\x\\repo\\docs and nothing else\n")
	writeFile(t, filepath.Join(p, "d--x-repo", "memory", "user.md"),
		"---\nname: u\ntype: user\n---\nbody\n")
	writeFile(t, filepath.Join(p, "d--x-repo", "memory", "raw.md"), "no frontmatter\n")
	writeFile(t, filepath.Join(p, "D--x-repo-other", "memory", "x.md"), "x\n")
	if err := os.MkdirAll(filepath.Join(p, "D--x-repo2"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Scan(cfg, `D:\x\repo`)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Dirs) != 2 {
		t.Fatalf("Dirs = %v, want 2", res.Dirs)
	}
	type want struct{ name, typ, dest, reason string }
	wants := []want{
		{"", "", "drop", "index"},
		{"fa", "feedback", "instruction", "type feedback"},
		{"pj", "project", "issue", "type project"},
		{"", "", "undecided", "no type"},
		{"rd", "reference", "docs", "type reference"},
		{"rh", "reference", "host", "type reference"},
		{"u", "user", "drop", "type user"},
	}
	if len(res.Files) != len(wants) {
		t.Fatalf("Files = %+v, want %d", res.Files, len(wants))
	}
	for i, w := range wants {
		f := res.Files[i]
		if f.Name != w.name || f.Type != w.typ || f.Destination != w.dest || f.Reason != w.reason {
			t.Errorf("file %d (%s) = %+v, want %+v", i, f.Path, f, w)
		}
		if f.Path == "" || f.Dir == "" {
			t.Errorf("file %d lacks Path or Dir: %+v", i, f)
		}
	}
}

func TestScanMissingProjects(t *testing.T) {
	res, err := Scan(t.TempDir(), "/x/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Dirs) != 0 || len(res.Files) != 0 {
		t.Fatalf("want empty result, got %+v", res)
	}
}

func TestMainCheckoutFromWorktree(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(root, "main")
	wt := filepath.Join(root, "wt")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = main
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "init")
	run("worktree", "add", "-q", wt, "-b", "side")

	got, err := MainCheckout(wt)
	if err != nil {
		t.Fatal(err)
	}
	if got != main {
		t.Fatalf("MainCheckout = %q, want %q", got, main)
	}
}
