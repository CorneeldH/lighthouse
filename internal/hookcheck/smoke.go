package hookcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/deadline"
	"github.com/CorneeldH/lighthouse/internal/packs"
)

const (
	// defaultTimeout is Claude Code's timeout of a command hook without a `timeout`.
	defaultTimeout = 60 * time.Second
	// waitDelay bounds the wait for pipes a grandchild keeps open after the group was killed.
	waitDelay = 2 * time.Second
	// maxDetail caps the output line a FAIL quotes.
	maxDetail = 200
	sessionID = "lighthouse-check-hooks"
	prompt    = "lighthouse check-hooks"
)

// smoke is step 3: every registration whose command points into the hook directory runs once,
// the way Claude Code runs it: `sh -c` on Linux and macOS, Git Bash (bash) on Windows, cwd and
// CLAUDE_PROJECT_DIR on the root, the timeout of the entry, a payload on stdin. All state goes
// to a temporary directory. Exit 0 or 2 without COULD NOT CHECK is ok; anything else is FAIL.
func smoke(env Env, regs []registration, bash string, r *report) {
	var ours []registration
	for _, reg := range regs {
		if packs.IsLighthouseCommand(reg.Command) {
			ours = append(ours, reg)
		}
	}
	if len(ours) == 0 {
		r.skip("no registration under " + packs.HookDir + " to run")
		return
	}
	shell := bash
	if env.GOOS == "windows" {
		if bash == "" {
			r.skip("hooks not run: no Git Bash (see the Git Bash line above)")
			return
		}
	} else {
		sh, err := env.LookPath("sh")
		if err != nil {
			r.fail("no sh on the PATH: Claude Code runs every hook with `sh -c`, so none runs — put a POSIX sh on the PATH")
			return
		}
		shell = sh
	}
	tmp, err := os.MkdirTemp("", "lighthouse-check-hooks-")
	if err != nil {
		r.fail(fmt.Sprintf("hooks not run: no temporary directory for their state (%v) — free or fix the temporary directory", err))
		return
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	childEnv, err := smokeEnv(env, tmp)
	if err != nil {
		r.fail(fmt.Sprintf("hooks not run: %v — free or fix the temporary directory", err))
		return
	}
	now := time.Now()
	for _, reg := range ours {
		where := reg.Event
		if reg.Matcher != "" {
			where += " " + reg.Matcher
		}
		line := "hook " + hookName(reg.Command) + " (" + where + ")"
		data, err := payload(reg.Event, reg.Matcher, now, tmp, projectDir(env.GOOS, env.Root))
		if err != nil {
			r.fail(fmt.Sprintf("%s: no payload (%v) — free or fix the temporary directory", line, err))
			continue
		}
		if verdict := runHook(shell, reg, env.Root, childEnv, data); verdict != "" {
			r.fail(line + ": " + verdict + " — fix what the line says; `lighthouse init --upgrade` reinstalls the hooks")
			continue
		}
		r.ok(line)
	}
}

// runHook runs one registration and returns "" when it is ok, else why not.
func runHook(shell string, reg registration, root string, childEnv []string, data []byte) string {
	timeout := reg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-c", reg.Command)
	cmd.Dir = root
	cmd.Env = childEnv
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// Its own group, killed whole at the timeout: a child of run.sh must not keep the pipes open.
	deadline.InOwnGroup(cmd)
	cmd.Cancel = func() error { deadline.KillGroup(cmd); return nil }
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Sprintf("timed out after %v", timeout)
	}
	if errors.Is(err, exec.ErrWaitDelay) {
		// The hook exited 0, but a child of it still held stdout or stderr after WaitDelay: stop
		// that child, and judge the hook by its exit as usual.
		deadline.KillGroup(cmd)
		err = nil
	}
	code := 0
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		code = exit.ExitCode()
	case err != nil:
		return fmt.Sprintf("does not start (%v)", err)
	}
	detail := firstLine(stderr.String())
	if detail == "" {
		detail = firstLine(stdout.String())
	}
	couldNot := strings.Contains(stdout.String(), "COULD NOT CHECK") || strings.Contains(stderr.String(), "COULD NOT CHECK")
	switch {
	case couldNot:
		return detail
	case code != 0 && code != 2:
		if detail == "" {
			return fmt.Sprintf("exit %d", code)
		}
		return fmt.Sprintf("exit %d: %s", code, detail)
	}
	return ""
}

// firstLine is the first non-empty line of s, trimmed and capped.
func firstLine(s string) string {
	for l := range strings.SplitSeq(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if len(l) > maxDetail {
				l = l[:maxDetail] + "…"
			}
			return l
		}
	}
	return ""
}

// hookName is what follows the hook directory in a command, without the `run.sh ` of the core
// hooks: `main-commit-gate`, `commit-test-gate pre`, `gofmt-check.sh`.
func hookName(command string) string {
	rest := command[strings.Index(command, packs.HookDir)+len(packs.HookDir):]
	return strings.TrimPrefix(strings.TrimSpace(rest), "run.sh ")
}

