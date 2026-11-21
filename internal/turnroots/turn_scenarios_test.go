package turnroots

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// promptTS is the timestamp of the prompt in the transcripts that need no real clock.
const promptTS = "2026-10-25T10:00:00.000Z"

// scenario is a project checkout with one linked worktree, both with their realpaths.
type scenario struct {
	dir  string
	main string
	wt   string
}

// scenarioEnv blanks what a controller run or a git hook leaks into the spawned git.
func scenarioEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"LIGHTHOUSE_CONTROLLER_RUN", "LIGHTHOUSE_REPO_ROOT", "LIGHTHOUSE_TOML", "CLAUDE_PROJECT_DIR"} {
		t.Setenv(name, "")
	}
	for _, name := range []string{"GIT_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

// commitAt commits everything in repo as one commit made at when (reflog entry included).
func commitAt(t *testing.T, repo string, when time.Time) {
	t.Helper()
	stamp := when.UTC().Format(time.RFC3339)
	t.Setenv("GIT_AUTHOR_DATE", stamp)
	t.Setenv("GIT_COMMITTER_DATE", stamp)
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-qm", "c")
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// newScenario is a repo with one commit from an hour ago and a worktree `wt` below it.
func newScenario(t *testing.T) scenario {
	t.Helper()
	scenarioEnv(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main")
	git(t, dir, "init", "-q", "-b", "main", main)
	writeFile(t, filepath.Join(main, "README.md"), "# repo\n")
	commitAt(t, main, time.Now().Add(-time.Hour))
	wt := filepath.Join(main, ".claude", "worktrees", "wt")
	git(t, main, "worktree", "add", "-q", wt, "-b", "wt")
	return scenario{dir: dir, main: main, wt: wt}
}

type entry = map[string]any

func promptEntry(cwd, ts string) entry {
	return entry{"type": "user", "timestamp": ts, "cwd": cwd, "message": entry{"role": "user", "content": "go"}}
}

func toolEntry(name string, input entry, cwd string) entry {
	return entry{
		"type": "assistant", "timestamp": "2026-10-25T10:00:01.000Z", "cwd": cwd,
		"message": entry{"role": "assistant", "content": []any{
			entry{"type": "tool_use", "id": "toolu_1", "name": name, "input": input},
		}},
	}
}

func resultEntry(cwd string) entry {
	return entry{
		"type": "user", "timestamp": "2026-10-25T10:00:02.000Z", "cwd": cwd,
		"message": entry{"role": "user", "content": []any{
			entry{"type": "tool_result", "tool_use_id": "toolu_1", "content": "ok"},
		}},
	}
}

func editEntry(path, cwd string) entry { return toolEntry("Edit", entry{"file_path": path}, cwd) }

func bashEntry(command, cwd string) entry { return toolEntry("Bash", entry{"command": command}, cwd) }

// compactSummary is what Claude Code writes for an auto-compaction: a user entry with a string
// content and no isMeta.
func compactSummary(cwd string) entry {
	return entry{
		"type": "user", "isCompactSummary": true, "isVisibleInTranscriptOnly": true,
		"timestamp": "2026-10-25T10:00:05.000Z", "cwd": cwd,
		"message": entry{"role": "user", "content": "This session is being continued from a previous conversation."},
	}
}

func writeTranscript(t *testing.T, path string, entries []entry) string {
	t.Helper()
	var lines []string
	for _, e := range entries {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(b))
	}
	writeFile(t, path, strings.Join(lines, "\n")+"\n")
	return path
}

func stopPayload(t *testing.T, cwd, transcript string) []byte {
	t.Helper()
	payload, err := json.Marshal(entry{"session_id": "s1", "transcript_path": transcript, "cwd": cwd, "hook_event_name": "Stop"})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

// Classification: io/filesystem. At risk: which checkouts a Stop hook checks. TestResolveRoots
// fails when an edit names the cwd instead of its own checkout, a Bash call loses its git target or
// the cwd of its own entry, a turn takes the edits of the previous turn, another repository counts,
// a compaction summary restarts the turn, or a subagent edit from before the prompt counts.
func TestResolveRoots(t *testing.T) {
	tests := []struct {
		name    string
		entries func(t *testing.T, s scenario) []entry
		want    func(s scenario) []string
	}{
		{
			"Edit in a worktree names the worktree, not the cwd",
			func(t *testing.T, s scenario) []entry {
				return []entry{
					promptEntry(s.main, promptTS), editEntry(filepath.Join(s.wt, "src", "new.py"), s.main), resultEntry(s.main),
					// A meta entry (hook context, a caveat) is no prompt.
					{"type": "user", "isMeta": true, "timestamp": promptTS, "cwd": s.main, "message": entry{"role": "user", "content": "go"}},
				}
			},
			func(s scenario) []string { return []string{s.wt} },
		},
		{
			"Write in a worktree",
			func(t *testing.T, s scenario) []entry {
				return []entry{promptEntry(s.main, promptTS), toolEntry("Write", entry{"file_path": filepath.Join(s.wt, "a.py")}, s.main)}
			},
			func(s scenario) []string { return []string{s.wt} },
		},
		{
			"MultiEdit in a worktree",
			func(t *testing.T, s scenario) []entry {
				return []entry{promptEntry(s.main, promptTS), toolEntry("MultiEdit", entry{"file_path": filepath.Join(s.wt, "a.py")}, s.main)}
			},
			func(s scenario) []string { return []string{s.wt} },
		},
		{
			"NotebookEdit names notebook_path",
			func(t *testing.T, s scenario) []entry {
				return []entry{promptEntry(s.main, promptTS), toolEntry("NotebookEdit", entry{"notebook_path": filepath.Join(s.wt, "a.ipynb")}, s.main)}
			},
			func(s scenario) []string { return []string{s.wt} },
		},
		{
			"Bash names its git target and its cwd",
			func(t *testing.T, s scenario) []entry {
				return []entry{promptEntry(s.main, promptTS), bashEntry("git -C "+s.wt+" commit -qm x", s.main), resultEntry(s.main)}
			},
			func(s scenario) []string { return []string{s.main, s.wt} },
		},
		{
			"Bash uses the cwd of its own entry",
			func(t *testing.T, s scenario) []entry {
				return []entry{promptEntry(s.main, promptTS), bashEntry("ls", s.wt), resultEntry(s.wt)}
			},
			func(s scenario) []string { return []string{s.wt} },
		},
		{
			"a turn without writes falls back to the cwd",
			func(t *testing.T, s scenario) []entry {
				return []entry{
					promptEntry(s.main, "2026-10-25T09:00:00.000Z"),
					editEntry(filepath.Join(s.wt, "a.py"), s.main),
					resultEntry(s.main),
					// The edit above belongs to the previous turn.
					promptEntry(s.main, promptTS),
					toolEntry("Read", entry{"file_path": filepath.Join(s.wt, "a.py")}, s.main),
				}
			},
			func(s scenario) []string { return []string{s.main} },
		},
		{
			"a path in another repository does not count",
			func(t *testing.T, s scenario) []entry {
				other := filepath.Join(s.dir, "other")
				git(t, s.dir, "init", "-q", "-b", "main", other)
				return []entry{promptEntry(s.main, promptTS), editEntry(filepath.Join(other, "a.py"), s.main)}
			},
			func(s scenario) []string { return []string{s.main} },
		},
		{
			"a compaction summary keeps the roots before it",
			func(t *testing.T, s scenario) []entry {
				return []entry{
					promptEntry(s.main, promptTS), editEntry(filepath.Join(s.wt, "a.go"), s.main),
					compactSummary(s.main), editEntry(filepath.Join(s.main, "b.go"), s.main),
				}
			},
			func(s scenario) []string { return []string{s.main, s.wt} },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newScenario(t)
			transcript := writeTranscript(t, filepath.Join(s.dir, "t", "s1.jsonl"), tt.entries(t, s))
			got, err := Resolve(stopPayload(t, s.main, transcript), s.main)
			if err != nil {
				t.Fatal(err)
			}
			if want := tt.want(s); strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("Resolve = %q, want %q", got, want)
			}
		})
	}
}

// Classification: io/filesystem. At risk: a subagent's edit decides which checkout a Stop hook
// checks. Fails when an edit from before the prompt counts, or one since the prompt does not.
func TestResolveSubagentEditSinceThePrompt(t *testing.T) {
	s := newScenario(t)
	old := filepath.Join(s.main, ".claude", "worktrees", "old")
	git(t, s.main, "worktree", "add", "-q", old, "-b", "old")
	transcript := filepath.Join(s.dir, "t", "s1.jsonl")
	writeTranscript(t, filepath.Join(s.dir, "t", "s1", "subagents", "agent-a1.jsonl"), []entry{
		{"type": "assistant", "isSidechain": true, "timestamp": "2026-10-25T09:59:59.000Z", "cwd": old,
			"message": toolEntry("Edit", entry{"file_path": filepath.Join(old, "a.py")}, old)["message"]},
		{"type": "assistant", "isSidechain": true, "timestamp": "2026-10-25T10:00:03.000Z", "cwd": s.wt,
			"message": toolEntry("Edit", entry{"file_path": filepath.Join(s.wt, "a.py")}, s.wt)["message"]},
	})
	writeTranscript(t, transcript, []entry{
		promptEntry(s.main, promptTS), toolEntry("Agent", entry{"prompt": "do it"}, s.main), resultEntry(s.main),
	})
	got, err := Resolve(stopPayload(t, s.main, transcript), s.main)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != s.wt {
		t.Errorf("Resolve = %q, want [%q]", got, s.wt)
	}
}

// Classification: cli-output. At risk: the reason a Stop hook prints when it cannot tell the
// checkout. Fails when the reason loses the path or the cause it names.
func TestResolveCannotTell(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, s scenario) ([]byte, string)
		want func(s scenario) string
	}{
		{
			"a missing transcript",
			func(t *testing.T, s scenario) ([]byte, string) {
				return stopPayload(t, s.main, filepath.Join(s.dir, "gone.jsonl")), s.main
			},
			func(s scenario) string {
				return "the transcript " + filepath.Join(s.dir, "gone.jsonl") + " cannot be read (No such file or directory)."
			},
		},
		{
			"a transcript without a prompt",
			func(t *testing.T, s scenario) ([]byte, string) {
				path := writeTranscript(t, filepath.Join(s.dir, "t", "s1.jsonl"), []entry{
					toolEntry("Edit", entry{"file_path": filepath.Join(s.wt, "a.py")}, s.main), resultEntry(s.main),
				})
				return stopPayload(t, s.main, path), s.main
			},
			func(s scenario) string {
				return "no prompt in the transcript " + filepath.Join(s.dir, "t", "s1.jsonl") + "."
			},
		},
		{
			"a cwd outside a repository",
			func(t *testing.T, s scenario) ([]byte, string) {
				outside := filepath.Join(s.dir, "plain")
				if err := os.Mkdir(outside, 0o700); err != nil {
					t.Fatal(err)
				}
				path := writeTranscript(t, filepath.Join(s.dir, "t", "s1.jsonl"), []entry{promptEntry(outside, promptTS)})
				return stopPayload(t, outside, path), s.main
			},
			func(s scenario) string { return "not inside a git repository (" + filepath.Join(s.dir, "plain") + ")." },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.HasPrefix(tt.name, "a missing") {
				t.Skip("the errno text differs on windows")
			}
			s := newScenario(t)
			payload, project := tt.run(t, s)
			_, err := Resolve(payload, project)
			var u *Undetermined
			if !errors.As(err, &u) || u.Msg != tt.want(s) {
				t.Errorf("Resolve error %v, want Undetermined %q", err, tt.want(s))
			}
		})
	}
}

