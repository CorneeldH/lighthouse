//go:build unix

package deadline

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// forwarded are the signals that can stop the hook itself; the child runs in its own process
// group and so does not get them by itself.
var forwarded = []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP}

// inOwnGroup makes the child a process group leader: pgid == pid.
func inOwnGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// signalGroup sends sig to the whole group of the child; a group already gone is no error.
func signalGroup(cmd *exec.Cmd, sig os.Signal) {
	s, ok := sig.(syscall.Signal)
	if !ok {
		s = syscall.SIGTERM
	}
	if err := syscall.Kill(-cmd.Process.Pid, s); err != nil && !errors.Is(err, syscall.ESRCH) {
		_ = cmd.Process.Signal(s)
	}
}

// hardKill is SIGKILL to the group.
func hardKill(cmd *exec.Cmd) { signalGroup(cmd, syscall.SIGKILL) }
