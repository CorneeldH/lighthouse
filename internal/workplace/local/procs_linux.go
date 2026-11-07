//go:build linux

package local

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// procFS reads a /proc tree; root and self are fields so tests can use a fake tree.
type procFS struct {
	root string
	uid  int
	self int
}

func newProcFS() Procs {
	return procFS{root: "/proc", uid: os.Getuid(), self: os.Getpid()}
}

func (p procFS) Marked(sid string) ([]int, error) {
	if sid == "" {
		return nil, errors.New("local workplace: empty session id")
	}
	entries, err := os.ReadDir(p.root)
	if err != nil {
		return nil, fmt.Errorf("local workplace: read %s: %w", p.root, err)
	}
	pids := []int{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == p.self {
			continue
		}
		ok, err := p.carries(pid, sid)
		if err != nil {
			return nil, err
		}
		if ok {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	return pids, nil
}

// carries reports whether pid is this user's and its environment holds markVar=<sid>. A pid that
// is gone, or an environ that is not ours to read, is false without error.
func (p procFS) carries(pid int, sid string) (bool, error) {
	dir := filepath.Join(p.root, strconv.Itoa(pid))
	info, err := os.Stat(dir)
	if err != nil {
		return false, nil // gone
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != p.uid {
		return false, nil
	}
	env, err := os.ReadFile(filepath.Join(dir, "environ"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.ESRCH) {
			return false, nil
		}
		return false, fmt.Errorf("local workplace: read environ of %d: %w", pid, err)
	}
	want := []byte(markVar + "=" + sid)
	for _, kv := range bytes.Split(env, []byte{0}) {
		if bytes.Equal(kv, want) {
			return true, nil
		}
	}
	return false, nil
}

func (p procFS) StartTime(pid int) (uint64, bool, error) {
	b, err := os.ReadFile(filepath.Join(p.root, strconv.Itoa(pid), "stat"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("local workplace: read stat of %d: %w", pid, err)
	}
	s := string(b)
	i := strings.LastIndex(s, ") ")
	if i < 0 {
		return 0, false, fmt.Errorf("local workplace: stat of %d has no comm field", pid)
	}
	// Fields after comm start at field 3 (state); field 22 is index 19.
	fields := strings.Fields(s[i+2:])
	if len(fields) < 20 {
		return 0, false, fmt.Errorf("local workplace: stat of %d has %d fields after comm, want 20", pid, len(fields))
	}
	if fields[0] == "Z" {
		return 0, false, nil
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("local workplace: starttime of %d: %w", pid, err)
	}
	return start, true, nil
}

// Signal first pins the process behind pid: on Linux os.FindProcess opens a pidfd, and a signal
// through it never reaches a later process that reuses the pid. Only then does it read the mark
// again; Marked read it earlier, and the pid may have changed hands since. When the pidfd holds
// a process that has ended, the mark read belongs to another process, but the signal goes to the
// ended one and is lost. Without a pidfd (an old kernel) os.Process signals by pid, and the mark
// read right before the signal is all that remains.
func (p procFS) Signal(pid int, sid string, sig syscall.Signal) error {
	if pid <= 0 {
		return fmt.Errorf("local workplace: refuse to signal pid %d", pid)
	}
	if sid == "" {
		return errors.New("local workplace: empty session id")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("local workplace: find %d: %w", pid, err)
	}
	defer func() { _ = proc.Release() }()
	ok, err := p.carries(pid, sid)
	if err != nil || !ok {
		return err
	}
	if err := proc.Signal(sig); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("local workplace: signal %d: %w", pid, err)
	}
	return nil
}