// Classification: io/filesystem. At risk: whether a Stop gate runs on a checkout the turn changed.
// TestClassify fails when a path in the domain no longer counts as touched (Edit, a dirty file
// since the prompt, a deletion, a commit in the turn), when something from before the prompt does,
// when a worktree's path counts for the main checkout, or when git failing no longer means touched.
func TestClassify(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		setup    func(t *testing.T, s scenario, start time.Time)
		entries  func(s scenario, start string) []entry
		want     []string // "<touched|untouched> main|wt"
	}{
		{
			name: "an edit in the domain is touched", patterns: []string{"*.go"},
			entries: func(s scenario, start string) []entry {
				return []entry{promptEntry(s.main, start), editEntry(filepath.Join(s.wt, "internal", "x", "a.go"), s.main)}
			},
			want: []string{"touched wt"},
		},
		{
			name: "a compaction summary does not restart the turn", patterns: []string{"*.go"},
			entries: func(s scenario, start string) []entry {
				return []entry{
					promptEntry(s.main, start), editEntry(filepath.Join(s.wt, "a.go"), s.main),
					compactSummary(s.main), editEntry(filepath.Join(s.wt, "README.md"), s.main),
				}
			},
			want: []string{"touched wt"},
		},
		{
			name: "an edit outside the domain is untouched", patterns: []string{"*.go"},
			entries: func(s scenario, start string) []entry {
				return []entry{promptEntry(s.main, start), editEntry(filepath.Join(s.wt, "README.md"), s.main)}
			},
			want: []string{"untouched wt"},
		},
		{
			name: "a Bash write since the prompt is touched", patterns: []string{"*.go"},
			setup: func(t *testing.T, s scenario, _ time.Time) {
				writeFile(t, filepath.Join(s.main, "a.go"), "package a\n")
			},
			entries: func(s scenario, start string) []entry {
				return []entry{promptEntry(s.main, start), bashEntry("ls", s.main)}
			},
			want: []string{"touched main"},
		},
		{
			name: "a dirty path from an earlier turn is untouched", patterns: []string{"*.go"},
			setup: func(t *testing.T, s scenario, start time.Time) {
				old := filepath.Join(s.main, "a.go")
				writeFile(t, old, "package a\n")
				earlier := start.Add(-time.Hour)
				if err := os.Chtimes(old, earlier, earlier); err != nil {
					t.Fatal(err)
				}
			},
			entries: func(s scenario, start string) []entry {
				return []entry{promptEntry(s.main, start), editEntry(filepath.Join(s.main, "README.md"), s.main)}
			},
			want: []string{"untouched main"},
		},
		{
			name: "a deleted path counts", patterns: []string{"*.go"},
			setup: func(t *testing.T, s scenario, start time.Time) {
				writeFile(t, filepath.Join(s.main, "a.go"), "package a\n")
				commitAt(t, s.main, start.Add(-time.Hour))
				if err := os.Remove(filepath.Join(s.main, "a.go")); err != nil {
					t.Fatal(err)
				}
			},
			entries: func(s scenario, start string) []entry {
				return []entry{promptEntry(s.main, start), bashEntry("ls", s.main)}
			},
			want: []string{"touched main"},
		},
		{
			name: "a commit in the turn is touched", patterns: []string{"*.go"},
			setup: func(t *testing.T, s scenario, start time.Time) {
				writeFile(t, filepath.Join(s.main, "a.go"), "package a\n")
				commitAt(t, s.main, start.Add(5*time.Second))
			},
			entries: func(s scenario, start string) []entry {
				return []entry{promptEntry(s.main, start), bashEntry("git commit -qm x", s.main)}
			},
			want: []string{"touched main"},
		},
		{
			name: "a commit before the turn is untouched", patterns: []string{"*.go"},
			setup: func(t *testing.T, s scenario, start time.Time) {
				writeFile(t, filepath.Join(s.main, "a.go"), "package a\n")
				commitAt(t, s.main, start.Add(-time.Minute))
			},
			entries: func(s scenario, start string) []entry {
				return []entry{promptEntry(s.main, start), bashEntry("ls", s.main)}
			},
			want: []string{"untouched main"},
		},
		{
			name: "a turn without writes judges the cwd", patterns: []string{"*.go"},
			entries: func(s scenario, start string) []entry { return []entry{promptEntry(s.main, start)} },
			want:    []string{"untouched main"},
		},
		{
			name: "a prompt without a timestamp is touched", patterns: []string{"*.go"},
			entries: func(s scenario, start string) []entry {
				prompt := promptEntry(s.main, start)
				delete(prompt, "timestamp")
				return []entry{prompt, bashEntry("ls", s.main)}
			},
			want: []string{"touched main"},
		},
		{
			name: "each root is judged", patterns: []string{"*.go"},
			entries: func(s scenario, start string) []entry {
				return []entry{promptEntry(s.main, start), editEntry(filepath.Join(s.wt, "a.go"), s.main), bashEntry("ls", s.main)}
			},
			want: []string{"untouched main", "touched wt"},
		},
		{
			name: "an edit in a worktree does not touch the main checkout", patterns: []string{"*.go"},
			entries: func(s scenario, start string) []entry {
				return []entry{
					promptEntry(s.main, start), editEntry(filepath.Join(s.wt, "a.go"), s.main),
					editEntry(filepath.Join(s.main, "README.md"), s.main),
				}
			},
			want: []string{"untouched main", "touched wt"},
		},
		{
			name: "a root git cannot read is touched", patterns: []string{"*.go"},
			setup: func(t *testing.T, s scenario, _ time.Time) {
				// A broken index: rev-parse still answers, git status fails.
				writeFile(t, filepath.Join(s.main, ".git", "index"), "garbage")
			},
			entries: func(s scenario, start string) []entry {
				return []entry{promptEntry(s.main, start), bashEntry("ls", s.main)}
			},
			want: []string{"touched main"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newScenario(t)
			start := time.Now().Add(-10 * time.Second)
			if tt.setup != nil {
				tt.setup(t, s, start)
			}
			transcript := writeTranscript(t, filepath.Join(s.dir, "t", "s1.jsonl"),
				tt.entries(s, start.UTC().Format("2006-01-02T15:04:05.000Z")))
			got, err := Classify(stopPayload(t, s.main, transcript), s.main, tt.patterns)
			if err != nil {
				t.Fatal(err)
			}
			var gotLines []string
			for _, v := range got {
				state, name := "untouched", "main"
				if v.Touched {
					state = "touched"
				}
				if v.Root == s.wt {
					name = "wt"
				} else if v.Root != s.main {
					name = v.Root
				}
				gotLines = append(gotLines, state+" "+name)
			}
			if strings.Join(gotLines, ",") != strings.Join(tt.want, ",") {
				t.Errorf("Classify = %q, want %q", gotLines, tt.want)
			}
		})
	}
}

// Classification: io/filesystem. At risk: a repository without a commit yet. Fails when an unborn
// HEAD is read as a git failure, which makes every root touched.
func TestClassifyUnbornRepository(t *testing.T) {
	tests := []struct {
		name, file string
		want       bool
	}{
		{"an edit outside the domain is untouched", "README.md", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scenarioEnv(t)
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			repo := filepath.Join(dir, "unborn")
			git(t, dir, "init", "-q", "-b", "main", repo)
			start := time.Now().Add(-10 * time.Second).UTC().Format("2006-01-02T15:04:05.000Z")
			transcript := writeTranscript(t, filepath.Join(dir, "t", "s1.jsonl"),
				[]entry{promptEntry(repo, start), editEntry(filepath.Join(repo, tt.file), repo)})
			got, err := Classify(stopPayload(t, repo, transcript), repo, []string{"*.go"})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Root != repo || got[0].Touched != tt.want {
				t.Errorf("Classify = %v, want %v for %s", got, tt.want, repo)
			}
		})
	}
}
