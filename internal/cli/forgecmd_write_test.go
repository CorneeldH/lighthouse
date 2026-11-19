package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// writeForge is a forge.Forge that records the write calls and answers from fields; every other
// method panics on the nil embedded interface.
type writeForge struct {
	forge.Forge
	kind      string
	err       error // returned by every write
	changeErr error // returned by CreateChange next to a created change

	created   []forge.NewIssue
	edits     map[int]forge.IssueEdit
	comments  map[int]string
	changes   []forge.NewPR
	blocks    [][2]int
	calls     []string // board calls in order
	statusSet string

	item       forge.BoardItem
	onBoard    bool
	milestones []forge.Milestone
	msEdits    []msEdit
}

type msEdit struct {
	number             int
	title, description *string
}

func (w *writeForge) Kind() string { return cmp.Or(w.kind, "github") }

func (w *writeForge) CreateIssue(_ context.Context, in forge.NewIssue) (forge.Issue, error) {
	if w.err != nil {
		return forge.Issue{}, w.err
	}
	w.created = append(w.created, in)
	return forge.Issue{Number: 41, URL: "https://x/issues/41"}, nil
}
func (w *writeForge) EditIssue(_ context.Context, n int, e forge.IssueEdit) (forge.Issue, error) {
	if w.edits == nil {
		w.edits = map[int]forge.IssueEdit{}
	}
	w.edits[n] = e
	return forge.Issue{Number: n}, w.err
}
func (w *writeForge) CreateComment(_ context.Context, n int, body string) (protocol.Comment, error) {
	if w.comments == nil {
		w.comments = map[int]string{}
	}
	w.comments[n] = body
	return protocol.Comment{URL: "https://x/c/1"}, w.err
}
func (w *writeForge) CreateChangeComment(ctx context.Context, n int, body string) (protocol.Comment, error) {
	return w.CreateComment(ctx, n, body)
}
func (w *writeForge) AddBlocker(_ context.Context, n, by int) error {
	w.blocks = append(w.blocks, [2]int{n, by})
	return w.err
}
func (w *writeForge) IssueItem(context.Context, int) (forge.BoardItem, bool, error) {
	w.calls = append(w.calls, "IssueItem")
	return w.item, w.onBoard, w.err
}
func (w *writeForge) AddToBoard(context.Context, int) (forge.BoardItem, error) {
	w.calls = append(w.calls, "AddToBoard")
	return forge.BoardItem{ItemID: "new"}, w.err
}
func (w *writeForge) SetStatus(_ context.Context, it forge.BoardItem, status string) error {
	w.calls = append(w.calls, "SetStatus:"+it.ItemID)
	w.statusSet = status
	return w.err
}
func (w *writeForge) CreateChange(_ context.Context, in forge.NewPR) (forge.Change, error) {
	w.changes = append(w.changes, in)
	if w.changeErr != nil {
		return forge.Change{Number: 12, URL: "https://x/pull/12"}, w.changeErr
	}
	return forge.Change{Number: 12, URL: "https://x/pull/12"}, w.err
}
func (w *writeForge) Milestones(context.Context, string) ([]forge.Milestone, error) {
	return w.milestones, w.err
}
func (w *writeForge) CreateMilestone(_ context.Context, title, desc string) (forge.Milestone, error) {
	return forge.Milestone{Number: 5, Title: title, Description: desc}, w.err
}
func (w *writeForge) EditMilestone(_ context.Context, n int, title, desc *string) (forge.Milestone, error) {
	w.msEdits = append(w.msEdits, msEdit{n, title, desc})
	return forge.Milestone{Number: n}, w.err
}

func writeEnv(f forge.Forge) *forgeEnv {
	return &forgeEnv{
		open:   func(context.Context) (forge.Forge, error) { return f, nil },
		branch: func() (string, error) { return "issue-9-x", nil },
		repo: func() (repoconfig.Repo, error) {
			return repoconfig.Repo{DefaultBranch: "trunk", Board: &repoconfig.Board{
				StatusOptions: map[string]string{"To do": "a", "In progress": "b", "Done": "c"}}}, nil
		},
	}
}

func bodyFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestIssueCreatePrintsNumberAndURL(t *testing.T) {
	w := &writeForge{}
	code, out, errOut := runForgeEnv(writeEnv(w), "issue", "create", "--title", "T", "--body-file", bodyFile(t, "the body\n"),
		"--label", "bug", "--label", "ready", "--milestone", "M3")
	if code != 0 || errOut != "" {
		t.Fatalf("code %d err %q", code, errOut)
	}
	if out != "41\nhttps://x/issues/41\n" {
		t.Errorf("out = %q", out)
	}
	want := []forge.NewIssue{{Title: "T", Body: "the body\n", Labels: []string{"bug", "ready"}, Milestone: "M3"}}
	if !reflect.DeepEqual(w.created, want) {
		t.Errorf("created = %+v, want %+v", w.created, want)
	}
}

func TestIssueCreateMissingBodyFileExit2(t *testing.T) {
	empty := bodyFile(t, "")
	missing := filepath.Join(t.TempDir(), "nope.md")
	for name, args := range map[string][]string{
		"no flag":  {"issue", "create", "--title", "T"},
		"missing":  {"issue", "create", "--title", "T", "--body-file", missing},
		"empty":    {"issue", "create", "--title", "T", "--body-file", empty},
		"no title": {"issue", "create", "--body-file", bodyFile(t, "x")},
	} {
		t.Run(name, func(t *testing.T) {
			w := &writeForge{}
			code, _, errOut := runForgeEnv(writeEnv(w), args...)
			if code != 2 || errOut == "" {
				t.Errorf("code %d err %q, want 2 with a message", code, errOut)
			}
			if len(w.created) != 0 {
				t.Errorf("the forge got a call: %+v", w.created)
			}
		})
	}
}

func TestIssueEditLabelsAndMilestone(t *testing.T) {
	w := &writeForge{}
	code, _, errOut := runForgeEnv(writeEnv(w), "issue", "edit", "7", "--add-label", "a", "--add-label", "b",
		"--remove-label", "c", "--milestone", "M4", "--title", "New", "--body-file", bodyFile(t, "nb"))
	if code != 0 || errOut != "" {
		t.Fatalf("code %d err %q", code, errOut)
	}
	title, body, ms := "New", "nb", "M4"
	want := forge.IssueEdit{Title: &title, Body: &body, Milestone: &ms, AddLabels: []string{"a", "b"}, RemoveLabels: []string{"c"}}
	if !reflect.DeepEqual(w.edits[7], want) {
		t.Errorf("edit = %+v, want %+v", w.edits[7], want)
	}
	// An empty --milestone removes the milestone: a non-nil pointer to "".
	w = &writeForge{}
	if code, _, e := runForgeEnv(writeEnv(w), "issue", "edit", "7", "--milestone", ""); code != 0 {
		t.Fatalf("code %d err %q", code, e)
	}
	if got := w.edits[7].Milestone; got == nil || *got != "" {
		t.Errorf("milestone = %v, want pointer to an empty string", got)
	}
}

func TestIssueEditNothingExit2(t *testing.T) {
	w := &writeForge{}
	code, _, errOut := runForgeEnv(writeEnv(w), "issue", "edit", "7")
	if code != 2 || !strings.Contains(errOut, "nothing to change") {
		t.Errorf("code %d err %q", code, errOut)
	}
	if len(w.edits) != 0 {
		t.Error("the forge got an edit")
	}
}

func TestIssueCommentSendsBody(t *testing.T) {
	w := &writeForge{}
	code, out, _ := runForgeEnv(writeEnv(w), "issue", "comment", "7", "--body-file", bodyFile(t, "hello"))
	if code != 0 || out != "https://x/c/1\n" || w.comments[7] != "hello" {
		t.Errorf("code %d out %q comments %v", code, out, w.comments)
	}
}

func TestIssueBlockPassesAPIRefusal(t *testing.T) {
	w := &writeForge{err: errors.New("403 Forbidden: blocking links need GitLab Premium")}
	code, _, errOut := runForgeEnv(writeEnv(w), "issue", "block", "7", "--by", "3")
	if code != 1 || !strings.Contains(errOut, "GitLab Premium") {
		t.Errorf("code %d err %q", code, errOut)
	}
	if !reflect.DeepEqual(w.blocks, [][2]int{{7, 3}}) {
		t.Errorf("blocks = %v, want [[7 3]]", w.blocks)
	}
	if code, _, _ := runForgeEnv(writeEnv(&writeForge{}), "issue", "block", "7"); code != 2 {
		t.Errorf("without --by: code %d, want 2", code)
	}
}

