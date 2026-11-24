//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/bashwrites"
)

const (
	hookDirPrefix = `"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/`
	unchecked     = "gofmt-check: COULD NOT CHECK — "
	parseError    = "x.go:2:8: expected ')', found 'EOF'"
	gofmtStub     = `#!/bin/bash
echo "$@" > "$STUB_LOG"
if [ "${STUB_RC:-0}" -eq 2 ]; then
  echo "` + parseError + `" >&2
fi
exit ${STUB_RC:-0}
`
)

var gofmtTools = []string{"bash", "jq", "cat"}

func TestGoHooksRegistered(t *testing.T) {
	bin := buildLighthouse(t)
	repo := packRepo(t, bin, map[string]string{"go.mod": "module x\n\ngo 1.22\n"}, "Packages: core, go (found go.mod)\n")
	for _, tc := range []struct {
		event, matcher, script string
		timeout                int
	}{
		{"PostToolUse", "Edit|Write", "gofmt-check.sh", 15},
		{"Stop", "", "go-lint-gate.sh", 120},
		{"Stop", "", "go-imports-gate.sh", 120},
	} {
		command, timeout := registeredHook(t, repo, tc.event, tc.matcher, tc.script)
		if !strings.HasPrefix(command, hookDirPrefix) {
			t.Errorf("%s: command %q does not start with %q", tc.script, command, hookDirPrefix)
		}
		if timeout != tc.timeout {
			t.Errorf("%s: timeout %d, want %d", tc.script, timeout, tc.timeout)
		}
	}
}

