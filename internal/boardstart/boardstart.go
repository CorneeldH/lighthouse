// Package boardstart is `lighthouse board-start`: it moves a picked-up issue from board status
// `To do` or `On hold` to `In progress`, and with `--reopen` a reopened issue from `Done` back.
//
// Called by start-issue right after the worktree step, and by close-issue when it reopens an
// issue. Whoever deliberately picks up a parked issue lifts the parking. `Controller` stays put
// because the controller selects on it and runs start-issue itself; `In progress` and `Done`
// stay put as well.
//
// The default mode never fails hard: exit 0 and exactly one line starting with `board: `, so
// that picking up an issue does not block on it; a token or forge error is `board: skipped
// (<error>)`. Mode `--reopen <nr>` is fail-closed: any other start status, an issue off the
// board, an error or a read-back status other than `In progress` gives one line
// `board: ERROR — <cause>` and exit 1. Without a board both modes print
// `board: skipped — no [forge.board] configured` and exit 0; a broken config prints
// `board: ERROR — ConfigError: <cause>` and exits 2.
//
// It talks to the board through forge.Forge (IssueItem, SetStatus), on GitHub and GitLab alike.
package boardstart

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/procrun"
)

const (
	skipped      = "board: skipped — no [forge.board] configured"
	usage        = "board: usage: lighthouse board-start <nr>"
	reopenUsage  = "board: usage: lighthouse board-start --reopen <nr>"
	inProgress   = "In progress"
	statusDone   = "Done"
	noStatusText = "without status"
)

// Env is what Run needs from the outside: the config loader, which decides whether there is a
// board at all, and the opener of the repo's forge, called only when there is one.
type Env struct {
	Config func() (*config.Config, error)
	Forge  func(ctx context.Context) (forge.Forge, error)
}

// LoadConfig reads lighthouse.toml: LIGHTHOUSE_TOML, else the one in the repo root.
func LoadConfig() (*config.Config, error) {
	path, err := config.Path()
	if err != nil {
		return nil, err
	}
	return config.Load(path)
}

// hasBoard reports whether the config has a [forge.board]. Any error of the loader is a config
// error, and so is a board without forge.repo.
func hasBoard(env Env) (bool, error) {
	cfg, err := env.Config()
	if err != nil {
		return false, &procrun.ConfigError{Msg: err.Error()}
	}
	if cfg.Board == nil {
		return false, nil
	}
	if cfg.Repo == "" {
		return false, &procrun.ConfigError{Msg: cfg.Path + ": [forge.board] needs forge.repo"}
	}
	return true, nil
}

// statusOf is the status board-start decides on: the status_options name of the item's status,
// or, for a board option that status_options does not list, its raw name on the board.
func statusOf(it forge.BoardItem) string {
	if strings.HasPrefix(it.Status, forge.UnknownStatusPrefix) {
		return it.StatusName
	}
	return it.Status
}

// at is `at <status>`, or `without status` for an empty one.
func at(status string) string {
	if status == "" {
		return noStatusText
	}
	return "at " + status
}

// oneLine is the message of err on one line: board-start prints exactly one.
func oneLine(err error) string {
	return strings.ReplaceAll(err.Error(), "\n", "; ")
}

// reopen sets `Done` to `In progress` and reads it back; any deviation is exit 1 (fail-closed).
func reopen(ctx context.Context, f forge.Forge, nr int, out io.Writer) int {
	fail := func(format string, a ...any) int {
		_, _ = fmt.Fprintf(out, "board: ERROR — "+format+"\n", a...)
		return 1
	}
	it, ok, err := f.IssueItem(ctx, nr)
	if err != nil {
		return fail("%s", oneLine(err))
	}
	if !ok {
		return fail("#%d is not on the board", nr)
	}
	switch status := statusOf(it); status {
	case inProgress:
		_, _ = fmt.Fprintf(out, "board: #%d is already In progress\n", nr)
		return 0
	case statusDone:
	default:
		return fail("#%d is %s, not at Done", nr, at(status))
	}
	if err := f.SetStatus(ctx, it, inProgress); err != nil {
		return fail("%s", oneLine(err))
	}
	back, ok, err := f.IssueItem(ctx, nr)
	if err != nil {
		return fail("%s", oneLine(err))
	}
	after := ""
	if ok {
		after = statusOf(back)
	}
	if after != inProgress {
		if after == "" {
			after = "no status"
		}
		return fail("#%d is at %s after the edit", nr, after)
	}
	_, _ = fmt.Fprintf(out, "board: #%d Done → In progress\n", nr)
	return 0
}

// pickUp moves an item at `To do` or `On hold` to `In progress`; never a failing exit code.
func pickUp(ctx context.Context, f forge.Forge, nr int, out io.Writer) int {
	it, ok, err := f.IssueItem(ctx, nr)
	if err == nil && !ok {
		_, _ = fmt.Fprintf(out, "board: #%d is not on the board\n", nr)
		return 0
	}
	status := statusOf(it)
	if err == nil {
		if status != "To do" && status != "On hold" {
			_, _ = fmt.Fprintf(out, "board: #%d stays %s\n", nr, at(status))
			return 0
		}
		err = f.SetStatus(ctx, it, inProgress)
	}
	if err != nil {
		_, _ = fmt.Fprintf(out, "board: skipped (%s)\n", oneLine(err))
		return 0
	}
	_, _ = fmt.Fprintf(out, "board: #%d %s → In progress\n", nr, status)
	return 0
}

// number is the issue number of arg, as the script read it with int(arg): ASCII digits only,
// leading zeros allowed. ok is false for anything else, and for a number past int.
func number(arg string) (int, bool) {
	if arg == "" {
		return 0, false
	}
	for _, r := range arg {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(arg)
	return n, err == nil
}

// Run executes one board-start with the raw arguments (`<nr>` or `--reopen <nr>`), writes its one
// line to out and returns the exit code.
func Run(ctx context.Context, env Env, args []string, out io.Writer) int {
	isReopen := len(args) > 0 && args[0] == "--reopen"
	var nr int
	if isReopen {
		ok := len(args) == 2
		if ok {
			nr, ok = number(args[1])
		}
		if !ok {
			_, _ = fmt.Fprintln(out, reopenUsage)
			return 1
		}
	} else {
		ok := len(args) == 1
		if ok {
			nr, ok = number(args[0])
		}
		if !ok {
			_, _ = fmt.Fprintln(out, usage)
			return 0
		}
	}
	board, err := hasBoard(env)
	if err != nil {
		_, _ = fmt.Fprintf(out, "board: ERROR — %s\n", procrun.PyName(err))
		return 2
	}
	if !board {
		_, _ = fmt.Fprintln(out, skipped)
		return 0
	}
	f, err := env.Forge(ctx)
	if err != nil {
		if isReopen {
			_, _ = fmt.Fprintf(out, "board: ERROR — %s\n", oneLine(err))
			return 1
		}
		_, _ = fmt.Fprintf(out, "board: skipped (%s)\n", oneLine(err))
		return 0
	}
	if isReopen {
		return reopen(ctx, f, nr, out)
	}
	return pickUp(ctx, f, nr, out)
}
