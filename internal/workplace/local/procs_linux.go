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
	want := []byte(markVar + "=" + sid)
	pids := []int{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == p.self {
			continue
		}
		dir := filepath.Join(p.root, e.Name())
		info, err := os.Stat(dir)
		if err != nil {
			continue // ended during the scan
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(st.Uid) != p.uid {
			continue
		}
		env, err := os.ReadFile(filepath.Join(dir, "environ"))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.ESRCH) {
				continue
			}
			return nil, fmt.Errorf("local workplace: read environ of %d: %w", pid, err)
		}
		for _, kv := range bytes.Split(env, []byte{0}) {
			if bytes.Equal(kv, want) {
				pids = append(pids, pid)
				break
			}
		}
	}
	sort.Ints(pids)
	return pids, nil
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

func (p procFS) Signal(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return fmt.Errorf("local workplace: refuse to signal pid %d", pid)
	}
	if err := syscall.Kill(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("local workplace: signal %d: %w", pid, err)
	}
	return nil
}
