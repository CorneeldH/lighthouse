package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

// triageToml is a dispatchable repo with [controller.triage], the labels of lighthouse and the
// board options triage reads and sets.
const triageToml = `[project]
language = "nl"

[forge]
repo = "o/r"

[forge.board]
project_number = 3
project_id = "PVT_x"
status_field_id = "SF"
priority_field_id = "PF"

[forge.board.status_options]
"To do" = "t1"
"In progress" = "p1"
"Controller" = "c1"
"On hold" = "h1"

[forge.board.priority_options]
P0 = "q0"
P1 = "q1"

[labels]
blocked_issue = "3-blocked-issue"
blocked_external = "3-blocked-external"
needs_shaping = "3-needs-shaping"
needs_decision = "3-needs-decision"
needs_hands = "3-needs-hands"
needs_testing = "3-needs-testing"
area_prefix = "2-area:"

[controller]

[controller.triage]

[usage_guard]
controller = true
`

// withTriage puts lines under [controller.triage] of triageToml.
func withTriage(lines string) string {
	return strings.Replace(triageToml, "[controller.triage]\n", "[controller.triage]\n"+lines, 1)
}

type triageFix struct {
	env   triageEnv
	db    string
	srv   *forgetest.Server
	dir   string // the main checkout
	judge *fakeJudge
	brake *fakeBrake
}

