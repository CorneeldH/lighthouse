package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/telemetry/collect"
	"github.com/CorneeldH/lighthouse/internal/telemetry/transcript"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// collectEvery is how often the tick collects the transcripts of open runs.
const collectEvery = time.Hour

// lastCollectFile is the file under <state>/telemetry that holds the moment the tick last
// collected the transcripts of open runs.
const lastCollectFile = "last-collect"

// collectRef is one transcript the tick may collect: the runs of one (node, workspace, session).
type collectRef struct {
	node, workspace, session, profile string
	firstRun                          int64
	open, due                         bool    // some run is open; some run still needs a pass
	closed                            []int64 // the closed runs that made the ref due
}

// collectRuns collects, per repo, the transcripts of runs through the workplace that holds them
// (workplace.RawTranscripts), before the sweeps remove those workplaces and mirrors. A run that
// is open is collected at most once an hour (the moment is in <state>/telemetry/last-collect);
// a closed run goes every tick until its setups row was recorded after its end, unless one
// pass showed it gets none (see closedTriedFile). A run without
// a workspace or session, or whose workspace is marked removed, is left alone. A transcript the
// workplace cannot show (gone, not on this host, no file) gets an "unreachable, not processed"
// line and is no failure; any other error gets a line with the run id and the reason and sets
// failed, and the tick goes on. failed reports such a line.
func collectRuns(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, repos []repoconfig.Repo, errOut io.Writer) (failed bool) {
	stateDir, err := collect.StateDir(env.Getenv)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "lighthouse: collect: %s\n", oneLine(err))
		return true
	}
	now := env.now()
	last := readTimeFile(stateDir, lastCollectFile)
	openDue := last.IsZero() || now.Sub(last) >= collectEvery
	var openTried bool
	for _, repo := range repos {
		refs, err := collectRefs(ctx, db, repo.Slug, stateDir, now, openDue)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "lighthouse: collect %s: %s\n", repo.Slug, oneLine(err))
			failed = true
			continue
		}
		if len(refs) == 0 {
			continue
		}
		if collectRepo(ctx, env, db, cfg, repo, stateDir, refs, errOut) {
			failed = true
		}
		for _, r := range refs {
			openTried = openTried || r.open
		}
	}
	if openTried {
		if err := workplace.WriteFileAtomic(filepath.Join(stateDir, "telemetry", lastCollectFile),
			[]byte(now.UTC().Format(time.RFC3339Nano)+"\n")); err != nil {
			_, _ = fmt.Fprintf(errOut, "lighthouse: collect: %s: %s\n", lastCollectFile, oneLine(err))
			failed = true
		}
	}
	return failed
}

// readTimeFile is the moment in <state>/telemetry/<name>; zero when the file is missing or
// does not hold a time, which makes the step due at once.
func readTimeFile(stateDir, name string) time.Time {
	raw, err := os.ReadFile(filepath.Join(stateDir, "telemetry", name))
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(raw)))
	if err != nil {
		return time.Time{}
	}
	return t
}

// closedTriedDir is the folder under <state>/telemetry with one file per closed run that a pass
// could not give a setups row: the transcript was unreachable, or no line of it falls in the
// run (a resume run whose claude never started). Such a run would be due every tick, bypassing
// the hourly throttle, with a whole-transcript fetch and rewrite each time, until the sweep
// marks its workspace removed. With the moment of that pass on disk it is due once an hour. A
// run that a pass gave a row, or whose pass failed (the node did not answer), is not recorded:
// it stays due every tick, so a closed run is collected in the first tick after it closes.
const closedTriedDir = "closed-tried"

func closedTriedPath(stateDir string, id int64) string {
	return filepath.Join(stateDir, "telemetry", closedTriedDir, fmt.Sprintf("%d", id))
}

// triedRecently says a pass of the last collectEvery gave closed run id no setups row.
func triedRecently(stateDir string, id int64, now time.Time) bool {
	raw, err := os.ReadFile(closedTriedPath(stateDir, id))
	if err != nil {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(raw)))
	return err == nil && now.Sub(t) < collectEvery
}

