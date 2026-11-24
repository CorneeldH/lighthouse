package issuestate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/procrun"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// usage is the line for a wrong number or key; exit 1, without a forge call.
const usage = "state: usage: lighthouse state show <nr> | label <nr> <key> | return <nr> <key>; key is one of "

// boardSkipped is the board line of return in a repo without [forge.board].
const boardSkipped = "board: skipped — no [forge.board] configured"

// Env is what the `lighthouse state` commands need from the outside: the config loader (exit 2
// on its error), the opener of the repo's forge and the run state of an issue. Run gives the
// state of the run of issue nr in repo ("open", "parked", or "" without a run); it lives outside
// this package, so the tick in internal/dispatch can import issuestate without a cycle.
type Env struct {
	Config func() (*config.Config, error)
	Forge  func(ctx context.Context) (forge.Forge, error)
	Run    func(ctx context.Context, repo string, nr int) (string, error)
}

// target is what every command reads before it calls the forge.
type target struct {
	cfg    *config.Config
	labels repoconfig.Labels
	f      forge.Forge
}

// open loads the config and the labels and opens the forge. It writes the error line and
// returns the exit code: 2 for a config error, 1 for a forge error.
func open(ctx context.Context, env Env, out io.Writer) (target, int) {
	cfg, err := env.Config()
	if err != nil {
		_, _ = fmt.Fprintf(out, "state: ERROR — %s\n", procrun.PyName(&procrun.ConfigError{Msg: err.Error()}))
		return target{}, 2
	}
	repo, err := repoconfig.LoadFile(cfg.Path)
	if err != nil {
		_, _ = fmt.Fprintf(out, "state: ERROR — %s\n", procrun.PyName(&procrun.ConfigError{Msg: err.Error()}))
		return target{}, 2
	}
	if cfg.Board != nil && cfg.Repo == "" {
		_, _ = fmt.Fprintf(out, "state: ERROR — %s\n", procrun.PyName(&procrun.ConfigError{Msg: cfg.Path + ": [forge.board] needs forge.repo"}))
		return target{}, 2
	}
	f, err := env.Forge(ctx)
	var te *forge.TokenError
	if errors.As(err, &te) {
		fail(out, err, "")
		return target{}, 2
	}
	if err != nil {
		return target{}, fail(out, err, "")
	}
	return target{cfg: cfg, labels: repo.Labels, f: f}, 0
}

// fail writes the error line with hint appended and returns exit 1.
func fail(out io.Writer, err error, hint string) int {
	_, _ = fmt.Fprintf(out, "state: ERROR — %s%s\n", strings.ReplaceAll(err.Error(), "\n", "; "), hint)
	return 1
}

// parse reads the issue number and, with key true, checks the key; a wrong one writes the usage
// line.
func parse(nrArg, keyArg string, key bool, out io.Writer) (int, bool) {
	nr, err := strconv.Atoi(nrArg)
	if err != nil || nr < 1 || (key && !slices.Contains(Keys, keyArg)) {
		_, _ = fmt.Fprintln(out, usage+strings.Join(Keys, ", "))
		return 0, false
	}
	return nr, true
}

// Show writes the state of issue nrArg: `state: #<nr> <state>`.
func Show(ctx context.Context, env Env, nrArg string, out io.Writer) int {
	nr, ok := parse(nrArg, "", false, out)
	if !ok {
		return 1
	}
	t, code := open(ctx, env, out)
	if code != 0 {
		return code
	}
	in, err := t.input(ctx, env, nr)
	if err != nil {
		return fail(out, err, "")
	}
	r := Of(in, t.labels)
	_, _ = fmt.Fprintf(out, "state: #%d %s\n", nr, r.State)
	return 0
}

