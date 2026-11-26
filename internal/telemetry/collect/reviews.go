package collect

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/review"
	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

// Review is one finished plan or spec review record with a trajectory, as far as the setup of
// a run needs it.
type Review struct {
	Kind         string // plan or spec
	Repo         string
	Issue        int
	At           time.Time // written_at
	Mode         string    // review_mode
	PromptHashes map[string]string
}

// promptHashKeys maps the hash fields of a record to the suffix of their key in
// setups.prompt_hashes: the reviewer prompt is the skill itself (review-plan, review-spec).
var promptHashKeys = []struct{ field, suffix string }{
	{"prompt_sha256", ""},
	{"verifier_prompt_sha256", ":verifier_prompt"},
	{"schema_sha256", ":schema"},
	{"verification_schema_sha256", ":verification_schema"},
}

// LoadReviews reads every finished attempt of every plan and spec review chain under dataRoot
// (the layout review-index walks). A record without trajectory is traced as review-index does
// (issue-<n> in chain_key or issue_spec.issue, the repo by reviewed_sha among repos); a record
// that stays untraced, or that cannot be read, is left out: telemetry is best effort here.
func LoadReviews(dataRoot string, repos []review.ServedRepo) []Review {
	if dataRoot == "" {
		return nil
	}
	var out []Review
	for _, kind := range []string{"plan", "spec"} {
		kindDir := filepath.Join(dataRoot, kind+"-review")
		chains, err := os.ReadDir(kindDir)
		if err != nil {
			continue
		}
		for _, c := range chains {
			if !c.IsDir() {
				continue
			}
			attempts, err := os.ReadDir(filepath.Join(kindDir, c.Name()))
			if err != nil {
				continue
			}
			for _, a := range attempts {
				if !a.IsDir() || strings.HasPrefix(a.Name(), ".") {
					continue // a staging attempt is not finished
				}
				if r, ok := readReview(filepath.Join(kindDir, c.Name(), a.Name(), "record.json"), kind, repos); ok {
					out = append(out, r)
				}
			}
		}
	}
	return out
}

func readReview(path, kind string, repos []review.ServedRepo) (Review, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Review{}, false
	}
	var doc struct {
		ChainKey    string `json:"chain_key"`
		ReviewedSHA string `json:"reviewed_sha"`
		ReviewMode  string `json:"review_mode"`
		WrittenAt   string `json:"written_at"`
		IssueSpec   *struct {
			Issue int `json:"issue"`
		} `json:"issue_spec"`
		Trajectory *struct {
			Repo  string `json:"repo"`
			Issue int    `json:"issue"`
		} `json:"trajectory"`
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &doc) != nil || json.Unmarshal(data, &fields) != nil {
		return Review{}, false
	}
	at, err := time.Parse(time.RFC3339, doc.WrittenAt)
	if err != nil || at.IsZero() {
		return Review{}, false
	}
	r := Review{Kind: kind, At: at, Mode: doc.ReviewMode}
	if t := doc.Trajectory; t != nil && t.Repo != "" && t.Issue >= 1 {
		r.Repo, r.Issue = t.Repo, t.Issue
	} else {
		var spec *int
		if doc.IssueSpec != nil {
			spec = &doc.IssueSpec.Issue
		}
		tr, ok := review.Trace(doc.ChainKey, spec, doc.ReviewedSHA, repos, review.HasCommit)
		if !ok {
			return Review{}, false
		}
		r.Repo, r.Issue = tr.Repo, tr.Issue
	}
	for _, k := range promptHashKeys {
		var s string
		if json.Unmarshal(fields[k.field], &s) != nil || s == "" {
			continue // null or missing: the optional verifier files
		}
		if r.PromptHashes == nil {
			r.PromptHashes = map[string]string{}
		}
		r.PromptHashes["review-"+kind+k.suffix] = s
	}
	return r, true
}

// reviewsOfRun returns the reviews of run's trajectory that belong to it: the run of the
// trajectory whose [started_at, ended_at] holds the record time, the latest start before the
// record for an open run. A review that belongs to no run, or to another run, is left out.
func reviewsOfRun(ctx context.Context, db *sql.DB, run runInfo, all []Review) ([]Review, error) {
	if run.issue == 0 {
		return nil, nil
	}
	var mine []Review
	for _, r := range all {
		if r.Issue != run.issue || !strings.EqualFold(r.Repo, run.repo) {
			continue
		}
		id, err := runAt(ctx, db, run.repo, run.issue, r.At)
		if err != nil {
			return nil, err
		}
		if id == run.ID {
			mine = append(mine, r)
		}
	}
	return mine, nil
}

// runAt is the run of the trajectory (repo, issue) at moment at, or 0 when none holds it.
func runAt(ctx context.Context, db *sql.DB, repo string, issue int, at time.Time) (int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT r.id, r.started_at, r.ended_at FROM runs r
		JOIN trajectories t ON t.id = r.trajectory_id
		WHERE t.repo = ? COLLATE NOCASE AND t.issue = ?`, repo, issue)
	if err != nil {
		return 0, fmt.Errorf("runs of %s#%d: %w", repo, issue, err)
	}
	defer func() { _ = rows.Close() }()
	var best int64
	var bestStart time.Time
	for rows.Next() {
		var id int64
		var started string
		var ended sql.NullString
		if err := rows.Scan(&id, &started, &ended); err != nil {
			return 0, fmt.Errorf("runs of %s#%d: %w", repo, issue, err)
		}
		s, err := time.Parse(time.RFC3339Nano, started)
		if err != nil {
			return 0, fmt.Errorf("run %d started_at: %w", id, err)
		}
		if s.After(at) {
			continue
		}
		if ended.Valid {
			e, err := time.Parse(time.RFC3339Nano, ended.String)
			if err != nil {
				return 0, fmt.Errorf("run %d ended_at: %w", id, err)
			}
			if e.Before(at) {
				continue
			}
		}
		if best == 0 || s.After(bestStart) || (s.Equal(bestStart) && id > best) {
			best, bestStart = id, s
		}
	}
	return best, rows.Err()
}

// deploymentOf is what the reviews of a run add to its setup: the prompt hashes (a later
// record of the same skill wins) and a reviewer switch for each fallback from Codex to Opus.
func deploymentOf(reviews []Review) (map[string]string, []telemetry.SetupSwitch) {
	var hashes map[string]string
	var switches []telemetry.SetupSwitch
	latest := map[string]time.Time{}
	for _, r := range reviews {
		for k, v := range r.PromptHashes {
			if t, ok := latest[k]; ok && t.After(r.At) {
				continue
			}
			if hashes == nil {
				hashes = map[string]string{}
			}
			hashes[k], latest[k] = v, r.At
		}
		if r.Mode == "fallback" {
			switches = append(switches, telemetry.SetupSwitch{At: r.At, Field: "reviewer", From: "codex", To: "opus"})
		}
	}
	return hashes, switches
}
