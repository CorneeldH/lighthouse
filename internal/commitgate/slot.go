package commitgate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/CorneeldH/lighthouse/internal/flock"
)

// MaxSlots is the most gate slots one host has.
const MaxSlots = 7

// ErrSlotsBusy reports that every slot was taken for the whole wait.
var ErrSlotsBusy = errors.New("all gate slots busy")

// SlotDirError is a slot directory that cannot be made.
type SlotDirError struct {
	Dir string
	Err error
}

func (e *SlotDirError) Error() string {
	return fmt.Sprintf("lock dir %s cannot be created (%v)", e.Dir, e.Err)
}
func (e *SlotDirError) Unwrap() error { return e.Err }

// SlotOpenError is a slot file that cannot be opened.
type SlotOpenError struct {
	Dir string
	Err error
}

func (e *SlotOpenError) Error() string {
	return fmt.Sprintf("lock files in %s cannot be opened", e.Dir)
}
func (e *SlotOpenError) Unwrap() error { return e.Err }

// slotDirFor is the slot directory of this host: see slotDir. gate.takeSlot uses it with the
// value of $LIGHTHOUSE_GATE_SLOT_DIR in the gate's own environment.
func slotDirFor(env string) string {
	_, runUser := runUserDir()
	return slotDir(runtime.GOOS, env, os.Getuid(), runUser, os.TempDir())
}

// slotDir is env ($LIGHTHOUSE_GATE_SLOT_DIR), else /run/user/$UID/lighthouse-gate-slots when that
// /run/user/$UID exists, else /tmp/lighthouse-gate-slots-$UID. Windows has no uid (os.Getuid is
// -1) and no /tmp: there it is lighthouse-gate-slots under tempDir, which is per user already.
func slotDir(goos, env string, uid int, runUserDirExists bool, tempDir string) string {
	switch {
	case env != "":
		return env
	case goos == "windows":
		return filepath.Join(tempDir, "lighthouse-gate-slots")
	case runUserDirExists:
		return "/run/user/" + strconv.Itoa(uid) + "/lighthouse-gate-slots"
	default:
		return "/tmp/lighthouse-gate-slots-" + strconv.Itoa(uid)
	}
}

// OpenSlots makes dir (0700) and opens slot-1 to slot-n in it. Read and write access, not append:
// LockFileEx on Windows needs one of them, and O_APPEND takes GENERIC_WRITE away there.
func OpenSlots(dir string, n int) ([]*os.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, &SlotDirError{Dir: dir, Err: err}
	}
	files := make([]*os.File, 0, n)
	for i := 1; i <= n; i++ {
		f, err := os.OpenFile(filepath.Join(dir, "slot-"+strconv.Itoa(i)), os.O_RDWR|os.O_CREATE, 0o666)
		if err != nil {
			for _, open := range files {
				_ = open.Close()
			}
			return nil, &SlotOpenError{Dir: dir, Err: err}
		}
		files = append(files, f)
	}
	return files, nil
}

// Acquire takes the first free slot and returns its number, counted from 1. The first round is
// immediate; after that it sleeps interval between rounds until wait has passed, and then gives
// ErrSlotsBusy (the files stay open for the caller to close). A held lock means "taken"; any other
// error of TryFile (a closed file, say) is returned unchanged, because then it was not tested
// whether the slot was free. After a success the files of the other slots are closed; the slot
// stays taken as long as its own file stays open.
func Acquire(files []*os.File, wait, interval time.Duration) (slot int, err error) {
	end := time.Now().Add(wait)
	for {
		for i, f := range files {
			held, err := flock.TryFile(f)
			if err != nil {
				return 0, err
			}
			if held {
				for j, other := range files {
					if j != i {
						_ = other.Close()
					}
				}
				return i + 1, nil
			}
		}
		if !time.Now().Before(end) {
			return 0, ErrSlotsBusy
		}
		time.Sleep(interval)
	}
}
