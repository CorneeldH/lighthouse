package workplace

import (
	"context"
	"testing"
	"time"
)

func TestFollowRemotePassesTimeout(t *testing.T) {
	var got []time.Duration
	run := func(_ context.Context, d time.Duration, _ string, _ ...string) (string, int, error) {
		got = append(got, d)
		return "", 0, nil // ls-remote finds no branch: nothing more to do
	}
	ws := Workspace{ID: "w", Path: "/w", Branch: "issue-1-x"}
	if err := FollowRemote(context.Background(), run, 42*time.Second, ws, true); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 42*time.Second {
		t.Errorf("timeouts = %v, want [42s]", got)
	}
}
