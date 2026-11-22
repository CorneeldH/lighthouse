package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/store"
)

const stateLabels = "\n[labels]\nneeds_shaping = \"3-needs-shaping\"\nneeds_hands = \"3-needs-hands\"\nneeds_testing = \"3-needs-testing\"\n"

// stateToml writes a lighthouse.toml for Owner/repo with extra appended; board adds a GitHub
// board PVT_test whose To do option is opt-todo.
func stateToml(t *testing.T, extra string, board bool) {
	t.Helper()
	text := "[forge]\nrepo = \"Owner/repo\"\n" + extra
	if board {
		text += "\n[forge.board]\nproject_number = 1\nproject_id = \"PVT_test\"\nstatus_field_id = \"FIELD_test\"\n\n" +
			"[forge.board.status_options]\n\"To do\" = \"opt-todo\"\n\"In progress\" = \"opt-progress\"\nDone = \"opt-done\"\n" +
			"\"On hold\" = \"opt-hold\"\nController = \"opt-ctrl\"\n"
	}
	toml := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(toml, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", toml)
}

// stateServer is a fake GitHub with open issue 7 carrying labels, on the board at status
// (option ID and name) when status is not "".
func stateServer(t *testing.T, labels []string, optionID, status string) *forgetest.Server {
	t.Helper()
	srv := forgetest.New(t)
	srv.ProjectID = "PVT_test"
	var ls []any
	for _, l := range labels {
		ls = append(ls, map[string]any{"name": l})
	}
	srv.Issues = map[int]map[string]any{7: {"number": 7, "title": "x", "state": "open", "labels": ls}}
	srv.BlockedBy = map[int][]map[string]any{7: {}}
	if status != "" {
		srv.BoardNodes = []map[string]any{{"id": "PVTI_a", "content": map[string]any{"number": 7},
			"fieldValues": map[string]any{"nodes": []any{map[string]any{"optionId": optionID, "name": status,
				"field": map[string]any{"id": "FIELD_test"}}}}}}
	}
	return srv
}

// runState runs `lighthouse state args...` against srv with runs as the run reader.
func runState(t *testing.T, srv *forgetest.Server, runs []dispatch.RunView, args ...string) (int, string, string) {
	t.Helper()
	o := newForgeOpener(func(string) string { return "" }, srv.URL())
	o.ghToken = func() string { return "gh-token" }
	cmd := newStateCommandWith(o, func(_ context.Context, repo string, nr int) (string, error) {
		for _, r := range runs {
			if r.Repo == repo && r.Issue == nr {
				return r.State, nil
			}
		}
		return "", nil
	})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true // as under the root command
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	code := execute(cmd, &errOut)
	return code, out.String(), errOut.String()
}

// stateWrites is the method and path of every write request the server got.
func stateWrites(srv *forgetest.Server) []string {
	var out []string
	for _, p := range srv.Posts {
		if p.Path != "/graphql" {
			out = append(out, p.Method+" "+p.Path)
		}
	}
	return out
}

func TestStateShow(t *testing.T) {
	for _, tc := range []struct {
		name             string
		labels           []string
		optionID, status string
		blockers         []map[string]any
		runs             []dispatch.RunView
		want             string
	}{
		{name: "In progress is active", optionID: "opt-progress", status: "In progress", want: "state: #7 active\n"},
		{name: "open blocker beats needs_hands", labels: []string{"3-needs-hands"}, optionID: "opt-todo", status: "To do",
			blockers: []map[string]any{{"number": 5, "state": "open"}}, want: "state: #7 blocked\n"},
		{name: "parked run is active", optionID: "opt-todo", status: "To do",
			runs: []dispatch.RunView{{Repo: "Owner/repo", Issue: 8, State: "open"}, {Repo: "Owner/repo", Issue: 7, State: "parked"}},
			want: "state: #7 active\n"},
		{name: "active with needs_hands needs a human", labels: []string{"3-needs-hands"}, optionID: "opt-ctrl", status: "Controller",
			want: "state: #7 active, needs human\n"},
		{name: "not on the board", labels: []string{"3-needs-shaping"}, want: "state: #7 needs_human\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stateToml(t, stateLabels, true)
			srv := stateServer(t, tc.labels, tc.optionID, tc.status)
			if tc.blockers != nil {
				for _, b := range tc.blockers {
					b["repository_url"] = srv.URL() + "/repos/Owner/repo"
				}
				srv.BlockedBy[7] = tc.blockers
			}
			code, out, errOut := runState(t, srv, tc.runs, "show", "7")
			if code != 0 || out != tc.want || errOut != "" {
				t.Errorf("state show 7 = %d, %q, stderr %q; want 0, %q", code, out, errOut, tc.want)
			}
			if w := stateWrites(srv); len(w) != 0 {
				t.Errorf("writes = %v, want none", w)
			}
		})
	}
}

