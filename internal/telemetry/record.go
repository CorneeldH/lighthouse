package telemetry

// This file holds the idempotent writers for the telemetry tables. Each
// writer runs in one transaction: a refused call leaves nothing behind, not
// even a trajectory. No transcript or review-record content is stored, only
// paths, sums, names and counts.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrSessionHasFindings: a new offering of a session would move it to
	// another trajectory (or unlink it) while findings point at the session.
	ErrSessionHasFindings = errors.New("telemetry: session has findings; its trajectory cannot change")
	// ErrSessionHasEvents: a new offering of a session would move it to
	// another trajectory (or unlink it) while events point at the session.
	ErrSessionHasEvents = errors.New("telemetry: session has events; its trajectory cannot change")
	// ErrTrajectoryMismatch: the run or session of a finding does not point
	// at the trajectory of the finding's repo and issue (or at none).
	ErrTrajectoryMismatch = errors.New("telemetry: run or session belongs to another trajectory")
)

// Origin says which writer recorded a row, from which source record, and
// when. SourceID is derived deterministically from the source record.
type Origin struct {
	Source     string
	SourceID   string
	RecordedAt time.Time
}

func (o Origin) validate() error {
	if o.Source == "" || o.SourceID == "" {
		return fmt.Errorf("telemetry: origin needs a source and a source id; got %q, %q", o.Source, o.SourceID)
	}
	if o.RecordedAt.IsZero() {
		return errors.New("telemetry: origin needs a recorded-at time")
	}
	return nil
}

// SessionRecord is one interactive session. Lines feed Link; the stored
// branch, trajectory and reason follow from it.
type SessionRecord struct {
	Origin
	Repo, Harness, SessionKey, TranscriptPath, DefaultBranch string
	Lines                                                    []Line
	StartedAt                                                time.Time
	EndedAt                                                  *time.Time
}

// SetupRecord is the setup of one run or one session: exactly one of RunID
// and SessionID is set.
type SetupRecord struct {
	Origin
	RunID, SessionID int64
	// A nil field is unknown and stored as NULL, never as ''. Lane is sdd or native.
	Model, Effort, Harness, HarnessVersion             *string
	SuperpowersVersion, LighthouseVersion, PackVersion *string
	SkillsHash, Lane                                   *string
	// PromptHashes maps a skill (review-plan, review-spec, review-plan:schema, ...) to its
	// sha256; ControllerSettings holds profile, spec_review, plan_review and at_round_limit.
	// Both are stored as a JSON object; nil keeps what a row already has (NULL on a new row).
	PromptHashes, ControllerSettings map[string]string
	// Switches are the changes of model or reviewer during the run. A switch already stored
	// for the setup (same at and field) is left as it is.
	Switches []SetupSwitch
}

// SetupSwitch is one change during a run: Field is model or reviewer; a fallback from Codex
// to Opus is {Field: "reviewer", From: "codex", To: "opus"}.
type SetupSwitch struct {
	At              time.Time
	Field, From, To string
}

// UsageRecord is the token use of one model in one run or session: exactly
// one of RunID and SessionID is set. Cost nil means unknown; a cost needs a
// CostKind (estimate, quota or actual) and an empty CostKind needs nil Cost.
type UsageRecord struct {
	Origin
	RunID, SessionID                                             int64
	Model                                                        string
	InputTokens, OutputTokens, CacheReadTokens, CacheWriteTokens int64
	Cost                                                         *float64
	CostKind                                                     string
}

