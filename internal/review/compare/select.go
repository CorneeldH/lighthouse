// Package compare reads the review records for the first comparison of #573: what the second
// reviewer adds to the first.
package compare

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/CorneeldH/lighthouse/internal/review"
)

// Finding is one finding of a record. Only round 1 carries Source and the severity per source.
type Finding struct {
	ID, Source, Severity, SevCodex, SevFallback, Decision string
	Round                                                 int
}

// Record is the newest attempt of one chain, kept for the comparison.
type Record struct {
	Kind, Chain, Path string
	Findings          []Finding
	Paired            bool
	Assignment        string
	AssignmentApplied *bool
	// Repo and Issue name the trajectory; Linked is false when it is not unambiguous, and the
	// record then counts for findings only, not for recovery.
	Repo   string
	Issue  int
	Linked bool
}

// Dropped counts the chains that did not make it into the selection, by the first reason.
type Dropped struct {
	Fallback, OtherMode, NoCodexVersion, NotPaired, NotFinished, Unreadable int
}

// Select returns, per chain, the newest attempt when it is a paired review_mode codex record with
// a filled reviewer.codex_version, and counts the rest. The checks run in order: mode, paired,
// codex_version. A trajectory comes from the record, else from review.Trace over repos.
func Select(dataRoot string, repos []review.ServedRepo) (kept []Record, dropped Dropped, err error) {
	for _, kind := range []string{"plan", "spec"} {
		chains, rerr := os.ReadDir(filepath.Join(dataRoot, kind+"-review"))
		if errors.Is(rerr, fs.ErrNotExist) {
			continue
		}
		if rerr != nil {
			return nil, dropped, rerr
		}
		for _, c := range chains {
			if !c.IsDir() {
				continue
			}
			rec, reason := selectChain(dataRoot, kind, c.Name(), repos)
			switch reason {
			case "":
				kept = append(kept, rec)
			case "fallback":
				dropped.Fallback++
			case "other":
				dropped.OtherMode++
			case "version":
				dropped.NoCodexVersion++
			case "paired":
				dropped.NotPaired++
			case "finished":
				dropped.NotFinished++
			default:
				dropped.Unreadable++
			}
		}
	}
	return kept, dropped, nil
}

func selectChain(dataRoot, kind, chain string, repos []review.ServedRepo) (Record, string) {
	path, ok, err := review.NewestRecordPath(filepath.Join(dataRoot, kind+"-review", chain))
	if err != nil {
		return Record{}, "unreadable"
	}
	if !ok {
		return Record{}, "finished"
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Record{}, "finished"
	}
	if err != nil {
		return Record{}, "unreadable"
	}
	var doc struct {
		ChainKey    string `json:"chain_key"`
		ReviewedSHA string `json:"reviewed_sha"`
		IssueSpec   *struct {
			Issue int `json:"issue"`
		} `json:"issue_spec"`
		Trajectory *struct {
			Repo  string `json:"repo"`
			Issue int    `json:"issue"`
		} `json:"trajectory"`
		Reviewer struct {
			CodexVersion *string `json:"codex_version"`
		} `json:"reviewer"`
		Findings []struct {
			ID       string  `json:"finding_id"`
			Severity string  `json:"severity"`
			Decision *string `json:"decision"`
			Source   string  `json:"source"`
			Round    int     `json:"round"`
			SevBySrc struct {
				Codex    string `json:"codex"`
				Fallback string `json:"fallback"`
			} `json:"severity_by_source"`
		} `json:"findings"`
		ReviewMode        string `json:"review_mode"`
		Paired            bool   `json:"paired"`
		Assignment        string `json:"assignment"`
		AssignmentApplied *bool  `json:"assignment_applied"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return Record{}, "unreadable"
	}
	switch {
	case doc.ReviewMode == "fallback":
		return Record{}, "fallback"
	case doc.ReviewMode != "codex":
		return Record{}, "other"
	case !doc.Paired:
		return Record{}, "paired"
	case doc.Reviewer.CodexVersion == nil || *doc.Reviewer.CodexVersion == "":
		return Record{}, "version"
	}
	r := Record{Kind: kind, Chain: chain, Path: path, Paired: doc.Paired,
		Assignment: doc.Assignment, AssignmentApplied: doc.AssignmentApplied}
	if t := doc.Trajectory; t != nil && t.Repo != "" && t.Issue >= 1 {
		r.Repo, r.Issue, r.Linked = t.Repo, t.Issue, true
	} else if len(repos) > 0 {
		var spec *int
		if doc.IssueSpec != nil {
			spec = &doc.IssueSpec.Issue
		}
		if tr, ok := review.Trace(doc.ChainKey, spec, doc.ReviewedSHA, repos, review.HasCommit); ok {
			r.Repo, r.Issue, r.Linked = tr.Repo, tr.Issue, true
		}
	}
	for _, f := range doc.Findings {
		d := ""
		if f.Decision != nil {
			d = review.FindingDecision(*f.Decision)
		}
		r.Findings = append(r.Findings, Finding{ID: f.ID, Source: f.Source, Severity: f.Severity,
			SevCodex: f.SevBySrc.Codex, SevFallback: f.SevBySrc.Fallback, Decision: d, Round: f.Round})
	}
	return r, ""
}
