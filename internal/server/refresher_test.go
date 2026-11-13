package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/overview"
)

// fakeBuild is a counting BuildFunc whose result can be set per call.
type fakeBuild struct {
	calls    atomic.Int32
	inFlight atomic.Int32
	maxSeen  atomic.Int32
	fn       func(ctx context.Context, call int) (overview.Overview, error)
}

func (f *fakeBuild) build(ctx context.Context) (overview.Overview, error) {
	n := int(f.calls.Add(1))
	cur := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		m := f.maxSeen.Load()
		if cur <= m || f.maxSeen.CompareAndSwap(m, cur) {
			break
		}
	}
	return f.fn(ctx, n)
}

func ovWith(title string) overview.Overview {
	return overview.Overview{Returned: []overview.Returned{{Number: 1, Title: title}}}
}

func TestRefresherOverviewWaitsForFirstBuild(t *testing.T) {
	release := make(chan struct{})
	fb := &fakeBuild{fn: func(context.Context, int) (overview.Overview, error) {
		<-release
		return ovWith("first"), nil
	}}
	r := NewRefresher(fb.build, time.Hour, time.Hour, time.Minute)

	type res struct {
		ov  overview.Overview
		err error
	}
	got := make(chan res, 1)
	go func() {
		ov, err := r.Overview(context.Background())
		got <- res{ov, err}
	}()
	refreshed := make(chan struct{})
	go func() { r.refresh(context.Background()); close(refreshed) }()

	select {
	case <-got:
		t.Fatal("Overview returned before the first build finished")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case g := <-got:
		if g.err != nil {
			t.Fatalf("err = %v", g.err)
		}
		if len(g.ov.Returned) != 1 || g.ov.Returned[0].Title != "first" {
			t.Fatalf("overview = %+v", g.ov)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Overview did not return after the build was released")
	}
	<-refreshed
	if n := fb.calls.Load(); n != 1 {
		t.Fatalf("build calls = %d, want 1", n)
	}
}

func TestRefresherOverviewHonoursContextBeforeFirstBuild(t *testing.T) {
	r := NewRefresher(func(context.Context) (overview.Overview, error) {
		return ovWith("x"), nil
	}, time.Hour, time.Hour, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ov, err := r.Overview(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(ov.Returned) != 0 || len(ov.Questions) != 0 {
		t.Fatalf("overview = %+v, want empty", ov)
	}
}

func TestRefresherServesStoredResultWithoutBuilding(t *testing.T) {
	fb := &fakeBuild{fn: func(context.Context, int) (overview.Overview, error) {
		return ovWith("stored"), nil
	}}
	r := NewRefresher(fb.build, time.Hour, time.Hour, time.Minute)
	r.refresh(context.Background())
	for i := 0; i < 3; i++ {
		ov, err := r.Overview(context.Background())
		if err != nil || ov.Returned[0].Title != "stored" {
			t.Fatalf("call %d: ov=%+v err=%v", i, ov, err)
		}
	}
	if n := fb.calls.Load(); n != 1 {
		t.Fatalf("build calls = %d, want 1", n)
	}
}

func TestRefresherFailureWinsOverOlderOverview(t *testing.T) {
	boom := errors.New("forge down")
	fb := &fakeBuild{fn: func(_ context.Context, call int) (overview.Overview, error) {
		if call == 2 {
			return overview.Overview{}, boom
		}
		return ovWith("ok"), nil
	}}
	r := NewRefresher(fb.build, time.Hour, time.Hour, time.Minute)
	ctx := context.Background()

	r.refresh(ctx)
	r.refresh(ctx)
	ov, err := r.Overview(ctx)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if len(ov.Returned) != 0 {
		t.Fatalf("old overview served next to the error: %+v", ov)
	}

	r.refresh(ctx)
	ov, err = r.Overview(ctx)
	if err != nil || ov.Returned[0].Title != "ok" {
		t.Fatalf("after recovery: ov=%+v err=%v", ov, err)
	}
}

func TestRefresherBuildTimeout(t *testing.T) {
	fb := &fakeBuild{fn: func(ctx context.Context, _ int) (overview.Overview, error) {
		<-ctx.Done()
		return overview.Overview{}, ctx.Err()
	}}
	r := NewRefresher(fb.build, time.Hour, time.Hour, 20*time.Millisecond)
	r.refresh(context.Background())
	_, err := r.Overview(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
}

func TestRefresherRunRebuildsOnInterval(t *testing.T) {
	reached := make(chan struct{})
	var once sync.Once
	fb := &fakeBuild{fn: func(_ context.Context, call int) (overview.Overview, error) {
		if call >= 3 {
			once.Do(func() { close(reached) })
		}
		return ovWith("ok"), nil
	}}
	r := NewRefresher(fb.build, 10*time.Millisecond, time.Hour, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("fewer than 3 builds within 5s")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRefresherRunRetriesSoonerAfterFailure(t *testing.T) {
	second := make(chan struct{})
	fb := &fakeBuild{fn: func(_ context.Context, call int) (overview.Overview, error) {
		if call == 1 {
			return overview.Overview{}, errors.New("first fails")
		}
		if call == 2 {
			defer close(second)
		}
		return ovWith("recovered"), nil
	}}
	r := NewRefresher(fb.build, time.Hour, 10*time.Millisecond, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	select {
	case <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("no retry within 5s although interval is 1h")
	}
	ctxO, cancelO := context.WithTimeout(ctx, 5*time.Second)
	defer cancelO()
	// The second build's result is stored just after it returns; poll via Overview until it is ok.
	for {
		ov, err := r.Overview(ctxO)
		if err == nil {
			if ov.Returned[0].Title != "recovered" {
				t.Fatalf("ov = %+v", ov)
			}
			return
		}
		if ctxO.Err() != nil {
			t.Fatalf("never recovered: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRefresherNoOverlappingBuilds(t *testing.T) {
	second := make(chan struct{})
	var once sync.Once
	fb := &fakeBuild{fn: func(_ context.Context, call int) (overview.Overview, error) {
		time.Sleep(30 * time.Millisecond)
		if call >= 2 {
			once.Do(func() { close(second) })
		}
		return ovWith("ok"), nil
	}}
	r := NewRefresher(fb.build, 5*time.Millisecond, time.Hour, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("fewer than 2 builds within 1s")
	}
	cancel()
	<-done
	if m := fb.maxSeen.Load(); m != 1 {
		t.Fatalf("max builds in flight = %d, want 1", m)
	}
}
