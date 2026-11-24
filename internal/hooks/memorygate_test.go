package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/hook"
	"github.com/CorneeldH/lighthouse/internal/levellog"
	"github.com/CorneeldH/lighthouse/internal/levels"
)

func memoryPayload(t *testing.T, tool, key, value, cwd string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"tool_name": tool, "cwd": cwd, "tool_input": map[string]string{key: value},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// red if MemoryGate lets a write into an auto-memory directory through, blocks a read, or
// misses a target given as `~`, a relative path or a Windows path.
func TestMemoryGate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	tmp := t.TempDir()
	mem := home + "/.claude/projects/p/memory/a.md"

	cases := []struct {
		name string
		raw  []byte
		code int
		want string
	}{
		{"write into memory", memoryPayload(t, "Write", "file_path", mem, tmp), 2, "memory-gate: auto-memory write blocked (" + mem + ")."},
		{"edit on a Windows path", memoryPayload(t, "Edit", "file_path", `D:\u\.claude\projects\D--r\memory\a.md`, tmp), 2, "memory-gate: auto-memory write blocked"},
		{"write elsewhere", memoryPayload(t, "Write", "file_path", tmp+"/repo/doc.md", tmp), 0, ""},
		{"relative path", memoryPayload(t, "Write", "file_path", "memory/a.md", home+"/.claude/projects/p"), 2, "memory-gate: auto-memory write blocked (memory/a.md)."},
		{"bash redirect", memoryPayload(t, "Bash", "command", "echo x > ~/.claude/projects/p/memory/a.md", tmp), 2, "memory-gate: auto-memory write blocked (~/.claude/projects/p/memory/a.md)."},
		{"bash relative redirect", memoryPayload(t, "Bash", "command", "echo x > memory/a.md", home+"/.claude/projects/p"), 2, "memory-gate: auto-memory write blocked"},
		{"bash read", memoryPayload(t, "Bash", "command", "cat ~/.claude/projects/p/memory/a.md", tmp), 0, ""},
		{"bash copy out", memoryPayload(t, "Bash", "command", "cp ~/.claude/projects/p/memory/a.md out/a.md", tmp), 0, ""},
		{"not json", []byte("not json"), 2, "COULD NOT CHECK"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := MemoryGate(context.Background(), "", "", hook.Payload{Raw: c.raw})
			if res.Code != c.code || !strings.HasPrefix(res.Stderr, c.want) && !strings.Contains(res.Stderr, c.want) {
				t.Fatalf("got code %d, stderr %q; want code %d, stderr starting with %q", res.Code, res.Stderr, c.code, c.want)
			}
			if c.code == 0 && res.Stderr != "" {
				t.Fatalf("stderr %q, want empty", res.Stderr)
			}
			if c.code == 2 && c.want != "COULD NOT CHECK" && !strings.Contains(res.Stderr, "/lighthouse-migrate-memory.") {
				t.Fatalf("stderr %q lacks the migrate hint", res.Stderr)
			}
		})
	}
}

// red if stand log makes memory-gate block or print, or leaves out the logbook line.
func TestMemoryGateOnStandLog(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if _, errOut, err := hook.RunGit(context.Background(), root, false, "init", "-q"); err != nil {
		t.Fatalf("git init: %v: %s", err, errOut)
	}
	env := hook.Env{Root: root, Level: levels.Installed{Level: "light", Components: map[string]levels.Stand{"memory-gate": levels.StandLog}}}
	raw := memoryPayload(t, "Write", "file_path", root+"/.claude/projects/p/memory/a.md", root)

	var out, errOut bytes.Buffer
	code := hook.Run(context.Background(), "memory-gate", "log", bytes.NewReader(raw), &out, &errOut, env)

	if code != 0 || out.Len() != 0 || errOut.Len() != 0 {
		t.Fatalf("code=%d out=%q err=%q, want 0 and silence", code, out.String(), errOut.String())
	}
	counts, err := levellog.Count(root, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if counts["memory-gate"] != 1 {
		t.Fatalf("logbook %v, want one line for memory-gate", counts)
	}
}