func TestGofmtCheck(t *testing.T) {
	skipOnWindows(t, "the pack hooks run through runPackHook's tool dir, which breaks bash -c quoting on Windows (#559)")
	needTool(t, "jq")
	bin := buildLighthouse(t)
	repo := packRepo(t, bin, map[string]string{"go.mod": "module x\n\ngo 1.22\n"}, "Packages: core, go (found go.mod)\n")
	command, _ := registeredHook(t, repo, "PostToolUse", "Edit|Write", "gofmt-check.sh")

	// edit writes a file in repo and returns its path and the payload of an edit to it.
	edit := func(name, text string) (string, map[string]any) {
		writeFile(t, repo, name, text)
		path := filepath.Join(repo, name)
		return path, map[string]any{"tool_input": map[string]string{"file_path": path}}
	}
	// stubbed is an environment whose `gofmt` is a stub that logs its arguments to STUB_LOG.
	stubbed := func(rc string) (env []string, log string) {
		dir := toolDir(t, bin, gofmtTools, "", map[string]string{"gofmt": gofmtStub})
		log = filepath.Join(t.TempDir(), "stub.log")
		return []string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"), "STUB_LOG=" + log, "STUB_RC=" + rc}, log
	}
	// without is an environment whose PATH holds the real tooling minus one tool.
	without := func(tool string) []string {
		return []string{"PATH=" + toolDir(t, bin, append([]string{"gofmt"}, gofmtTools...), tool, nil)}
	}
	firstLine := func(s string) string { return strings.SplitN(s, "\n", 2)[0] }

	t.Run("formats_a_go_file", func(t *testing.T) {
		path, payload := edit("a.go", "package m\n")
		env, log := stubbed("0")
		code, stdout, stderr := runPackHook(t, bin, repo, command, payload, env)
		if code != 0 {
			t.Fatalf("rc %d\n%s%s", code, stdout, stderr)
		}
		got, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(got)) != "-w "+path {
			t.Fatalf("gofmt called with %q, want %q", got, "-w "+path)
		}
	})

	t.Run("blocks_a_file_that_does_not_parse", func(t *testing.T) {
		path, payload := edit("a.go", "package m\n")
		env, _ := stubbed("2")
		code, stdout, stderr := runPackHook(t, bin, repo, command, payload, env)
		if code != 2 {
			t.Fatalf("rc %d\n%s", code, stdout)
		}
		if want := "[gofmt-check/parse-error] gofmt-check: " + path + " does not parse — fix the syntax."; firstLine(stderr) != want {
			t.Fatalf("first line %q, want %q", firstLine(stderr), want)
		}
		if !strings.Contains(stderr, parseError) {
			t.Fatalf("no parse error in:\n%s", stderr)
		}
	})

	t.Run("other_extension_passes", func(t *testing.T) {
		_, payload := edit("a.py", "package m\n")
		env, log := stubbed("0")
		code, _, stderr := runPackHook(t, bin, repo, command, payload, env)
		if code != 0 {
			t.Fatalf("rc %d\n%s", code, stderr)
		}
		if _, err := os.Stat(log); err == nil {
			t.Fatal("gofmt was called for a .py file")
		}
	})

	t.Run("missing_file_passes", func(t *testing.T) {
		env, log := stubbed("0")
		payload := map[string]any{"tool_input": map[string]string{"file_path": filepath.Join(repo, "gone.go")}}
		if code, _, stderr := runPackHook(t, bin, repo, command, payload, env); code != 0 {
			t.Fatalf("rc %d\n%s", code, stderr)
		}
		if _, err := os.Stat(log); err == nil {
			t.Fatal("gofmt was called for a file that does not exist")
		}
	})

	t.Run("no_path_passes", func(t *testing.T) {
		env, _ := stubbed("0")
		if code, _, stderr := runPackHook(t, bin, repo, command, map[string]any{"tool_input": map[string]string{}}, env); code != 0 {
			t.Fatalf("rc %d\n%s", code, stderr)
		}
	})

	t.Run("classify_reads_the_parse_error_as_a_finding", func(t *testing.T) {
		needTool(t, "gofmt")
		_, payload := edit("x.go", "package m\n\nfunc F( {\n")
		code, stdout, stderr := runPackHook(t, bin, repo, command, payload, nil)
		if code != 2 {
			t.Fatalf("rc %d\n%s", code, stdout)
		}
		if !strings.HasPrefix(stderr, "[gofmt-check/parse-error] gofmt-check: ") {
			t.Fatalf("stderr: %s", stderr)
		}
		if got := bashwrites.Classify(code, []byte(stderr)); got != "finding" {
			t.Fatalf("classify: %q, want finding", got)
		}
	})

	t.Run("without_gofmt_could_not_check", func(t *testing.T) {
		_, payload := edit("a.go", "package m\n")
		code, stdout, stderr := runPackHook(t, bin, repo, command, payload, without("gofmt"))
		if code != 2 {
			t.Fatalf("rc %d\n%s", code, stdout)
		}
		if !strings.HasPrefix(firstLine(stderr), unchecked) {
			t.Fatalf("stderr: %s", stderr)
		}
		if got := bashwrites.Classify(code, []byte(stderr)); got != "unchecked" {
			t.Fatalf("classify: %q, want unchecked", got)
		}
	})

	t.Run("without_gofmt_other_extension_passes", func(t *testing.T) {
		_, payload := edit("a.md", "# hi\n")
		code, _, stderr := runPackHook(t, bin, repo, command, payload, without("gofmt"))
		if code != 0 || stderr != "" {
			t.Fatalf("rc %d, stderr %q", code, stderr)
		}
	})

	t.Run("without_jq_could_not_check", func(t *testing.T) {
		_, payload := edit("a.go", "package m\n")
		code, stdout, stderr := runPackHook(t, bin, repo, command, payload, without("jq"))
		if code != 2 {
			t.Fatalf("rc %d\n%s", code, stdout)
		}
		if want := unchecked + "jq is missing."; firstLine(stderr) != want {
			t.Fatalf("first line %q, want %q", firstLine(stderr), want)
		}
	})

	for name, payload := range map[string]string{"empty_payload_could_not_check": "", "invalid_json_could_not_check": "{"} {
		t.Run(name, func(t *testing.T) {
			env, _ := stubbed("0")
			code, stdout, stderr := runPackHook(t, bin, repo, command, payload, env)
			if code != 2 {
				t.Fatalf("rc %d\n%s", code, stdout)
			}
			if !strings.HasPrefix(firstLine(stderr), unchecked) {
				t.Fatalf("stderr: %s", stderr)
			}
		})
	}
}