func TestIssueStatusMovesBoardItem(t *testing.T) {
	// Not on the board: AddToBoard first, then SetStatus on the item it returned.
	w := &writeForge{onBoard: false}
	code, _, errOut := runForgeEnv(writeEnv(w), "issue", "status", "7", "In progress")
	if code != 0 || errOut != "" {
		t.Fatalf("code %d err %q", code, errOut)
	}
	if want := []string{"IssueItem", "AddToBoard", "SetStatus:new"}; !reflect.DeepEqual(w.calls, want) || w.statusSet != "In progress" {
		t.Errorf("calls %v status %q, want %v", w.calls, w.statusSet, want)
	}
	// On the board: no AddToBoard.
	w = &writeForge{onBoard: true, item: forge.BoardItem{ItemID: "old"}}
	if code, _, _ = runForgeEnv(writeEnv(w), "issue", "status", "7", "Done"); code != 0 {
		t.Fatalf("code %d", code)
	}
	if want := []string{"IssueItem", "SetStatus:old"}; !reflect.DeepEqual(w.calls, want) {
		t.Errorf("calls %v, want %v", w.calls, want)
	}
	// An unknown name: exit 2 naming the valid ones, no board call.
	w = &writeForge{}
	code, _, errOut = runForgeEnv(writeEnv(w), "issue", "status", "7", "Doing")
	if code != 2 || !strings.Contains(errOut, "Done, In progress, To do") {
		t.Errorf("code %d err %q", code, errOut)
	}
	if len(w.calls) != 0 {
		t.Errorf("board calls %v", w.calls)
	}
}

func TestPRCreateDefaultsHeadAndBase(t *testing.T) {
	w := &writeForge{}
	code, out, errOut := runForgeEnv(writeEnv(w), "pr", "create", "--title", "T", "--body-file", bodyFile(t, "pb"), "--milestone", "M1")
	if code != 0 {
		t.Fatalf("code %d err %q", code, errOut)
	}
	if out != "12\nhttps://x/pull/12\n" {
		t.Errorf("out = %q", out)
	}
	want := []forge.NewPR{{Title: "T", Body: "pb", Head: "issue-9-x", Base: "trunk", Milestone: "M1"}}
	if !reflect.DeepEqual(w.changes, want) {
		t.Errorf("changes = %+v, want %+v", w.changes, want)
	}
	w = &writeForge{}
	runForgeEnv(writeEnv(w), "pr", "create", "--title", "T", "--body-file", bodyFile(t, "pb"), "--head", "h", "--base", "b")
	if got := w.changes[0]; got.Head != "h" || got.Base != "b" {
		t.Errorf("explicit head/base lost: %+v", got)
	}
}

func TestPRCreatePrintsChangeBeforeFailing(t *testing.T) {
	w := &writeForge{changeErr: errors.New("PR #12 created (https://x/pull/12), but setting the milestone failed: boom")}
	code, out, errOut := runForgeEnv(writeEnv(w), "pr", "create", "--title", "T", "--body-file", bodyFile(t, "pb"))
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if out != "12\nhttps://x/pull/12\n" {
		t.Errorf("stdout = %q, want number and URL", out)
	}
	if !strings.Contains(errOut, "PR #12 created") {
		t.Errorf("stderr = %q, want the wrapped error", errOut)
	}
}

func TestPRCreateSaysMROnGitLab(t *testing.T) {
	args := []string{"pr", "create", "--title", "T", "--body-file", ""}
	args[5] = bodyFile(t, "pb")
	_, _, errOut := runForgeEnv(writeEnv(&writeForge{kind: "gitlab"}), args...)
	if !strings.Contains(errOut, "MR") || strings.Contains(errOut, "PR") {
		t.Errorf("gitlab text = %q, want MR", errOut)
	}
	_, _, errOut = runForgeEnv(writeEnv(&writeForge{}), args...)
	if !strings.Contains(errOut, "PR") || strings.Contains(errOut, "MR") {
		t.Errorf("github text = %q, want PR", errOut)
	}
}

