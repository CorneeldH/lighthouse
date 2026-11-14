// Package boardstart is `lighthouse board-start`: it moves a picked-up issue from board status
// `To do` or `On hold` to `In progress`, and with `--reopen` a reopened issue from `Done` back.
//
// Called by start-issue right after the worktree step, and by close-issue when it reopens an
// issue. Whoever deliberately picks up a parked issue lifts the parking. `Controller` stays put
// because the controller selects on it and runs start-issue itself; `In progress` and `Done`
// stay put as well.
//
// The default mode never fails hard: exit 0 and exactly one line starting with `board: `, so
// that picking up an issue does not block on it. Mode `--reopen <nr>` is fail-closed: any other
// start status, an issue off the board, an error or a read-back status other than `In progress`
// gives one line `board: ERROR — <cause>` and exit 1. Without a board both modes print
// `board: skipped — no [forge.board] configured` and exit 0; a broken config prints
// `board: ERROR — <cause>` and exits 2.
//
// It replaces board_start.py. The output lines and exit codes are the script's, the fixtures in
// tests/pack/fixtures/scripts/board-start prove it; only the usage lines name the command now.
package boardstart

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/procrun"
)

// query is the GraphQL query of board_start.py, byte for byte. Owner and name go along as
// variables: the query does not know the repo.
const query = `
query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) {
      projectItems(first: 20) {
        nodes {
          id
          project { id }
          fieldValueByName(name: "Status") {
            ... on ProjectV2ItemFieldSingleSelectValue { name }
          }
        }
      }
    }
  }
}
`

const (
	skipped      = "board: skipped — no [forge.board] configured"
	usage        = "board: usage: lighthouse board-start <nr>"
	reopenUsage  = "board: usage: lighthouse board-start --reopen <nr>"
	ghTimeout    = 60 * time.Second
	inProgress   = "In progress"
	statusDone   = "Done"
	noStatusText = "without status"
)

// Env is what Run needs from the outside: the process runner (gh) and the config loader.
type Env struct {
	Run    procrun.Func
	Config func() (*config.Config, error)
}

// DefaultEnv runs the real gh, with the 60 s timeout of the script, and reads lighthouse.toml.
func DefaultEnv() Env {
	return Env{
		Run: procrun.Exec(ghTimeout),
		Config: func() (*config.Config, error) {
			path, err := config.Path()
			if err != nil {
				return nil, err
			}
			return config.Load(path)
		},
	}
}

// board is the part of the config a run uses.
type board struct {
	owner, name                     string
	projectID, fieldID, progressOpt string
	hasProgressOpt                  bool
}

// identity is the repo's board config, `nil` without a board; a board without forge.repo is a
// config error, as is any error of the loader.
func identity(env Env) (*board, error) {
	cfg, err := env.Config()
	if err != nil {
		return nil, &procrun.ConfigError{Msg: err.Error()}
	}
	if cfg.Board == nil {
		return nil, nil
	}
	if cfg.Repo == "" {
		return nil, &procrun.ConfigError{Msg: cfg.Path + ": [forge.board] needs forge.repo"}
	}
	b := &board{owner: "", name: cfg.Repo, projectID: cfg.Board.ProjectID, fieldID: cfg.Board.StatusFieldID}
	if i := strings.LastIndex(cfg.Repo, "/"); i >= 0 {
		b.owner, b.name = cfg.Repo[:i], cfg.Repo[i+1:]
	}
	b.progressOpt, b.hasProgressOpt = cfg.Board.StatusOptions[inProgress]
	return b, nil
}

func gh(ctx context.Context, env Env, args ...string) (string, error) {
	res, err := env.Run(ctx, "", nil, "gh", args...)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", &procrun.CalledProcessError{Code: res.Code, Argv: append([]string{"gh"}, args...)}
	}
	return res.Stdout, nil
}

// item is the issue on the board: the id of its project item and its status ("" without one).
type item struct{ id, status string }

// key reads one key of a JSON object; anything else than an object is a TypeError, a missing key
// a KeyError, as the subscripts of the script were.
func key(v any, k string) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, &procrun.TypeError{Msg: fmt.Sprintf("cannot read %q of %T", k, v)}
	}
	got, ok := m[k]
	if !ok {
		return nil, &procrun.KeyError{Key: k}
	}
	return got, nil
}

