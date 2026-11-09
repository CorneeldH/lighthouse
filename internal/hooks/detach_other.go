//go:build !unix

package hooks

import "syscall"

// detachAttr has no session to leave outside unix: the refresh starts without attributes.
func detachAttr() *syscall.SysProcAttr { return nil }
