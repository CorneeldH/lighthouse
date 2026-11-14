package flock

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLockWaitsForTheHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	first, err := TryLock(path)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan error, 1)
	go func() {
		f, err := Lock(path)
		if err == nil {
			err = f.Close()
		}
		got <- err
	}()
	select {
	case err := <-got:
		t.Fatalf("Lock returned while the lock was held: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("Lock after release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Lock did not return after the holder released")
	}
}
