package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
)

// TestBoardStartTakesRawArgumentsAndExitCodes runs the real command: the arguments `<nr>` and
// `--reopen <nr>` reach boardstart.Run unparsed, and its exit code is the process exit code.
func TestBoardStartTakesRawArgumentsAndExitCodes(t *testing.T) {
	toml := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(toml, []byte("[forge]\nrepo = \"Owner/repo\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", toml)
	for _, tc := range []struct {
		args []string
		out  string
		code int
	}{
		{[]string{"board-start", "7"}, "board: skipped — no [forge.board] configured\n", 0},
		{[]string{"board-start", "--reopen", "7"}, "board: skipped — no [forge.board] configured\n", 0},
		{[]string{"board-start", "--reopen"}, "board: usage: lighthouse board-start --reopen <nr>\n", 1},
		{[]string{"board-start", "--help"}, "board: usage: lighthouse board-start <nr>\n", 0},
	} {
		var out, errOut bytes.Buffer
		root := NewRoot(strings.NewReader(""), &out, &errOut)
		root.SetArgs(tc.args)
		if code := execute(root, &errOut); code != tc.code || out.String() != tc.out || errOut.Len() != 0 {
			t.Errorf("%v = %d, %q, stderr %q; want %d, %q", tc.args, code, out.String(), errOut.String(), tc.code, tc.out)
		}
	}
}

// boardStartToml is a lighthouse.toml with a GitHub board PVT_test, extra appended under [forge].
func boardStartToml(t *testing.T, extra string) {
	t.Helper()
	toml := filepath.Join(t.TempDir(), "lighthouse.toml")
	text := "[forge]\nrepo = \"Owner/repo\"\n" + extra + "\n[forge.board]\nproject_number = 1\nproject_id = \"PVT_test\"\n" +
		"status_field_id = \"FIELD_test\"\n\n[forge.board.status_options]\n\"To do\" = \"opt-todo\"\n\"In progress\" = \"opt-progress\"\nDone = \"opt-done\"\n\"On hold\" = \"opt-hold\"\n"
	if err := os.WriteFile(toml, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", toml)
}

// TestBoardStartOpensTheForgeOfLighthouseToml runs board-start against a fake GitHub: the forge
// is the repo and board of LIGHTHOUSE_TOML, with the token of `gh auth token` when no variable
// holds one, and the token variable of [forge] token_env when it is set.
func TestBoardStartOpensTheForgeOfLighthouseToml(t *testing.T) {
	for _, tc := range []struct {
		name, extra, ghToken, want string
		sets                       int
	}{
		{"gh auth token", "", "gh-token", "board: #7 To do → In progress\n", 1},
		{"no token at all", "", "", "board: skipped (no GitHub token: set GH_TOKEN or GITHUB_TOKEN)\n", 0},
		{"token_env names the variable", "token_env = \"MY_TOKEN\"", "", "board: skipped (no GitHub token: set MY_TOKEN)\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			boardStartToml(t, tc.extra)
			srv := forgetest.New(t)
			srv.ProjectID = "PVT_test"
			srv.BoardNodes = []map[string]any{{"id": "PVTI_a", "content": map[string]any{"number": 7, "repository": map[string]any{"nameWithOwner": "Owner/repo"}},
				"fieldValues": map[string]any{"nodes": []any{map[string]any{"optionId": "opt-todo", "name": "To do",
					"field": map[string]any{"id": "FIELD_test"}}}}}}
			o := newForgeOpener(func(string) string { return "" }, srv.URL())
			o.ghToken = func() string { return tc.ghToken }
			cmd := newBoardStartCommandWith(o)
			cmd.SilenceUsage, cmd.SilenceErrors = true, true // as under the root command
			var out, errOut bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs([]string{"7"})
			if code := execute(cmd, &errOut); code != 0 || out.String() != tc.want || errOut.Len() != 0 {
				t.Errorf("board-start 7 = %d, %q, stderr %q; want 0, %q", code, out.String(), errOut.String(), tc.want)
			}
			if len(srv.StatusSets) != tc.sets {
				t.Fatalf("StatusSets = %v; want %d", srv.StatusSets, tc.sets)
			}
			if tc.sets == 0 {
				return
			}
			if got := srv.StatusSets[0]; got["project"] != "PVT_test" || got["item"] != "PVTI_a" || got["option"] != "opt-progress" {
				t.Errorf("SetStatus variables = %v; want project PVT_test, item PVTI_a, option opt-progress", got)
			}
			// The item of Owner/repo#7 comes from the REST board of owner Owner, without GraphQL.
			if n, gql := srv.Count("/users/Owner/projectsV2/1/items"), srv.GraphQLCount(); n != 1 || gql != 0 {
				t.Errorf("board item reads = %d, GraphQL requests = %d; want 1 and 0", n, gql)
			}
		})
	}
}
