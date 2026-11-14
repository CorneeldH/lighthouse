package server

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/CorneeldH/lighthouse/internal/overview"
)

// tickTimer and pageQueries hold the two intervals of the bound that live outside this package:
// how often the tick runs, and how often the page asks serve for the overview.
const (
	tickTimer   = "../../deploy/systemd/lighthouse-tick.timer"
	pageQueries = "../../web/src/lib/queries.ts"
)

// tickInterval reads OnUnitActiveSec from the tick timer. Only the forms the unit uses are
// accepted (a number with s or min); anything else fails the test rather than guess.
func tickInterval(t *testing.T) time.Duration {
	t.Helper()
	b, err := os.ReadFile(tickTimer)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^OnUnitActiveSec=(\d+)(s|min)\s*$`).FindSubmatch(b)
	if m == nil {
		t.Fatalf("%s: no OnUnitActiveSec=<n>s|min line", tickTimer)
	}
	n, _ := strconv.Atoi(string(m[1]))
	if string(m[2]) == "min" {
		return time.Duration(n) * time.Minute
	}
	return time.Duration(n) * time.Second
}

// pagePoll reads QUESTIONS_INTERVAL_MS, the interval at which the page re-reads
// /api/questions and /api/returned.
func pagePoll(t *testing.T) time.Duration {
	t.Helper()
	b, err := os.ReadFile(pageQueries)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^export const QUESTIONS_INTERVAL_MS = ([\d_]+)\s*$`).FindSubmatch(b)
	if m == nil {
		t.Fatalf("%s: no QUESTIONS_INTERVAL_MS = <ms> line", pageQueries)
	}
	ms, err := strconv.Atoi(strings.ReplaceAll(string(m[1]), "_", ""))
	if err != nil {
		t.Fatal(err)
	}
	return time.Duration(ms) * time.Millisecond
}

// TestNewCommentOnPageWithinTickInterval is AC 3 of #307: a new question or return is on the
// page within one tick interval of its comment. It runs the real Refresher with the real
// constants on a fake clock, in the worst case: every build reads the forge at its start and
// then runs until just before BuildTimeout, and the comment lands right after a build read
// the forge. The page then polls just before the overview changes, so it sees the comment one
// poll interval later.
func TestNewCommentOnPageWithinTickInterval(t *testing.T) {
	tick, poll := tickInterval(t), pagePoll(t)

	synctest.Test(t, func(t *testing.T) {
		var posted atomic.Bool
		build := func(ctx context.Context) (overview.Overview, error) {
			seen := posted.Load()
			select {
			case <-time.After(BuildTimeout - time.Millisecond):
			case <-ctx.Done():
				return overview.Overview{}, ctx.Err()
			}
			if seen {
				return ovWith("new"), nil
			}
			return overview.Overview{}, nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		r := NewRefresher(build, RefreshInterval, RefreshRetry, BuildTimeout)
		go r.Run(ctx)

		synctest.Wait() // the first build has read the forge and is still running
		postedAt := time.Now()
		posted.Store(true)

		for {
			ov, err := r.Overview(ctx)
			if err != nil {
				t.Fatalf("Overview: %v", err)
			}
			if len(ov.Returned) == 1 {
				break
			}
			if time.Since(postedAt) > 2*tick {
				t.Fatalf("comment not in the overview %s after it was posted", 2*tick)
			}
			time.Sleep(100 * time.Millisecond)
		}
		inOverview := time.Since(postedAt)
		if onPage := inOverview + poll; onPage > tick {
			t.Errorf("worst case comment to page = %s (in overview after %s + page poll %s), want <= tick interval %s",
				onPage, inOverview, poll, tick)
		}
	})
}
