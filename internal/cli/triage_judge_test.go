package cli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tri "github.com/CorneeldH/lighthouse/internal/triage"
)

// fakeClaude puts a claude on the PATH that writes its arguments (NUL-separated), its stdin and
// its cwd into dir, then runs body.
func fakeClaude(t *testing.T, body string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	bin := t.TempDir()
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do printf '%s\\0' \"$a\"; done > " + filepath.Join(dir, "args") + "\n" +
		"cat > " + filepath.Join(dir, "stdin") + "\n" +
		"pwd > " + filepath.Join(dir, "cwd") + "\n" +
		body + "\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestExecJudgeFlags(t *testing.T) {
	rec := fakeClaude(t, `printf '{"is_error":false}'`)
	work := t.TempDir()

	settings := `{"disableAllHooks":true}`
	out, err := execJudge(context.Background(), work, "the prompt\n", judgeConfig{Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"is_error":false}` {
		t.Errorf("stdout = %q", out)
	}
	args := strings.Split(strings.TrimSuffix(readFile(t, filepath.Join(rec, "args")), "\x00"), "\x00")
	want := []string{"-p", "--model", "opus", "--tools", "Read,Grep,Glob", "--strict-mcp-config",
		"--setting-sources", "", "--settings", settings, "--no-session-persistence", "--output-format", "json",
		"--json-schema", tri.Schema}
	if !slices.Equal(args, want) {
		t.Errorf("args:\n got %q\nwant %q", args, want)
	}
	if got := readFile(t, filepath.Join(rec, "stdin")); got != "the prompt\n" {
		t.Errorf("stdin = %q", got)
	}
	wantDir, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(rec, "cwd"))); got != wantDir {
		t.Errorf("cwd = %q, want %q", got, wantDir)
	}
}

func TestExecJudgeEnv(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "env")
	fakeClaude(t, "env > "+envFile+`; printf '{}'`)
	t.Setenv("LH_T", "0") // the settings env wins over the process env
	if _, err := execJudge(context.Background(), t.TempDir(), "p", judgeConfig{Env: []string{"LH_T=1"}}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, envFile)
	if !strings.Contains(got, "\nLH_T=1\n") && !strings.HasPrefix(got, "LH_T=1\n") {
		t.Errorf("env lacks LH_T=1:\n%s", got)
	}
	if !strings.Contains(got, "PATH=") {
		t.Errorf("env lacks the inherited PATH:\n%s", got)
	}
}

func TestExecJudgeNonZero(t *testing.T) {
	fakeClaude(t, "printf 'not logged in%0300d' 0 >&2\nexit 1")
	_, err := execJudge(context.Background(), t.TempDir(), "p", judgeConfig{})
	if err == nil {
		t.Fatal("err = nil, want the failed exit")
	}
	msg := err.Error()
	if !strings.Contains(msg, "exit status 1") || !strings.Contains(msg, "not logged in") {
		t.Errorf("err = %q, want the exit status and the start of stderr", msg)
	}
	if strings.Contains(msg, strings.Repeat("0", 200)) {
		t.Errorf("err carries more than 200 bytes of stderr: %q", msg)
	}
}

func TestExecJudgeTimeout(t *testing.T) {
	fakeClaude(t, "exec sleep 5")
	old := judgeTimeout
	judgeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { judgeTimeout = old })

	start := time.Now()
	_, err := execJudge(context.Background(), t.TempDir(), "p", judgeConfig{})
	if err == nil {
		t.Fatal("err = nil, want a timeout")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") {
		t.Errorf("err = %v, want the deadline", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("took %v, want under 2s", took)
	}
}

func TestLoadJudgeConfig(t *testing.T) {
	write := func(t *testing.T, content string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("env and modelSettings only", func(t *testing.T) {
		p := write(t, `{"env":{"B":"2","A":"1"},"modelSettings":{ "effort": "high" },"permissions":{"allow":["Read(/**)"]}}`)
		cfg, err := loadJudgeConfig(p)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"A=1", "B=2"}; !slices.Equal(cfg.Env, want) {
			t.Errorf("Env = %q, want %q", cfg.Env, want)
		}
		if want := `{"disableAllHooks":true,"modelSettings":{"effort":"high"}}`; cfg.Settings != want {
			t.Errorf("Settings = %s, want %s", cfg.Settings, want)
		}
		if strings.Contains(cfg.Settings, "permissions") || strings.Contains(strings.Join(cfg.Env, " "), "Read") {
			t.Errorf("permissions leaked: %+v", cfg)
		}
	})
	t.Run("missing file", func(t *testing.T) {
		for _, p := range []string{filepath.Join(t.TempDir(), "none.json"), ""} {
			cfg, err := loadJudgeConfig(p)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Settings != `{"disableAllHooks":true}` || len(cfg.Env) != 0 {
				t.Errorf("cfg = %+v", cfg)
			}
		}
	})
	t.Run("null modelSettings is absent", func(t *testing.T) {
		cfg, err := loadJudgeConfig(write(t, `{"modelSettings":null}`))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Settings != `{"disableAllHooks":true}` {
			t.Errorf("Settings = %s", cfg.Settings)
		}
	})
	t.Run("unreadable is an error with the path", func(t *testing.T) {
		p := t.TempDir() // a directory: read fails, not ErrNotExist
		if _, err := loadJudgeConfig(p); err == nil || !strings.Contains(err.Error(), p) {
			t.Errorf("err = %v, want the path", err)
		}
	})
	for name, content := range map[string]string{"invalid json": "{", "env not strings": `{"env":{"A":1}}`, "env not object": `{"env":["A"]}`} {
		t.Run(name, func(t *testing.T) {
			p := write(t, content)
			if _, err := loadJudgeConfig(p); err == nil || !strings.Contains(err.Error(), p) {
				t.Errorf("err = %v, want an error with the path", err)
			}
		})
	}
}

func TestJudgeSettingsPath(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if got := judgeSettingsPath(env(map[string]string{"CLAUDE_CONFIG_DIR": "/c", "HOME": "/h"})); got != "/c/settings.json" {
		t.Errorf("got %q", got)
	}
	if got := judgeSettingsPath(env(map[string]string{"HOME": "/h"})); got != "/h/.claude/settings.json" {
		t.Errorf("got %q", got)
	}
	if got := judgeSettingsPath(env(nil)); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