func TestStateLabelReplacesTheHigherRank(t *testing.T) {
	stateToml(t, stateLabels, true)
	srv := stateServer(t, []string{"3-needs-testing", "2-area:x"}, "opt-todo", "To do")
	code, out, errOut := runState(t, srv, nil, "label", "7", "needs_shaping")
	if want := "label: #7 3-needs-shaping (+3-needs-shaping, -3-needs-testing)\n"; code != 0 || out != want || errOut != "" {
		t.Errorf("state label = %d, %q, stderr %q; want 0, %q", code, out, errOut, want)
	}
	want := []string{"POST /repos/Owner/repo/issues/7/labels", "DELETE /repos/Owner/repo/issues/7/labels/3-needs-testing"}
	if got := stateWrites(srv); !reflect.DeepEqual(got, want) {
		t.Errorf("writes = %v, want %v", got, want)
	}
	if got := srv.Posts[0].Body["labels"]; !reflect.DeepEqual(got, []any{"3-needs-shaping"}) {
		t.Errorf("POST labels = %v, want [3-needs-shaping]", got)
	}
}

func TestStateLabelKeepsTheLowerRank(t *testing.T) {
	stateToml(t, stateLabels, true)
	srv := stateServer(t, []string{"3-needs-shaping"}, "opt-todo", "To do")
	code, out, _ := runState(t, srv, nil, "label", "7", "needs_hands")
	if want := "label: #7 keeps 3-needs-shaping\n"; code != 0 || out != want {
		t.Errorf("state label = %d, %q; want 0, %q", code, out, want)
	}
	if w := stateWrites(srv); len(w) != 0 {
		t.Errorf("writes = %v, want none", w)
	}
}

func TestStateRefusesBeforeTheForge(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra string
		args  []string
		code  int
		out   string // prefix of the one line on stdout
	}{
		{"unknown key", stateLabels, []string{"label", "7", "needs-hands"}, 1, "state: usage: "},
		{"unknown key on return", stateLabels, []string{"return", "7", "blocked"}, 1, "state: usage: "},
		{"not a number", stateLabels, []string{"show", "x7"}, 1, "state: usage: "},
		{"empty label name in lighthouse.toml", "\n[labels]\nneeds_testing = \"\"\n", []string{"label", "7", "needs_hands"}, 2,
			"state: ERROR — ConfigError: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stateToml(t, tc.extra, true)
			srv := stateServer(t, nil, "opt-todo", "To do")
			code, out, _ := runState(t, srv, nil, tc.args...)
			if code != tc.code || !strings.HasPrefix(out, tc.out) || strings.Count(out, "\n") != 1 {
				t.Errorf("state %v = %d, %q; want %d and one line starting %q", tc.args, code, out, tc.code, tc.out)
			}
			if len(srv.Posts) != 0 || srv.Count("/repos/Owner/repo/issues/7") != 0 {
				t.Errorf("forge requests: posts %v, issue reads %d; want none", srv.Posts, srv.Count("/repos/Owner/repo/issues/7"))
			}
		})
	}
}

func TestStateReturnFromController(t *testing.T) {
	stateToml(t, stateLabels, true)
	srv := stateServer(t, nil, "opt-ctrl", "Controller")
	code, out, errOut := runState(t, srv, nil, "return", "7", "needs_hands")
	want := "label: #7 3-needs-hands (+3-needs-hands)\nboard: #7 Controller → To do\n"
	if code != 0 || out != want || errOut != "" {
		t.Errorf("state return = %d, %q, stderr %q; want 0, %q", code, out, errOut, want)
	}
	if len(srv.StatusSets) != 1 || srv.StatusSets[0]["option"] != "opt-todo" || srv.StatusSets[0]["item"] != "PVTI_a" {
		t.Errorf("StatusSets = %v, want one to opt-todo on PVTI_a", srv.StatusSets)
	}
}

