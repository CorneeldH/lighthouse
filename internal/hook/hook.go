// Package hook is the runtime behind `lighthouse hook <name> [stand]`: a registry of hooks, the
// exit codes and output they produce, and the notices that tell a session that the installed
// binary and the repository disagree (spec, hook runtime).
package hook

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ExitUnknown is the exit code for a hook this binary does not know; a gate that cannot check
// must not read as a pass.
const ExitUnknown = 3

const (
	buildCommand = "go build -o .lighthouse/bin/lighthouse ./cmd/lighthouse"
	buildFailed  = ".lighthouse/bin/build.failed"
	buildLog     = ".lighthouse/bin/build.log"
	devVersion   = "dev"
	// sourceMain marks lighthouse's own checkout: only there does a `dev` repo build its binary.
	sourceMain = "cmd/lighthouse/main.go"
)

// Payload is what the harness writes to stdin.
type Payload struct {
	SessionID string `json:"session_id"`
	Raw       []byte `json:"-"`
}

// Result is the outcome of a hook. Output is printed as JSON on stdout when it is not empty.
type Result struct {
	Code   int
	Output map[string]any
	Stderr string
}

// Func is one hook. stand is the optional second argument (for example pre or post).
type Func func(ctx context.Context, root string, stand string, p Payload) Result

// Env is the repository and binary state a run needs.
type Env struct {
	Root, ManifestVersion, BinaryVersion string
	// StateDir holds the once-per-session markers; empty means none is known, and the
	// notices stay quiet.
	StateDir string
}

var (
	mu       sync.Mutex
	registry = map[string]Func{}
)

// Register adds a hook. It panics on a duplicate name: that is a programming error.
func Register(name string, f Func) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := registry[name]; ok {
		panic("hook: duplicate hook " + name)
	}
	registry[name] = f
}

// Run runs hook name and returns the exit code. A result with code 0 gets the version and
// build notices added, once per session; a blocking result gets none, so the notice is not
// spent on a message the user reads next to a block.
func Run(ctx context.Context, name, stand string, in io.Reader, out, errOut io.Writer, env Env) int {
	mu.Lock()
	f, ok := registry[name]
	mu.Unlock()
	if !ok {
		_, _ = fmt.Fprintf(errOut, "COULD NOT CHECK: unknown hook %s — %s\n", name, remedy(env))
		return ExitUnknown
	}
	raw, err := io.ReadAll(in)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "COULD NOT CHECK: hook %s: reading stdin: %v\n", name, err)
		return ExitUnknown
	}
	p := Payload{Raw: raw}
	// A payload that is not JSON still reaches the hook, which decides what it needs.
	_ = json.Unmarshal(raw, &p)

	res, ok := invoke(ctx, name, f, env.Root, stand, p, errOut)
	if !ok {
		return ExitUnknown
	}
	if res.Code == 0 {
		res.Output = withNotices(res.Output, env, p.SessionID)
	}
	if res.Stderr != "" {
		_, _ = io.WriteString(errOut, res.Stderr)
	}
	if len(res.Output) > 0 {
		enc := json.NewEncoder(out)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(res.Output); err != nil {
			_, _ = fmt.Fprintf(errOut, "COULD NOT CHECK: hook %s: writing output: %v\n", name, err)
			return ExitUnknown
		}
	}
	return res.Code
}

// invoke runs f and turns a panic into a COULD NOT CHECK on errOut: a gate that crashed did not
// check, and exit 3 lets run.sh apply the blocking list.
func invoke(ctx context.Context, name string, f Func, root, stand string, p Payload, errOut io.Writer) (res Result, ok bool) {
	defer func() {
		if v := recover(); v != nil {
			_, _ = fmt.Fprintf(errOut, "COULD NOT CHECK: hook %s: panic: %v\n", name, v)
			res, ok = Result{}, false
		}
	}()
	return f(ctx, root, stand, p), true
}

