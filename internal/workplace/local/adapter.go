package local

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Adapter is a workplace.Workplace on this host: one git worktree per branch, records under
// StateDir, and claude -p as a detached process.
type Adapter struct {
	RepoDir       string // main checkout of the repo (repoconfig.Repo.Dir)
	DefaultBranch string // [forge] default_branch
	Repo          string // [forge] repo, owner/name: records live under StateDir/local/<owner>/<name>
	StateDir      string // ${XDG_STATE_HOME}/lighthouse/dispatch
	Run           workplace.Runner
	Procs         Procs               // nil: newProcFS()
	Home          string              // "": os.UserHomeDir
	Sleep         func(time.Duration) // nil: time.Sleep
	Now           func() time.Time    // nil: time.Now
}

var (
	_ workplace.Workplace   = (*Adapter)(nil)
	_ workplace.Transcripts = (*Adapter)(nil)
)

// gitTimeout applies to every git command of the adapter, FollowRemote included.
const gitTimeout = 60 * time.Second

// errNotBuilt marks a method that a later task of #253 builds.
var errNotBuilt = errors.New("local: not built yet")

var localID = regexp.MustCompile(`^` + regexp.QuoteMeta(workplace.LocalIDPrefix) + `[A-Za-z0-9._-]+$`)

// checkLocal refuses an ID the local adapter never hands out, a kenn-forge one say. The error
// is no workplace.ErrGone: the workspace may well exist, only not here.
func checkLocal(id string) error {
	if !localID.MatchString(id) {
		return fmt.Errorf("workspace %s is not a local workspace", id)
	}
	return nil
}

var unsafeChar = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// newID is local-<slug>-<6 hex>: slug is branch with every character outside [A-Za-z0-9._-]
// turned into '-', and the hex is fresh per call, so a worktree made again on the same
// branch never reuses the ID of a removed one.
func newID(branch string) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b) // crypto/rand.Read never fails
	return workplace.LocalIDPrefix + unsafeChar.ReplaceAllString(branch, "-") + "-" + hex.EncodeToString(b)
}

func (a *Adapter) procs() Procs {
	if a.Procs == nil {
		return newProcFS()
	}
	return a.Procs
}

func (a *Adapter) now() time.Time {
	if a.Now == nil {
		return time.Now()
	}
	return a.Now()
}

// git runs one git command in dir and returns trimmed stdout and the exit code. A command
// that could not run or timed out is an error, never a negative answer.
func (a *Adapter) git(ctx context.Context, dir string, args ...string) (string, int, error) {
	out, code, err := a.Run(ctx, gitTimeout, "git", append([]string{"-C", dir}, args...)...)
	if err != nil {
		return "", code, fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(out), code, nil
}

// gitOK is git with a non-zero exit as an error.
func (a *Adapter) gitOK(ctx context.Context, dir string, args ...string) (string, error) {
	out, code, err := a.git(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("git %s: exit code %d", strings.Join(args, " "), code)
	}
	return out, nil
}

// Lookup reads the workspace record and its worktree. The branch comes from git, so a branch
// switched by hand shows; a vanished worktree whose run still has processes keeps the branch
// of the record.
func (a *Adapter) Lookup(ctx context.Context, workspaceID string) (workplace.Workspace, error) {
	if err := checkLocal(workspaceID); err != nil {
		return workplace.Workspace{}, err
	}
	rec, wt, err := a.resolve(ctx, workspaceID)
	if err != nil {
		return workplace.Workspace{}, err
	}
	branch := rec.Branch
	if wt != nil {
		branch = wt.Branch
	}
	return workplace.Workspace{ID: rec.ID, Path: rec.Path, Branch: branch}, nil
}

// Spawn is built in a later task.
func (a *Adapter) Spawn(_ context.Context, ws workplace.Workspace, _ workplace.LaunchSpec) (workplace.Session, error) {
	if err := checkLocal(ws.ID); err != nil {
		return workplace.Session{}, fmt.Errorf("%w: %w", workplace.ErrNotAccepted, err)
	}
	return workplace.Session{}, errNotBuilt
}

// Sessions is built in a later task.
func (a *Adapter) Sessions(_ context.Context, workspaceID string) ([]workplace.Session, error) {
	if err := checkLocal(workspaceID); err != nil {
		return nil, err
	}
	return nil, errNotBuilt
}

// Stop is built in a later task.
func (a *Adapter) Stop(_ context.Context, workspaceID string, _ []string) error {
	if err := checkLocal(workspaceID); err != nil {
		return err
	}
	return errNotBuilt
}

// Kill is built in a later task.
func (a *Adapter) Kill(_ context.Context, workspaceID string, _ []string) error {
	if err := checkLocal(workspaceID); err != nil {
		return err
	}
	return errNotBuilt
}

// Remove is built in a later task.
func (a *Adapter) Remove(_ context.Context, workspaceID string) error {
	if err := checkLocal(workspaceID); err != nil {
		return err
	}
	return errNotBuilt
}

// Discard is built in a later task.
func (a *Adapter) Discard(_ context.Context, workspaceID string) error {
	if err := checkLocal(workspaceID); err != nil {
		return err
	}
	return errNotBuilt
}

// Transcript is built in a later task.
func (a *Adapter) Transcript(_ context.Context, workspaceID, _ string) (workplace.Transcript, error) {
	if err := checkLocal(workspaceID); err != nil {
		return workplace.Transcript{}, err
	}
	return workplace.Transcript{}, errNotBuilt
}
