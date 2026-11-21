package review

// This file indexes the rulings of an SDD ledger as task-review findings. The
// ledger has no fixed format in this repo (it comes from the external
// subagent-driven-development skill); the parser reads only the plan line, the
// "Task <N>:" prefix and the "Ruling:" marker.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

// LedgerSource is the telemetry source of findings indexed from an SDD ledger.
const LedgerSource = "sdd-ledger"

var (
	ledgerPlanRe   = regexp.MustCompile(`^# SDD ledger — plan: (.+)$`)
	ledgerTaskRe   = regexp.MustCompile(`^Task (\d+):`)
	ledgerParkedRe = regexp.MustCompile(`^Task \d+: parked\b`)
	ledgerIssueRe  = regexp.MustCompile(`issue-(\d+)`)
)

// LedgerFinding is one ruling of a task in an SDD ledger.
type LedgerFinding struct {
	Task     int
	Text     string
	Decision string
}

// ParseLedger returns the plan path from the first line and one finding per
// "Task <N>:" line that holds a "Ruling:". A parked task keeps its code
// (rejected); any other ruling is left open. A "Ruling:" line without a task
// prefix is not a task review and is skipped.
func ParseLedger(data []byte) (planPath string, findings []LedgerFinding) {
	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 {
		if m := ledgerPlanRe.FindStringSubmatch(strings.TrimSpace(lines[0])); m != nil {
			planPath = strings.TrimSpace(m[1])
		}
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		m := ledgerTaskRe.FindStringSubmatch(line)
		i := strings.Index(line, "Ruling:")
		if m == nil || i < 0 {
			continue
		}
		task, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		decision := "left_open"
		if ledgerParkedRe.MatchString(line) {
			decision = "rejected"
		}
		findings = append(findings, LedgerFinding{
			Task: task, Text: strings.TrimSpace(line[i+len("Ruling:"):]), Decision: decision,
		})
	}
	return planPath, findings
}

// LedgerIssue returns the issue number in the file name of a plan path.
func LedgerIssue(planPath string) (int, bool) {
	name := planPath
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	m := ledgerIssueRe.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// IndexLedger indexes the rulings of the ledger at path under trajectory tr.
// A nil tr skips the file with a reason. now stamps RecordedAt, since a ledger
// has no written_at; it does not influence Unchanged, which compares sums.
func IndexLedger(ctx context.Context, db *sql.DB, path string, tr *Trajectory, now func() time.Time) (IndexResult, error) {
	var res IndexResult
	if tr == nil || tr.Repo == "" || tr.Issue < 1 {
		res.Skipped = append(res.Skipped, Skip{Path: path, Reason: "no trajectory for the ledger: not linked to a repo and issue"})
		return res, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return res, err
	}
	planPath, findings := ParseLedger(data)
	sum, at := Sum(data), now()
	perTask := map[int]int{}
	for _, f := range findings {
		perTask[f.Task]++
		k := perTask[f.Task]
		sourceID := fmt.Sprintf("%s#%d#%d", planPath, f.Task, k)
		old, found, err := telemetry.FindingSum(ctx, db, LedgerSource, sourceID)
		if err != nil {
			return res, err
		}
		if found && old == sum {
			res.Unchanged++
			continue
		}
		if _, _, err := telemetry.RecordFinding(ctx, db, telemetry.FindingRecord{
			Origin: telemetry.Origin{Source: LedgerSource, SourceID: sourceID, RecordedAt: at},
			Repo:   tr.Repo, Issue: tr.Issue,
			Component: "task_review", FindingKey: fmt.Sprintf("task-%d-ruling-%d", f.Task, k),
			Decision: f.Decision, RecordPath: path, RecordSum: sum,
		}); err != nil {
			return res, err
		}
		if found {
			res.Updated++
		} else {
			res.New++
		}
	}
	return res, nil
}
