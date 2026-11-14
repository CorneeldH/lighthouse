package flock

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTryLockHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	first, err := TryLock(path)
	if err != nil {
		t.Fatalf("first TryLock: %v", err)
	}
	// flock binds to the open file description, so a second open in the
	// same process conflicts like another process would.
	second, err := TryLock(path)
	if !errors.Is(err, ErrHeld) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("second TryLock err = %v, want ErrHeld", err)
	}
	if second != nil {
		t.Errorf("second TryLock returned a file next to ErrHeld")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	third, err := TryLock(path)
	if err != nil {
		t.Fatalf("third TryLock after Close: %v", err)
	}
	if err := third.Close(); err != nil {
		t.Errorf("Close third: %v", err)
	}
}

func TestTryLockOpenErrorNamesPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "x.lock")
	f, err := TryLock(path)
	if err == nil {
		_ = f.Close()
		t.Fatal("TryLock in a missing directory succeeded")
	}
	if errors.Is(err, ErrHeld) {
		t.Errorf("err = %v, must not be ErrHeld", err)
	}
	// The tail is the OS's own text, which differs per platform (#90).
	if want := "open lock file: open " + path + ": "; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("err = %q, want prefix %q", err.Error(), want)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}
}

func openRW(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// Twin of test_posix_lock_is_exclusive_until_unlocked: closing the file is the unlock.
func TestTryFileIsExclusiveUntilTheHolderCloses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slot")
	a, b := openRW(t, path), openRW(t, path)
	if held, err := TryFile(a); err != nil || !held {
		t.Fatalf("first TryFile = %v, %v; want held", held, err)
	}
	if held, err := TryFile(b); err != nil || held {
		t.Fatalf("second TryFile = %v, %v; want busy (false, nil)", held, err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if held, err := TryFile(b); err != nil || !held {
		t.Fatalf("TryFile after the holder closed = %v, %v; want held", held, err)
	}
}

// Twin of test_posix_blocking_lock_holds_the_file: a lock taken with Lock is busy for TryFile.
func TestTryFileSeesALockTakenWithLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slot")
	locked, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = locked.Close() }()
	if held, err := TryFile(openRW(t, path)); err != nil || held {
		t.Fatalf("TryFile = %v, %v; want busy (false, nil)", held, err)
	}
}

func TestTryFileOnAClosedFileIsAnError(t *testing.T) {
	f := openRW(t, filepath.Join(t.TempDir(), "slot"))
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if held, err := TryFile(f); err == nil || held {
		t.Fatalf("TryFile on a closed file = %v, %v; want an error", held, err)
	}
}
