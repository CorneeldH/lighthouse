package config

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// addrNotAvail is a bind on an address that is not on this machine. Winsock answers
// WSAEADDRNOTAVAIL (10049), never syscall.EADDRNOTAVAIL (#559).
func addrNotAvail(err error) bool {
	return errors.Is(err, windows.WSAEADDRNOTAVAIL) || errors.Is(err, syscall.EADDRNOTAVAIL)
}
