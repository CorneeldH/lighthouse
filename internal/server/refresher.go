package server

import (
	"context"
	"sync"
	"time"

	"github.com/CorneeldH/lighthouse/internal/overview"
)

const (
	// RefreshInterval is the time from the start of one build to the start of the next.
	RefreshInterval = 2 * time.Minute
	// RefreshRetry is the wait after a failed build, measured from its start.
	RefreshRetry = 20 * time.Second
	// BuildTimeout bounds a single build.
	BuildTimeout = 90 * time.Second
)

// BuildFunc builds the forge overview once.
type BuildFunc func(ctx context.Context) (overview.Overview, error)

// Refresher keeps the last forge overview in memory and rebuilds it in the background.
// Builds never overlap: Run builds, waits, builds again.
type Refresher struct {
	build    BuildFunc
	interval time.Duration
	retry    time.Duration
	timeout  time.Duration

	ready     chan struct{}
	readyOnce sync.Once

	mu  sync.RWMutex
	ov  overview.Overview
	err error
}

// NewRefresher returns a Refresher that has not built anything yet.
func NewRefresher(build BuildFunc, interval, retry, timeout time.Duration) *Refresher {
	return &Refresher{
		build:    build,
		interval: interval,
		retry:    retry,
		timeout:  timeout,
		ready:    make(chan struct{}),
	}
}

// Overview returns the result of the last build, waiting for the first one. A failed last
// build returns its error, not an older overview.
func (r *Refresher) Overview(ctx context.Context) (overview.Overview, error) {
	select {
	case <-r.ready:
	case <-ctx.Done():
		return overview.Overview{}, ctx.Err()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ov, r.err
}

// Run builds now and then on the interval (sooner after a failure) until ctx is done.
func (r *Refresher) Run(ctx context.Context) {
	for {
		start := time.Now()
		wait := r.interval
		if !r.refresh(ctx) {
			wait = r.retry
		}
		if ctx.Err() != nil {
			return
		}
		timer := time.NewTimer(time.Until(start.Add(wait)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// refresh does exactly one build and stores its result. It reports whether the build
// succeeded. A build cut short because ctx ended stores nothing.
func (r *Refresher) refresh(ctx context.Context) bool {
	bctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	ov, err := r.build(bctx)
	if ctx.Err() != nil {
		return false
	}
	r.mu.Lock()
	r.ov, r.err = ov, err
	r.mu.Unlock()
	r.readyOnce.Do(func() { close(r.ready) })
	return err == nil
}
