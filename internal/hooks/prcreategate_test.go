package hooks

import (
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/hook"
	"github.com/CorneeldH/lighthouse/internal/prbody"
)

const prGateTail = "\n" +
	"This command may contain a 'gh pr create'. 'Could not check' is\n" +
	"not a green light, so this does not pass silently.\n"

// fakeGate replaces runGate with one that writes stdout and stderr and returns code; the command
// it was given lands in *got.
func fakeGate(t *testing.T, code int, stdout, stderr string, got *string) {
	t.Helper()
	old := runGate
	t.Cleanup(func() { runGate = old })
	runGate = func(_ context.Context, _ string, stdin io.Reader, out, errOut io.Writer, _ prbody.Env) int {
		b, _ := io.ReadAll(stdin)
		if got != nil {
			*got = string(b)
		}
		_, _ = io.WriteString(out, stdout)
		_, _ = io.WriteString(errOut, stderr)
		return code
	}
}

func prGate(t *testing.T, payload any) hook.Result {
	t.Helper()
	return call(t, PrCreateGate, "", payload)
}

func bashCommand(command string) map[string]any {
	return map[string]any{"tool_input": map[string]any{"command": command}, "cwd": "/"}
}

// The exit codes of the body check, each mapped the way the bash wrapper mapped them.
func TestPrCreateGateMapsTheExitCodes(t *testing.T) {
	cases := []struct {
		name           string
		code           int
		stdout, stderr string
		want           hook.Result
	}{
		{"pass with lines", 0, "pr-create-gate: body checked — No issue\n\n", "", hook.Result{Output: map[string]any{
			"systemMessage": "pr-create-gate: body checked — No issue",
			"hookSpecificOutput": map[string]any{
				"hookEventName":     "PreToolUse",
				"additionalContext": "pr-create-gate: body checked — No issue",
			},
		}}},
		{"pass without lines", 0, "", "", hook.Result{}},
		{"block passes stderr unchanged", 2, "", "pr-create-gate: BLOCKED — x\n", hook.Result{Code: 2, Stderr: "pr-create-gate: BLOCKED — x\n"}},
		{"open quote with gh and pr", 3, "", "", hook.Result{Code: 2,
			Stderr: "pr-create-gate: COULD NOT CHECK — the command cannot be parsed (open quote). Split the command.\n" + prGateTail}},
		{"config error", 1, "", "pr-create-gate: no lighthouse.toml\n", hook.Result{Code: 2,
			Stderr: "pr-create-gate: no lighthouse.toml\npr-create-gate: COULD NOT CHECK — the body parser failed (rc 1).\n" + prGateTail}},
		{"other code", 7, "", "", hook.Result{Code: 2,
			Stderr: "pr-create-gate: COULD NOT CHECK — the body parser failed (rc 7).\n" + prGateTail}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var command string
			fakeGate(t, c.code, c.stdout, c.stderr, &command)

			got := prGate(t, bashCommand("gh pr create --body x"))

			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %#v\nwant %#v", got, c.want)
			}
			if command != "gh pr create --body x" {
				t.Errorf("the gate read %q from stdin", command)
			}
		})
	}
}

// Two-step rule at exit 3: without both `gh` and `pr` in the raw text it passes silently.
func TestPrCreateGateOpenQuoteWithoutGhAndPrPasses(t *testing.T) {
	for _, command := range []string{"echo 'x", "gh issue list 'x", "echo pr 'x"} {
		t.Run(command, func(t *testing.T) {
			fakeGate(t, 3, "", "", nil)

			got := prGate(t, bashCommand(command))

			if !reflect.DeepEqual(got, hook.Result{}) {
				t.Errorf("got %#v, want a silent pass", got)
			}
		})
	}
}

