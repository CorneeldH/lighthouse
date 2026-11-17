package pusher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// finding is the key of one lint finding: no line number, so a finding that only moved is the
// same finding on both sides.
type finding struct{ Rule, File, Text string }

// findings counts each key, so a second instance of an existing finding is still new.
type findings map[finding]int

// lintTools are the tools Lint runs; each must be on PATH.
var lintTools = []string{"actionlint", "zizmor"}

// Lint runs actionlint and zizmor --offline on the workflows among files, at c.SHA and at
// c.MergeBase, and is red for a finding at the SHA that the merge-base did not have. The tools
// see copies of the blobs from the clone under generated names in a directory of the pusher,
// never a path of the branch. A missing tool or one that fails is red with COULD NOT CHECK.
func Lint(ctx context.Context, req Request, c Checked, files []string) *Red {
	wanted := lintTargets(files)
	if len(wanted) == 0 {
		return nil
	}
	for _, tool := range lintTools {
		if _, err := exec.LookPath(tool); err != nil {
			return &Red{StepLint, "COULD NOT CHECK: " + tool + " is not on PATH"}
		}
	}
	head, red := lintRev(ctx, req.CloneDir, c.SHA, wanted)
	if red != nil {
		return red
	}
	base, red := lintRev(ctx, req.CloneDir, c.MergeBase, wanted)
	if red != nil {
		return red
	}
	var lines []string
	for key, n := range head {
		if extra := n - base[key]; extra > 0 {
			lines = append(lines, fmt.Sprintf("- %s in %s: %s (%d new)", key.Rule, key.File, key.Text, extra))
		}
	}
	if len(lines) == 0 {
		return nil
	}
	sort.Strings(lines)
	return &Red{StepLint, "findings that the merge-base does not have:\n" + strings.Join(lines, "\n")}
}

// lintTargets is the sorted, distinct workflow paths among files; its index names the copy.
func lintTargets(files []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		if isWorkflow(f) && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// lintRev copies the wanted workflows at rev into a fresh directory as
// .github/workflows/w<index>.yml and runs both tools there.
func lintRev(ctx context.Context, clone, rev string, wanted []string) (findings, *Red) {
	entries, err := workflowEntries(ctx, clone, rev)
	if err != nil {
		return nil, &Red{StepLint, "COULD NOT CHECK: " + err.Error()}
	}
	tmp, err := os.MkdirTemp("", "lighthouse-push-lint-")
	if err != nil {
		return nil, &Red{StepLint, "COULD NOT CHECK: " + err.Error()}
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := os.MkdirAll(filepath.Join(tmp, workflowDir), 0o700); err != nil {
		return nil, &Red{StepLint, "COULD NOT CHECK: " + err.Error()}
	}
	names := map[string]string{} // generated base name → path in the branch
	var args []string
	for i, p := range wanted {
		e, ok := entries[p]
		if !ok {
			continue
		}
		text, reason, err := readWorkflow(ctx, clone, e)
		if err != nil {
			return nil, &Red{StepLint, "COULD NOT CHECK: " + err.Error()}
		}
		if reason != "" {
			continue // the policy reports it
		}
		name := "w" + strconv.Itoa(i) + ".yml"
		rel := workflowDir + "/" + name
		if err := os.WriteFile(filepath.Join(tmp, rel), text, 0o600); err != nil {
			return nil, &Red{StepLint, "COULD NOT CHECK: " + err.Error()}
		}
		names[name] = p
		args = append(args, rel)
	}
	found := findings{}
	if len(args) == 0 {
		return found, nil
	}
	if red := actionlint(ctx, tmp, args, names, found); red != nil {
		return nil, red
	}
	if red := zizmor(ctx, tmp, args, names, found); red != nil {
		return nil, red
	}
	return found, nil
}

// runLinter runs tool in dir with the pusher's minimal environment (no token, no GIT_ variable)
// and returns stdout when the exit code is one of ok.
func runLinter(ctx context.Context, dir, tool string, args []string, ok func(code int) bool) ([]byte, *Red) {
	cmd := exec.CommandContext(ctx, tool, args...)
	cmd.Dir = dir
	cmd.Env = minimalEnv()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return nil, &Red{StepLint, fmt.Sprintf("COULD NOT CHECK: %s did not run: %v", tool, err)}
		}
		code = exit.ExitCode()
	}
	if !ok(code) {
		return nil, &Red{StepLint, fmt.Sprintf("COULD NOT CHECK: %s exited %d: %s", tool, code, strings.TrimSpace(stderr.String()))}
	}
	return out, nil
}

func minimalEnv() []string {
	var env []string
	for _, k := range envAllow {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// fileOf maps a path a tool printed back to the path in the branch by its generated base name.
func fileOf(names map[string]string, printed string) string {
	if p, ok := names[filepath.Base(printed)]; ok {
		return p
	}
	return printed
}

// actionlint exits 0 without and 1 with findings, and prints them as a JSON list.
func actionlint(ctx context.Context, dir string, args []string, names map[string]string, found findings) *Red {
	out, red := runLinter(ctx, dir, "actionlint", append([]string{"-no-color", "-format", "{{json .}}"}, args...),
		func(code int) bool { return code == 0 || code == 1 })
	if red != nil {
		return red
	}
	var list []struct {
		Message  string `json:"message"`
		Filepath string `json:"filepath"`
		Kind     string `json:"kind"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return &Red{StepLint, "COULD NOT CHECK: actionlint output is not JSON: " + err.Error()}
	}
	for _, f := range list {
		found[finding{"actionlint/" + f.Kind, fileOf(names, f.Filepath), f.Message}]++
	}
	return nil
}

// zizmorFinding is the part of zizmor's JSON output the key uses.
type zizmorFinding struct {
	Ident     string `json:"ident"`
	Desc      string `json:"desc"`
	Locations []struct {
		Symbolic struct {
			Key struct {
				Local *struct {
					GivenPath string `json:"given_path"`
				} `json:"Local"`
			} `json:"key"`
			Annotation string `json:"annotation"`
			Kind       string `json:"kind"`
		} `json:"symbolic"`
		Concrete struct {
			Feature string `json:"feature"`
		} `json:"concrete"`
	} `json:"locations"`
}

// zizmor exits 0 without findings and 10 plus the highest severity with them. An ignored
// finding counts too: the ignore comment is the branch's.
func zizmor(ctx context.Context, dir string, args []string, names map[string]string, found findings) *Red {
	out, red := runLinter(ctx, dir, "zizmor", append([]string{"--offline", "--format", "json"}, args...),
		func(code int) bool { return code == 0 || (code >= 10 && code <= 14) })
	if red != nil {
		return red
	}
	var list []zizmorFinding
	if err := json.Unmarshal(out, &list); err != nil {
		return &Red{StepLint, "COULD NOT CHECK: zizmor output is not JSON: " + err.Error()}
	}
	for _, f := range list {
		file, text := "?", f.Desc
		for i, loc := range f.Locations {
			if i > 0 && loc.Symbolic.Kind != "Primary" {
				continue
			}
			if loc.Symbolic.Key.Local != nil {
				file = fileOf(names, loc.Symbolic.Key.Local.GivenPath)
			}
			text = f.Desc + " | " + loc.Symbolic.Annotation + " | " + loc.Concrete.Feature
			if loc.Symbolic.Kind == "Primary" {
				break
			}
		}
		found[finding{"zizmor/" + f.Ident, file, text}]++
	}
	return nil
}
