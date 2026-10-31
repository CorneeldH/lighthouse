package dispatch

import (
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Input is everything one tick decides on.
type Input struct {
	Now   time.Time   `json:"now"`
	Night string      `json:"night"`
	Repos []RepoInput `json:"repos"`
}

// RepoInput is what Decide knows about one repo.
type RepoInput struct {
	Snapshot      Snapshot                  `json:"snapshot"`
	Controller    repoconfig.Controller     `json:"controller"`
	Language      string                    `json:"language"`
	DefaultBranch string                    `json:"default_branch"`
	Installed     map[string]bool           `json:"installed"`
	History       []runs.Row                `json:"history"`
	Stands        map[int64]workplace.Stand `json:"stands"` // per open run id; missing = not measurable
}

// Kind is what an action does.
type Kind string

const (
	KindStart  Kind = "start"
	KindResume Kind = "resume"
	KindReturn Kind = "return"
	KindDone   Kind = "done"
	KindClose  Kind = "close"
	KindRocket Kind = "rocket"
	KindDefer  Kind = "defer"
)

// Action is one thing apply does. Reason and Skip are heading keys, never posted text.
type Action struct {
	Kind       Kind         `json:"kind"`
	Repo       string       `json:"repo"`
	Issue      int          `json:"issue"`
	RunID      int64        `json:"run_id,omitempty"`
	Outcome    runs.Outcome `json:"outcome,omitempty"`
	Trigger    string       `json:"trigger,omitempty"`
	TriggerKey string       `json:"trigger_key,omitempty"`
	Attempt    int          `json:"attempt,omitempty"`
	Mode       string       `json:"mode,omitempty"`
	SessionID  string       `json:"session_id,omitempty"` // resume: the session; start: "" (apply makes one)
	Command    string       `json:"command,omitempty"`
	Prompt     string       `json:"prompt,omitempty"`
	Branch     string       `json:"branch,omitempty"`
	Native     bool         `json:"native,omitempty"`
	Skip       string       `json:"skip,omitempty"`       // a headings key
	CommentID  int64        `json:"comment_id,omitempty"` // 🚀 target; for resume also the answer id of 🤖 Resumed
	Reason     string       `json:"reason,omitempty"`     // a headings key
	ReasonArg  string       `json:"reason_arg,omitempty"` // the argument of Reason, literal
	PRURL      string       `json:"pr_url,omitempty"`
	Language   string       `json:"language,omitempty"`
}

// Decide is the pure decision of one tick: per repo, per item, first the orphan rule, then
// the settlement of the item's open runs, then the 🚀 repair.
func Decide(in Input) []Action {
	out := []Action{}
	for _, repo := range in.Repos {
		for _, it := range repo.Snapshot.Items {
			out = append(out, decideIssue(repo, it)...)
		}
	}
	return out
}

func decideIssue(repo RepoInput, it Item) []Action {
	hs := headers(it)
	base := Action{Repo: repo.Snapshot.Repo, Issue: it.Issue, Language: repo.Language}
	var out []Action
	if a, ok := orphan(repo.History, hs, base); ok {
		out = append(out, a)
	}
	for _, row := range repo.History {
		if !row.Open || row.Issue != it.Issue {
			continue
		}
		if a, ok := settle(repo, it, row, hs, base); ok {
			out = append(out, a)
		}
	}
	return append(out, repair(repo.History, it, base)...)
}

// parsed is a protocol comment with its header and its time.
type parsed struct {
	Comment Comment
	Header  protocol.Header
	Time    time.Time
}

// headers returns the protocol comments of it, oldest first as the snapshot keeps them. An
// unreadable CreatedAt is the zero time.
func headers(it Item) []parsed {
	var out []parsed
	for _, c := range it.Comments {
		h, ok := protocol.ParseHeader(c.Body)
		if !ok {
			continue
		}
		t, err := time.Parse(time.RFC3339, c.CreatedAt)
		if err != nil {
			t = time.Time{}
		}
		out = append(out, parsed{Comment: c, Header: h, Time: t})
	}
	return out
}

// orphan returns a return with reason_state_lost when the last protocol comment (Rebased
// does not count) is a Started or Resumed whose session no run in the history has: the
// database lost the run.
func orphan(history []runs.Row, hs []parsed, base Action) (Action, bool) {
	var last *parsed
	for i := range hs {
		if hs[i].Header.Kind != protocol.KindRebased {
			last = &hs[i]
		}
	}
	if last == nil || (last.Header.Kind != protocol.KindStarted && last.Header.Kind != protocol.KindResumed) {
		return Action{}, false
	}
	for _, row := range history {
		if row.SessionID == last.Header.Session {
			return Action{}, false
		}
	}
	a := base
	a.Kind, a.Reason, a.SessionID = KindReturn, "reason_state_lost", last.Header.Session
	return a, true
}

// settle decides whether the open run row ends in this tick. Only comments from row's start
// on count; a Done or Question also needs row's session, a Handoff carries none. A Done
// closes, then a Handoff that does not end the chain, then a Question; else a green PR with
// a session that is Done gives the done action.
func settle(repo RepoInput, it Item, row runs.Row, hs []parsed, base Action) (Action, bool) {
	var done, question bool
	var handoff *parsed
	for i, p := range hs {
		if p.Time.Before(row.StartedAt) {
			continue
		}
		ofRun := p.Header.Session != "" && p.Header.Session == row.SessionID
		switch p.Header.Kind {
		case protocol.KindDone:
			done = done || ofRun
		case protocol.KindQuestion:
			question = question || ofRun
		case protocol.KindHandoff:
			handoff = &hs[i]
		}
	}
	a := base
	a.RunID = row.ID
	switch {
	case done:
		a.Kind, a.Outcome = KindClose, runs.Done
		return a, true
	case handoff != nil && !endsChain(repo, it.Issue, handoff.Header):
		a.Kind, a.Outcome = KindClose, runs.Handoff
		return a, true
	case question:
		a.Kind, a.Outcome = KindClose, runs.Question
		return a, true
	}
	pr, green := greenPR(it.PRs)
	if stand, measured := repo.Stands[row.ID]; green && measured && stand == workplace.Done {
		a.Kind, a.SessionID, a.PRURL = KindDone, row.SessionID, pr.URL
		return a, true
	}
	return Action{}, false
}

// endsChain reports whether a handoff names the end command: the run then waits for green
// and the handoff does not close it. An unreadable handoff does not end the chain.
func endsChain(repo RepoInput, issue int, h protocol.Header) bool {
	ho, err := protocol.ParseHandoff(h)
	if err != nil {
		return false
	}
	command, _ := ReviewSkip(ho.Next, repo.Controller)
	return Judge(command, issue, ho.Lane, repo.Installed).Verdict == VerdictEnd
}

// greenPR returns the first PR that is open, not a draft, mergeable and with green checks.
func greenPR(prs []forge.PR) (forge.PR, bool) {
	for _, pr := range prs {
		if pr.State == "OPEN" && !pr.Draft && pr.Mergeable == "MERGEABLE" && pr.Checks == "SUCCESS" {
			return pr, true
		}
	}
	return forge.PR{}, false
}

// repair puts back the 🚀 on the comment of a spent handoff or answer trigger: one rocket
// per comment that is in the snapshot without one.
func repair(history []runs.Row, it Item, base Action) []Action {
	rocket := make(map[int64]bool, len(it.Comments))
	for _, c := range it.Comments {
		rocket[c.ID] = c.Rocket
	}
	var out []Action
	emitted := map[int64]bool{}
	for _, row := range history {
		id, ok := triggerComment(row.TriggerKey)
		if !ok || emitted[id] {
			continue
		}
		if has, present := rocket[id]; !present || has {
			continue
		}
		emitted[id] = true
		a := base
		a.Kind, a.CommentID = KindRocket, id
		out = append(out, a)
	}
	return out
}

// triggerComment returns the comment id of a handoff:<id> or answer:<id> trigger key.
func triggerComment(key string) (int64, bool) {
	for _, prefix := range []string{"handoff:", "answer:"} {
		if rest, ok := strings.CutPrefix(key, prefix); ok {
			id, err := strconv.ParseInt(rest, 10, 64)
			return id, err == nil
		}
	}
	return 0, false
}
