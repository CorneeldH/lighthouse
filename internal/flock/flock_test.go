package flock

import (
	"errors"
	"path/filepath"
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
	if want := "open lock file: open " + path + ": no such file or directory"; err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}