// Design choice 6: a panic in the body check blocks with this hook's own text, not exit 3.
func TestPanicBecomesCouldNotCheck(t *testing.T) {
	old := runGate
	t.Cleanup(func() { runGate = old })
	runGate = func(_ context.Context, _ string, _ io.Reader, _, errOut io.Writer, _ prbody.Env) int {
		_, _ = io.WriteString(errOut, "before\n")
		panic("boom")
	}

	got := prGate(t, bashCommand("gh pr create --body x"))

	want := hook.Result{Code: 2, Stderr: "before\npr-create-gate: COULD NOT CHECK — the body parser failed (panic: boom).\n" + prGateTail}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
}

// The payload as `jq -r '.tool_input.command // empty'` reads it; the verdicts are those of the
// bash hook (tests/pack/test_pr_create_gate.py, test_payload_shapes).
func TestPrCreateGatePayloadShapes(t *testing.T) {
	invalid := hook.Result{Code: 2, Stderr: "pr-create-gate: COULD NOT CHECK — the payload is not valid JSON.\n" +
		"\n" +
		"Without the command nothing was checked; that is not a green light.\n"}
	cases := []struct {
		name, payload string
		want          hook.Result
		gateRuns      bool
	}{
		{"not json", "geen json {", invalid, false},
		{"array", "[1]", invalid, false},
		{"string tool_input", `{"tool_input": "gh pr create"}`, invalid, false},
		{"null", "null", hook.Result{}, false},
		{"empty command", `{"tool_input": {"command": ""}}`, hook.Result{}, false},
		{"number command", `{"tool_input": {"command": 5}}`, hook.Result{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			command := "<not run>"
			fakeGate(t, 0, "", "", &command)

			got := prGate(t, c.payload)

			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %#v\nwant %#v", got, c.want)
			}
			if c.gateRuns != (command != "<not run>") {
				t.Errorf("gate ran with %q; want it to run: %v", command, c.gateRuns)
			}
		})
	}
}

// The real body check: an open quote with `gh pr` blocks, a plain command passes silently.
func TestPrCreateGateWithTheRealBodyCheck(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", t.TempDir())
	cases := []struct {
		command string
		want    hook.Result
	}{
		{"gh pr create --body 'x", hook.Result{Code: 2,
			Stderr: "pr-create-gate: COULD NOT CHECK — the command cannot be parsed (open quote). Split the command.\n" + prGateTail}},
		{"echo 'x", hook.Result{}},
		{"git status", hook.Result{}},
	}
	for _, c := range cases {
		t.Run(c.command, func(t *testing.T) {
			got := prGate(t, bashCommand(c.command))

			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %#v\nwant %#v", got, c.want)
			}
		})
	}
}

// root reaches the config as LIGHTHOUSE_REPO_ROOT, as LIB_ROOT did for the Python lib.
func TestPrCreateGatePassesRootToTheConfig(t *testing.T) {
	t.Setenv("LIGHTHOUSE_REPO_ROOT", "")
	var seen string
	old := runGate
	t.Cleanup(func() { runGate = old })
	runGate = func(context.Context, string, io.Reader, io.Writer, io.Writer, prbody.Env) int {
		seen = os.Getenv("LIGHTHOUSE_REPO_ROOT")
		return 0
	}
	raw := fmt.Appendf(nil, `{"tool_input": {"command": "gh pr create"}, "cwd": %q}`, "/")

	PrCreateGate(context.Background(), "/the/root", "", hook.Payload{Raw: raw})

	if seen != "/the/root" {
		t.Errorf("LIGHTHOUSE_REPO_ROOT %q, want /the/root", seen)
	}
}

// Without git nothing can be checked, also not a plain command.
func TestPrCreateGateWithoutGitBlocks(t *testing.T) {
	emptyPath(t)

	got := prGate(t, bashCommand("ls"))

	want := hook.Result{Code: 2, Stderr: "pr-create-gate: COULD NOT CHECK — missing tooling: git\n" +
		"\n" +
		"This hook checks the body of every 'gh pr create'.\n" +
		"Without that tooling nothing was checked; that is not a green light.\n"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
}
