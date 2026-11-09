//go:build unix

package hooks

import "syscall"

// detachAttr starts the refresh in a session of its own, so it outlives the hook.
func detachAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