// gitIn runs git in dir and returns its trimmed output.
func gitIn(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %v: %w\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitIn(dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// triageFixture is a main checkout with toml as lighthouse.toml, a bare origin with main pushed,
// a host.toml listing the checkout, a fake GitHub with token login lh-bot (lh-bot and alice are
// admin), a fake judge, an open brake and a superpowers plugin with one skill.
func triageFixture(t *testing.T, toml string) *triageFix {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
		"GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_SYSTEM": "/dev/null",
	} {
		t.Setenv(k, v)
	}
	root := t.TempDir()
	bare, dir := filepath.Join(root, "origin.git"), filepath.Join(root, "work")
	mustGit(t, root, "init", "-q", "--bare", "-b", "main", bare)
	mustGit(t, root, "init", "-q", "-b", "main", dir)
	mustGit(t, dir, "remote", "add", "origin", bare)
	if err := os.WriteFile(filepath.Join(dir, "lighthouse.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "lighthouse.toml")
	mustGit(t, dir, "commit", "-q", "-m", "init (#3)")
	mustGit(t, dir, "push", "-q", "origin", "main")

	tenv, db := tickFixture(t, dir)
	srv := fakeForge(t, &tenv)
	srv.Roles = map[string]string{"lh-bot": "admin", "alice": "admin"}
	srv.ProjectID = "PVT_x"
	srv.Issues = map[int]map[string]any{}

	plugin := t.TempDir()
	skill := filepath.Join(plugin, "skills", "writing-plans")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: writing-plans\ndescription: Plans.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plugins := filepath.Join(t.TempDir(), "installed_plugins.json")
	data := fmt.Sprintf(`{"plugins": {"superpowers@x": [{"scope": "user", "installPath": %q}]}}`, plugin)
	if err := os.WriteFile(plugins, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	fx := &triageFix{db: db, srv: srv, dir: dir, brake: &fakeBrake{closeAfter: -1}}
	fx.judge = &fakeJudge{t: t, dir: dir, answers: map[int]string{}, prompts: map[int]string{}}
	fx.env = triageEnv{tickEnv: tenv, Judge: fx.judge.judge, Brake: fx.brake, PluginsFile: plugins}
	return fx
}

func (fx *triageFix) run(opts triageOpts) (out, errOut string, err error) {
	var o, e bytes.Buffer
	err = triage(context.Background(), fx.env, opts, fx.db, &o, &e)
	return o.String(), e.String(), err
}

// issue adds an open issue of alice in open milestone 1 to the fake; mod may change it.
func (fx *triageFix) issue(n int, labels ...string) map[string]any {
	ls := []any{}
	for _, l := range labels {
		ls = append(ls, map[string]any{"name": l})
	}
	it := map[string]any{"number": n, "title": "Issue " + strconv.Itoa(n), "state": "open", "body": "Body.",
		"user": map[string]any{"login": "alice"}, "labels": ls, "updated_at": "2026-10-30T10:00:00Z",
		"node_id": "I_" + strconv.Itoa(n), "html_url": "https://github.com/o/r/issues/" + strconv.Itoa(n),
		"milestone": map[string]any{"number": 1, "title": "M2", "state": "open", "description": "Doel."}}
	fx.srv.Issues[n] = it
	return it
}

// onBoard puts issue n on the board as item PVTI_<n> with status option status.
func (fx *triageFix) onBoard(n int, status string, extra map[string]any) {
	fx.srv.BoardNodes = append(fx.srv.BoardNodes,
		boardNode("PVTI_"+strconv.Itoa(n), status, issueContent(n, "Issue "+strconv.Itoa(n), extra)))
}

// fakeJudge answers a fixed structured_output per issue number and records each call.
type fakeJudge struct {
	t       *testing.T
	dir     string
	mu      sync.Mutex
	answers map[int]string
	errs    map[int]error // a failed judgment instead of an answer
	calls   []int
	prompts map[int]string
}

var promptIssue = regexp.MustCompile(`(?m)^issue number: #([0-9]+)$`)

func (j *fakeJudge) judge(_ context.Context, dir, prompt string) ([]byte, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if dir != j.dir {
		j.t.Errorf("judge dir = %q, want the main checkout %q", dir, j.dir)
	}
	m := promptIssue.FindStringSubmatch(prompt)
	if m == nil {
		j.t.Errorf("prompt names no issue number:\n%s", prompt)
		return nil, errors.New("no issue")
	}
	n, _ := strconv.Atoi(m[1])
	j.calls = append(j.calls, n)
	j.prompts[n] = prompt
	if err := j.errs[n]; err != nil {
		return nil, err
	}
	ans, ok := j.answers[n]
	if !ok {
		j.t.Errorf("unexpected judgment of #%d", n)
		return nil, errors.New("no answer")
	}
	return []byte(`{"type":"result","is_error":false,"structured_output":` + ans + `}`), nil
}

func (j *fakeJudge) called() []int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]int{}, j.calls...)
}

// answer is a judgment: no redundancy, headless, without design questions.
func answer(route string, gate int, sub string) string {
	return answerWith(route, gate, sub, nil)
}

func answerWith(route string, gate int, sub string, extra map[string]any) string {
	m := map[string]any{"route_line": route, "gate": gate, "sub": sub, "redundancy": "none",
		"redundancy_issue": 0, "redundancy_evidence": "", "headless": true, "headless_reason": "",
		"design_questions": []any{}}
	for k, v := range extra {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func forward(n int) string {
	return answer("#"+strconv.Itoa(n)+" Poort 4b: writing-plans — plan ligt klaar", 4, "b")
}

func stopAt1(n int) string {
	return answer("#"+strconv.Itoa(n)+" Poort 1: lighthouse-write-issue — recept bestaat", 1, "")
}

// fakeBrake is open until closeAfter checks have passed; a negative closeAfter never closes.
type fakeBrake struct {
	closeAfter int
	calls      int
	reason     string
}

func (b *fakeBrake) Check() (string, bool) {
	b.calls++
	if b.closeAfter >= 0 && b.calls > b.closeAfter {
		return b.reason, false
	}
	return "", true
}

// triageWrites are the writes the round sent, in order: "comment <n>: <first line>",
// "AddToBoard <content>" and "SetStatus <item> <option>". A failed POST is in it too.
func triageWrites(srv *forgetest.Server) []string {
	var out []string
	for _, p := range srv.Posts {
		if p.Path == "/graphql" {
			q, _ := p.Body["query"].(string)
			vars, _ := p.Body["variables"].(map[string]any)
			switch {
			case strings.HasPrefix(q, "mutation SetStatus"):
				out = append(out, fmt.Sprintf("SetStatus %v %v", vars["item"], vars["option"]))
			case strings.HasPrefix(q, "mutation AddToBoard"):
				out = append(out, fmt.Sprintf("AddToBoard %v", vars["content"]))
			case strings.HasPrefix(q, "mutation"):
				out = append(out, "mutation "+q)
			}
			continue
		}
		parts := strings.Split(p.Path, "/") // /repos/o/r/issues/7/comments
		body, _ := p.Body["body"].(string)
		first, _, _ := strings.Cut(body, "\n")
		if len(parts) == 7 && parts[6] == "comments" {
			out = append(out, "comment "+parts[5]+": "+first)
		} else {
			out = append(out, "POST "+p.Path)
		}
	}
	return out
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return -1
}

func summary(c, j, f, d, o, inv, w, s, e int) string {
	return fmt.Sprintf("lighthouse: triage o/r: candidates %d, judgments %d, forwarded %d, design %d, other %d, invalid %d, running work %d, skipped %d, errors %d\n",
		c, j, f, d, o, inv, w, s, e)
}

const (
	marker4b = `<!-- lighthouse: {"kind":"triage","version":1,"gate":"4b"} -->`
	robotHO  = "🤖 Handoff"
)

func assertWrites(t *testing.T, srv *forgetest.Server, want ...string) {
	t.Helper()
	if got := triageWrites(srv); !slices.Equal(got, want) {
		t.Errorf("writes:\n got %q\nwant %q", got, want)
	}
}

func TestTriageForwardsWithNightHandoff(t *testing.T) {
	fx := triageFixture(t, triageToml)
	fx.issue(7, "2-area:cli", "3-needs-hands")
	fx.onBoard(7, "t1", nil)
	fx.judge.answers[7] = forward(7)

	_, errOut, err := fx.run(triageOpts{})
	if err != nil {
		t.Fatalf("triage = %v\n%s", err, errOut)
	}
	assertWrites(t, fx.srv, "comment 7: "+marker4b, "comment 7: "+robotHO, "SetStatus PVTI_7 c1")
	posted := bodies(fx.srv, 7)
	if len(posted) == 2 {
		want := "🤖 Handoff\nissue: #7\nworktree: " + fx.dir + "\nbranch: main\nnext: /lighthouse-start-issue 7\nwhen: night"
		if posted[1] != want {
			t.Errorf("handoff = %q, want %q", posted[1], want)
		}
	}
	if errOut != summary(1, 1, 1, 0, 0, 0, 0, 0, 0) {
		t.Errorf("errOut = %q", errOut)
	}
}

func TestTriageAddsIssueToBoard(t *testing.T) {
	fx := triageFixture(t, triageToml)
	fx.issue(7, "2-area:cli")
	fx.judge.answers[7] = forward(7)

	if _, errOut, err := fx.run(triageOpts{}); err != nil {
		t.Fatalf("triage = %v\n%s", err, errOut)
	}
	assertWrites(t, fx.srv, "comment 7: "+marker4b, "comment 7: "+robotHO, "AddToBoard I_7", "SetStatus item-I_7 c1")
}

func TestTriageNoWaitForNight(t *testing.T) {
	fx := triageFixture(t, withTriage("wait_for_night = false\n"))
	fx.issue(7, "2-area:cli")
	fx.onBoard(7, "t1", nil)
	fx.judge.answers[7] = forward(7)

	if _, errOut, err := fx.run(triageOpts{}); err != nil {
		t.Fatalf("triage = %v\n%s", err, errOut)
	}
	assertWrites(t, fx.srv, "comment 7: "+marker4b, "SetStatus PVTI_7 c1")
}

func TestTriageDesignAndStops(t *testing.T) {
	fx := triageFixture(t, triageToml)
	for _, n := range []int{8, 9, 10, 11} {
		fx.issue(n, "2-area:cli")
		fx.onBoard(n, "t1", nil)
	}
	fx.srv.Issues[11]["user"] = map[string]any{"login": "mallory"} // no role: not in answer_permissions
	fx.judge.answers[11] = forward(11)
	fx.judge.answers[8] = answerWith("#8 Poort 4c: brainstorming — richting open", 4, "c", map[string]any{
		"design_questions": []any{map[string]any{"question": "Welke kant?", "options": []any{"Links", "Rechts"}, "recommended": 0}}})
	fx.judge.answers[9] = stopAt1(9)
	fx.judge.answers[10] = answerWith("#10 Poort 4b: writing-plans — x", 4, "b", map[string]any{
		"redundancy": "duplicate", "redundancy_issue": 7})

	_, errOut, err := fx.run(triageOpts{})
	if err != nil {
		t.Fatalf("triage = %v\n%s", err, errOut)
	}
	assertWrites(t, fx.srv,
		`comment 8: <!-- lighthouse: {"kind":"triage","version":1,"gate":"4c"} -->`,
		`comment 9: <!-- lighthouse: {"kind":"triage","version":1,"gate":"1"} -->`,
		`comment 10: <!-- lighthouse: {"kind":"triage","version":1,"gate":"4b"} -->`,
		`comment 11: <!-- lighthouse: {"kind":"triage","version":1,"gate":"4b"} -->`)
	if errOut != summary(4, 4, 0, 1, 3, 0, 0, 0, 0) {
		t.Errorf("errOut = %q", errOut)
	}
	if b := bodies(fx.srv, 8); len(b) != 1 || !strings.Contains(b[0], "Welke kant?") {
		t.Errorf("4c comment = %q, want the design question", b)
	}
}

func TestTriageGate0FromGo(t *testing.T) {
	fx := triageFixture(t, triageToml)
	fx.issue(11, "2-area:cli")
	fx.srv.BlockedBy = map[int][]map[string]any{11: {{"number": 5, "state": "open", "repository_url": fx.srv.URL() + "/repos/o/r"}}}
	fx.judge.answers[11] = answer("", 0, "")

	if _, errOut, err := fx.run(triageOpts{}); err != nil {
		t.Fatalf("triage = %v\n%s", err, errOut)
	}
	if p := fx.judge.prompts[11]; !strings.Contains(p, "Gate 0 is already decided by Go") || !strings.Contains(p, "blocked_by: #5 (open)") {
		t.Errorf("prompt lacks the fixed gate 0 and its blocker:\n%s", p)
	}
	assertWrites(t, fx.srv, `comment 11: <!-- lighthouse: {"kind":"triage","version":1,"gate":"0","blockers":[5],"label":null} -->`)
}

// The judgment sees the git log of origin after the fetch, and the other issues of the milestone.
func TestTriagePromptFacts(t *testing.T) {
	fx := triageFixture(t, triageToml)
	fx.issue(7, "2-area:cli")
	fx.onBoard(7, "t1", nil)
	fx.issue(8, "2-area:cli")["title"] = "Buurman"
	fx.srv.Issues[9] = map[string]any{"number": 9, "title": "Al klaar", "state": "closed",
		"closed_at": time.Now().UTC().Format(time.RFC3339),
		"milestone": map[string]any{"number": 1, "title": "M2", "state": "open"}}
	fx.judge.answers[7] = stopAt1(7)
	fx.judge.answers[8] = stopAt1(8)
	other := filepath.Join(t.TempDir(), "other")
	mustGit(t, filepath.Dir(other), "clone", "-q", mustGit(t, fx.dir, "remote", "get-url", "origin"), other)
	mustGit(t, other, "commit", "-q", "--allow-empty", "-m", "fix: de kern (#7)")
	mustGit(t, other, "commit", "-q", "--allow-empty", "-m", "feat: iets anders (#70)")
	mustGit(t, other, "push", "-q", "origin", "main")

	if _, errOut, err := fx.run(triageOpts{}); err != nil {
		t.Fatalf("triage = %v\n%s", err, errOut)
	}
	p := fx.judge.prompts[7]
	for _, want := range []string{"fix: de kern (#7)", "#8 Buurman", "#9 Al klaar"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "#70") {
		t.Errorf("prompt has the commit of #70:\n%s", p)
	}
}

func TestTriageInvariantNoJudgment(t *testing.T) {
	fx := triageFixture(t, triageToml)
	fx.issue(12)
	fx.onBoard(12, "t1", nil)

	_, errOut, err := fx.run(triageOpts{})
	if err != nil {
		t.Fatalf("triage = %v\n%s", err, errOut)
	}
	assertWrites(t, fx.srv, `comment 12: <!-- lighthouse: {"kind":"triage","version":1,"gate":"invariant"} -->`)
	if c := fx.judge.called(); len(c) != 0 {
		t.Errorf("judged %v, want no judgment for a broken invariant", c)
	}
	if errOut != summary(1, 0, 0, 0, 1, 0, 0, 0, 0) {
		t.Errorf("errOut = %q", errOut)
	}
}

func TestTriageSecondRoundIdle(t *testing.T) {
	fx := triageFixture(t, triageToml)
	fx.issue(7, "2-area:cli")
	fx.onBoard(7, "t1", nil)
	fx.judge.answers[7] = forward(7)
	if _, errOut, err := fx.run(triageOpts{}); err != nil {
		t.Fatalf("first round = %v\n%s", err, errOut)
	}
	first := triageWrites(fx.srv)
	if len(first) != 3 {
		t.Fatalf("first round wrote %q, want comment, handoff and status", first)
	}

	_, errOut, err := fx.run(triageOpts{})
	if err != nil {
		t.Fatalf("second round = %v\n%s", err, errOut)
	}
	if got := triageWrites(fx.srv); len(got) != len(first) {
		t.Errorf("second round wrote %q", got[len(first):])
	}
	if c := fx.judge.called(); len(c) != 1 {
		t.Errorf("judgments = %v, want only the one of the first round", c)
	}
	if errOut != summary(0, 0, 0, 0, 0, 0, 0, 0, 0) {
		t.Errorf("second errOut = %q", errOut)
	}
}

func TestTriageSecondRoundIdleWriteToken(t *testing.T) {
	fx := triageFixture(t, withTriage("wait_for_night = false\n"))
	fx.srv.Roles["lh-bot"] = "write"
	fx.issue(7, "2-area:cli")
	fx.onBoard(7, "t1", nil)
	fx.judge.answers[7] = forward(7)
	if _, errOut, err := fx.run(triageOpts{}); err != nil {
		t.Fatalf("first round = %v\n%s", err, errOut)
	}
	assertWrites(t, fx.srv, "comment 7: "+marker4b, "SetStatus PVTI_7 c1")

	if _, errOut, err := fx.run(triageOpts{}); err != nil {
		t.Fatalf("second round = %v\n%s", err, errOut)
	}
	assertWrites(t, fx.srv, "comment 7: "+marker4b, "SetStatus PVTI_7 c1")
}

func TestTriageBoardBlocker(t *testing.T) {
	fx := triageFixture(t, triageToml)
	fx.issue(17, "2-area:cli")
	fx.onBoard(17, "t1", map[string]any{"blockedBy": map[string]any{"nodes": []any{map[string]any{
		"number": 5, "state": "OPEN", "repository": map[string]any{"nameWithOwner": "o/r"},
	}}}})
	fx.judge.answers[17] = answer("", 0, "")

	if _, errOut, err := fx.run(triageOpts{}); err != nil {
		t.Fatalf("triage = %v\n%s", err, errOut)
	}
	assertWrites(t, fx.srv, `comment 17: <!-- lighthouse: {"kind":"triage","version":1,"gate":"0","blockers":[5],"label":null} -->`)
	if n := fx.srv.Count("/repos/o/r/issues/17/dependencies/blocked_by"); n != 0 {
		t.Errorf("blocked_by REST reads = %d, want 0 for an issue on the board", n)
	}
}

func TestTriageMilestonesOnly(t *testing.T) {
	for _, tc := range []struct {
		name  string
		toml  string
		judge []int
		reads int // requests on the comments of #7: none for an issue the cheap rules reject
	}{
		{"default true", triageToml, nil, 0},
		{"false", withTriage("milestones_only = false\n"), []int{7}, 3}, // gather, recheck, post
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := triageFixture(t, tc.toml)
			delete(fx.issue(7, "2-area:cli"), "milestone")
			fx.onBoard(7, "t1", nil)
			fx.judge.answers[7] = stopAt1(7)
			if _, errOut, err := fx.run(triageOpts{}); err != nil {
				t.Fatalf("triage = %v\n%s", err, errOut)
			}
			if got := fx.judge.called(); !slices.Equal(got, tc.judge) {
				t.Errorf("judged %v, want %v", got, tc.judge)
			}
			if n := fx.srv.Count("/repos/o/r/issues/7/comments"); n != tc.reads {
				t.Errorf("requests on the comments of #7 = %d, want %d", n, tc.reads)
			}
		})
	}
}

func TestTriageRunningWork(t *testing.T) {
	fx := triageFixture(t, triageToml)
	for _, n := range []int{13, 14, 15, 16} {
		fx.issue(n, "2-area:cli")
		fx.onBoard(n, "t1", nil)
	}
	fx.srv.Pulls = []map[string]any{
		{"number": 40, "title": "Iets", "body": "Refs #13", "head": map[string]any{"ref": "feature-x"}},
		{"number": 41, "title": "Ander", "body": "Zie #130 en #150", "head": map[string]any{"ref": "other"}},
	}
	mustGit(t, fx.dir, "push", "-q", "origin", "HEAD:refs/heads/issue-14-x")
	wt := filepath.Join(t.TempDir(), "wt15")
	mustGit(t, fx.dir, "worktree", "add", "-q", "-b", "issue-15-y", wt)
	wt, err := filepath.EvalSymlinks(wt)
	if err != nil {
		t.Fatal(err)
	}
	run, err := runs.Take(context.Background(), openStore(t, fx.db), runs.Claim{Repo: "o/r", Issue: 16,
		SessionID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", Mode: "start", Trigger: "handoff", TriggerKey: "start:16",
		Command: "/lighthouse-start-issue 16", Now: time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}

	_, errOut, err := fx.run(triageOpts{})
	if err != nil {
		t.Fatalf("triage = %v\n%s", err, errOut)
	}
	want := "lighthouse: triage o/r#13: running work: PR #40\n" +
		"lighthouse: triage o/r#14: running work: branch origin/issue-14-x\n" +
		"lighthouse: triage o/r#15: running work: branch issue-15-y, worktree " + wt + "\n" +
		"lighthouse: triage o/r#16: running work: run " + strconv.FormatInt(run.ID, 10) + "\n" +
		summary(0, 0, 0, 0, 0, 0, 4, 0, 0)
	if errOut != want {
		t.Errorf("errOut:\n%s\nwant:\n%s", errOut, want)
	}
	assertWrites(t, fx.srv)
	if c := fx.judge.called(); len(c) != 0 {
		t.Errorf("judged %v, want none", c)
	}
}

// onSecondRead runs f once, at the second comments read of issue n: the read of the recheck.
func onSecondRead(fx *triageFix, n int, f func()) {
	var mu sync.Mutex
	reads := 0
	fx.srv.BeforeComments = func(got int) {
		if got != n {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		reads++
		if reads == 2 {
			f()
		}
	}
}

func TestTriageRecheckSkips(t *testing.T) {
	fx := triageFixture(t, triageToml)
	fx.issue(7, "2-area:cli")
	fx.onBoard(7, "t1", nil)
	fx.judge.answers[7] = forward(7)
	onSecondRead(fx, 7, func() {
		fx.srv.BoardNodes[0] = boardNode("PVTI_7", "p1", issueContent(7, "Issue 7", nil))
	})

	_, errOut, err := fx.run(triageOpts{})
	if err != nil {
		t.Fatalf("triage = %v\n%s", err, errOut)
	}
	assertWrites(t, fx.srv)
	if want := "lighthouse: triage o/r#7: changed since gather, skipped\n" + summary(1, 1, 0, 0, 0, 0, 0, 1, 0); errOut != want {
		t.Errorf("errOut = %q, want %q", errOut, want)
	}
}

// A board column that status_options does not list (Done, Review) is not "no status": the
// issue is no candidate, and a move into it before the recheck is a change.
func TestTriageUnlistedStatus(t *testing.T) {
	t.Run("gather", func(t *testing.T) {
		fx := triageFixture(t, triageToml)
		fx.issue(7, "2-area:cli")
		fx.onBoard(7, "d1", nil)

		_, errOut, err := fx.run(triageOpts{})
		if err != nil {
			t.Fatalf("triage = %v\n%s", err, errOut)
		}
		assertWrites(t, fx.srv)
		if c := fx.judge.called(); len(c) != 0 {
			t.Errorf("judged %v, want none for an issue in an unlisted column", c)
		}
		if errOut != summary(0, 0, 0, 0, 0, 0, 0, 0, 0) {
			t.Errorf("errOut = %q", errOut)
		}
	})
	t.Run("recheck", func(t *testing.T) {
		fx := triageFixture(t, triageToml)
		fx.issue(7, "2-area:cli")
		fx.onBoard(7, "", nil) // on the board without a status
		fx.judge.answers[7] = forward(7)
		onSecondRead(fx, 7, func() {
			fx.srv.BoardNodes[0] = boardNode("PVTI_7", "d1", issueContent(7, "Issue 7", nil))
		})

		_, errOut, err := fx.run(triageOpts{})
		if err != nil {
			t.Fatalf("triage = %v\n%s", err, errOut)
		}
		assertWrites(t, fx.srv)
		if want := "lighthouse: triage o/r#7: changed since gather, skipped\n" + summary(1, 1, 0, 0, 0, 0, 0, 1, 0); errOut != want {
			t.Errorf("errOut = %q, want %q", errOut, want)
		}
	})
}

// The board lists no archived item, but GitHub still has it: an issue whose item is archived is
// skipped with a line, never judged and never written, and an item archived before the
// recheck is a change.
func TestTriageArchivedItem(t *testing.T) {
	t.Run("gather", func(t *testing.T) {
		fx := triageFixture(t, triageToml)
		fx.issue(7, "2-area:cli")
		fx.onBoard(7, "t1", nil)
		fx.srv.BoardNodes[0]["isArchived"] = true

		_, errOut, err := fx.run(triageOpts{})
		if err != nil {
			t.Fatalf("triage = %v\n%s", err, errOut)
		}
		assertWrites(t, fx.srv)
		if c := fx.judge.called(); len(c) != 0 {
			t.Errorf("judged %v, want none for an archived item", c)
		}
		if want := "lighthouse: triage o/r#7: archived board item, skipped\n" + summary(0, 0, 0, 0, 0, 0, 0, 1, 0); errOut != want {
			t.Errorf("errOut = %q, want %q", errOut, want)
		}
	})
	t.Run("recheck", func(t *testing.T) {
		fx := triageFixture(t, triageToml)
		fx.issue(7, "2-area:cli")
		fx.onBoard(7, "t1", nil)
		fx.judge.answers[7] = forward(7)
		onSecondRead(fx, 7, func() {
			node := boardNode("PVTI_7", "t1", issueContent(7, "Issue 7", nil))
			node["isArchived"] = true
			fx.srv.BoardNodes[0] = node
		})

		_, errOut, err := fx.run(triageOpts{})
		if err != nil {
			t.Fatalf("triage = %v\n%s", err, errOut)
		}
		assertWrites(t, fx.srv)
		if want := "lighthouse: triage o/r#7: changed since gather, skipped\n" + summary(1, 1, 0, 0, 0, 0, 0, 1, 0); errOut != want {
			t.Errorf("errOut = %q, want %q", errOut, want)
		}
	})
}

// An item the board read did not list, but the issue's own read finds, carries no blockers:
// they come from the REST read (decision 10), never from the empty list of that item.
func TestTriageItemOffBoardListReadsBlockers(t *testing.T) {
	fx := triageFixture(t, triageToml)
	fx.issue(11, "2-area:cli")
	fx.srv.BlockedBy = map[int][]map[string]any{11: {{"number": 5, "state": "open", "repository_url": fx.srv.URL() + "/repos/o/r"}}}
	fx.judge.answers[11] = answer("", 0, "")
	var once sync.Once
	fx.srv.BeforeComments = func(n int) {
		if n == 11 {
			once.Do(func() { fx.onBoard(11, "t1", nil) }) // after the board read, before the item read
		}
	}

	if _, errOut, err := fx.run(triageOpts{}); err != nil {
		t.Fatalf("triage = %v\n%s", err, errOut)
	}
	assertWrites(t, fx.srv, `comment 11: <!-- lighthouse: {"kind":"triage","version":1,"gate":"0","blockers":[5],"label":null} -->`)
	if n := fx.srv.Count("/repos/o/r/issues/11/dependencies/blocked_by"); n != 1 {
		t.Errorf("blocked_by REST reads = %d, want 1", n)
	}
}

func TestTriageRecheckSeesNewWork(t *testing.T) {
	for _, tc := range []struct {
		name string
		work func(t *testing.T, fx *triageFix) func()
	}{
		{"run", func(t *testing.T, fx *triageFix) func() {
			conn := openStore(t, fx.db)
			return func() {
				_, err := runs.Take(context.Background(), conn, runs.Claim{Repo: "o/r", Issue: 7,
					SessionID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", Mode: "start", Trigger: "handoff",
					TriggerKey: "start:7", Command: "/lighthouse-start-issue 7", Now: time.Now()})
				if err != nil {
					t.Errorf("claim: %v", err)
				}
			}
		}},
		{"branch", func(t *testing.T, fx *triageFix) func() {
			return func() {
				if _, err := gitIn(fx.dir, "push", "-q", "origin", "HEAD:refs/heads/issue-7-z"); err != nil {
					t.Errorf("push: %v", err)
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := triageFixture(t, triageToml)
			fx.issue(7, "2-area:cli")
			fx.onBoard(7, "t1", nil)
			fx.judge.answers[7] = forward(7)
			onSecondRead(fx, 7, tc.work(t, fx))

			_, errOut, err := fx.run(triageOpts{})
			if err != nil {
				t.Fatalf("triage = %v\n%s", err, errOut)
			}
			assertWrites(t, fx.srv)
			if want := "lighthouse: triage o/r#7: changed since gather, skipped\n" + summary(1, 1, 0, 0, 0, 0, 0, 1, 0); errOut != want {
				t.Errorf("errOut = %q, want %q", errOut, want)
			}
		})
	}
}

func TestTriageMaxForwarded(t *testing.T) {
	fx := triageFixture(t, withTriage("max_forwarded = 1\n"))
	for _, n := range []int{7, 8} {
		fx.issue(n, "2-area:cli")
		fx.onBoard(n, "t1", nil)
		fx.judge.answers[n] = forward(n)
	}
	_, errOut, err := fx.run(triageOpts{})
	if err != nil {
		t.Fatalf("triage = %v\n%s", err, errOut)
	}
	assertWrites(t, fx.srv, "comment 7: "+marker4b, "comment 7: "+robotHO, "SetStatus PVTI_7 c1")
	if c := fx.judge.called(); !slices.Equal(c, []int{7}) {
		t.Errorf("judged %v, want [7]", c)
	}
	if errOut != summary(2, 1, 1, 0, 0, 0, 0, 0, 0) {
		t.Errorf("errOut = %q", errOut)
	}
}

func TestTriageMaxJudgments(t *testing.T) {
	fx := triageFixture(t, withTriage("max_judgments = 2\n"))
	for _, n := range []int{7, 8, 9} {
		fx.issue(n, "2-area:cli")
		fx.onBoard(n, "t1", nil)
		fx.judge.answers[n] = stopAt1(n)
	}
	_, errOut, err := fx.run(triageOpts{})
	if err != nil {
		t.Fatalf("triage = %v\n%s", err, errOut)
	}
	if c := fx.judge.called(); !slices.Equal(c, []int{7, 8}) {
		t.Errorf("judged %v, want [7 8]", c)
	}
	if errOut != summary(3, 2, 0, 0, 2, 0, 0, 0, 0) {
		t.Errorf("errOut = %q", errOut)
	}
}

func TestTriageInvalidJudgment(t *testing.T) {
	fx := triageFixture(t, triageToml)
	fx.issue(7, "2-area:cli")
	fx.onBoard(7, "t1", nil)
	fx.judge.answers[7] = answer("#7 Poort 4: writing-plans — plan", 4, "")

	_, errOut, err := fx.run(triageOpts{})
	if err != nil {
		t.Fatalf("triage = %v, want nil: an invalid judgment is no failed round\n%s", err, errOut)
	}
	assertWrites(t, fx.srv)
	want := "lighthouse: triage o/r#7: invalid judgment: gate 4 without sub-letter\n" + summary(1, 1, 0, 0, 0, 1, 0, 0, 0)
	if errOut != want {
		t.Errorf("errOut = %q, want %q", errOut, want)
	}
}

// A timeout counts as an invalid judgment; any other failed judgment is an error of the issue.
func TestTriageJudgeFails(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		code    int
		line    string
		summary string
	}{
		{"timeout", fmt.Errorf("claude: %w", context.DeadlineExceeded), 0,
			"lighthouse: triage o/r#7: invalid judgment: claude: context deadline exceeded\n", summary(1, 1, 0, 0, 0, 1, 0, 0, 0)},
		{"failure", errors.New("claude: exit status 1: not logged in"), 2,
			"lighthouse: triage o/r#7: judge: claude: exit status 1: not logged in\n", summary(1, 1, 0, 0, 0, 0, 0, 0, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := triageFixture(t, triageToml)
			fx.issue(7, "2-area:cli")
			fx.onBoard(7, "t1", nil)
			fx.judge.errs = map[int]error{7: tc.err}
			_, errOut, err := fx.run(triageOpts{})
			if exitCode(err) != tc.code {
				t.Errorf("triage = %v, want exit code %d", err, tc.code)
			}
			if errOut != tc.line+tc.summary {
				t.Errorf("errOut = %q, want %q", errOut, tc.line+tc.summary)
			}
			assertWrites(t, fx.srv)
		})
	}
}

func TestTriageStepFailsContinues(t *testing.T) {
	fx := triageFixture(t, triageToml)
	for _, n := range []int{7, 8} {
		fx.issue(n, "2-area:cli")
		fx.onBoard(n, "t1", nil)
		fx.judge.answers[n] = forward(n)
	}
	fx.srv.Fail = map[string]int{"POST /repos/o/r/issues/7/comments": 500}

	_, errOut, err := fx.run(triageOpts{})
	if exitCode(err) != 2 {
		t.Fatalf("triage = %v, want exit code 2\n%s", err, errOut)
	}
	assertWrites(t, fx.srv, "comment 7: "+marker4b, "comment 8: "+marker4b, "comment 8: "+robotHO, "SetStatus PVTI_8 c1")
	if !strings.HasPrefix(errOut, "lighthouse: triage o/r#7: comment: ") {
		t.Errorf("errOut = %q, want a comment step line for #7 first", errOut)
	}
	if !strings.HasSuffix(errOut, summary(2, 2, 1, 0, 0, 0, 0, 0, 1)) {
		t.Errorf("errOut = %q", errOut)
	}
}

func TestTriageBlockedByFailsNoWrite(t *testing.T) {
	fx := triageFixture(t, triageToml)
	fx.issue(7, "2-area:cli") // not on the board: blocked_by comes from REST
	fx.issue(8, "2-area:cli")
	fx.onBoard(8, "t1", nil)
	fx.judge.answers[8] = forward(8)
	fx.srv.Fail = map[string]int{"GET /repos/o/r/issues/7/dependencies/blocked_by": 500}

	_, errOut, err := fx.run(triageOpts{})
	if exitCode(err) != 2 {
		t.Fatalf("triage = %v, want exit code 2\n%s", err, errOut)
	}
	assertWrites(t, fx.srv, "comment 8: "+marker4b, "comment 8: "+robotHO, "SetStatus PVTI_8 c1")
	if c := fx.judge.called(); !slices.Equal(c, []int{8}) {
		t.Errorf("judged %v, want only #8", c)
	}
	if !strings.HasPrefix(errOut, "lighthouse: triage o/r#7: gather: ") {
		t.Errorf("errOut = %q, want a gather line for #7", errOut)
	}
	if !strings.HasSuffix(errOut, summary(1, 1, 1, 0, 0, 0, 0, 0, 1)) {
		t.Errorf("errOut = %q", errOut)
	}
}

func TestTriageBrakeAtStart(t *testing.T) {
	fx := triageFixture(t, triageToml)
	fx.issue(7, "2-area:cli")
	fx.brake.closeAfter, fx.brake.reason = 0, "usage: not measured"

	_, errOut, err := fx.run(triageOpts{})
	if exitCode(err) != 1 {
		t.Fatalf("triage = %v, want exit code 1", err)
	}
	if errOut != "lighthouse: triage skipped: usage: not measured\n" {
		t.Errorf("errOut = %q", errOut)
	}
	for _, key := range []string{"/user", "Board", "/repos/o/r/issues"} {
		if n := fx.srv.Count(key); n != 0 {
			t.Errorf("%s requests = %d, want 0 after a closed brake", key, n)
		}
	}
	if _, err := os.Stat(fx.db); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("database opened before the brake: %v", err)
	}
}

func TestTriageBrakeMidRound(t *testing.T) {
	fx := triageFixture(t, triageToml)
	for _, n := range []int{7, 8} {
		fx.issue(n, "2-area:cli")
		fx.onBoard(n, "t1", nil)
		fx.judge.answers[n] = stopAt1(n)
	}
	fx.brake.closeAfter, fx.brake.reason = 2, "usage: session 87%, week 10%"

	_, errOut, err := fx.run(triageOpts{})
	if err != nil {
		t.Fatalf("triage = %v, want nil: the round ran\n%s", err, errOut)
	}
	assertWrites(t, fx.srv, `comment 7: <!-- lighthouse: {"kind":"triage","version":1,"gate":"1"} -->`)
	if want := "lighthouse: triage stopped: usage: session 87%, week 10%\n" + summary(2, 1, 0, 0, 1, 0, 0, 0, 0); errOut != want {
		t.Errorf("errOut = %q, want %q", errOut, want)
	}
}

func TestTriageNoRepoEnabled(t *testing.T) {
	fx := triageFixture(t, strings.Replace(triageToml, "[controller.triage]\n", "", 1))
	_, errOut, err := fx.run(triageOpts{})
	if exitCode(err) != 1 {
		t.Fatalf("triage = %v, want exit code 1", err)
	}
	if errOut != "lighthouse: no repo with [controller.triage]\n" {
		t.Errorf("errOut = %q", errOut)
	}
	if fx.brake.calls != 0 {
		t.Errorf("brake checked %d times, want 0", fx.brake.calls)
	}
}

func TestTriageRepoNotTriageable(t *testing.T) {
	fx := triageFixture(t, strings.Replace(triageToml, "\"To do\" = \"t1\"\n", "", 1))
	fx.issue(7, "2-area:cli")
	_, errOut, err := fx.run(triageOpts{})
	if exitCode(err) != 2 {
		t.Fatalf("triage = %v, want exit code 2\n%s", err, errOut)
	}
	if want := "lighthouse: skip " + fx.dir + ": missing forge.board.status_options.To do\n"; errOut != want {
		t.Errorf("errOut = %q, want %q", errOut, want)
	}
}

// A [controller.triage] error skips the repo with exit code 2, under --dry-run too: a typo in
// max_judgments must not fall back to 15 judgments.
func TestTriageConfigError(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts triageOpts
	}{
		{"live", triageOpts{}},
		{"dry run", triageOpts{DryRun: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := triageFixture(t, withTriage("max_judgment = 3\n"))
			fx.issue(7, "2-area:cli")
			fx.onBoard(7, "t1", nil)
			fx.judge.answers[7] = stopAt1(7)

			out, errOut, err := fx.run(tc.opts)
			if exitCode(err) != 2 {
				t.Fatalf("triage = %v, want exit code 2\n%s", err, errOut)
			}
			if want := "lighthouse: skip " + fx.dir + ": unknown key controller.triage.max_judgment\n"; errOut != want {
				t.Errorf("errOut = %q, want %q", errOut, want)
			}
			if c := fx.judge.called(); len(c) != 0 || out != "" {
				t.Errorf("judged %v, out %q; want nothing in a skipped repo", c, out)
			}
			assertWrites(t, fx.srv)
		})
	}
}

func TestTriageTokenNotTrusted(t *testing.T) {
	t.Run("live", func(t *testing.T) {
		fx := triageFixture(t, triageToml)
		fx.srv.Roles["lh-bot"] = "write"
		fx.issue(7, "2-area:cli")
		fx.judge.answers[7] = forward(7)
		_, errOut, err := fx.run(triageOpts{})
		if exitCode(err) != 2 {
			t.Fatalf("triage = %v, want exit code 2\n%s", err, errOut)
		}
		if !strings.HasPrefix(errOut, "lighthouse: skip "+fx.dir+": ") || !strings.Contains(errOut, "answer_permissions") ||
			strings.Count(errOut, "\n") != 1 {
			t.Errorf("errOut = %q, want one skip line naming answer_permissions", errOut)
		}
		assertWrites(t, fx.srv)
		if c := fx.judge.called(); len(c) != 0 {
			t.Errorf("judged %v in a skipped repo", c)
		}
	})
	t.Run("dry run", func(t *testing.T) {
		fx := triageFixture(t, triageToml)
		fx.srv.Roles["lh-bot"] = "write"
		fx.issue(7, "2-area:cli")
		fx.judge.answers[7] = forward(7)
		out, errOut, err := fx.run(triageOpts{DryRun: true})
		if err != nil {
			t.Fatalf("triage = %v\n%s", err, errOut)
		}
		if !strings.HasPrefix(errOut, "lighthouse: warning: "+fx.dir+": ") || !strings.Contains(errOut, "answer_permissions") {
			t.Errorf("errOut = %q, want a warning naming answer_permissions", errOut)
		}
		if out != "o/r #7 Poort 4b: writing-plans — plan ligt klaar\n" {
			t.Errorf("out = %q", out)
		}
	})
}

func TestTriageLockHeld(t *testing.T) {
	fx := triageFixture(t, triageToml)
	dir := filepath.Join(fx.env.Getenv("XDG_RUNTIME_DIR"), "lighthouse")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	held, err := flock.TryLock(filepath.Join(dir, "triage.lock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })

	_, errOut, err := fx.run(triageOpts{})
	if exitCode(err) != 1 {
		t.Fatalf("triage = %v, want exit code 1", err)
	}
	if errOut != "lighthouse: previous triage still running\n" {
		t.Errorf("errOut = %q", errOut)
	}
}

func TestTriageDryRun(t *testing.T) {
	fx := triageFixture(t, triageToml)
	fx.issue(7, "2-area:cli")
	fx.onBoard(7, "t1", nil)
	fx.issue(12) // breaks the invariant
	fx.judge.answers[7] = forward(7)

	out, errOut, err := fx.run(triageOpts{DryRun: true})
	if err != nil {
		t.Fatalf("triage = %v\n%s", err, errOut)
	}
	assertWrites(t, fx.srv)
	if want := "o/r #7 Poort 4b: writing-plans — plan ligt klaar\no/r #12 invariant\n"; out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
}

func TestTriageDryRunIssues(t *testing.T) {
	fx := triageFixture(t, triageToml)
	fx.issue(7, "2-area:cli")
	fx.onBoard(7, "t1", nil)
	fx.issue(20, "2-area:cli")
	fx.onBoard(20, "p1", nil) // In progress: no candidate, but asked for
	fx.judge.answers[20] = stopAt1(20)

	out, errOut, err := fx.run(triageOpts{DryRun: true, Issues: []issueRef{{Repo: "o/r", Number: 20}}})
	if err != nil {
		t.Fatalf("triage = %v\n%s", err, errOut)
	}
	if c := fx.judge.called(); !slices.Equal(c, []int{20}) {
		t.Errorf("judged %v, want only #20", c)
	}
	if want := "o/r #20 Poort 1: lighthouse-write-issue — recept bestaat\n"; out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	assertWrites(t, fx.srv)
}

func TestTriageIssueWithoutDryRun(t *testing.T) {
	fx := triageFixture(t, triageToml)
	_, _, err := fx.run(triageOpts{Issues: []issueRef{{Repo: "o/r", Number: 7}}})
	if exitCode(err) != 2 || err.Error() != "--issue requires --dry-run" {
		t.Errorf("triage = %v (code %d), want code 2 with --issue requires --dry-run", err, exitCode(err))
	}
	if n := fx.srv.Count("/user"); n != 0 {
		t.Errorf("/user requests = %d, want 0", n)
	}
}

func TestParseIssueRef(t *testing.T) {
	got, err := parseIssueRef("o/r#12")
	if err != nil || got != (issueRef{Repo: "o/r", Number: 12}) {
		t.Errorf("parseIssueRef(o/r#12) = %+v, %v", got, err)
	}
	for _, bad := range []string{"o/r", "#12", "o/r#x", "r#12", "o/r#0", "o/#3"} {
		if got, err := parseIssueRef(bad); err == nil {
			t.Errorf("parseIssueRef(%q) = %+v, want an error", bad, got)
		}
	}
}
