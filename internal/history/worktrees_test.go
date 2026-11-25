package history

import (
	"strings"
	"testing"
)

func worktreesFinding(env Env, recs ...Record) Finding {
	d := newWorktrees(env)
	feed(d, recs...)
	return d.Finish(env)
}

func TestWorktrees_SyncFolder(t *testing.T) {
	f := worktreesFinding(Env{},
		bashRec("s", "b", `C:\Users\u\r`, `git worktree add C:\Users\u\OneDrive\wt -b x`),
		bashRec("s", "b", `C:\Users\u\OneDrive\wt`, "ls"),
	)
	sf, _ := f.Measured["sync_folders"].(map[string]int)
	if f.Status != "found" || sf["OneDrive"] < 1 || f.Proposal.Kind != "advice" {
		t.Errorf("%+v", f)
	}
	if f.Measured["adds"] != 1 || f.Measured["distinct_worktrees"] != 1 {
		t.Errorf("measured %v", f.Measured)
	}
}

func TestWorktrees_SyncVariants(t *testing.T) {
	for path, want := range map[string]string{
		`C:\Users\u\iCloudDrive\wt`:                "iCloud",
		`"/Users/u/Library/Mobile Documents/x/wt"`: "iCloud",
		"/home/u/Dropbox/wt":                       "Dropbox",
		`C:\Users\u\OneDrive - Contoso\wt`:         "OneDrive",
		"'/home/u/Dropbox (Personal)/wt'":          "Dropbox",
	} {
		f := worktreesFinding(Env{}, bashRec("s", "b", "/r", "git worktree add "+path))
		sf, _ := f.Measured["sync_folders"].(map[string]int)
		if sf[want] != 1 {
			t.Errorf("%s: %v", path, f.Measured)
		}
	}
}

func TestWorktrees_ClaudeFolderOnly(t *testing.T) {
	f := worktreesFinding(Env{},
		bashRec("s", "b", "/home/u/r", "git worktree add /home/u/.claude/worktrees/a"),
		bashRec("s", "b", "/home/u/r", "git -C /home/u/r worktree add -b nb /home/u/.claude/worktrees/b main"),
		bashRec("s", "b", "/home/u/.claude/worktrees/c", "ls"),
	)
	pp := f.Measured["path_patterns"].(map[string]int)
	if f.Status != "found" || pp["~/.claude/worktrees"] != 2 || f.Measured["distinct_worktrees"] != 3 {
		t.Errorf("%+v", f.Measured)
	}
	if _, ok := f.Measured["sync_folders"]; ok {
		t.Errorf("sync map present: %v", f.Measured)
	}
}

func TestWorktrees_RepoSibling(t *testing.T) {
	env := Env{RepoRoot: "/home/u/code/r"}
	f := worktreesFinding(env,
		bashRec("s", "b", "/home/u/code/r", "git worktree add ../r-wt"),
		bashRec("s", "b", "/home/u/code/r", "git worktree add /home/u/code/r-two"),
		bashRec("s", "b", "/home/u/code/r", "git worktree add /elsewhere/x"),
	)
	pp := f.Measured["path_patterns"].(map[string]int)
	if pp["repo-sibling"] != 2 || f.Measured["adds"] != 3 {
		t.Errorf("%v", f.Measured)
	}
}

func TestWorktrees_None(t *testing.T) {
	f := worktreesFinding(Env{}, bashRec("s", "b", "/home/u/r", "git status"), Record{Type: "user"})
	if f.Status != "none" || f.Key != "worktrees" || f.Measured["adds"] != 0 {
		t.Errorf("%+v", f)
	}
}

func TestWorktrees_NoLeak(t *testing.T) {
	f := worktreesFinding(Env{RepoRoot: `C:\Users\secretuser\code\proj`},
		bashRec("s", "b", `C:\Users\secretuser\code\proj`, `git worktree add C:\Users\secretuser\OneDrive\hiddenseg\wt`),
		bashRec("s", "b", `C:\Users\secretuser\OneDrive\hiddenseg\wt`, "ls"),
		bashRec("s", "b", `C:\Users\secretuser\code\proj`, `git worktree add C:\Users\secretuser\code\proj-wt`),
	)
	s := strings.ToLower(toJSON(t, f))
	for _, bad := range []string{"secretuser", "hiddenseg", "users", "proj", "code"} {
		if strings.Contains(s, bad) {
			t.Errorf("leak %q: %s", bad, s)
		}
	}
}
