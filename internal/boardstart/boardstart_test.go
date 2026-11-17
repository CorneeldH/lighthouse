package boardstart

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// boardConfig is the lighthouse.toml of most cases: a GitHub repo with a board that maps the
// four statuses board-start knows.
const boardConfig = `
[forge]
kind = "github"
repo = "Owner/repo"

[forge.board]
project_number = 1
project_id = "PVT_test"
status_field_id = "FIELD_test"

[forge.board.status_options]
"To do" = "opt-todo"
"In progress" = "opt-progress"
Done = "opt-done"
"On hold" = "opt-hold"
`

// configIn writes text as lighthouse.toml in a temp dir and returns the loader of Env.
func configIn(t *testing.T, text string) func() (*config.Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return func() (*config.Config, error) { return config.Load(path) }
}

// call is one forge call board-start must make, in order, and the forge's answer to it.
// want is "IssueItem <number>" or "SetStatus <item ID> <status>".
type call struct {
	want string
	item forge.BoardItem // IssueItem's item
	ok   bool            // IssueItem's ok
	err  error
}

// scriptedForge answers IssueItem and SetStatus from calls, in order, and fails the test on
// any call that deviates from the script. Every other method of forge.Forge is nil and panics.
type scriptedForge struct {
	forge.Forge
	t     *testing.T
	calls []call
	n     int
}

func (f *scriptedForge) next(got string) call {
	f.t.Helper()
	if f.n >= len(f.calls) {
		f.t.Fatalf("forge call %d: %s, but the script has only %d calls", f.n+1, got, len(f.calls))
	}
	c := f.calls[f.n]
	f.n++
	if got != c.want {
		f.t.Fatalf("forge call %d: got %s, want %s", f.n, got, c.want)
	}
	return c
}

func (f *scriptedForge) IssueItem(_ context.Context, number int) (forge.BoardItem, bool, error) {
	c := f.next(fmt.Sprintf("IssueItem %d", number))
	return c.item, c.ok, c.err
}

func (f *scriptedForge) SetStatus(_ context.Context, item forge.BoardItem, status string) error {
	return f.next(fmt.Sprintf("SetStatus %s %s", item.ItemID, status)).err
}

// itemAt is the item PVTI_a of issue 7 at a mapped status ("" none): Status and StatusName agree.
func itemAt(status string) forge.BoardItem {
	return forge.BoardItem{ItemID: "PVTI_a", Repo: "Owner/repo", Issue: 7, Status: status, StatusName: status}
}

// unmapped is the item PVTI_a of issue 7 at board option raw, which status_options does not
// list: the forge gives Status "unknown:<option ID>" and the raw name in StatusName.
func unmapped(optionID, raw string) forge.BoardItem {
	return forge.BoardItem{ItemID: "PVTI_a", Repo: "Owner/repo", Issue: 7,
		Status: forge.UnknownStatusPrefix + optionID, StatusName: raw}
}

func issueItem(item forge.BoardItem) call { return call{want: "IssueItem 7", item: item, ok: true} }

var setsInProgress = call{want: "SetStatus PVTI_a In progress"}

// errAPI is a forge error as the GitHub client words a failed request.
var errAPI = errors.New("forge POST https://api.github.com/graphql: 502 Bad Gateway")

// boardCase is one run of board-start: its arguments and lighthouse.toml, the forge calls it
// must make, and its one line and exit code. openErr is the error of opening the forge.
type boardCase struct {
	name    string
	argv    []string
	config  string
	openErr error
	calls   []call
	want    string
	code    int
}