// collectRefs lists the transcripts of repo to collect now, one per (node, workspace, session),
// in run order: the runs with a session and a workspace that no row marks removed, and that
// are open while openDue, or closed without a setups row recorded after their end.
func collectRefs(ctx context.Context, db *sql.DB, repo, stateDir string, now time.Time, openDue bool) ([]collectRef, error) {
	rows, err := db.QueryContext(ctx, `SELECT r.id, r.node, r.profile, r.workspace_id, r.session_id, r.ended_at,
			(SELECT recorded_at FROM setups WHERE run_id = r.id)
		FROM runs r
		WHERE r.repo = ? AND r.session_id != '' AND r.workspace_id != '' AND r.workspace_removed_at IS NULL
		ORDER BY r.id`, repo)
	if err != nil {
		return nil, fmt.Errorf("runs to collect: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []collectRef
	index := map[[3]string]int{}
	for rows.Next() {
		var id int64
		var node, profile, ws, sid string
		var ended, setup sql.NullString
		if err := rows.Scan(&id, &node, &profile, &ws, &sid, &ended, &setup); err != nil {
			return nil, fmt.Errorf("runs to collect: %w", err)
		}
		due := false
		var closedIDs []int64
		switch {
		case !ended.Valid:
			due = openDue
		case !setup.Valid:
			due = true
		default:
			e, eerr := time.Parse(time.RFC3339Nano, ended.String)
			s, serr := time.Parse(time.RFC3339Nano, setup.String)
			due = eerr != nil || serr != nil || s.Before(e)
		}
		tried := due && ended.Valid && triedRecently(stateDir, id, now)
		if tried {
			due = false
		} else if due && ended.Valid {
			closedIDs = append(closedIDs, id)
		}
		k := [3]string{node, ws, sid}
		i, ok := index[k]
		if !ok {
			i = len(out)
			index[k] = i
			out = append(out, collectRef{node: node, workspace: ws, session: sid, profile: profile, firstRun: id})
		}
		out[i].open = out[i].open || !ended.Valid
		out[i].due = out[i].due || due
		out[i].closed = append(out[i].closed, closedIDs...)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runs to collect: %w", err)
	}
	due := out[:0]
	for _, r := range out {
		if r.due {
			due = append(due, r)
		}
	}
	return due, nil
}

// collectRepo collects the transcripts refs of one repo; see collectRuns.
func collectRepo(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, repo repoconfig.Repo,
	stateDir string, refs []collectRef, errOut io.Writer) (failed bool) {
	opt, err := collectOptions(env, repo, stateDir)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "lighthouse: collect %s: %s\n", repo.Slug, oneLine(err))
		return true
	}
	dispatchDir, err := env.stateDir()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "lighthouse: collect %s: %s\n", repo.Slug, oneLine(err))
		return true
	}
	place := env.place(cfg, repo, dispatchDir)
	for _, ref := range refs {
		raw, ok := place(ref.node, ref.profile).(workplace.RawTranscripts)
		if !ok {
			continue
		}
		recorded := env.now()
		deltas, err := raw.RawTranscript(ctx, ref.workspace, ref.session)
		var out collect.Outcome
		if err == nil {
			out, err = processDeltas(ctx, db, ref, deltas, recorded, opt)
		}
		switch {
		case err == nil:
			noteClosedTried(errOut, stateDir, ref, out.RunIDs, recorded)
		case unreachable(err):
			noteClosedTried(errOut, stateDir, ref, nil, recorded)
		}
		switch {
		case err == nil && out.Kind == "skipped":
			_, _ = fmt.Fprintf(errOut, "lighthouse: collect %s: run %d: skip: %s\n", repo.Slug, ref.firstRun, out.Reason)
		case err == nil:
		case unreachable(err):
			_, _ = fmt.Fprintf(errOut, "lighthouse: collect %s: run %d: unreachable, not processed: %s\n", repo.Slug, ref.firstRun, oneLine(err))
		default:
			failed = true
			_, _ = fmt.Fprintf(errOut, "lighthouse: collect %s: run %d: %s\n", repo.Slug, ref.firstRun, oneLine(err))
		}
	}
	return failed
}

// noteClosedTried writes the moment of this pass for each closed run of ref that the pass gave
// no row (not in gotRow); see closedTriedDir. A failed write only costs a retry next tick.
func noteClosedTried(errOut io.Writer, stateDir string, ref collectRef, gotRow []int64, at time.Time) {
	for _, id := range ref.closed {
		if slices.Contains(gotRow, id) {
			continue
		}
		if err := workplace.WriteFileAtomic(closedTriedPath(stateDir, id), []byte(at.UTC().Format(time.RFC3339Nano)+"\n")); err != nil {
			_, _ = fmt.Fprintf(errOut, "lighthouse: collect: closed run %d: %s\n", id, oneLine(err))
		}
	}
}

// unreachable says the transcript is not there to read: the workspace is gone, there is no
// file, or this host cannot show it. A node that did not answer is not: it may answer next tick.
func unreachable(err error) bool {
	if errors.Is(err, workplace.ErrUnanswered) {
		return false
	}
	return errors.Is(err, workplace.ErrGone) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, workplace.ErrNotMeasurable)
}

// collectOptions are the repo facts of one collection: its slug, default branch, plans
// directory and the state directory of the sidecar files. The tick must keep serving a repo
// whose lighthouse.toml config.Load rejects (repoconfig loads it more leniently), so the plans
// directory then falls back to its default.
func collectOptions(env tickEnv, repo repoconfig.Repo, stateDir string) (collect.Options, error) {
	var plans string
	if cfg, err := config.Load(filepath.Join(repo.Dir, "lighthouse.toml")); err == nil {
		if plans, err = configValue(cfg, "paths.plans"); err != nil {
			return collect.Options{}, err
		}
	} else {
		def, ok := config.Default("paths.plans")
		if !ok {
			return collect.Options{}, err
		}
		plans, _ = def.(string)
	}
	return collect.Options{
		Repo: repo.Slug, DefaultBranch: repo.DefaultBranch, PlansDir: plans, RepoDir: repo.Dir,
		StateDir: stateDir, Now: env.Now, SinceLast: true,
	}, nil
}

// processDeltas hands the whole files of one session to collect.Process. The bytes carry no
// mtime, so Process settles a run by its end and its setups row.
func processDeltas(ctx context.Context, db *sql.DB, ref collectRef, deltas []workplace.FileDelta, recorded time.Time, opt collect.Options) (collect.Outcome, error) {
	files := make([]transcript.File, len(deltas))
	for i, d := range deltas {
		if d.From != 0 {
			return collect.Outcome{}, fmt.Errorf("transcript of %s: %s is no whole file", ref.session, d.Name)
		}
		files[i] = transcript.File{Name: d.Name, R: bytes.NewReader(d.Data)}
	}
	return collect.Process(ctx, db, collect.Source{
		SessionID: ref.session, TranscriptPath: "workplace:" + ref.workspace + "/" + ref.session + ".jsonl",
		Files: files, Recorded: recorded,
	}, opt)
}
