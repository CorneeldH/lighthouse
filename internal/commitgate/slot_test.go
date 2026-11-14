package commitgate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/flock"
)

// A second open of the same path is its own open file description and so clashes with a flock on
// the first, even within one process. That way a test keeps a slot "taken" without a second process.

// twoSlots opens slot-1 and slot-2 the way the hook does.
func twoSlots(t *testing.T) (dir string, files []*os.File) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "slots")
	files, err := OpenSlots(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, f := range files {
			_ = f.Close()
		}
	})
	return dir, files
}

// hold takes slot n of dir through its own open file description.
func hold(t *testing.T, dir string, n int) *os.File {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, "slot-"+string(rune('0'+n))), os.O_RDWR|os.O_CREATE, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	held, err := flock.TryFile(f)
	if err != nil || !held {
		t.Fatalf("hold slot %d: %v, %v", n, held, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestAcquireTakesTheFirstFreeSlot(t *testing.T) {
	_, files := twoSlots(t)
	if slot, err := Acquire(files, 0, time.Second); err != nil || slot != 1 {
		t.Fatalf("Acquire = %d, %v; want 1", slot, err)
	}
}

func TestAcquireSkipsAHeldSlot(t *testing.T) {
	dir, files := twoSlots(t)
	hold(t, dir, 1)
	if slot, err := Acquire(files, 0, time.Second); err != nil || slot != 2 {
		t.Fatalf("Acquire = %d, %v; want 2", slot, err)
	}
}

func TestAcquireGivesBusyWhenBothAreHeld(t *testing.T) {
	dir, files := twoSlots(t)
	hold(t, dir, 1)
	hold(t, dir, 2)
	start := time.Now()
	slot, err := Acquire(files, 0, time.Second)
	if !errors.Is(err, ErrSlotsBusy) || slot != 0 {
		t.Fatalf("Acquire = %d, %v; want ErrSlotsBusy", slot, err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("a wait of 0 took %v", time.Since(start))
	}
	if err.Error() != "all gate slots busy" {
		t.Errorf("text = %q", err)
	}
}

func TestAcquireWaitsForASlotThatFrees(t *testing.T) {
	dir, files := twoSlots(t)
	hold(t, dir, 1)
	second := hold(t, dir, 2)
	time.AfterFunc(300*time.Millisecond, func() { _ = second.Close() })
	if slot, err := Acquire(files, 2*time.Second, 100*time.Millisecond); err != nil || slot != 2 {
		t.Fatalf("Acquire = %d, %v; want 2", slot, err)
	}
}

// A closed file is a lock that was never tested: that is not "taken".
func TestAcquireReturnsTheErrorOfABadFile(t *testing.T) {
	_, files := twoSlots(t)
	_ = files[0].Close()
	slot, err := Acquire(files, 0, time.Second)
	if err == nil || errors.Is(err, ErrSlotsBusy) || slot != 0 {
		t.Fatalf("Acquire = %d, %v; want a lock error", slot, err)
	}
}

// The slot outlives Acquire: it stays taken as long as its file stays open, and the file of the
// slot that was not taken is closed.
func TestAcquireKeepsTheSlotHeldAndClosesTheOthers(t *testing.T) {
	dir, files := twoSlots(t)
	if slot, err := Acquire(files, 0, time.Second); err != nil || slot != 1 {
		t.Fatalf("Acquire = %d, %v; want 1", slot, err)
	}
	probe, err := os.OpenFile(filepath.Join(dir, "slot-1"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = probe.Close() }()
	if held, err := flock.TryFile(probe); err != nil || held {
		t.Errorf("slot-1 after Acquire: held = %v, %v; want it still taken", held, err)
	}
	if _, err := files[1].Stat(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("file of slot 2 after Acquire: %v, want closed", err)
	}
	if err := files[0].Close(); err != nil {
		t.Fatal(err)
	}
	if held, err := flock.TryFile(probe); err != nil || !held {
		t.Errorf("slot-1 after closing its file: held = %v, %v; want free", held, err)
	}
}

func TestOpenSlotsMakesAPrivateDirAndN(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "slots")
	files, err := OpenSlots(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		_ = f.Close()
	}
	if len(files) != 3 {
		t.Errorf("got %d files, want 3", len(files))
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("slot dir = %v, %v; want mode 0700", info, err)
	}
	for _, name := range []string{"slot-1", "slot-2", "slot-3"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Error(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "slot-4")); !os.IsNotExist(err) {
		t.Errorf("slot-4 exists: %v", err)
	}
}

func TestOpenSlotsDirErrorHasTheHookText(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(blocker, "slots")
	files, err := OpenSlots(dir, 2)
	var target *SlotDirError
	if !errors.As(err, &target) || files != nil {
		t.Fatalf("OpenSlots = %v, %v; want *SlotDirError", files, err)
	}
	if !strings.HasPrefix(err.Error(), "lock dir "+dir+" cannot be created (") || !strings.HasSuffix(err.Error(), ")") {
		t.Errorf("text = %q", err)
	}
}

func TestOpenSlotsFileErrorHasTheHookText(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "slot-2"), 0o700); err != nil {
		t.Fatal(err)
	}
	files, err := OpenSlots(dir, 2)
	var target *SlotOpenError
	if !errors.As(err, &target) || files != nil {
		t.Fatalf("OpenSlots = %v, %v; want *SlotOpenError", files, err)
	}
	if want := "lock files in " + dir + " cannot be opened"; err.Error() != want {
		t.Errorf("text = %q, want %q", err, want)
	}
}

func TestSlotDirFollowsTheThreeRules(t *testing.T) {
	for _, c := range []struct {
		name       string
		env        string
		uid        int
		runUserDir bool
		want       string
	}{
		{"env wins", "/x/slots", 1000, true, "/x/slots"},
		{"run user dir", "", 1000, true, "/run/user/1000/lighthouse-gate-slots"},
		{"tmp fallback", "", 1000, false, "/tmp/lighthouse-gate-slots-1000"},
	} {
		if got := slotDir(c.env, c.uid, c.runUserDir); got != c.want {
			t.Errorf("%s: slotDir = %q, want %q", c.name, got, c.want)
		}
	}
	t.Setenv("LIGHTHOUSE_GATE_SLOT_DIR", "/seam/slots")
	if got := SlotDir(); got != "/seam/slots" {
		t.Errorf("SlotDir with the seam = %q", got)
	}
}