// run runs c through Run with a scriptedForge, and checks the line, the exit code and that
// every scripted call was made. Without calls and openErr, opening the forge fails the test.
func (c boardCase) run(t *testing.T) {
	t.Helper()
	f := &scriptedForge{t: t, calls: c.calls}
	open := func(context.Context) (forge.Forge, error) {
		if c.openErr != nil {
			return nil, c.openErr
		}
		if len(c.calls) == 0 {
			t.Error("the forge was opened, but the case expects no forge call")
		}
		return f, nil
	}
	var out bytes.Buffer
	code := Run(context.Background(), Env{Config: configIn(t, c.config), Forge: open}, c.argv, &out)
	if code != c.code || out.String() != c.want {
		t.Errorf("Run(%v) = %d, %q; want %d, %q", c.argv, code, out.String(), c.code, c.want)
	}
	if f.n != len(c.calls) {
		t.Errorf("made %d of %d scripted forge calls", f.n, len(c.calls))
	}
}

// TestBoardStartCases are the 37 cases of the former tests/pack/fixtures/scripts/board-start
// cases.json, by their names there, now on a fake forge.Forge instead of a fake gh. The lines of
// the status and flow cases are the old ones, letter for letter; an error now prints the forge's
// message where it printed a Python exception name. What the GraphQL reply cases tested (own
// board, owner/name, number as Int, JSON shape) is the forge's job now: TestIssueItem,
// TestIssueItemGraphQLErrors and the forgesuite cases test it there.
func TestBoardStartCases(t *testing.T) {
	otherRepo := strings.NewReplacer("Owner/repo", "Other/repo", "PVT_test", "PVT_other", "FIELD_test", "FIELD_other").Replace(boardConfig)
	for _, c := range []boardCase{
		{name: "test_pickable_becomes_in_progress[To do]", argv: []string{"7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("To do")), setsInProgress},
			want:  "board: #7 To do → In progress\n"},
		{name: "test_pickable_becomes_in_progress[On hold]", argv: []string{"7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("On hold")), setsInProgress},
			want:  "board: #7 On hold → In progress\n"},
		{name: "test_other_status_stays[Controller]", argv: []string{"7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("Controller"))}, want: "board: #7 stays at Controller\n"},
		{name: "test_other_status_stays[In progress]", argv: []string{"7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("In progress"))}, want: "board: #7 stays at In progress\n"},
		{name: "test_other_status_stays[Done]", argv: []string{"7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("Done"))}, want: "board: #7 stays at Done\n"},
		{name: "test_other_status_stays[None]", argv: []string{"7"}, config: boardConfig,
			calls: []call{issueItem(itemAt(""))}, want: "board: #7 stays without status\n"},
		// The item of another board is the forge's to skip (TestIssueItem, P2); IssueItem
		// returns the item of the own board only.
		{name: "test_only_item_on_own_board", argv: []string{"7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("Controller"))}, want: "board: #7 stays at Controller\n"},
		{name: "test_not_on_board", argv: []string{"7"}, config: boardConfig,
			calls: []call{{want: "IssueItem 7", ok: false}}, want: "board: #7 is not on the board\n"},
		{name: "test_gh_failure_does_not_block", argv: []string{"7"}, config: boardConfig,
			calls: []call{{want: "IssueItem 7", err: errAPI}},
			want:  "board: skipped (forge POST https://api.github.com/graphql: 502 Bad Gateway)\n"},
		{name: "test_no_argument", argv: nil, config: boardConfig,
			want: "board: usage: lighthouse board-start <nr>\n"},
		{name: "test_reopen_done_becomes_in_progress", argv: []string{"--reopen", "7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("Done")), setsInProgress, issueItem(itemAt("In progress"))},
			want:  "board: #7 Done → In progress\n"},
		{name: "test_reopen_already_in_progress", argv: []string{"--reopen", "7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("In progress"))}, want: "board: #7 is already In progress\n"},
		{name: "test_reopen_other_status_fails[To do]", argv: []string{"--reopen", "7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("To do"))}, want: "board: ERROR — #7 is at To do, not at Done\n", code: 1},
		{name: "test_reopen_other_status_fails[On hold]", argv: []string{"--reopen", "7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("On hold"))}, want: "board: ERROR — #7 is at On hold, not at Done\n", code: 1},
		{name: "test_reopen_other_status_fails[Controller]", argv: []string{"--reopen", "7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("Controller"))}, want: "board: ERROR — #7 is at Controller, not at Done\n", code: 1},
		{name: "test_reopen_other_status_fails[None]", argv: []string{"--reopen", "7"}, config: boardConfig,
			calls: []call{issueItem(itemAt(""))}, want: "board: ERROR — #7 is without status, not at Done\n", code: 1},
		{name: "test_reopen_api_error_fails", argv: []string{"--reopen", "7"}, config: boardConfig,
			calls: []call{{want: "IssueItem 7", err: errAPI}},
			want:  "board: ERROR — forge POST https://api.github.com/graphql: 502 Bad Gateway\n", code: 1},
		{name: "test_reopen_edit_error_fails", argv: []string{"--reopen", "7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("Done")), {want: "SetStatus PVTI_a In progress",
				err: fmt.Errorf("forge POST https://api.github.com/graphql: %w", context.DeadlineExceeded)}},
			want: "board: ERROR — forge POST https://api.github.com/graphql: context deadline exceeded\n", code: 1},
		{name: "test_reopen_read_back_mismatch_fails", argv: []string{"--reopen", "7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("Done")), setsInProgress, issueItem(itemAt("Done"))},
			want:  "board: ERROR — #7 is at Done after the edit\n", code: 1},
		{name: "test_reopen_not_on_board", argv: []string{"--reopen", "7"}, config: boardConfig,
			calls: []call{{want: "IssueItem 7", ok: false}}, want: "board: ERROR — #7 is not on the board\n", code: 1},
		{name: "test_reopen_invalid_argument[--reopen]", argv: []string{"--reopen"}, config: boardConfig,
			want: "board: usage: lighthouse board-start --reopen <nr>\n", code: 1},
		{name: "test_reopen_invalid_argument[--reopen-x]", argv: []string{"--reopen", "x"}, config: boardConfig,
			want: "board: usage: lighthouse board-start --reopen <nr>\n", code: 1},
		{name: "test_reopen_invalid_argument[--reopen-1-2]", argv: []string{"--reopen", "1", "2"}, config: boardConfig,
			want: "board: usage: lighthouse board-start --reopen <nr>\n", code: 1},
		// Owner, name and board IDs reach the forge through the opener (repoconfig), not
		// through board-start: TestBoardStartOpensTheForgeOfLighthouseToml in internal/cli.
		{name: "test_other_repo_sends_own_owner_name_and_board", argv: []string{"7"}, config: otherRepo,
			calls: []call{issueItem(itemAt("To do")), setsInProgress},
			want:  "board: #7 To do → In progress\n"},
		{name: "test_no_board_default_mode", argv: []string{"7"}, config: "[forge]\nrepo = \"Owner/repo\"\n",
			want: "board: skipped — no [forge.board] configured\n"},
		{name: "test_no_board_reopen", argv: []string{"--reopen", "7"}, config: "[project]\nlanguage = \"en\"\n",
			want: "board: skipped — no [forge.board] configured\n"},
		{name: "test_runs_as_installed_file", argv: []string{"7"}, config: "[forge]\nrepo = \"Owner/repo\"\n",
			want: "board: skipped — no [forge.board] configured\n"},
		// The reply's JSON is the forge's to parse; a reply with a trailing newline is an
		// item like any other.
		{name: "test_graphql_reply_with_trailing_newline", argv: []string{"7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("To do")), setsInProgress},
			want:  "board: #7 To do → In progress\n"},
		{name: "test_edit_failure_does_not_block", argv: []string{"7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("To do")), {want: "SetStatus PVTI_a In progress", err: errAPI}},
			want:  "board: skipped (forge POST https://api.github.com/graphql: 502 Bad Gateway)\n"},
		{name: "test_invalid_json_is_skipped", argv: []string{"7"}, config: boardConfig,
			calls: []call{{want: "IssueItem 7",
				err: errors.New("forge POST https://api.github.com/graphql: decode: invalid character 'o' in literal null (expecting 'u')")}},
			want: "board: skipped (forge POST https://api.github.com/graphql: decode: invalid character 'o' in literal null (expecting 'u'))\n"},
		// GitHub's reply without data, or with repository null, is ErrIssueNotFound in the forge.
		{name: "test_reply_without_data_is_skipped", argv: []string{"7"}, config: boardConfig,
			calls: []call{{want: "IssueItem 7", err: fmt.Errorf("forge: issue Owner/repo#7: %w", forge.ErrIssueNotFound)}},
			want:  "board: skipped (forge: issue Owner/repo#7: issue not found)\n"},
		{name: "test_reply_with_null_repository_is_skipped", argv: []string{"7"}, config: boardConfig,
			calls: []call{{want: "IssueItem 7", err: fmt.Errorf("forge: issue Owner/repo#7: %w", forge.ErrIssueNotFound)}},
			want:  "board: skipped (forge: issue Owner/repo#7: issue not found)\n"},
		{name: "test_number_is_sent_as_int", argv: []string{"007"}, config: boardConfig,
			calls: []call{issueItem(itemAt("Controller"))}, want: "board: #7 stays at Controller\n"},
		{name: "test_reopen_without_board_node_status_name_empty", argv: []string{"--reopen", "7"}, config: boardConfig,
			calls: []call{issueItem(itemAt(""))}, want: "board: ERROR — #7 is without status, not at Done\n", code: 1},
	} {
		t.Run(c.name, c.run)
	}
}

// TestBoardStartConfigErrors are the three config cases of cases.json: the path of the temp
// file is in the line, so they assert its fixed parts, one line and exit 2, as the fixture did.
func TestBoardStartConfigErrors(t *testing.T) {
	noRepo := strings.Replace(boardConfig, "repo = \"Owner/repo\"\n", "", 1)
	for _, tc := range []struct {
		name, config string
		argv, parts  []string
	}{
		{"test_broken_config_is_error[7]", "dit is geen toml [", []string{"7"},
			[]string{"board: ERROR — ConfigError: ", "lighthouse.toml"}},
		{"test_broken_config_is_error[--reopen-7]", "dit is geen toml [", []string{"--reopen", "7"},
			[]string{"board: ERROR — ConfigError: ", "lighthouse.toml"}},
		{"test_board_without_repo_is_an_error", noRepo, []string{"7"},
			[]string{"board: ERROR — ConfigError: ", "lighthouse.toml: [forge.board] needs forge.repo\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			open := func(context.Context) (forge.Forge, error) {
				t.Error("the forge was opened with a broken lighthouse.toml")
				return nil, errors.New("not opened")
			}
			var out bytes.Buffer
			code := Run(context.Background(), Env{Config: configIn(t, tc.config), Forge: open}, tc.argv, &out)
			line := out.String()
			if code != 2 || strings.Count(line, "\n") != 1 || !strings.HasPrefix(line, tc.parts[0]) || !strings.Contains(line, tc.parts[1]) {
				t.Errorf("Run(%v) = %d, %q; want exit 2 and one line with %q", tc.argv, code, line, tc.parts)
			}
		})
	}
}

// TestUsageLinesAreTheNewNames pins the one text that changed with the port from Python: the
// usage lines name the command, and keep the exit codes of the script.
func TestUsageLinesAreTheNewNames(t *testing.T) {
	for _, c := range []boardCase{
		{name: "no argument", want: "board: usage: lighthouse board-start <nr>\n"},
		{name: "not a number", argv: []string{"x"}, want: "board: usage: lighthouse board-start <nr>\n"},
		{name: "two numbers", argv: []string{"7", "8"}, want: "board: usage: lighthouse board-start <nr>\n"},
		{name: "negative", argv: []string{"-7"}, want: "board: usage: lighthouse board-start <nr>\n"},
		{name: "reopen alone", argv: []string{"--reopen"}, want: "board: usage: lighthouse board-start --reopen <nr>\n", code: 1},
		{name: "reopen two", argv: []string{"--reopen", "1", "2"}, want: "board: usage: lighthouse board-start --reopen <nr>\n", code: 1},
	} {
		c.config = boardConfig
		t.Run(c.name, c.run)
	}
}

// TestBoardStartStatusNameDecides: a board option that status_options does not list (say the
// On hold option was made anew, under another ID) decides on its raw name, as the script did:
// the item is still picked up, and a reopen still finds Done.
func TestBoardStartStatusNameDecides(t *testing.T) {
	for _, c := range []boardCase{
		{name: "On hold outside status_options is picked up", argv: []string{"7"}, config: boardConfig,
			calls: []call{issueItem(unmapped("opt-hold-new", "On hold")), setsInProgress},
			want:  "board: #7 On hold → In progress\n"},
		{name: "an unknown option stays at its raw name", argv: []string{"7"}, config: boardConfig,
			calls: []call{issueItem(unmapped("opt-review", "Review"))}, want: "board: #7 stays at Review\n"},
		{name: "reopen from Done outside status_options", argv: []string{"--reopen", "7"}, config: boardConfig,
			calls: []call{issueItem(unmapped("opt-done-new", "Done")), setsInProgress, issueItem(itemAt("In progress"))},
			want:  "board: #7 Done → In progress\n"},
		{name: "reopen refuses an unknown option by its raw name", argv: []string{"--reopen", "7"}, config: boardConfig,
			calls: []call{issueItem(unmapped("opt-review", "Review"))},
			want:  "board: ERROR — #7 is at Review, not at Done\n", code: 1},
		{name: "read back In progress outside status_options", argv: []string{"--reopen", "7"}, config: boardConfig,
			calls: []call{issueItem(itemAt("Done")), setsInProgress, issueItem(unmapped("opt-progress-new", "In progress"))},
			want:  "board: #7 Done → In progress\n"},
	} {
		t.Run(c.name, c.run)
	}
}

// TestBoardStartTokenErrorExit0: a forge that cannot open for want of a token is no reason to
// block picking up an issue (exit 0); a reopen stays fail-closed (exit 1).
func TestBoardStartTokenErrorExit0(t *testing.T) {
	tokenErr := &forge.TokenError{Kind: "github", Vars: []string{"GH_TOKEN", "GITHUB_TOKEN"}}
	for _, c := range []boardCase{
		{name: "default mode", argv: []string{"7"}, config: boardConfig, openErr: tokenErr,
			want: "board: skipped (no GitHub token: set GH_TOKEN or GITHUB_TOKEN)\n"},
		{name: "reopen", argv: []string{"--reopen", "7"}, config: boardConfig, openErr: tokenErr,
			want: "board: ERROR — no GitHub token: set GH_TOKEN or GITHUB_TOKEN\n", code: 1},
		{name: "another open error", argv: []string{"7"}, config: boardConfig,
			openErr: errors.New("read /x/lighthouse.toml: no such file"),
			want:    "board: skipped (read /x/lighthouse.toml: no such file)\n"},
	} {
		t.Run(c.name, c.run)
	}
}

// TestBoardStartErrorIsOneLine: board-start prints exactly one line, also for a forge error
// over several lines (errors.Join).
func TestBoardStartErrorIsOneLine(t *testing.T) {
	joined := errors.Join(errors.New("first"), errors.New("second"))
	for _, c := range []boardCase{
		{name: "default mode", argv: []string{"7"}, config: boardConfig,
			calls: []call{{want: "IssueItem 7", err: joined}}, want: "board: skipped (first; second)\n"},
		{name: "reopen", argv: []string{"--reopen", "7"}, config: boardConfig,
			calls: []call{{want: "IssueItem 7", err: joined}}, want: "board: ERROR — first; second\n", code: 1},
	} {
		t.Run(c.name, c.run)
	}
}

// githubForge is the real GitHub client against a forgetest server whose board is PVT_test and
// whose issue 7 is on it at the board option optionID named name ("" none), plus its server.
func githubForge(t *testing.T, optionID, name string) (forge.Forge, *forgetest.Server) {
	t.Helper()
	srv := forgetest.New(t)
	srv.ProjectID = "PVT_test"
	values := []any{}
	if optionID != "" {
		values = append(values, map[string]any{"optionId": optionID, "name": name, "field": map[string]any{"id": "FIELD_test"}})
	}
	srv.BoardNodes = []map[string]any{{"id": "PVTI_a", "fieldValues": map[string]any{"nodes": values},
		"content": map[string]any{"number": 7}}}
	board := &repoconfig.Board{ProjectID: "PVT_test", StatusFieldID: "FIELD_test", StatusOptions: map[string]string{
		"To do": "opt-todo", "In progress": "opt-progress", "Done": "opt-done"}}
	f, err := forge.Open(forge.Config{Repo: "Owner/repo", Token: "t", BaseURL: srv.URL(), Board: board})
	if err != nil {
		t.Fatal(err)
	}
	return f, srv
}

// TestBoardStartIssueNotFound runs the real GitHub client: an issue that does not exist is the
// forge's ErrIssueNotFound, a skip in the default mode and an error on a reopen.
func TestBoardStartIssueNotFound(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want string
		code int
	}{
		{[]string{"99"}, "board: skipped (forge: issue Owner/repo#99: issue not found)\n", 0},
		{[]string{"--reopen", "99"}, "board: ERROR — forge: issue Owner/repo#99: issue not found\n", 1},
	} {
		f, srv := githubForge(t, "opt-todo", "To do")
		open := func(context.Context) (forge.Forge, error) { return f, nil }
		var out bytes.Buffer
		code := Run(context.Background(), Env{Config: configIn(t, boardConfig), Forge: open}, tc.argv, &out)
		if code != tc.code || out.String() != tc.want {
			t.Errorf("Run(%v) = %d, %q; want %d, %q", tc.argv, code, out.String(), tc.code, tc.want)
		}
		if len(srv.StatusSets) != 0 {
			t.Errorf("Run(%v) set a status: %v", tc.argv, srv.StatusSets)
		}
	}
}

// TestBoardStartOnGitHubForge runs the real GitHub client: board-start sets the option ID that
// status_options gives "In progress" on the item of issue 7, and decides an option that
// status_options does not list on the name the board gives it.
func TestBoardStartOnGitHubForge(t *testing.T) {
	for _, tc := range []struct {
		name, optionID, optionName, want string
		sets                             int
	}{
		{"mapped To do", "opt-todo", "To do", "board: #7 To do → In progress\n", 1},
		{"unmapped On hold", "opt-hold", "On hold", "board: #7 On hold → In progress\n", 1},
		{"unmapped Review", "opt-review", "Review", "board: #7 stays at Review\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, srv := githubForge(t, tc.optionID, tc.optionName)
			open := func(context.Context) (forge.Forge, error) { return f, nil }
			var out bytes.Buffer
			code := Run(context.Background(), Env{Config: configIn(t, boardConfig), Forge: open}, []string{"7"}, &out)
			if code != 0 || out.String() != tc.want {
				t.Errorf("Run = %d, %q; want 0, %q", code, out.String(), tc.want)
			}
			if len(srv.StatusSets) != tc.sets {
				t.Fatalf("StatusSets = %v; want %d", srv.StatusSets, tc.sets)
			}
			if tc.sets == 1 {
				got := srv.StatusSets[0]
				if got["project"] != "PVT_test" || got["item"] != "PVTI_a" || got["field"] != "FIELD_test" || got["option"] != "opt-progress" {
					t.Errorf("SetStatus variables = %v; want project PVT_test, item PVTI_a, field FIELD_test, option opt-progress", got)
				}
			}
		})
	}
}
