package hooks

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/gitinvocations"
	"github.com/CorneeldH/lighthouse/internal/hook"
	"github.com/CorneeldH/lighthouse/internal/workflowguard"
)

const pushGatePrefix = "Workflow-push gate: "

// pushValueOptions are the `git push` options whose value is the next word.
var pushValueOptions = map[string]bool{"-o": true, "--push-option": true, "--repo": true, "--receive-pack": true, "--exec": true}

// pushManyOptions push refs the gate cannot list from the command: it does not guess them.
var pushManyOptions = map[string]bool{"--all": true, "--branches": true, "--mirror": true, "--tags": true, "--follow-tags": true}

// WorkflowPushGate blocks, only in a controller run, a `git push` whose range touches
// .github/workflows/: the workflow pusher pushes that, after V2b and a handoff with
// `next: push-workflows`. The range is origin/<branch>..<tip>, or from the merge-base with
// origin/<default branch> when the branch is not on origin. It reads `git -C <path>`, a
// preceding `cd`, refspecs and shell wrappers; an alias it cannot see. What it cannot measure
// blocks with COULD NOT CHECK. Outside a controller run it does nothing.
func WorkflowPushGate(ctx context.Context, _, _ string, p hook.Payload) hook.Result {
	if !uuidShape.MatchString(os.Getenv("LIGHTHOUSE_CONTROLLER_RUN")) {
		return hook.Result{}
	}
	command, cwd, ok := readGatePayload(p.Raw)
	if !ok {
		return pushUncheckable("the payload is not valid JSON, so the command is unknown.")
	}
	if !strings.Contains(command, "git") || !strings.Contains(command, "push") {
		return hook.Result{}
	}
	if _, err := exec.LookPath("git"); err != nil {
		return pushUncheckable("missing tooling: git.")
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	steps, err := gitinvocations.Walk(command, cwd)
	if errors.Is(err, gitinvocations.ErrUnparseable) {
		return pushUncheckable("the command cannot be parsed (open quote).")
	}
	if err != nil {
		return pushUncheckable("the command parser failed: " + err.Error())
	}
	var defaultBr string
	for _, step := range steps {
		if gitinvocations.Basename(step.Words[step.Idx]) != "git" {
			continue
		}
		target, known, raw, _, idx := gitinvocations.GitTarget(step)
		if idx >= len(step.Words) || step.Words[idx] != "push" {
			continue
		}
		if !known {
			return pushUncheckable("the path '" + raw + "' of this push cannot be resolved.")
		}
		if defaultBr == "" {
			branch, errText, ok := defaultBranch()
			if !ok {
				return pushUncheckable("the default branch could not be read: " + errText)
			}
			defaultBr = branch
		}
		if res, stop := checkPush(ctx, target, step.Words[idx+1:], step.Literal[idx+1:], defaultBr); stop {
			return res
		}
	}
	return hook.Result{}
}

// pushSpec is one ref a push sends: the local source and the branch on origin it lands on ("" when
// the destination is no branch).
type pushSpec struct{ src, dest string }

// checkPush judges one push run in dir; stop is true when it blocks.
func checkPush(ctx context.Context, dir string, args []string, literal []bool, defaultBr string) (hook.Result, bool) {
	if existing, ok := gitinvocations.ExistingAncestor(dir); ok {
		dir = existing
	} else {
		return hook.Result{}, false
	}
	root, gitMessage, err := runGit(ctx, dir, true, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		if containsAny(gitMessage, notARepo) {
			return hook.Result{}, false
		}
		return pushUncheckable("git could not read '" + dir + "': " + orNoMessage(gitMessage) + "."), true
	}
	specs, why := pushSpecs(args, literal)
	if why != "" {
		return pushUncheckable(why), true
	}
	git := func(a ...string) (string, error) {
		out, msg, err := runGit(ctx, root, true, a...)
		if err != nil {
			return "", errors.New(orNoMessage(msg))
		}
		return out, nil
	}
	for _, s := range specs {
		if s.src == "" {
			continue // a deletion pushes no commits
		}
		if s.src == "HEAD" && s.dest == "HEAD" {
			current, err := git("symbolic-ref", "--quiet", "--short", "HEAD")
			if err != nil {
				return pushUncheckable("HEAD in '" + root + "' is on no branch, so the destination is unknown."), true
			}
			s.dest = current
		}
		tip, err := git("rev-parse", "--verify", "--quiet", "--end-of-options", s.src+"^{commit}")
		if err != nil || tip == "" {
			return pushUncheckable("'" + s.src + "' is no commit in '" + root + "'."), true
		}
		rng, paths, err := workflowguard.Check(git, tip, s.dest, defaultBr)
		if err != nil {
			return pushUncheckable(err.Error()), true
		}
		if len(paths) > 0 {
			return blocked(pushGatePrefix + "blocked — " + workflowguard.Refusal(rng, paths)), true
		}
	}
	return hook.Result{}, false
}

// pushSpecs are the refs the push arguments send. why is set when they cannot be known: a
// non-literal refspec, or an option that pushes refs the command does not name. A push of
// deletions only gives no specs.
func pushSpecs(args []string, literal []bool) (specs []pushSpec, why string) {
	var positional []string
	options := true
	for k := 0; k < len(args); k++ {
		arg := args[k]
		if strings.HasPrefix(arg, "<") || strings.HasPrefix(arg, ">") || strings.HasPrefix(arg, "2>") {
			break
		}
		if options && arg == "--" {
			options = false
			continue
		}
		if options && strings.HasPrefix(arg, "-") && arg != "-" {
			name, _, inline := strings.Cut(arg, "=")
			switch {
			case pushManyOptions[name]:
				return nil, "'" + arg + "' pushes refs this command does not name."
			case name == "--delete" || name == "-d":
				return nil, ""
			case pushValueOptions[name] && !inline:
				k++
			}
			continue
		}
		// The first positional word is the remote: where it points does not change the range.
		if len(positional) > 0 && !literal[k] {
			return nil, "the refspec '" + arg + "' is not literal."
		}
		positional = append(positional, arg)
	}
	if len(positional) <= 1 {
		return []pushSpec{{"HEAD", "HEAD"}}, ""
	}
	for _, ref := range positional[1:] {
		ref = strings.TrimPrefix(ref, "+")
		src, dest, colon := strings.Cut(ref, ":")
		if !colon {
			dest = src
		}
		if src == "@" {
			src = "HEAD"
		}
		dest = strings.TrimPrefix(dest, "refs/heads/")
		if strings.HasPrefix(dest, "refs/") {
			dest = "" // a tag or another namespace: no branch on origin to start from
		}
		specs = append(specs, pushSpec{src, dest})
	}
	return specs, ""
}

func pushUncheckable(reason string) hook.Result {
	return blocked(pushGatePrefix + workflowguard.Uncheckable(reason))
}
