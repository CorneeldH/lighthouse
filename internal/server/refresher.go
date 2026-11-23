package server

import (
	"context"
	"sync"
	"time"
)

const (
	// RefreshInterval is the time from the start of one build to the start of the next.
	// A new comment reaches the page at worst RefreshInterval + BuildTimeout + the page poll
	// (QUESTIONS_INTERVAL_MS) after it is posted; that sum stays within the 3-minute tick
	// interval (#307, TestNewCommentOnPageWithinTickInterval).
	RefreshInterval = 90 * time.Second
	// RefreshRetry is the wait after a failed build, measured from its start.
	RefreshRetry = 20 * time.Second
	// BuildTimeout bounds a single build: three times the 16–20 s of a live build, for a slow
	// GitHub. Builds read repositories one after the other, so more repositories need more.
	BuildTimeout = 60 * time.Second
	// PrioritiesInterval is the time from the start of one prioritisation build to the next:
	// slower than the overview, as the planning reads are heavier and change less often.
	PrioritiesInterval = 300 * time.Second
	// PrioritiesRetry is the wait after a failed prioritisation build: longer than RefreshRetry,
	// as every retry repeats the planning reads, which cost API points.
	PrioritiesRetry = 60 * time.Second
)

// BuildFunc builds one value of type T once.
type BuildFunc[T any] func(ctx context.Context) (T, error)

// Refresher keeps the last built value in memory and rebuilds it in the background.
// Builds never overlap: Run builds, waits, builds again.
type Refresher[T any] struct {
	build    BuildFunc[T]
	interval time.Duration
	retry    time.Duration
	timeout  time.Duration

	ready     chan struct{}
	readyOnce sync.Once

	mu  sync.RWMutex
	val T
	err error
	gen uint64 // bumped by Modify; a build that started before a bump is discarded
}

// NewRefresher returns a Refresher that has not built anything yet.
func NewRefresher[T any](build BuildFunc[T], interval, retry, timeout time.Duration) *Refresher[T] {
	return &Refresher[T]{
		build:    build,
		interval: interval,
		retry:    retry,
		timeout:  timeout,
		ready:    make(chan struct{}),
	}
}

// Latest returns the result of the last build, waiting for the first one. A failed last
// build returns its error, not an older value.
func (r *Refresher[T]) Latest(ctx context.Context) (T, error) {
	select {
	case <-r.ready:
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.val, r.err
}

// Modify applies fn to the last built value under the lock, so a change that is already
// known shows at once instead of after the next build. It does nothing and returns false
// when there is no value yet or the last build failed. A build that was running during
// the call is discarded, as it may predate the change.
func (r *Refresher[T]) Modify(fn func(T) T) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case <-r.ready:
	default:
		return false
	}
	if r.err != nil {
		return false
	}
	r.val = fn(r.val)
	r.gen++
	return true
}

// Run builds now and then on the interval (sooner after a failure) until ctx is done.
func (r *Refresher[T]) Run(ctx context.Context) {
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
// succeeded. A build cut short because ctx ended stores nothing, and neither does one
// overtaken by Modify; that counts as success so the normal interval follows.
func (r *Refresher[T]) refresh(ctx context.Context) bool {
	bctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	r.mu.RLock()
	gen := r.gen
	r.mu.RUnlock()
	val, err := r.build(bctx)
	if ctx.Err() != nil {
		return false
	}
	r.mu.Lock()
	if r.gen != gen {
		r.mu.Unlock()
		return true
	}
	r.val, r.err = val, err
	r.mu.Unlock()
	r.readyOnce.Do(func() { close(r.ready) })
	return err == nil
}
