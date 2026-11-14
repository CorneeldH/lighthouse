//go:build !unix

package deadline

import (
	"os"
	"os/exec"
	"syscall"
)

// forwarded are the signals this platform has (Windows has no SIGHUP).
var forwarded = []os.Signal{syscall.SIGTERM, os.Interrupt}

// InOwnGroup does nothing here: process groups are a Unix notion.
func InOwnGroup(*exec.Cmd) {}

// signalGroup kills the child itself. Unlike deadline.py (`taskkill /F /T`, the whole tree), a
// grandchild is not stopped here: process groups are a Unix notion, and the tree kill is not
// required of this port.
func signalGroup(cmd *exec.Cmd, _ os.Signal) { _ = cmd.Process.Kill() }

// KillGroup kills the child itself; a grandchild is not stopped (see signalGroup).
func KillGroup(cmd *exec.Cmd) { _ = cmd.Process.Kill() }