func remedy(env Env) string {
	switch {
	case env.ManifestVersion != "" && env.ManifestVersion != devVersion:
		return "update lighthouse to " + tag(env.ManifestVersion)
	case hasSource(env.Root):
		return buildCommand
	default:
		return "install the latest lighthouse release"
	}
}

// hasSource reports whether root is lighthouse's own checkout, the one place a binary is built.
func hasSource(root string) bool {
	if root == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(sourceMain)))
	return err == nil
}

// tag is v with a v prefix; "dev", the version of a binary built from source, is no tag.
func tag(v string) string {
	if v == devVersion || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// withNotices joins the notices that are due after the systemMessage of the hook. The result is
// output itself when nothing is due.
func withNotices(output map[string]any, env Env, session string) map[string]any {
	var messages []string
	if own, _ := output["systemMessage"].(string); own != "" {
		messages = append(messages, own)
	}
	due := 0
	if env.ManifestVersion != "" && env.ManifestVersion != devVersion && env.BinaryVersion != env.ManifestVersion &&
		firstInSession(env.StateDir, session, "version") {
		messages = append(messages, fmt.Sprintf("lighthouse %s ≠ %s in this repo — run lighthouse init --upgrade",
			tag(env.BinaryVersion), tag(env.ManifestVersion)))
		due++
	}
	if buildHasFailed(env.Root) && firstInSession(env.StateDir, session, "build") {
		messages = append(messages, fmt.Sprintf("lighthouse build failed — see %s; build it by hand with %s", buildLog, buildCommand))
		due++
	}
	if due == 0 {
		return output
	}
	merged := make(map[string]any, len(output)+1)
	for k, v := range output {
		merged[k] = v
	}
	merged["systemMessage"] = strings.Join(messages, "\n")
	return merged
}

// buildHasFailed reports whether the build from lighthouse's own checkout at root failed.
func buildHasFailed(root string) bool {
	if !hasSource(root) {
		return false
	}
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(buildFailed)))
	return err == nil
}

// firstInSession reports whether kind was not yet shown in session, and records that it now is.
// The claim is one exclusive create, so parallel calls of one session show it once. When the
// marker cannot be written the notice is shown anyway: a repeated notice is better than a
// silent one. Without a state directory there is no private place for a marker at all, and
// a notice on every hook call would drown the session: then it stays quiet.
func firstInSession(stateDir, session, kind string) bool {
	if stateDir == "" {
		return false
	}
	sum := sha256.Sum256([]byte(session))
	marker := filepath.Join(stateDir, hex.EncodeToString(sum[:8])+"-"+kind)
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		return true
	}
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return false
	}
	if err == nil {
		_ = f.Close()
	}
	return true
}

// JQFields is the fields of one JSON value as jq indexes them: by exact key, the last of a
// duplicate key winning. A missing value, empty input and null have no fields; ok is false for
// anything that is not JSON or not an object, where jq fails on `.key`. (encoding/json into a
// struct matches keys case-insensitively, so `Tool_Input` would shadow `tool_input`.)
func JQFields(raw []byte) (fields map[string]json.RawMessage, ok bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, true
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	return fields, true
}

// JQText is the output of `jq -r '.key // empty'` for one value, without the trailing newline:
// a missing value, null and false are empty, a string is its text, any other value its JSON text.
func JQText(v json.RawMessage) string {
	text := string(bytes.TrimSpace(v))
	switch text {
	case "", "null", "false":
		return ""
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	return text
}

// RunGit runs `git -C dir args…`, with LC_ALL=C when cLocale is set: a message matched as text
// must not be translated. The output comes back untrimmed; each caller trims it the way its
// shell original did. When git does not start at all and wrote nothing but newlines, stderr
// carries the reason, as a shell would print it.
func RunGit(ctx context.Context, dir string, cLocale bool, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	if cLocale {
		cmd.Env = append(os.Environ(), "LC_ALL=C")
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	stdout, stderr = out.Bytes(), errOut.Bytes()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) && len(bytes.TrimRight(stderr, "\n")) == 0 {
		stderr = []byte(err.Error())
	}
	return stdout, stderr, err
}
