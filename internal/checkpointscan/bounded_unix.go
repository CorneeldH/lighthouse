//go:build unix

package checkpointscan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

// repairRuntimeDir: VS Code `claude` runs in the logind scope without XDG_RUNTIME_DIR and
// sometimes without DBUS_SESSION_BUS_ADDRESS; without a bus `systemctl --user` fails and the
// ceiling drops out on exactly the path that needs it. Same repair as the commit-test-gate.
func repairRuntimeDir() {
	runtime := fmt.Sprintf("/run/user/%d", os.Getuid())
	if os.Getenv("XDG_RUNTIME_DIR") != "" || os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "" {
		return
	}
	if st, err := os.Stat(runtime); err == nil && st.IsDir() {
		_ = os.Setenv("XDG_RUNTIME_DIR", runtime)
	}
}

// probeCeiling says why there is no scope here: the first missing tool, or a user bus that
// does not answer. "" means there is one.
func probeCeiling() string {
	repairRuntimeDir()
	for _, tool := range []string{"systemctl", "systemd-run", "choom"} {
		if _, err := exec.LookPath(tool); err != nil {
			return "no " + tool
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "systemctl", "--user", "show-environment").Run(); err != nil {
		return "systemctl --user failed"
	}
	return ""
}

// unboundedCommand is what runs without a scope: still GOMAXPROCS and nice.
func unboundedCommand(env string, argv []string) []string {
	return append([]string{"env", env, "nice", "-n", "10"}, argv...)
}

// execWait runs argv with stdout and stderr in out and waits for it with wait4: only wait4
// returns the child's rusage. Every link of the chain (systemd-run --scope, env, choom, nice)
// execs, so it is the command's. Its floor is this process's RSS at the fork.
func execWait(argv []string, cwd string, out *os.File) (int, *int, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = cwd, out, out
	if err := cmd.Start(); err != nil {
		return 0, nil, err
	}
	var ws syscall.WaitStatus
	var ru syscall.Rusage
	for {
		_, err := syscall.Wait4(cmd.Process.Pid, &ws, 0, &ru)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			_ = cmd.Process.Release()
			return 0, nil, err
		}
		break
	}
	_ = cmd.Process.Release()
	return exitCode(ws), peakRSSKB(int64(ru.Maxrss)), nil
}

// exitCode is the exit status, or -<signal> for a command a signal killed (Go's ExitCode says
// -1 for that), so a SIGKILL from the memory ceiling stays -9.
func exitCode(ws syscall.WaitStatus) int {
	if ws.Signaled() {
		return -int(ws.Signal())
	}
	return ws.ExitStatus()
}

// peakRSSKB converts ru_maxrss: KB on Linux, bytes on darwin; other systems are not measured.
func peakRSSKB(maxrss int64) *int {
	var kb int
	switch runtime.GOOS {
	case "linux":
		kb = int(maxrss)
	case "darwin":
		kb = int(maxrss / 1024)
	default:
		return nil
	}
	return &kb
}
