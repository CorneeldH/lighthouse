package boardstart

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/procrun"
	"github.com/CorneeldH/lighthouse/internal/scriptcase"
)

const fixtures = "../../tests/pack/fixtures/scripts/board-start/cases.json"

// configIn writes text as lighthouse.toml in a temp dir and returns the loader of Env.
func configIn(t *testing.T, text string) func() (*config.Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return func() (*config.Config, error) { return config.Load(path) }
}

// TestBoardStartCases replays every case of cases.json, the one fixture that the Python script
// ran against first, through Run.
func TestBoardStartCases(t *testing.T) {
	for _, c := range scriptcase.Load(t, fixtures) {
		t.Run(c.Name, func(t *testing.T) {
			if c.Only != "" && c.Only != "go" {
				t.Skip("not a Go case")
			}
			var out bytes.Buffer
			env := Env{Run: scriptcase.Replay(t, c.Calls), Config: configIn(t, c.Config)}
			code := Run(context.Background(), env, c.Argv, &out)
			if code != c.Expect.Code {
				t.Errorf("exit code = %d, want %d; stdout %q", code, c.Expect.Code, out.String())
			}
			if c.Expect.Stdout != "" || len(c.Expect.StdoutContains) == 0 {
				if out.String() != c.Expect.Stdout {
					t.Errorf("stdout = %q, want %q", out.String(), c.Expect.Stdout)
				}
			}
			for _, want := range c.Expect.StdoutContains {
				if !strings.Contains(out.String(), want) {
					t.Errorf("stdout %q lacks %q", out.String(), want)
				}
			}
		})
	}
}

// TestUsageLinesAreTheNewNames pins the one text that changes with the port (plan decision 4):
// the usage lines name the command, and keep the exit codes of the script.
func TestUsageLinesAreTheNewNames(t *testing.T) {
	env := Env{Run: scriptcase.Replay(t, nil), Config: configIn(t, "")}
	for _, tc := range []struct {
		args []string
		want string
		code int
	}{
		{nil, "board: usage: lighthouse board-start <nr>\n", 0},
		{[]string{"x"}, "board: usage: lighthouse board-start <nr>\n", 0},
		{[]string{"--reopen"}, "board: usage: lighthouse board-start --reopen <nr>\n", 1},
		{[]string{"--reopen", "1", "2"}, "board: usage: lighthouse board-start --reopen <nr>\n", 1},
	} {
		var out bytes.Buffer
		if code := Run(context.Background(), env, tc.args, &out); code != tc.code || out.String() != tc.want {
			t.Errorf("Run(%v) = %d, %q; want %d, %q", tc.args, code, out.String(), tc.code, tc.want)
		}
	}
}

// TestMissingGhIsFileNotFound covers the error name no fixture can reach: the fake gh is on
// the PATH of the Python runner, so it can never be missing there.
func TestMissingGhIsFileNotFound(t *testing.T) {
	cfg := "[forge]\nrepo = \"Owner/repo\"\n[forge.board]\nproject_number = 1\nproject_id = \"P\"\nstatus_field_id = \"F\"\n" +
		"[forge.board.status_options]\n\"To do\" = \"a\"\n\"In progress\" = \"b\"\nDone = \"c\"\n\"On hold\" = \"d\"\n"
	missing := func(context.Context, string, *string, string, ...string) (procrun.Result, error) {
		return procrun.Result{}, &procrun.NotFound{Name: "gh"}
	}
	var out bytes.Buffer
	code := Run(context.Background(), Env{Run: missing, Config: configIn(t, cfg)}, []string{"7"}, &out)
	if code != 0 || out.String() != "board: skipped (FileNotFoundError)\n" {
		t.Errorf("got %d, %q", code, out.String())
	}
}
