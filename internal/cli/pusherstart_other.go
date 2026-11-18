//go:build !unix

package cli

import "syscall"

// pusherAttr has no session to leave outside unix: the pusher starts without attributes.
func pusherAttr() *syscall.SysProcAttr { return nil }
