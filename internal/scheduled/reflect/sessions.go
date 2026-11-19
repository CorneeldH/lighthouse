package reflect

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
	"github.com/CorneeldH/lighthouse/internal/workplace/kennforge"
)

const (
	kindInteractive = "interactive"
	kindController  = "controller"

	// worktreeInfix is what Claude Code puts between a repo directory and the name of one of its
	// own worktrees (`claude --worktree`).
	worktreeInfix = "--claude-worktrees-"
)

// sessionFile is one Claude Code session of the repo: <id>.jsonl in one of its project directories.
type sessionFile struct {
	Path string
	ID   string
	Mod  time.Time
}

// sessionDirs are the project directories under home/.claude/projects that hold sessions of repo:
// its own directory, its Claude worktrees (the `--claude-worktrees-` form only: a clone next to it
// with a longer name is another project) and the worktrees kenn-forge makes for it. The last is
// derived from the kenn-forge adapter, not from a pattern of its own.
func sessionDirs(home string, repo repoconfig.Repo) []string {
	proj := workplace.ProjectDir(home, repo.Dir)
	forge := workplace.ProjectDir(home, kennforge.WorktreeRoot(home, repo.Kind, repo.Host, repo.Slug)) + "-"
	parent := filepath.Dir(proj)
	dirs := []string{proj}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return dirs
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(name, filepath.Base(proj)+worktreeInfix) || strings.HasPrefix(filepath.Join(parent, name), forge) {
			dirs = append(dirs, filepath.Join(parent, name))
		}
	}
	return dirs
}

// findSessions lists the sessions of repo. A directory that does not exist counts for nothing; a
// subagent transcript (a file below the session's own directory) is not a session.
func findSessions(home string, repo repoconfig.Repo) ([]sessionFile, error) {
	var out []sessionFile
	for _, d := range sessionDirs(home, repo) {
		entries, err := os.ReadDir(d)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			id, ok := strings.CutSuffix(e.Name(), ".jsonl")
			if !ok || !e.Type().IsRegular() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue // gone since the listing
			}
			out = append(out, sessionFile{Path: filepath.Join(d, e.Name()), ID: id, Mod: info.ModTime()})
		}
	}
	return out, nil
}

// classify reads the run history: the session ids of runs the controller started (every trigger
// but scheduled), and those of scheduled runs.
func classify(history []runs.Row) (controller, scheduledIDs map[string]bool) {
	controller, scheduledIDs = map[string]bool{}, map[string]bool{}
	for _, r := range history {
		switch {
		case r.SessionID == "":
		case r.Trigger == runs.TriggerScheduled:
			scheduledIDs[r.SessionID] = true
		default:
			controller[r.SessionID] = true
		}
	}
	return controller, scheduledIDs
}

// kind is controller when the database knows the session as a run of the controller, else
// interactive. The database is the source, not the path: a session a person opened in a kenn-forge
// workplace stays interactive.
func kind(sessionID string, controller map[string]bool) string {
	if controller[sessionID] {
		return kindController
	}
	return kindInteractive
}
