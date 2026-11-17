//go:build unix

package cli

import "syscall"

// pusherAttr starts the pusher in a session of its own, so it outlives tick.
func pusherAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