func TestStateReturnBoardLines(t *testing.T) {
	for _, tc := range []struct {
		name             string
		board            bool
		optionID, status string
		want             string
	}{
		{"already To do", true, "opt-todo", "To do", "board: #7 stays To do\n"},
		{"not on the board", true, "", "", "board: #7 is not on the board\n"},
		{"no board configured", false, "opt-ctrl", "Controller", "board: skipped — no [forge.board] configured\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stateToml(t, stateLabels, tc.board)
			srv := stateServer(t, nil, tc.optionID, tc.status)
			code, out, errOut := runState(t, srv, nil, "return", "7", "needs_hands")
			want := "label: #7 3-needs-hands (+3-needs-hands)\n" + tc.want
			if code != 0 || out != want || errOut != "" {
				t.Errorf("state return = %d, %q, stderr %q; want 0, %q", code, out, errOut, want)
			}
			if len(srv.StatusSets) != 0 {
				t.Errorf("StatusSets = %v, want none", srv.StatusSets)
			}
		})
	}
}

func TestStateLabelHalfWriteAsksToRunAgain(t *testing.T) {
	stateToml(t, stateLabels, true)
	srv := stateServer(t, []string{"3-needs-testing"}, "opt-todo", "To do")
	srv.Fail = map[string]int{"DELETE /repos/Owner/repo/issues/7/labels/3-needs-testing": 500}
	code, out, _ := runState(t, srv, nil, "label", "7", "needs_shaping")
	if code != 1 || !strings.HasPrefix(out, "state: ERROR — ") || !strings.HasSuffix(out, "; run it again to finish the label edit\n") {
		t.Errorf("state label = %d, %q; want 1 and an ERROR line that asks to run it again", code, out)
	}
}

// The default run reader opens the database without the serve lock: it works next to a serve.
func TestStateRunsOpenNextToAServe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lighthouse.db")
	st, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if _, err := dbRunState(path)(context.Background(), "Owner/repo", 7); err != nil {
		t.Errorf("dbRunState next to an open store: %v", err)
	}
}

// Red when Show stops writing "needs human" for an open 🤖 Vraag on an active issue.
func TestStateShowReportsOpenQuestionNeedsHuman(t *testing.T) {
	stateToml(t, stateLabels, true)
	srv := stateServer(t, nil, "opt-ctrl", "Controller")
	srv.Comments = map[int][]map[string]any{7: {{"id": 1, "body": "🤖 Vraag\n\nWelke kant op?", "user": map[string]any{"login": "lh-bot"}}}}
	code, out, errOut := runState(t, srv, nil, "show", "7")
	if want := "state: #7 active, needs human\n"; code != 0 || out != want || errOut != "" {
		t.Errorf("state show 7 = %d, %q, stderr %q; want 0, %q", code, out, errOut, want)
	}
}

// Red when a missing token stops being exit 2 (as forgeOpenWith gives for forge commands).
func TestStateMissingTokenExitCode(t *testing.T) {
	stateToml(t, stateLabels, true)
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	srv := stateServer(t, nil, "opt-todo", "To do")
	o := newForgeOpener(func(string) string { return "" }, srv.URL())
	cmd := newStateCommandWith(o, func(context.Context, string, int) (string, error) { return "", nil })
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"show", "7"})
	if code := execute(cmd, &errOut); code != 2 {
		t.Errorf("state show without token = %d, %q; want 2", code, out.String())
	}
}

// Red when dbRunState goes back to store.OpenShared: that creates and migrates the database.
func TestDBRunStateDoesNotCreateAMissingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "lighthouse.db")
	got, err := dbRunState(path)(context.Background(), "Owner/repo", 7)
	if err != nil || got != "" {
		t.Errorf("dbRunState on a missing database = %q, %v; want \"\", nil", got, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("stat %s: %v; want the directory not to exist", filepath.Dir(path), err)
	}
}
