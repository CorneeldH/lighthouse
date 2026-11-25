//go:build !windows

package config

import (
	"errors"
	"syscall"
)

// addrNotAvail is a bind on an address that is not on this machine.
func addrNotAvail(err error) bool { return errors.Is(err, syscall.EADDRNOTAVAIL) }
