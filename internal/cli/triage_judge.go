package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tri "github.com/CorneeldH/lighthouse/internal/triage"
)

// judgeTimeout bounds one judgment; a variable so a test can shorten it.
var judgeTimeout = tri.JudgmentTimeout

// judgeWaitDelay is how long Wait still waits for stdout after the kill of a judgment that ran
// out of time: a grandchild holding the pipe must not hold the round.
const judgeWaitDelay = 2 * time.Second

// judgeStderr is how much of claude's stderr a failed judgment carries.
const judgeStderr = 200

// judgeConfig is what a judgment takes from the user's settings: Env as KEY=value sorted by key,
// for the process, and Settings, the --settings JSON. Nothing else of those settings reaches it:
// above all not permissions, which would widen what the judgment can read.
type judgeConfig struct {
	Env      []string
	Settings string
}

// judgeSettingsPath is the user's settings file: $CLAUDE_CONFIG_DIR/settings.json, else
// $HOME/.claude/settings.json; "" when neither variable is set.
func judgeSettingsPath(getenv func(string) string) string {
	if d := getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "settings.json")
	}
	if h := getenv("HOME"); h != "" {
		return filepath.Join(h, ".claude", "settings.json")
	}
	return ""
}

// loadJudgeConfig reads env and modelSettings from the settings file at path. A missing file (or
// "") gives the config without them; a file that is no JSON, or an env that is no object of
// strings, is an error with the path.
func loadJudgeConfig(path string) (judgeConfig, error) {
	var raw struct {
		Env           map[string]string `json:"env"`
		ModelSettings json.RawMessage   `json:"modelSettings"`
	}
	if path != "" {
		b, err := os.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return judgeConfig{}, fmt.Errorf("read %s: %w", path, err)
		default:
			if err := json.Unmarshal(b, &raw); err != nil {
				return judgeConfig{}, fmt.Errorf("settings %s: %w", path, err)
			}
		}
	}
	var out struct {
		DisableAllHooks bool            `json:"disableAllHooks"`
		ModelSettings   json.RawMessage `json:"modelSettings,omitempty"`
	}
	out.DisableAllHooks = true
	if len(raw.ModelSettings) > 0 && string(raw.ModelSettings) != "null" {
		var buf bytes.Buffer
		if err := json.Compact(&buf, raw.ModelSettings); err != nil {
			return judgeConfig{}, fmt.Errorf("settings %s: modelSettings: %w", path, err)
		}
		out.ModelSettings = buf.Bytes()
	}
	settings, err := json.Marshal(out)
	if err != nil {
		return judgeConfig{}, fmt.Errorf("settings %s: %w", path, err)
	}
	cfg := judgeConfig{Settings: string(settings)}
	for _, k := range slices.Sorted(maps.Keys(raw.Env)) {
		cfg.Env = append(cfg.Env, k+"="+raw.Env[k])
	}
	return cfg, nil
}

// judgeArgs are the flags of one judgment (spec "Het oordeel", "De leesgrens"): only Read, Grep
// and Glob, no MCP servers, no settings source at all (settings is the only --settings: hooks
// off, plus modelSettings), no session file, and JSON output against the schema.
func judgeArgs(settings string) []string {
	return []string{"-p", "--model", tri.Model, "--tools", "Read,Grep,Glob", "--strict-mcp-config",
		"--setting-sources", "", "--settings", settings, "--no-session-persistence",
		"--output-format", "json", "--json-schema", tri.Schema}
}

// execJudge runs one judgment with claude in dir, prompt on stdin, cfg.Env added to the environment, and returns its stdout. A
// failed exit carries the first bytes of stderr; a timeout wraps context.DeadlineExceeded.
func execJudge(ctx context.Context, dir, prompt string, cfg judgeConfig) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, judgeTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "claude", judgeArgs(cfg.Settings)...)
	cmd.Env = append(os.Environ(), cfg.Env...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = judgeWaitDelay
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("claude: %w", ctx.Err())
	}
	if err != nil {
		msg := stderr.Bytes()
		if len(msg) > judgeStderr {
			msg = msg[:judgeStderr]
		}
		return nil, fmt.Errorf("claude: %w: %s", err, strings.TrimSpace(string(msg)))
	}
	return stdout.Bytes(), nil
}