// input reads the Input of issue nr from the forge, the board (when configured) and the runs.
func (t target) input(ctx context.Context, env Env, nr int) (Input, error) {
	is, err := t.f.Issue(ctx, nr)
	if err != nil {
		return Input{}, err
	}
	in := Input{Closed: is.State == "closed", Labels: is.Labels}
	if in.Blockers, err = t.f.BlockedBy(ctx, nr); err != nil {
		return Input{}, err
	}
	if t.cfg.Board != nil {
		it, ok, err := t.f.IssueItem(ctx, nr)
		if err != nil {
			return Input{}, err
		}
		if ok {
			in.Status = it.EffectiveStatus()
		}
	}
	comments, err := t.f.Comments(ctx, nr)
	if err != nil {
		return Input{}, err
	}
	in.OpenQuestion = protocol.Classify(comments).State == protocol.StateQuestion
	if in.Run, err = env.Run(ctx, t.cfg.Repo, nr); err != nil {
		return Input{}, err
	}
	return in, nil
}

// refuseClosed reads only the issue and, when it is closed, writes the error line and returns
// exit 1: a closed issue gets no label and no board move. It returns 0 for an open issue.
func (t target) refuseClosed(ctx context.Context, nr int, out io.Writer) int {
	is, err := t.f.Issue(ctx, nr)
	if err != nil {
		return fail(out, err, "")
	}
	if is.State == "closed" {
		return fail(out, fmt.Errorf("issue #%d is closed", nr), "")
	}
	return 0
}

// labelLine is `label: #<nr> <winner> (+<add>, -<remove>)`, or `label: #<nr> keeps <winner>`
// without an edit.
func labelLine(nr int, e LabelEdit) string {
	var parts []string
	for _, a := range e.Add {
		parts = append(parts, "+"+a)
	}
	for _, r := range e.Remove {
		parts = append(parts, "-"+r)
	}
	if len(parts) == 0 {
		return fmt.Sprintf("label: #%d keeps %s", nr, e.Winner)
	}
	return fmt.Sprintf("label: #%d %s (%s)", nr, e.Winner, strings.Join(parts, ", "))
}

// halfWriteHint is appended to the error of a label edit that may have landed half.
const halfWriteHint = "; run it again to finish the label edit"

// labelFailure writes the error of a failed label edit; an edit that was sent may have landed
// half, so it asks to run the command again.
func labelFailure(out io.Writer, e LabelEdit, err error) int {
	if len(e.Add) > 0 || len(e.Remove) > 0 {
		return fail(out, err, halfWriteHint)
	}
	return fail(out, err, "")
}

// Label sets blocking label keyArg on issue nrArg with the rank rule.
func Label(ctx context.Context, env Env, nrArg, keyArg string, out io.Writer) int {
	nr, ok := parse(nrArg, keyArg, true, out)
	if !ok {
		return 1
	}
	t, code := open(ctx, env, out)
	if code != 0 {
		return code
	}
	if code := t.refuseClosed(ctx, nr, out); code != 0 {
		return code
	}
	e, err := Apply(ctx, t.f, nr, keyArg, t.labels)
	if err != nil {
		return labelFailure(out, e, err)
	}
	_, _ = fmt.Fprintln(out, labelLine(nr, e))
	return 0
}

// GiveBack gives issue nrArg back with blocking label keyArg: the label line, then the board line.
func GiveBack(ctx context.Context, env Env, nrArg, keyArg string, out io.Writer) int {
	nr, ok := parse(nrArg, keyArg, true, out)
	if !ok {
		return 1
	}
	t, code := open(ctx, env, out)
	if code != 0 {
		return code
	}
	if code := t.refuseClosed(ctx, nr, out); code != 0 {
		return code
	}
	r, err := Return(ctx, t.f, nr, keyArg, t.labels, t.cfg.Board != nil)
	if !r.Labeled {
		return labelFailure(out, r.Label, err)
	}
	_, _ = fmt.Fprintln(out, labelLine(nr, r.Label))
	switch {
	case err != nil:
		return fail(out, err, "")
	case t.cfg.Board == nil:
		_, _ = fmt.Fprintln(out, boardSkipped)
	case !r.OnBoard:
		_, _ = fmt.Fprintf(out, "board: #%d is not on the board\n", nr)
	case r.From == ToDo:
		_, _ = fmt.Fprintf(out, "board: #%d stays %s\n", nr, ToDo)
	default:
		from := r.From
		if from == "" {
			from = "without status"
		}
		_, _ = fmt.Fprintf(out, "board: #%d %s → %s\n", nr, from, ToDo)
	}
	return 0
}