// projectDir is CLAUDE_PROJECT_DIR as Claude Code passes it: the native form, with backslashes
// on Windows.
func projectDir(goos, root string) string {
	if goos == "windows" {
		return strings.ReplaceAll(root, "/", `\`)
	}
	return root
}

// stateVars point every place a hook keeps state at the temporary directory; the hook notices
// (XDG_STATE_HOME), the usage cache (XDG_CACHE_HOME) and the controller state among them.
var stateVars = []string{"LIGHTHOUSE_STATE_DIR", "XDG_RUNTIME_DIR", "XDG_STATE_HOME", "XDG_CACHE_HOME", "TMPDIR"}

// dropped are the variables of this process a hook must not see: a controller run would make
// permission-check write, and a LIGHTHOUSE_REPO_ROOT of another checkout would aim the hooks
// there.
var dropped = []string{"LIGHTHOUSE_CONTROLLER_RUN", "LIGHTHOUSE_REPO_ROOT", "LIGHTHOUSE_UNDER_BASH_WRITE_CHECK"}

// smokeEnv is the environment of this process with the state in tmp, LIGHTHOUSE_USAGE_REFRESH=1
// (usage-gate then does nothing) and CLAUDE_PROJECT_DIR on the root.
func smokeEnv(env Env, tmp string) ([]string, error) {
	set := map[string]string{
		"CLAUDE_PROJECT_DIR":       projectDir(env.GOOS, env.Root),
		"LIGHTHOUSE_USAGE_REFRESH": "1",
		"LIGHTHOUSE_HOST_TOML":     filepath.Join(tmp, "host.toml"),
	}
	for _, name := range stateVars {
		dir := filepath.Join(tmp, strings.ToLower(name))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		set[name] = dir
	}
	same := func(a, b string) bool {
		if env.GOOS == "windows" {
			return strings.EqualFold(a, b)
		}
		return a == b
	}
	var out []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		skip := slices.ContainsFunc(dropped, func(d string) bool { return same(d, key) })
		for name := range set {
			skip = skip || same(name, key)
		}
		if !skip {
			out = append(out, kv)
		}
	}
	for name, value := range set {
		out = append(out, name+"="+value)
	}
	return out, nil
}

// Payload is the stdin of one smoke run, chosen so a healthy hook does nothing and writes
// nothing (spec, step 3). It writes the transcript it points at into tmp.
func Payload(event, matcher string, now time.Time, tmp string) ([]byte, error) {
	return payload(event, matcher, now, tmp, "")
}

// payload is Payload with the cwd Claude Code sends; "" leaves it out.
func payload(event, matcher string, now time.Time, tmp, cwd string) ([]byte, error) {
	transcript, err := writeTranscript(now, tmp, cwd)
	if err != nil {
		return nil, err
	}
	p := map[string]any{
		"session_id":      sessionID,
		"transcript_path": transcript,
		"hook_event_name": event,
	}
	if cwd != "" {
		p["cwd"] = cwd
	}
	switch event {
	case "PreToolUse", "PostToolUse", "PostToolUseFailure":
		if editMatcher(matcher) {
			p["tool_name"] = "Write"
			p["tool_input"] = map[string]any{"file_path": filepath.Join(tmp, "absent", "check-hooks.txt"), "content": ""}
		} else {
			p["tool_name"] = "Bash"
			p["tool_input"] = map[string]any{"command": "true", "description": prompt}
		}
		switch event {
		case "PostToolUse":
			p["tool_response"] = map[string]any{"stdout": "", "stderr": "", "interrupted": false}
		case "PostToolUseFailure":
			p["error"] = ""
		}
	case "Stop", "SubagentStop":
		p["stop_hook_active"] = false
	case "UserPromptSubmit":
		p["prompt"] = prompt
	case "Notification":
		p["notification_type"] = "permission_prompt"
		p["message"] = "Claude needs your permission"
	}
	return json.Marshal(p)
}

// editMatcher says whether a matcher selects the file tools and not Bash.
func editMatcher(matcher string) bool {
	parts := strings.Split(matcher, "|")
	if slices.Contains(parts, "Bash") {
		return false
	}
	return slices.ContainsFunc(parts, func(p string) bool { return p == "Edit" || p == "Write" || p == "MultiEdit" })
}

// writeTranscript writes a transcript of one prompt without tool use. Its timestamp is now
// rounded up to the whole second, in UTC: no mtime and no reflog entry from before the check
// falls on or after it, so the Stop gates find the turn untouched and check nothing.
func writeTranscript(now time.Time, tmp, cwd string) (string, error) {
	start := now.UTC()
	if t := start.Truncate(time.Second); !t.Equal(start) {
		start = t.Add(time.Second)
	}
	entry := map[string]any{
		"type":      "user",
		"sessionId": sessionID,
		"timestamp": start.Format(time.RFC3339),
		"message":   map[string]any{"role": "user", "content": prompt},
	}
	if cwd != "" {
		entry["cwd"] = cwd
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}
	path := filepath.Join(tmp, "transcript.jsonl")
	return path, os.WriteFile(path, append(line, '\n'), 0o600)
}
