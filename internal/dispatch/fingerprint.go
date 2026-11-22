package dispatch

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

type fpItem struct {
	Issue          int         `json:"issue"`
	Status         string      `json:"status"`
	Priority       string      `json:"priority"`
	ItemUpdatedAt  time.Time   `json:"item_updated_at"`
	IssueUpdatedAt time.Time   `json:"issue_updated_at"`
	Blockers       []fpBlocker `json:"blockers"`
	PRs            []fpPR      `json:"prs"`
}

type fpBlocker struct {
	Repo     string `json:"repo"`
	Issue    int    `json:"issue"`
	State    string `json:"state"`
	MergedPR bool   `json:"merged_pr"`
}

type fpPR struct {
	Number    int    `json:"number"`
	State     string `json:"state"`
	Draft     bool   `json:"draft"`
	Mergeable string `json:"mergeable"`
	HeadSHA   string `json:"head_sha"`
	Checks    string `json:"checks"`
}

type fpRun struct {
	ID        int64            `json:"id"`
	Issue     int              `json:"issue"`
	StartedAt time.Time        `json:"started_at"`
	Stand     *workplace.Stand `json:"stand"` // nil: not measurable
	MergeSHA  string           `json:"merge_sha,omitempty"`
	PushedSHA string           `json:"pushed_sha,omitempty"`
	// PRAt and PRs are of a scheduled run: its pr_at and the PRs of its branch. Empty for an
	// issue run, whose PRs are on its item, so its hash is as before they existed.
	PRAt time.Time `json:"pr_at,omitzero"`
	PRs  []fpPR    `json:"prs,omitempty"`
}

type fpMarker struct {
	Key string `json:"key"` // session, created_at and reset_at: a new parking is a new key
}

// Fingerprint is the hex SHA-256 of what decides whether a tick has work: the board fields
// of the items, the blockers of parked items (To do, which Prepare keeps only with an own
// 🤖 Parked or an open run, and the On hold remnant), the PRs of items with an open run, and the
// open runs with the stand of their session, the resume markers and the end of the pause.
// Comments are left out: a new comment moves IssueUpdatedAt. Activity and permission prompts
// are left out too: they change every tick, and Boundaries makes the tick look when their
// max_wait runs out. Lists are sorted and an empty list hashes like a nil one, so order and
// nil-ness never change the result.
func Fingerprint(pre Pre) string {
	hasRun := map[int]bool{}
	runsIn := make([]fpRun, 0, len(pre.OpenRuns))
	for _, r := range pre.OpenRuns {
		if r.Schedule == "" { // a scheduled run has no issue; it still counts as a run below
			hasRun[r.Issue] = true
		}
		f := fpRun{ID: r.ID, Issue: r.Issue, StartedAt: r.StartedAt.UTC(), MergeSHA: r.MergeSHA,
			PushedSHA: r.PushedSHA}
		if r.Schedule != "" {
			f.PRAt, f.PRs = r.PRAt.UTC(), fpPRs(pre.ScheduledPRs[r.Branch])
		}
		if stand, ok := pre.Stands[r.ID]; ok {
			f.Stand = &stand
		}
		runsIn = append(runsIn, f)
	}
	slices.SortFunc(runsIn, func(a, b fpRun) int { return cmp.Compare(a.ID, b.ID) })

	items := make([]fpItem, 0, len(pre.Items))
	for _, it := range pre.Items {
		f := fpItem{
			Issue: it.Issue, Status: it.Status, Priority: it.Priority,
			ItemUpdatedAt: it.ItemUpdatedAt.UTC(), IssueUpdatedAt: it.IssueUpdatedAt.UTC(),
			Blockers: []fpBlocker{}, PRs: []fpPR{},
		}
		if it.Status == statusToDo || it.Status == statusOnHold {
			for _, b := range it.BlockedBy {
				f.Blockers = append(f.Blockers, fpBlocker{b.Repo, b.Issue, b.State, b.MergedPR})
			}
			slices.SortFunc(f.Blockers, func(a, b fpBlocker) int {
				return cmp.Or(cmp.Compare(a.Repo, b.Repo), cmp.Compare(a.Issue, b.Issue))
			})
		}
		if hasRun[it.Issue] {
			f.PRs = append(f.PRs, fpPRs(it.PRs)...)
		}
		items = append(items, f)
	}
	slices.SortFunc(items, func(a, b fpItem) int { return cmp.Compare(a.Issue, b.Issue) })

	markers := make([]fpMarker, 0, len(pre.Markers))
	for _, m := range pre.Markers {
		markers = append(markers, fpMarker{m.Key()})
	}
	slices.SortFunc(markers, func(a, b fpMarker) int { return cmp.Compare(a.Key, b.Key) })

	data, err := json.Marshal(struct {
		Items      []fpItem   `json:"items"`
		Runs       []fpRun    `json:"runs"`
		Markers    []fpMarker `json:"markers"`
		PauseUntil time.Time  `json:"pause_until"`
	}{items, runsIn, markers, pre.PauseUntil.UTC()})
	if err != nil {
		// Plain structs of strings, ints, bools and times cannot fail to marshal.
		panic("dispatch: fingerprint: " + err.Error())
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// fpPRs is prs as the fingerprint keeps them, sorted by number; nil for none.
func fpPRs(prs []forge.PR) []fpPR {
	var out []fpPR
	for _, p := range prs {
		out = append(out, fpPR{p.Number, p.State, p.Draft, p.Mergeable, p.HeadSHA, p.Checks})
	}
	slices.SortFunc(out, func(a, b fpPR) int { return cmp.Compare(a.Number, b.Number) })
	return out
}
