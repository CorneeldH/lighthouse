package commitgate

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const gibKB = 1024 * 1024

func writeMeminfo(t *testing.T, path string, availKB, swapFreeKB int) {
	t.Helper()
	text := "MemTotal: 16777216 kB\nMemFree: 123456 kB\n" +
		"MemAvailable: " + strconv.Itoa(availKB) + " kB\nSwapTotal: 25165824 kB\nSwapFree: " + strconv.Itoa(swapFreeKB) + " kB\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fptr(v float64) *float64 { return &v }

// fakeClock advances only through sleep.
type fakeClock struct {
	t      time.Time
	sleeps []time.Duration
	onTick func()
}

func (c *fakeClock) now() time.Time { return c.t }
func (c *fakeClock) sleep(d time.Duration) {
	c.sleeps = append(c.sleeps, d)
	c.t = c.t.Add(d)
	if c.onTick != nil {
		c.onTick()
	}
}

func newClock() *fakeClock { return &fakeClock{t: time.Unix(1000, 0)} }

func TestReadMeminfoMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent")
	_, err := ReadMeminfo(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("want an error naming %s, got %v", path, err)
	}
}

func TestReadMeminfoNoMemAvailableMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	if err := os.WriteFile(path, []byte("MemTotal: 1 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadMeminfo(path)
	if err == nil || err.Error() != path+": no MemAvailable: line" {
		t.Fatalf("got %v", err)
	}
}

func TestPyG(t *testing.T) {
	for v, want := range map[float64]string{4: "4", 0.75: "0.75", 1234567: "1.23457e+06", 1e16: "1e+16", 100000: "100000", 1e6: "1e+06", 0.0001: "0.0001", 0.00001: "1e-05"} {
		if got := pyG(v); got != want {
			t.Errorf("pyG(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestWaitMemoryOK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	writeMeminfo(t, path, 8*gibKB, 4*gibKB)
	c := newClock()
	notes, below, err := WaitMemory(path, Thresholds{fptr(4), fptr(2)}, 0, c.sleep, c.now)
	if err != nil || below != "" || len(notes) != 0 || len(c.sleeps) != 0 {
		t.Errorf("got %q %q %v sleeps %v", notes, below, err, c.sleeps)
	}
}

func TestWaitMemoryNotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	if err := os.WriteFile(path, []byte("MemAvailable: 8388608 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newClock()
	notes, below, err := WaitMemory(path, Thresholds{MinSwapFreeGiB: fptr(2)}, 0, c.sleep, c.now)
	if err != nil || below != "" || len(notes) != 1 || notes[0] != "no swap on this host, SwapFree not checked" {
		t.Errorf("got %q %q %v", notes, below, err)
	}
}

func TestWaitMemoryBelowJoinsShortfalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	writeMeminfo(t, path, 734003, 524288)
	c := newClock()
	_, below, err := WaitMemory(path, Thresholds{fptr(4), fptr(2)}, 0, c.sleep, c.now)
	want := "MemAvailable 0.7 GiB < 4 GiB; SwapFree 0.5 GiB < 2 GiB"
	if err != nil || below != want {
		t.Errorf("got %q %v, want %q", below, err, want)
	}
}

func TestWaitMemoryWaitsUntilEnough(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	writeMeminfo(t, path, 734003, 4*gibKB)
	c := newClock()
	c.onTick = func() {
		if len(c.sleeps) == 3 {
			writeMeminfo(t, path, 8*gibKB, 4*gibKB)
		}
	}
	_, below, err := WaitMemory(path, Thresholds{MinAvailableGiB: fptr(4)}, 5*time.Second, c.sleep, c.now)
	if err != nil || below != "" || len(c.sleeps) != 3 {
		t.Errorf("below %q err %v sleeps %v", below, err, c.sleeps)
	}
	for _, s := range c.sleeps {
		if s != time.Second {
			t.Errorf("sleep %v, want 1s", s)
		}
	}
}

func TestWaitMemoryDoesNotOvershoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	writeMeminfo(t, path, 734003, 4*gibKB)
	c := newClock()
	_, below, err := WaitMemory(path, Thresholds{MinAvailableGiB: fptr(4)}, 2500*time.Millisecond, c.sleep, c.now)
	if err != nil || below != "MemAvailable 0.7 GiB < 4 GiB" {
		t.Fatalf("got %q %v", below, err)
	}
	var total time.Duration
	for _, s := range c.sleeps {
		total += s
	}
	if total != 2500*time.Millisecond || c.sleeps[len(c.sleeps)-1] != 500*time.Millisecond {
		t.Errorf("sleeps %v", c.sleeps)
	}
}

func TestWaitMemoryUnreadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent")
	c := newClock()
	_, _, err := WaitMemory(path, Thresholds{MinAvailableGiB: fptr(4)}, 0, c.sleep, c.now)
	if err == nil || !strings.HasPrefix(err.Error(), path+": ") {
		t.Errorf("got %v", err)
	}
}
