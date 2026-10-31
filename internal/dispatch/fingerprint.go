package dispatch

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"
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
	ID        int64     `json:"id"`
	Issue     int       `json:"issue"`
	StartedAt time.Time `json:"started_at"`
}

// Fingerprint is the hex SHA-256 of what decides whether a tick has work: the board fields
// of the items, the blockers of On hold items, the PRs of items with an open run, and the
// open runs. Comments are left out: a new comment moves IssueUpdatedAt. Lists are sorted
// and an empty list hashes like a nil one, so order and nil-ness never change the result.
func Fingerprint(pre Pre) string {
	hasRun := map[int]bool{}
	runsIn := make([]fpRun, 0, len(pre.OpenRuns))
	for _, r := range pre.OpenRuns {
		hasRun[r.Issue] = true
		runsIn = append(runsIn, fpRun{ID: r.ID, Issue: r.Issue, StartedAt: r.StartedAt.UTC()})
	}
	slices.SortFunc(runsIn, func(a, b fpRun) int { return cmp.Compare(a.ID, b.ID) })

	items := make([]fpItem, 0, len(pre.Items))
	for _, it := range pre.Items {
		f := fpItem{
			Issue: it.Issue, Status: it.Status, Priority: it.Priority,
			ItemUpdatedAt: it.ItemUpdatedAt.UTC(), IssueUpdatedAt: it.IssueUpdatedAt.UTC(),
			Blockers: []fpBlocker{}, PRs: []fpPR{},
		}
		if it.Status == "On hold" {
			for _, b := range it.BlockedBy {
				f.Blockers = append(f.Blockers, fpBlocker{b.Repo, b.Issue, b.State, b.MergedPR})
			}
			slices.SortFunc(f.Blockers, func(a, b fpBlocker) int {
				return cmp.Or(cmp.Compare(a.Repo, b.Repo), cmp.Compare(a.Issue, b.Issue))
			})
		}
		if hasRun[it.Issue] {
			for _, p := range it.PRs {
				f.PRs = append(f.PRs, fpPR{p.Number, p.State, p.Draft, p.Mergeable, p.HeadSHA, p.Checks})
			}
			slices.SortFunc(f.PRs, func(a, b fpPR) int { return cmp.Compare(a.Number, b.Number) })
		}
		items = append(items, f)
	}
	slices.SortFunc(items, func(a, b fpItem) int { return cmp.Compare(a.Issue, b.Issue) })

	data, err := json.Marshal(struct {
		Items []fpItem `json:"items"`
		Runs  []fpRun  `json:"runs"`
	}{items, runsIn})
	if err != nil {
		// Plain structs of strings, ints, bools and times cannot fail to marshal.
		panic("dispatch: fingerprint: " + err.Error())
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