// FindingRecord is one review finding on the trajectory (Repo, Issue),
// optionally on one of its runs or sessions. Decision "" means no decision
// yet (NULL). The record itself stays a file: only its path and sum are kept.
type FindingRecord struct {
	Origin
	Repo                                                             string
	Issue                                                            int
	RunID, SessionID                                                 int64
	Component, FindingKey, Severity, Decision, RecordPath, RecordSum string
	// ReviewerSource is codex, fallback or both; SeverityCodex and SeverityFallback
	// are the severity each reviewer gave. Empty is NULL.
	ReviewerSource, SeverityCodex, SeverityFallback string
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// nullID maps 0 to NULL.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullPtr maps a nil pointer to NULL.
func nullPtr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func oneOwner(runID, sessionID int64) error {
	if (runID == 0) == (sessionID == 0) {
		return fmt.Errorf("telemetry: exactly one of run and session must be set; got run %d, session %d", runID, sessionID)
	}
	return nil
}

// inTx runs fn in one transaction, committing only when fn succeeds.
func inTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) (int64, bool, error)) (int64, bool, error) {
	tx, err := db.BeginTx(ctx, nil) // _txlock=immediate: BEGIN IMMEDIATE
	if err != nil {
		return 0, false, fmt.Errorf("telemetry: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	id, isNew, err := fn(tx)
	if err != nil {
		return 0, false, err
	}
	if err := tx.Commit(); err != nil {
		return 0, false, fmt.Errorf("telemetry: commit: %w", err)
	}
	return id, isNew, nil
}

// resolveRepo returns the spelling of repo already stored, compared without
// case, from trajectories first and then runs; an unknown repo keeps its own
// spelling. An exact match wins over another spelling.
func resolveRepo(ctx context.Context, tx *sql.Tx, repo string) (string, error) {
	for _, table := range []string{"trajectories", "runs"} {
		var stored string
		err := tx.QueryRowContext(ctx, `SELECT repo FROM `+table+` WHERE repo = ? COLLATE NOCASE
			ORDER BY repo = ? DESC, id LIMIT 1`, repo, repo).Scan(&stored)
		if err == nil {
			return stored, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("telemetry: resolve repo in %s: %w", table, err)
		}
	}
	return repo, nil
}

// findTrajectory returns the id of the trajectory (repo, issue), and false
// when it does not exist. repo must already be resolved.
func findTrajectory(ctx context.Context, tx *sql.Tx, repo string, issue int) (int64, bool, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM trajectories WHERE repo = ? AND issue = ?`, repo, issue).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("telemetry: find trajectory: %w", err)
	}
	return id, true, nil
}

// ensureTrajectory returns the id of the trajectory (repo, issue), creating
// it at time at when it does not exist. repo must already be resolved.
func ensureTrajectory(ctx context.Context, tx *sql.Tx, repo string, issue int, at time.Time) (int64, error) {
	if issue < 1 {
		return 0, fmt.Errorf("telemetry: trajectory needs an issue above 0; got %d", issue)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO trajectories (repo, issue, created_at) VALUES (?, ?, ?)
		ON CONFLICT (repo, issue) DO NOTHING`, repo, issue, stamp(at)); err != nil {
		return 0, fmt.Errorf("telemetry: insert trajectory: %w", err)
	}
	id, _, err := findTrajectory(ctx, tx, repo, issue)
	return id, err
}

// insertOrFind runs an INSERT … ON CONFLICT DO NOTHING. When it inserted no
// row, it looks up the existing row with each lookup in turn: (source,
// source_id) first, then the object key.
func insertOrFind(ctx context.Context, tx *sql.Tx, table, insert string, args []any, lookups ...lookup) (int64, bool, error) {
	res, err := tx.ExecContext(ctx, insert, args...)
	if err != nil {
		return 0, false, fmt.Errorf("telemetry: insert %s: %w", table, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, false, fmt.Errorf("telemetry: insert %s: %w", table, err)
	}
	if n == 1 {
		id, err := res.LastInsertId()
		if err != nil {
			return 0, false, fmt.Errorf("telemetry: insert %s: %w", table, err)
		}
		return id, true, nil
	}
	for _, l := range lookups {
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM `+table+` WHERE `+l.where, l.args...).Scan(&id)
		if err == nil {
			return id, false, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, false, fmt.Errorf("telemetry: find %s: %w", table, err)
		}
	}
	return 0, false, fmt.Errorf("telemetry: %s row conflicted but was not found", table)
}

type lookup struct {
	where string
	args  []any
}

func bySource(o Origin) lookup {
	return lookup{"source = ? AND source_id = ?", []any{o.Source, o.SourceID}}
}

// byOwner finds a row of a run or a session, with extra conditions.
func byOwner(runID, sessionID int64, extra string, args ...any) lookup {
	if runID != 0 {
		return lookup{"run_id = ?" + extra, append([]any{runID}, args...)}
	}
	return lookup{"session_id = ?" + extra, append([]any{sessionID}, args...)}
}

// RecordSession records an interactive session, linked by Link. A new
// offering of the same session (harness, session key) updates branch,
// trajectory, basis, reason, ended_at and recorded_at, and reports not new;
// origin, started_at, transcript path and assignment stay. Moving a session
// with findings to another trajectory is refused with ErrSessionHasFindings.
// The find-then-write inside relies on the store's _txlock=immediate: the
// write lock is taken at BEGIN, so two writers never both find nothing.
func RecordSession(ctx context.Context, db *sql.DB, s SessionRecord) (int64, bool, error) {
	return inTx(ctx, db, func(tx *sql.Tx) (int64, bool, error) { return recordSessionTx(ctx, tx, s) })
}

func recordSessionTx(ctx context.Context, tx *sql.Tx, s SessionRecord) (int64, bool, error) {
	if err := s.validate(); err != nil {
		return 0, false, err
	}
	link := Link(s.DefaultBranch, s.Lines)
	repo, err := resolveRepo(ctx, tx, s.Repo)
	if err != nil {
		return 0, false, err
	}
	existing, found, err := findSession(ctx, tx, s)
	if err != nil {
		return 0, false, err
	}
	if found {
		if err := checkRetarget(ctx, tx, existing, repo, link.Issue); err != nil {
			return 0, false, err
		}
	}

	var trajectory, basis, reason any
	branch := ""
	if link.Issue > 0 {
		tid, err := ensureTrajectory(ctx, tx, repo, link.Issue, s.StartedAt)
		if err != nil {
			return 0, false, err
		}
		trajectory, basis, branch = tid, link.Basis, link.Branch
	} else {
		reason = link.Reason
	}
	var ended any
	if s.EndedAt != nil {
		ended = stamp(*s.EndedAt)
	}

	if found {
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET branch = ?, trajectory_id = ?, link_basis = ?,
				unlinked_reason = ?, ended_at = ?, recorded_at = ? WHERE id = ?`,
			branch, trajectory, basis, reason, ended, stamp(s.RecordedAt), existing.id); err != nil {
			return 0, false, fmt.Errorf("telemetry: update session: %w", err)
		}
		return existing.id, false, nil
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO sessions (source, source_id, recorded_at, repo, harness,
			session_key, transcript_path, branch, trajectory_id, link_basis, unlinked_reason, started_at, ended_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Source, s.SourceID, stamp(s.RecordedAt), repo, s.Harness, s.SessionKey, s.TranscriptPath,
		branch, trajectory, basis, reason, stamp(s.StartedAt), ended)
	if err != nil {
		return 0, false, fmt.Errorf("telemetry: insert session: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, false, fmt.Errorf("telemetry: insert session: %w", err)
	}
	return id, true, nil
}

func (s SessionRecord) validate() error {
	if err := s.Origin.validate(); err != nil {
		return err
	}
	if s.Repo == "" || s.Harness == "" || s.SessionKey == "" {
		return fmt.Errorf("telemetry: session needs a repo, harness and session key; got %q, %q, %q",
			s.Repo, s.Harness, s.SessionKey)
	}
	if s.StartedAt.IsZero() {
		return errors.New("telemetry: session needs a start time")
	}
	return nil
}

type storedSession struct {
	id         int64
	trajectory sql.NullInt64
}

// findSession finds the stored row of the same session (harness, session
// key), or of the same origin. An origin already used for another session
// is an error.
func findSession(ctx context.Context, tx *sql.Tx, s SessionRecord) (storedSession, bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, trajectory_id, harness = ? AND session_key = ? FROM sessions
		WHERE (harness = ? AND session_key = ?) OR (source = ? AND source_id = ?)`,
		s.Harness, s.SessionKey, s.Harness, s.SessionKey, s.Source, s.SourceID)
	if err != nil {
		return storedSession{}, false, fmt.Errorf("telemetry: find session: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var got storedSession
	var found bool
	for rows.Next() {
		var row storedSession
		var same bool
		if err := rows.Scan(&row.id, &row.trajectory, &same); err != nil {
			return storedSession{}, false, fmt.Errorf("telemetry: find session: %w", err)
		}
		if !same {
			return storedSession{}, false, fmt.Errorf("telemetry: source %s/%s already records another session",
				s.Source, s.SourceID)
		}
		got, found = row, true
	}
	if err := rows.Err(); err != nil {
		return storedSession{}, false, fmt.Errorf("telemetry: find session: %w", err)
	}
	return got, found, nil
}

// checkRetarget refuses a change of trajectory for a session with findings
// or events.
// It creates nothing: a trajectory that does not exist yet is a change.
func checkRetarget(ctx context.Context, tx *sql.Tx, existing storedSession, repo string, issue int) error {
	same := issue == 0 && !existing.trajectory.Valid
	if issue > 0 {
		id, ok, err := findTrajectory(ctx, tx, repo, issue)
		if err != nil {
			return err
		}
		same = ok && existing.trajectory.Valid && existing.trajectory.Int64 == id
	}
	if same {
		return nil
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM findings WHERE session_id = ?`, existing.id).Scan(&n); err != nil {
		return fmt.Errorf("telemetry: count session findings: %w", err)
	}
	if n > 0 {
		return ErrSessionHasFindings
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM trajectory_events WHERE session_id = ?`, existing.id).Scan(&n); err != nil {
		return fmt.Errorf("telemetry: count session events: %w", err)
	}
	if n > 0 {
		return ErrSessionHasEvents
	}
	return nil
}

// RecordSetup records the setup of one run or one session. A second offering
// by the same owner (same run or session) updates the derived fields, lane
// and recorded_at; source, source_id and owner stay, and isNew is false. A
// (source, source_id) already used for a different run or session returns
// that existing row's id with isNew false and changes nothing, per spec;
// callers must derive source_id per object.
func RecordSetup(ctx context.Context, db *sql.DB, s SetupRecord) (int64, bool, error) {
	return inTx(ctx, db, func(tx *sql.Tx) (int64, bool, error) { return recordSetupTx(ctx, tx, s) })
}

func recordSetupTx(ctx context.Context, tx *sql.Tx, s SetupRecord) (int64, bool, error) {
	if err := s.validate(); err != nil {
		return 0, false, err
	}
	if err := oneOwner(s.RunID, s.SessionID); err != nil {
		return 0, false, err
	}
	for _, w := range s.Switches {
		if w.At.IsZero() || w.From == "" || w.To == "" || (w.Field != "model" && w.Field != "reviewer") {
			return 0, false, fmt.Errorf("telemetry: setup switch needs a time, field model or reviewer, from and to; got %+v", w)
		}
	}
	hashes, err := jsonObject(s.PromptHashes)
	if err != nil {
		return 0, false, err
	}
	settings, err := jsonObject(s.ControllerSettings)
	if err != nil {
		return 0, false, err
	}
	fields := []any{nullPtr(s.Model), nullPtr(s.Effort), nullPtr(s.Harness), nullPtr(s.HarnessVersion),
		nullPtr(s.SuperpowersVersion), nullPtr(s.LighthouseVersion), nullPtr(s.PackVersion),
		nullPtr(s.SkillsHash), nullPtr(s.Lane), hashes, settings}
	args := append([]any{s.Source, s.SourceID, stamp(s.RecordedAt), nullID(s.RunID), nullID(s.SessionID)}, fields...)
	owner := byOwner(s.RunID, s.SessionID, "")
	id, isNew, err := insertOrFind(ctx, tx, "setups", `INSERT INTO setups (source, source_id, recorded_at, run_id, session_id,
		model, effort, harness, harness_version, superpowers_version, lighthouse_version, pack_version, skills_hash, lane,
		prompt_hashes, controller_settings)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		args, bySource(s.Origin), owner)
	if err != nil {
		return 0, false, err
	}
	if !isNew {
		upd := append(append([]any{}, fields...), stamp(s.RecordedAt), id)
		upd = append(upd, owner.args...)
		if _, err := tx.ExecContext(ctx, `UPDATE setups SET model = ?, effort = ?, harness = ?, harness_version = ?,
			superpowers_version = ?, lighthouse_version = ?, pack_version = ?, skills_hash = ?, lane = ?,
			prompt_hashes = coalesce(?, prompt_hashes), controller_settings = coalesce(?, controller_settings),
			recorded_at = ? WHERE id = ? AND `+owner.where, upd...); err != nil {
			return 0, false, fmt.Errorf("telemetry: update setup: %w", err)
		}
	}
	// Only the owner's own row gets switches: a (source, source_id) of another run or session
	// changes nothing, per RecordSetup.
	for _, w := range s.Switches {
		wargs := append([]any{id, stamp(w.At), w.Field, w.From, w.To, id}, owner.args...)
		if _, err := tx.ExecContext(ctx, `INSERT INTO setup_switches (setup_id, at, field, from_value, to_value)
			SELECT ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM setups WHERE id = ? AND `+owner.where+`)
			ON CONFLICT DO NOTHING`, wargs...); err != nil {
			return 0, false, fmt.Errorf("telemetry: insert setup switch: %w", err)
		}
	}
	return id, isNew, nil
}

// jsonObject is m as a JSON object with sorted keys, or NULL for a nil map.
func jsonObject(m map[string]string) (any, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("telemetry: encode %v: %w", m, err)
	}
	return string(b), nil
}

// RecordUsage records the tokens and cost of one model in one run or
// session. A second offering by the same owner and model updates the tokens,
// cost, cost kind and recorded_at; source, source_id and owner stay, and
// isNew is false. A (source, source_id) already used for a different owner
// or model returns that existing row's id with isNew false and changes
// nothing, per spec; callers must derive source_id per object.
func RecordUsage(ctx context.Context, db *sql.DB, u UsageRecord) (int64, bool, error) {
	return inTx(ctx, db, func(tx *sql.Tx) (int64, bool, error) { return recordUsageTx(ctx, tx, u) })
}

func recordUsageTx(ctx context.Context, tx *sql.Tx, u UsageRecord) (int64, bool, error) {
	if err := u.validate(); err != nil {
		return 0, false, err
	}
	if err := oneOwner(u.RunID, u.SessionID); err != nil {
		return 0, false, err
	}
	if u.Model == "" {
		return 0, false, errors.New("telemetry: usage needs a model")
	}
	if (u.Cost == nil) != (u.CostKind == "") {
		return 0, false, fmt.Errorf("telemetry: usage needs a cost and a cost kind together, or neither; got kind %q", u.CostKind)
	}
	var cost any
	if u.Cost != nil {
		cost = *u.Cost
	}
	owner := byOwner(u.RunID, u.SessionID, " AND model = ?", u.Model)
	id, isNew, err := insertOrFind(ctx, tx, "usage", `INSERT INTO usage (source, source_id, recorded_at, run_id, session_id,
		model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost, cost_kind)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		[]any{u.Source, u.SourceID, stamp(u.RecordedAt), nullID(u.RunID), nullID(u.SessionID), u.Model,
			u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens, cost, nullString(u.CostKind)},
		bySource(u.Origin), owner)
	if err != nil || isNew {
		return id, isNew, err
	}
	upd := []any{u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens, cost,
		nullString(u.CostKind), stamp(u.RecordedAt), id}
	upd = append(upd, owner.args...)
	if _, err := tx.ExecContext(ctx, `UPDATE usage SET input_tokens = ?, output_tokens = ?, cache_read_tokens = ?,
		cache_write_tokens = ?, cost = ?, cost_kind = ?, recorded_at = ? WHERE id = ? AND `+owner.where, upd...); err != nil {
		return 0, false, fmt.Errorf("telemetry: update usage: %w", err)
	}
	return id, false, nil
}

// RecordFinding records a finding on the trajectory (Repo, Issue). A run or
// session given with it must point at that trajectory, else
// ErrTrajectoryMismatch; nothing is written then, not even the trajectory. A
// second offering of the same origin updates severity, decision, record path,
// record sum, recorded_at and the reviewer columns, and reports not new.
func RecordFinding(ctx context.Context, db *sql.DB, f FindingRecord) (int64, bool, error) {
	return inTx(ctx, db, func(tx *sql.Tx) (int64, bool, error) { return recordFindingTx(ctx, tx, f) })
}

func recordFindingTx(ctx context.Context, tx *sql.Tx, f FindingRecord) (int64, bool, error) {
	if err := f.validate(); err != nil {
		return 0, false, err
	}
	if f.Repo == "" || f.Issue < 1 {
		return 0, false, fmt.Errorf("telemetry: finding needs a repo and an issue above 0; got %q, %d", f.Repo, f.Issue)
	}
	if f.RunID != 0 && f.SessionID != 0 {
		return 0, false, fmt.Errorf("telemetry: finding takes a run or a session, not both; got run %d, session %d",
			f.RunID, f.SessionID)
	}
	repo, err := resolveRepo(ctx, tx, f.Repo)
	if err != nil {
		return 0, false, err
	}
	if err := checkOwnerFits(ctx, tx, f, repo); err != nil {
		return 0, false, err
	}
	tid, err := ensureTrajectory(ctx, tx, repo, f.Issue, f.RecordedAt)
	if err != nil {
		return 0, false, err
	}
	id, isNew, err := insertOrFind(ctx, tx, "findings", `INSERT INTO findings (source, source_id, recorded_at,
		trajectory_id, run_id, session_id, component, finding_key, severity, decision, record_path, record_sum,
		reviewer_source, severity_codex, severity_fallback)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		[]any{f.Source, f.SourceID, stamp(f.RecordedAt), tid, nullID(f.RunID), nullID(f.SessionID),
			f.Component, f.FindingKey, f.Severity, nullString(f.Decision), f.RecordPath, f.RecordSum,
			nullString(f.ReviewerSource), nullString(f.SeverityCodex), nullString(f.SeverityFallback)},
		bySource(f.Origin))
	if err != nil || isNew {
		return id, isNew, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE findings SET severity = ?, decision = ?, record_path = ?,
		record_sum = ?, recorded_at = ?, reviewer_source = ?, severity_codex = ?, severity_fallback = ? WHERE id = ?`,
		f.Severity, nullString(f.Decision), f.RecordPath, f.RecordSum, stamp(f.RecordedAt),
		nullString(f.ReviewerSource), nullString(f.SeverityCodex), nullString(f.SeverityFallback), id); err != nil {
		return 0, false, fmt.Errorf("telemetry: update finding: %w", err)
	}
	return id, false, nil
}

// checkOwnerFits checks that the run or session of a finding points at the
// trajectory (repo, issue). It creates nothing.
func checkOwnerFits(ctx context.Context, tx *sql.Tx, f FindingRecord, repo string) error {
	if f.RunID == 0 && f.SessionID == 0 {
		return nil
	}
	table, owner := "runs", f.RunID
	if f.SessionID != 0 {
		table, owner = "sessions", f.SessionID
	}
	var got sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT trajectory_id FROM `+table+` WHERE id = ?`, owner).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("telemetry: finding names %s %d, which does not exist", table, owner)
	}
	if err != nil {
		return fmt.Errorf("telemetry: read %s %d: %w", table, owner, err)
	}
	want, ok, err := findTrajectory(ctx, tx, repo, f.Issue)
	if err != nil {
		return err
	}
	if !got.Valid || !ok || got.Int64 != want {
		return fmt.Errorf("%w: %s %d is not on %s#%d", ErrTrajectoryMismatch, table, owner, repo, f.Issue)
	}
	return nil
}

