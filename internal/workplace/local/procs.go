// Package local is the Workplace adapter that needs no Kenn-Forge: a git worktree and a
// detached `claude -p`.
package local

import "syscall"

// markVar marks every process of a run; the carrier sets it.
const markVar = "LIGHTHOUSE_CONTROLLER_RUN"

// Procs finds and signals the processes of a run.
type Procs interface {
	// Marked lists, sorted, the pids of this user whose environment holds markVar=<sid>, never
	// the calling process. A process that ends during the scan, or an environ that is not ours
	// to read, is skipped; an unreadable /proc, or an empty sid, is an error.
	Marked(sid string) ([]int, error)
	// StartTime is field 22 of /proc/<pid>/stat; ok is false when pid is gone or a zombie (state Z).
	StartTime(pid int) (start uint64, ok bool, err error)
	// Signal sends sig to pid; a pid that is gone is no error.
	Signal(pid int, sig syscall.Signal) error
}
