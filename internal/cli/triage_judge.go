package cli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
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

// judgeArgs are the flags of one judgment (spec "Het oordeel"): only Read, Grep and Glob, no
// MCP servers, no project settings and no hooks, and JSON output against the schema.
func judgeArgs() []string {
	return []string{"-p", "--model", tri.Model, "--tools", "Read,Grep,Glob", "--strict-mcp-config",
		"--setting-sources", "user", "--settings", `{"disableAllHooks": true}`,
		"--output-format", "json", "--json-schema", tri.Schema}
}

// execJudge runs one judgment with claude in dir, prompt on stdin, and returns its stdout. A
// failed exit carries the first bytes of stderr; a timeout wraps context.DeadlineExceeded.
func execJudge(ctx context.Context, dir, prompt string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, judgeTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "claude", judgeArgs()...)
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