// FindingSum returns the record_sum stored for the finding with this origin.
func FindingSum(ctx context.Context, db *sql.DB, source, sourceID string) (string, bool, error) {
	var sum string
	err := db.QueryRowContext(ctx, `SELECT record_sum FROM findings WHERE source = ? AND source_id = ?`,
		source, sourceID).Scan(&sum)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("telemetry: read finding sum: %w", err)
	}
	return sum, true, nil
}

// DeleteFindingsUnder deletes the findings of source whose source_id starts
// with prefix and not with keep (all of them under prefix when keep is
// empty), and returns how many. It compares with substr, never LIKE: "_" and
// "%" in a chain name are no wildcards.
func DeleteFindingsUnder(ctx context.Context, db *sql.DB, source, prefix, keep string) (int64, error) {
	n, _, err := inTx(ctx, db, func(tx *sql.Tx) (int64, bool, error) {
		query := `DELETE FROM findings WHERE source = ? AND substr(source_id, 1, length(?)) = ?`
		args := []any{source, prefix, prefix}
		if keep != "" {
			query += ` AND substr(source_id, 1, length(?)) != ?`
			args = append(args, keep, keep)
		}
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return 0, false, fmt.Errorf("telemetry: delete findings: %w", err)
		}
		n, err := res.RowsAffected()
		return n, false, err
	})
	return n, err
}
