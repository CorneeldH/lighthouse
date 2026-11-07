//go:build linux

package local

import (
	"errors"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

var errBoom = errors.New("boom")

const stopWS = "local-feature"

func TestKillTermEndsAll(t *testing.T) {
	a, procs, slept := newAdapter(t, t.TempDir())
	procs.marked["s1"] = []int{1001, 2002}
	if err := a.Kill(t.Context(), stopWS, []string{"s1"}); err != nil {
		t.Fatalf("Kill = %v, want nil", err)
	}
	want := []sentSignal{{1001, syscall.SIGTERM}, {2002, syscall.SIGTERM}}
	if !reflect.DeepEqual(procs.sent, want) {
		t.Errorf("sent = %v, want %v", procs.sent, want)
	}
	if *slept != 1 {
		t.Errorf("Sleep calls = %d, want 1", *slept)
	}
}

func TestKillEscalatesToSigkill(t *testing.T) {
	a, procs, slept := newAdapter(t, t.TempDir())
	procs.marked["s1"] = []int{1001, 2002}
	procs.termProof[2002] = true
	if err := a.Kill(t.Context(), stopWS, []string{"s1"}); err != nil {
		t.Fatalf("Kill = %v, want nil", err)
	}
	last := procs.sent[len(procs.sent)-1]
	if last != (sentSignal{2002, syscall.SIGKILL}) {
		t.Errorf("last signal = %v, want SIGKILL to 2002", last)
	}
	if len(procs.sent) != 3 {
		t.Errorf("sent = %v, want 3 signals (SIGKILL only to the remaining pid)", procs.sent)
	}
	if *slept != 6 {
		t.Errorf("Sleep calls = %d, want 6", *slept)
	}
}

func TestKillSurvivorNamesPids(t *testing.T) {
	a, procs, _ := newAdapter(t, t.TempDir())
	procs.marked["s1"] = []int{1001, 2002}
	procs.survivors[2002] = true
	err := a.Kill(t.Context(), stopWS, []string{"s1"})
	if err == nil || !strings.Contains(err.Error(), "2002") || strings.Contains(err.Error(), "1001") {
		t.Fatalf("Kill = %v, want an error naming only 2002", err)
	}
}

func TestKillFindsLeftoverWithoutClaude(t *testing.T) {
	a, procs, _ := newAdapter(t, t.TempDir())
	procs.marked["s1"] = []int{3003} // a dev server whose claude has ended; no session record
	if err := a.Kill(t.Context(), stopWS, []string{"s1"}); err != nil {
		t.Fatalf("Kill = %v, want nil", err)
	}
	want := []sentSignal{{3003, syscall.SIGTERM}}
	if !reflect.DeepEqual(procs.sent, want) {
		t.Errorf("sent = %v, want %v", procs.sent, want)
	}
}

func TestStopEqualsKill(t *testing.T) {
	a, procs, slept := newAdapter(t, t.TempDir())
	procs.marked["s1"] = []int{1001, 2002}
	if err := a.Stop(t.Context(), stopWS, []string{"s1"}); err != nil {
		t.Fatalf("Stop = %v, want nil", err)
	}
	want := []sentSignal{{1001, syscall.SIGTERM}, {2002, syscall.SIGTERM}}
	if !reflect.DeepEqual(procs.sent, want) || *slept != 1 {
		t.Errorf("sent = %v, slept = %d; want %v and 1", procs.sent, *slept, want)
	}
}

func TestKillEmptySidBeforeProcs(t *testing.T) {
	a, procs, _ := newAdapter(t, t.TempDir())
	procs.marked["s1"] = []int{1001}
	if err := a.Kill(t.Context(), stopWS, []string{"s1", ""}); err == nil {
		t.Fatal("Kill with an empty sid = nil, want an error")
	}
	if procs.calls != 0 {
		t.Errorf("Procs calls = %d, want 0 (nothing signalled before the sid check)", procs.calls)
	}
}

func TestKillMarkedErrorWrapsSid(t *testing.T) {
	a, procs, _ := newAdapter(t, t.TempDir())
	procs.err = errBoom
	err := a.Kill(t.Context(), stopWS, []string{"s1"})
	if err == nil || !strings.Contains(err.Error(), "s1") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Kill = %v, want an error with the sid and the cause", err)
	}
}
