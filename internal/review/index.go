package review

// This file indexes review records into the telemetry database. Only paths,
// sums, names, severities and decisions go in; the record stays a file.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

// IndexSource is the telemetry source of findings indexed from review records.
const IndexSource = "plan-review-record"

// FindingDecision translates a plan or spec record decision into a
// findings.decision value; no decision (or an unknown one) is "".
func FindingDecision(recordDecision string) string {
	switch recordDecision {
	case "accept", "spec", "minor_yes":
		return "adopted"
	case "reject", "minor_no":
		return "rejected"
	case "accepted_open", "defer":
		return "left_open"
	}
	return ""
}

// Skip is a record that was not indexed, with the reason.
type Skip struct{ Path, Reason string }

// IndexResult counts what one IndexRecords run did.
type IndexResult struct {
	New, Updated, Unchanged, Removed int
	// Runs counts the review_runs rows written new or updated.
	Runs    int
	Skipped []Skip
}

// IndexRecords walks <dataRoot>/{plan,spec}-review/<chain>/ and indexes the
// findings of the newest attempt of each chain. An attempt that is only
// staged (no record.json) counts as not succeeded: the findings of older
// attempts of that chain are removed and none are added. A record without trajectory is traced
// through repos (see Trace); nil repos trace nothing.
func IndexRecords(ctx context.Context, db *sql.DB, dataRoot string, _ func() time.Time, repos []ServedRepo) (IndexResult, error) {
	var res IndexResult
	for _, kind := range []string{"plan", "spec"} {
		chains, err := os.ReadDir(filepath.Join(dataRoot, kind+"-review"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			res.Skipped = append(res.Skipped, Skip{Path: filepath.Join(dataRoot, kind+"-review"), Reason: "unreadable directory: " + err.Error()})
			continue
		}
		for _, c := range chains {
			if !c.IsDir() {
				continue
			}
			if err := indexChain(ctx, db, dataRoot, kind, c.Name(), repos, &res); err != nil {
				return res, err
			}
		}
	}
	return res, nil
}

// newestAttempt returns the greatest attempt name in chainDir and whether it
// has a finished (non-staging) directory.
func newestAttempt(chainDir string) (attempt string, finished bool, err error) {
	entries, err := os.ReadDir(chainDir)
	if err != nil {
		return "", false, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name, staging := e.Name(), false
		if strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".tmp") {
			name, staging = strings.TrimSuffix(strings.TrimPrefix(name, "."), ".tmp"), true
		}
		switch {
		case name > attempt:
			attempt, finished = name, !staging
		case name == attempt && !staging:
			finished = true
		}
	}
	return attempt, finished, nil
}

// reviewersOf derives who reviewed round 1 from the record's real composition: a fallback ran Opus
// alone (a Codex failure, or variant:…=opus recorded as fallback); a paired round 1 ran Codex and
// Opus; any other Codex round ran Codex alone (the agents skill, an Opus side that failed, or
// variant:…=codex that skipped it). A record without review_mode has no reviewers.
func reviewersOf(mode string, paired bool) []string {
	switch {
	case mode == "":
		return nil
	case mode == "fallback":
		return []string{"opus"}
	case paired:
		return []string{"codex", "opus"}
	}
	return []string{"codex"}
}

// unlinkedRepo is the repo of a review run that belongs to no trajectory:
// the row stores no repo, but RecordReviewRun wants a non-empty one.
const unlinkedRepo = "-"

// deleteChain removes, in one transaction, the rows of the chain outside keep
// ("" removes them all). Only findings count in Removed.
func deleteChain(ctx context.Context, db *sql.DB, chainPrefix, keep string, res *IndexResult) error {
	var removed int64
	err := telemetry.InTransaction(ctx, db, func(tx *telemetry.Tx) error {
		var err error
		removed, _, err = tx.DeleteChain(ctx, IndexSource, chainPrefix, keep)
		return err
	})
	if err == nil {
		res.Removed += int(removed)
	}
	return err
}

