package roborev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/orderedjson"
	"github.com/CorneeldH/lighthouse/internal/usage"
	"github.com/CorneeldH/lighthouse/internal/workflowguard"
)

// Budget is the Codex budget check: the exit code (6 means `fallback`) and the JSON on stdout of
// `codex-usage budget`.
type Budget func() (code int, stdout string)

// DefaultBudget asks usage.ReadBudget in-process. A panic is an unreadable budget, as a
// crashing subprocess was.
func DefaultBudget() Budget { return budgetFrom(usage.ReadCodexStrict) }

// budgetFrom is DefaultBudget with the Codex reading as a parameter.
func budgetFrom(read func(context.Context, time.Duration) (usage.CodexState, error)) Budget {
	return func() (code int, stdout string) {
		defer func() {
			if recover() != nil {
				code, stdout = 1, ""
			}
		}()
		outcome, code, _ := usage.ReadBudget(context.Background(), read)
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(outcome); err != nil {
			return 1, ""
		}
		return code, buf.String()
	}
}

// budgetSkip is the skip reason when Codex may not be used, else "".
func budgetSkip(budget Budget) string {
	code, out := budget()
	unreadable := fmt.Sprintf("Codex-budget: onleesbaar - exit %d", code)
	var data map[string]any
	if json.Unmarshal([]byte(out), &data) != nil {
		return unreadable
	}
	decision, okD := data["decision"]
	reason, okR := data["reason"]
	if !okD || !okR || (code != 0 && code != 6) {
		return unreadable
	}
	if decision == "codex" && code == 0 {
		return ""
	}
	return fmt.Sprintf("Codex-budget: %s - %s", pyStr(decision), pyStr(reason))
}

// settings are the two config values the gate reads.
func settings(root string) (setting, defaultBranch string, err error) {
	cfg, err := config.Load(filepath.Join(root, "lighthouse.toml"))
	if err != nil {
		return "", "", checkf("config: %v", err)
	}
	s, err := config.Lookup(cfg, "review.roborev")
	if err != nil {
		return "", "", checkf("config: %v", err)
	}
	b, err := config.Lookup(cfg, "forge.default_branch")
	if err != nil {
		return "", "", checkf("config: %v", err)
	}
	return pyStr(s), pyStr(b), nil
}

func base(root string) (string, error) {
	_, branch, err := settings(root)
	if err != nil {
		return "", err
	}
	return git(root, true, "merge-base", "origin/"+branch, "HEAD")
}

// Gate prints (returns) the verdict the skill acts on: off, done, skip, classify or run. Every
// verdict except off carries `state` and `raw`, the absolute paths of <stem>.json and <stem>.md.
// A skip is written to the state file at once, so a restart repeats the reason instead of
// deciding again on a budget that may have moved.
func Gate(root string, budget Budget) (*orderedjson.Object, error) {
	return gate(root, budget, false)
}

// GatePush is Gate for a branch whose workflow changes the workflow pusher pushes: it reviews the
// unpushed range (origin/<branch>..HEAD, or from the merge-base when the branch is not on origin,
// as workflow-push-gate measures it) without the code-line threshold, and reads and writes only
// the push record `push-<head>.json` (raw review `push-<head>.md`). Every other skip reason stays
// a skip, which the pusher counts red.
func GatePush(root string, budget Budget) (*orderedjson.Object, error) {
	return gate(root, budget, true)
}

func gate(root string, budget Budget, push bool) (*orderedjson.Object, error) {
	setting, defaultBranch, err := settings(root)
	if err != nil {
		return nil, err
	}
	out := orderedjson.NewObject()
	if setting != "local" {
		out.Set("verdict", "off")
		return out, nil
	}
	var head, state, raw string
	if push {
		if head, err = git(root, true, "rev-parse", "HEAD"); err != nil {
			return nil, err
		}
		state, raw, err = PushPaths(root, head)
	} else {
		state, raw, err = StatePaths(root)
	}
	if err != nil {
		return nil, err
	}
	// The skill redirects the review into `raw`: the directory must exist before roborev runs.
	if err := mkdir(filepath.Dir(state)); err != nil {
		return nil, err
	}
	withPaths := func(o *orderedjson.Object) *orderedjson.Object {
		o.Set("state", state)
		o.Set("raw", raw)
		return o
	}

	recorded, err := readState(state)
	if err != nil {
		return nil, err
	}
	if recorded != nil {
		return withPaths(recorded), nil
	}
	if isFile(raw) {
		out.Set("verdict", "classify")
		return withPaths(out), nil
	}

	if !push {
		if head, err = git(root, true, "rev-parse", "HEAD"); err != nil {
			return nil, err
		}
	}
	var start string
	if push {
		start, err = pushBase(root, head, defaultBranch)
	} else {
		start, err = git(root, true, "merge-base", "origin/"+defaultBranch, "HEAD")
	}
	if err != nil {
		return nil, err
	}
	numstat, err := git(root, false, "diff", "--numstat", "--no-renames", "-z", start, "HEAD")
	if err != nil {
		return nil, err
	}
	lines := CountCodeLines(numstat)
	rng := start + ".." + head

	var reason string
	_, noRoborev := exec.LookPath("roborev")
	_, noCodex := exec.LookPath("codex")
	switch {
	case !isFile(filepath.Join(root, ".roborev.toml")):
		reason = "geen .roborev.toml"
	case noRoborev != nil || noCodex != nil:
		reason = "geen roborev of codex op PATH"
	case !push && lines < Threshold:
		reason = fmt.Sprintf("%d coderegels (drempel %d)", lines, Threshold)
	default:
		reason = budgetSkip(budget)
	}
	if reason != "" {
		skip := orderedjson.NewObject()
		skip.Set("range", rng)
		skip.Set("uitkomst", "skip")
		skip.Set("reden", reason)
		if err := writeState(state, skip); err != nil {
			return nil, err
		}
		out.Set("verdict", "skip")
		out.Set("reason", reason)
		return withPaths(out), nil
	}
	out.Set("verdict", "run")
	out.Set("range", rng)
	out.Set("lines", lines)
	return withPaths(out), nil
}

// pushBase is the start of the unpushed range of head, measured as workflow-push-gate does.
func pushBase(root, head, defaultBranch string) (string, error) {
	branch, err := git(root, true, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	if branch == "HEAD" {
		branch = ""
	}
	start, err := workflowguard.Base(func(args ...string) (string, error) { return git(root, true, args...) },
		head, branch, defaultBranch)
	if err != nil {
		return "", checkf("unpushed range: %v", err)
	}
	return start, nil
}
