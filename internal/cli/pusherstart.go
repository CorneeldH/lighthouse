package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
)

// startPusher is dispatch.Env.StartPusher: it resolves every path of the pusher in tick's own
// environment and starts `lighthouse push-workflows` with them (pushWorkflowsArgs), so tick and
// the pusher open one database and one pause flag. It returns once the process runs.
func (e tickEnv) startPusher(stateDir string) func(context.Context, dispatch.PusherSpec) error {
	return func(ctx context.Context, spec dispatch.PusherSpec) error {
		db, pause, clone, err := pusherPaths(e.Getenv, e.DBPath, spec.Repo)
		if err != nil {
			return err
		}
		host, err := filepath.Abs(e.HostToml)
		if err != nil || e.HostToml == "" {
			return fmt.Errorf("host.toml %q has no absolute path for the pusher", e.HostToml)
		}
		args := pushWorkflowsArgs(pushOptions{
			repo: spec.Repo, issue: spec.Issue, worktree: spec.Worktree, permit: spec.Permit,
			runID: spec.RunID, db: db, pauseFile: pause, cloneDir: clone, stateDir: stateDir, hostToml: host,
		})
		spawn := e.Spawn
		if spawn == nil {
			spawn = spawnDetached
		}
		return spawn(ctx, args, spec.Env)
	}
}

// spawnDetached starts this binary with args and exactly env, in a session of its own, with no
// stdin, stdout or stderr of tick's. It does not wait for the process: a goroutine reaps it, so
// no zombie stays while tick runs, and a tick that ends first leaves it to init.
func spawnDetached(_ context.Context, args, env []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find lighthouse: %w", err)
	}
	// Not CommandContext: the pusher outlives the tick that started it.
	cmd := exec.Command(exe, args...)
	cmd.Env = env
	cmd.SysProcAttr = pusherAttr()
	// Nil Stdin, Stdout and Stderr are /dev/null, never tick's.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start push-workflows: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