func TestPRCommentSendsBody(t *testing.T) {
	w := &writeForge{}
	code, _, _ := runForgeEnv(writeEnv(w), "pr", "comment", "12", "--body-file", bodyFile(t, "lgtm"))
	if code != 0 || w.comments[12] != "lgtm" {
		t.Errorf("code %d comments %v", code, w.comments)
	}
}

func TestMilestoneCreateAndEdit(t *testing.T) {
	w := &writeForge{milestones: []forge.Milestone{{Number: 4, Title: "M4"}}}
	code, out, _ := runForgeEnv(writeEnv(w), "milestone", "create", "--title", "M5", "--description-file", bodyFile(t, "d"))
	if code != 0 || out != "5\n" {
		t.Errorf("create: code %d out %q", code, out)
	}
	code, _, errOut := runForgeEnv(writeEnv(w), "milestone", "edit", "M4", "--description-file", bodyFile(t, "new"))
	if code != 0 || len(w.msEdits) != 1 {
		t.Fatalf("edit: code %d err %q edits %v", code, errOut, w.msEdits)
	}
	if e := w.msEdits[0]; e.number != 4 || e.title != nil || e.description == nil || *e.description != "new" {
		t.Errorf("edit = %+v", e)
	}
	if code, _, _ = runForgeEnv(writeEnv(w), "milestone", "edit", "M4"); code != 2 {
		t.Errorf("edit nothing: code %d, want 2", code)
	}
}

func TestMilestoneEditGroupExit1(t *testing.T) {
	w := &writeForge{milestones: []forge.Milestone{{Number: 4, Title: "M4"}}, err: &forge.GroupMilestone{Number: 4}}
	code, _, errOut := runForgeEnv(writeEnv(w), "milestone", "edit", "M4", "--title", "N")
	if code != 1 || !strings.Contains(errOut, "lighthouse forge api") {
		t.Errorf("code %d err %q", code, errOut)
	}
}

func TestMilestoneAmbiguousExit2(t *testing.T) {
	amb := &forge.AmbiguousMilestone{Title: "M", Numbers: []int{3, 8}}
	code, _, errOut := runForgeEnv(writeEnv(&writeForge{err: amb}), "issue", "create", "--title", "T",
		"--body-file", bodyFile(t, "b"), "--milestone", "M")
	if code != 2 || !strings.Contains(errOut, "3") || !strings.Contains(errOut, "8") {
		t.Errorf("issue create: code %d err %q", code, errOut)
	}
	dup := &writeForge{milestones: []forge.Milestone{{Number: 3, Title: "M"}, {Number: 8, Title: "M"}}}
	code, _, errOut = runForgeEnv(writeEnv(dup), "milestone", "edit", "M", "--title", "N")
	if code != 2 || !strings.Contains(errOut, "3 and 8") || len(dup.msEdits) != 0 {
		t.Errorf("milestone edit: code %d err %q edits %v", code, errOut, dup.msEdits)
	}
}

func TestForgeIssueStatusReads(t *testing.T) {
	noBoard := func(f forge.Forge) *forgeEnv {
		e := writeEnv(f)
		e.repo = func() (repoconfig.Repo, error) { return repoconfig.Repo{DefaultBranch: "trunk"}, nil }
		return e
	}
	tests := []struct {
		name      string
		w         *writeForge
		env       func(forge.Forge) *forgeEnv
		want      map[string]any
		wantCalls []string
	}{
		{"on the board", &writeForge{onBoard: true, item: forge.BoardItem{Status: "In progress"}}, writeEnv,
			map[string]any{"board": true, "on_board": true, "status": "In progress"}, []string{"IssueItem"}},
		{"not on the board", &writeForge{}, writeEnv,
			map[string]any{"board": true, "on_board": false, "status": nil}, []string{"IssueItem"}},
		{"on the board without status", &writeForge{onBoard: true}, writeEnv,
			map[string]any{"board": true, "on_board": true, "status": nil}, []string{"IssueItem"}},
		{"no board configured", &writeForge{}, noBoard,
			map[string]any{"board": false, "on_board": false, "status": nil}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runForgeEnv(tc.env(tc.w), "issue", "status", "7")
			if code != 0 || errOut != "" {
				t.Fatalf("code %d err %q", code, errOut)
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("out %q: %v", out, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(tc.w.calls, tc.wantCalls) {
				t.Errorf("calls %v, want %v", tc.w.calls, tc.wantCalls)
			}
		})
	}
}