func indexChain(ctx context.Context, db *sql.DB, dataRoot, kind, chain string, repos []ServedRepo, res *IndexResult) error {
	chainDir := filepath.Join(dataRoot, kind+"-review", chain)
	// Data and read errors of one chain are skips, so that one bad chain does not
	// stop the others; only database errors are returned.
	attempt, finished, err := newestAttempt(chainDir)
	if err != nil {
		res.Skipped = append(res.Skipped, Skip{Path: chainDir, Reason: "unreadable chain: " + err.Error()})
		return nil
	}
	if attempt == "" {
		return nil
	}
	chainPrefix := kind + "/" + chain + "/"
	keep := ""
	recordPath := filepath.Join(chainDir, attempt, "record.json")
	var data []byte
	if finished {
		if data, err = os.ReadFile(recordPath); err == nil {
			keep = chainPrefix + attempt + "/"
		} else if !errors.Is(err, fs.ErrNotExist) {
			res.Skipped = append(res.Skipped, Skip{Path: recordPath, Reason: "unreadable record: " + err.Error()})
			return nil
		}
	}
	if keep == "" {
		return deleteChain(ctx, db, chainPrefix, "", res)
	}
	skip := func(reason string) {
		res.Skipped = append(res.Skipped, Skip{Path: recordPath, Reason: reason})
	}
	var doc struct {
		SchemaVersion int    `json:"schema_version"`
		ChainKey      string `json:"chain_key"`
		ReviewedSHA   string `json:"reviewed_sha"`
		IssueSpec     *struct {
			Issue int `json:"issue"`
		} `json:"issue_spec"`
		Trajectory *struct {
			Repo  string `json:"repo"`
			Issue int    `json:"issue"`
		} `json:"trajectory"`
		Findings []struct {
			ID       string  `json:"finding_id"`
			Severity string  `json:"severity"`
			Decision *string `json:"decision"`
			Source   string  `json:"source"`
			SevBySrc struct {
				Codex    string `json:"codex"`
				Fallback string `json:"fallback"`
			} `json:"severity_by_source"`
		} `json:"findings"`
		Outcome           string `json:"outcome"`
		WrittenAt         string `json:"written_at"`
		ReviewMode        string `json:"review_mode"`
		Paired            bool   `json:"paired"`
		Assignment        string `json:"assignment"`
		AssignmentApplied *bool  `json:"assignment_applied"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		skip("unreadable record: " + err.Error())
		return deleteChain(ctx, db, chainPrefix, keep, res)
	}
	at, err := time.Parse(time.RFC3339, doc.WrittenAt)
	if err == nil && at.IsZero() {
		err = errors.New("the zero time " + doc.WrittenAt)
	}
	if err != nil {
		skip("bad written_at: " + err.Error())
		return deleteChain(ctx, db, chainPrefix, keep, res)
	}
	linked := doc.Trajectory != nil && doc.Trajectory.Repo != "" && doc.Trajectory.Issue >= 1
	repo, issue := unlinkedRepo, 0
	if linked {
		repo, issue = doc.Trajectory.Repo, doc.Trajectory.Issue
	} else if len(repos) > 0 {
		var spec *int
		if doc.IssueSpec != nil {
			spec = &doc.IssueSpec.Issue
		}
		if tr, ok := Trace(doc.ChainKey, spec, doc.ReviewedSHA, repos, hasCommit); ok {
			linked, repo, issue = true, tr.Repo, tr.Issue
		}
	}
	if !linked {
		skip(fmt.Sprintf("no trajectory in the record (schema %d): not linked to a repo and issue", doc.SchemaVersion))
	}
	sum := Sum(data)
	runID := keep + "record.json"
	runSum, runFound, err := telemetry.ReviewRunSum(ctx, db, IndexSource, runID)
	if err != nil {
		return err
	}
	runCurrent := runFound && runSum == sum
	// Findings of an unlinked record are not written; the run row counts them anyway.
	type pending struct {
		sourceID, id, severity, decision string
		reviewer, sevCodex, sevFallback  string
		found                            bool
	}
	var todo []pending
	unchanged := 0
	if linked {
		for _, f := range doc.Findings {
			sourceID := keep + f.ID
			old, found, err := telemetry.FindingSum(ctx, db, IndexSource, sourceID)
			if err != nil {
				return err
			}
			if found && old == sum {
				unchanged++
				continue
			}
			decision := ""
			if f.Decision != nil {
				decision = FindingDecision(*f.Decision)
			}
			reviewer := ""
			if f.Source == "codex" || f.Source == "fallback" || f.Source == "both" {
				reviewer = f.Source
			}
			todo = append(todo, pending{sourceID, f.ID, f.Severity, decision, reviewer, f.SevBySrc.Codex, f.SevBySrc.Fallback, found})
		}
	}
	var removed int64
	err = telemetry.InTransaction(ctx, db, func(tx *telemetry.Tx) error {
		var err error
		if removed, _, err = tx.DeleteChain(ctx, IndexSource, chainPrefix, keep); err != nil {
			return err
		}
		if !runCurrent {
			if _, _, err := tx.RecordReviewRun(ctx, telemetry.ReviewRunRecord{
				Origin: telemetry.Origin{Source: IndexSource, SourceID: runID, RecordedAt: at},
				Repo:   repo, Issue: issue, Component: kind + "_review",
				RecordPath: recordPath, RecordSum: sum, RanAt: at,
				Outcome: doc.Outcome, FindingCount: len(doc.Findings),
				ReviewMode: doc.ReviewMode, Reviewers: reviewersOf(doc.ReviewMode, doc.Paired),
				Assignment: doc.Assignment, AssignmentApplied: doc.AssignmentApplied,
			}); err != nil {
				return err
			}
		}
		for _, f := range todo {
			if _, _, err := tx.RecordFinding(ctx, telemetry.FindingRecord{
				Origin: telemetry.Origin{Source: IndexSource, SourceID: f.sourceID, RecordedAt: at},
				Repo:   repo, Issue: issue,
				Component: kind + "_review", FindingKey: f.id, Severity: f.severity, Decision: f.decision,
				RecordPath: recordPath, RecordSum: sum,
				ReviewerSource: f.reviewer, SeverityCodex: f.sevCodex, SeverityFallback: f.sevFallback,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err // rolled back: nothing counts
	}
	res.Removed += int(removed)
	if !runCurrent {
		res.Runs++
	}
	res.Unchanged += unchanged
	for _, f := range todo {
		if f.found {
			res.Updated++
		} else {
			res.New++
		}
	}
	return nil
}
