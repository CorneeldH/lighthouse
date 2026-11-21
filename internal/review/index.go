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
	Skipped                          []Skip
}

// IndexRecords walks <dataRoot>/{plan,spec}-review/<chain>/ and indexes the
// findings of the newest attempt of each chain. An attempt that is only
// staged (no record.json) counts as not succeeded: the findings of older
// attempts of that chain are removed and none are added.
func IndexRecords(ctx context.Context, db *sql.DB, dataRoot string, _ func() time.Time) (IndexResult, error) {
	var res IndexResult
	for _, kind := range []string{"plan", "spec"} {
		chains, err := os.ReadDir(filepath.Join(dataRoot, kind+"-review"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return res, err
		}
		for _, c := range chains {
			if !c.IsDir() {
				continue
			}
			if err := indexChain(ctx, db, dataRoot, kind, c.Name(), &res); err != nil {
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

func indexChain(ctx context.Context, db *sql.DB, dataRoot, kind, chain string, res *IndexResult) error {
	chainDir := filepath.Join(dataRoot, kind+"-review", chain)
	attempt, finished, err := newestAttempt(chainDir)
	if err != nil {
		return err
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
			return err
		}
	}
	removed, err := telemetry.DeleteFindingsUnder(ctx, db, IndexSource, chainPrefix, keep)
	if err != nil {
		return err
	}
	res.Removed += int(removed)
	if keep == "" {
		return nil
	}
	skip := func(reason string) {
		res.Skipped = append(res.Skipped, Skip{Path: recordPath, Reason: reason})
	}
	var doc struct {
		SchemaVersion int `json:"schema_version"`
		Trajectory    *struct {
			Repo  string `json:"repo"`
			Issue int    `json:"issue"`
		} `json:"trajectory"`
		Findings []struct {
			ID       string  `json:"finding_id"`
			Severity string  `json:"severity"`
			Decision *string `json:"decision"`
		} `json:"findings"`
		WrittenAt string `json:"written_at"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		skip("unreadable record: " + err.Error())
		return nil
	}
	if doc.Trajectory == nil || doc.Trajectory.Repo == "" || doc.Trajectory.Issue < 1 {
		skip(fmt.Sprintf("no trajectory in the record (schema %d): not linked to a repo and issue", doc.SchemaVersion))
		return nil
	}
	at, err := time.Parse(time.RFC3339, doc.WrittenAt)
	if err != nil {
		skip("bad written_at: " + err.Error())
		return nil
	}
	sum := Sum(data)
	for _, f := range doc.Findings {
		sourceID := keep + f.ID
		old, found, err := telemetry.FindingSum(ctx, db, IndexSource, sourceID)
		if err != nil {
			return err
		}
		if found && old == sum {
			res.Unchanged++
			continue
		}
		decision := ""
		if f.Decision != nil {
			decision = FindingDecision(*f.Decision)
		}
		if _, _, err := telemetry.RecordFinding(ctx, db, telemetry.FindingRecord{
			Origin: telemetry.Origin{Source: IndexSource, SourceID: sourceID, RecordedAt: at},
			Repo:   doc.Trajectory.Repo, Issue: doc.Trajectory.Issue,
			Component: kind + "_review", FindingKey: f.ID, Severity: f.Severity, Decision: decision,
			RecordPath: recordPath, RecordSum: sum,
		}); err != nil {
			return err
		}
		if found {
			res.Updated++
		} else {
			res.New++
		}
	}
	return nil
}