func path(v any, keys ...string) (any, error) {
	for _, k := range keys {
		var err error
		if v, err = key(v, k); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// pyStr is str(v) for the id of a node.
func pyStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return "None"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// readItem is the item of the issue on the board of the config, or nil when it is not on it.
func readItem(ctx context.Context, env Env, b *board, number string) (*item, error) {
	out, err := gh(ctx, env,
		"api", "graphql",
		"-f", "query="+query,
		"-f", "owner="+b.owner,
		"-f", "name="+b.name,
		"-F", "number="+number)
	if err != nil {
		return nil, err
	}
	var data any
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return nil, err
	}
	nodes, err := path(data, "data", "repository", "issue", "projectItems", "nodes")
	if err != nil {
		return nil, err
	}
	list, ok := nodes.([]any)
	if !ok {
		return nil, &procrun.TypeError{Msg: fmt.Sprintf("cannot iterate %T", nodes)}
	}
	for _, node := range list {
		id, err := path(node, "project", "id")
		if err != nil {
			return nil, err
		}
		if id != b.projectID {
			continue
		}
		it := &item{}
		if value, _ := node.(map[string]any)["fieldValueByName"].(map[string]any); value != nil {
			it.status, _ = value["name"].(string)
		}
		raw, err := key(node, "id")
		if err != nil {
			return nil, err
		}
		it.id = pyStr(raw)
		return it, nil
	}
	return nil, nil
}

func setInProgress(ctx context.Context, env Env, b *board, itemID string) error {
	if !b.hasProgressOpt {
		return &procrun.KeyError{Key: inProgress}
	}
	_, err := gh(ctx, env,
		"project", "item-edit",
		"--id", itemID,
		"--project-id", b.projectID,
		"--field-id", b.fieldID,
		"--single-select-option-id", b.progressOpt)
	return err
}

// at is `at <status>`, or `without status` for an empty one.
func at(status string) string {
	if status == "" {
		return noStatusText
	}
	return "at " + status
}

// reopen sets `Done` to `In progress` and reads it back; any deviation is exit 1 (fail-closed).
func reopen(ctx context.Context, env Env, b *board, number string, out io.Writer) int {
	fail := func(format string, a ...any) int {
		_, _ = fmt.Fprintf(out, "board: ERROR — "+format+"\n", a...)
		return 1
	}
	it, err := readItem(ctx, env, b, number)
	if err != nil {
		return fail("%s", procrun.PyName(err))
	}
	if it == nil {
		return fail("#%s is not on the board", number)
	}
	if it.status == inProgress {
		_, _ = fmt.Fprintf(out, "board: #%s is already In progress\n", number)
		return 0
	}
	if it.status != statusDone {
		return fail("#%s is %s, not at Done", number, at(it.status))
	}
	if err := setInProgress(ctx, env, b, it.id); err != nil {
		return fail("%s", procrun.PyName(err))
	}
	back, err := readItem(ctx, env, b, number)
	if err != nil {
		return fail("%s", procrun.PyName(err))
	}
	after := ""
	if back != nil {
		after = back.status
	}
	if after != inProgress {
		if after == "" {
			after = "no status"
		}
		return fail("#%s is at %s after the edit", number, after)
	}
	_, _ = fmt.Fprintf(out, "board: #%s Done → In progress\n", number)
	return 0
}

// number is the issue number as the script printed and sent it: int(arg), so without leading
// zeros. ok is false for anything but ASCII digits.
func number(arg string) (string, bool) {
	if arg == "" {
		return "", false
	}
	for _, r := range arg {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	if trimmed := strings.TrimLeft(arg, "0"); trimmed != "" {
		return trimmed, true
	}
	return "0", true
}

// Run executes one board-start with the raw arguments (`<nr>` or `--reopen <nr>`), writes its one
// line to out and returns the exit code.
func Run(ctx context.Context, env Env, args []string, out io.Writer) int {
	isReopen := len(args) > 0 && args[0] == "--reopen"
	var nr string
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
	b, err := identity(env)
	if err != nil {
		_, _ = fmt.Fprintf(out, "board: ERROR — %s\n", procrun.PyName(err))
		return 2
	}
	if b == nil {
		_, _ = fmt.Fprintln(out, skipped)
		return 0
	}
	if isReopen {
		return reopen(ctx, env, b, nr, out)
	}
	it, err := readItem(ctx, env, b, nr)
	if err == nil && it == nil {
		_, _ = fmt.Fprintf(out, "board: #%s is not on the board\n", nr)
		return 0
	}
	if err == nil {
		if it.status != "To do" && it.status != "On hold" {
			_, _ = fmt.Fprintf(out, "board: #%s stays %s\n", nr, at(it.status))
			return 0
		}
		err = setInProgress(ctx, env, b, it.id)
	}
	if err != nil {
		_, _ = fmt.Fprintf(out, "board: skipped (%s)\n", procrun.PyName(err))
		return 0
	}
	_, _ = fmt.Fprintf(out, "board: #%s %s → In progress\n", nr, it.status)
	return 0
}
