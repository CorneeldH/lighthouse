//go:build !linux

package local

import (
	"errors"
	"syscall"
)

var errNeedsProc = errors.New("local workplace: needs /proc, which only linux has")

type noProcs struct{}

func newProcFS() Procs { return noProcs{} }

func (noProcs) Marked(string) ([]int, error)             { return nil, errNeedsProc }
func (noProcs) StartTime(int) (uint64, bool, error)      { return 0, false, errNeedsProc }
func (noProcs) Signal(int, string, syscall.Signal) error { return errNeedsProc }
