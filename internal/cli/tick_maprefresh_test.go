package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Classification: io (SQLite, fake forge, fake workplace) through the tick with the job from the
// registry. These tests fail if the tick binary no longer registers map-refresh, or if a schedule
// with invalid params is gated, started or logged without its config error.

func TestMapRefreshIsRegisteredInTheTickBinary(t *testing.T) {
	// Guards the blank import of the job package in tick_scheduled.go.
	if j, ok := scheduled.Lookup("map-refresh"); !ok || j.Name() != "map-refresh" {
		t.Fatalf("Lookup(map-refresh) = %v, %v; want the job", j, ok)
	}
}

func TestTickScheduledInvalidParamsAreAConfigErrorEachTick(t *testing.T) {
	// An empty documents list is a config error: the tick names it, starts nothing, and keeps the
	// slot open (holding the repo's tick state) so the error returns until the config is fixed.
	env, db := tickFixture(t, repoDir(t, scheduledToml+"\n[schedule.params]\ndocuments = []\n"))
	_, fw := startable(t, &env)
	env.Workplace = func(repoconfig.Repo, string, string) workplace.Workplace { return fw }
	env.Run = tickGit
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); !errors.Is(err, errSilent) {
		t.Fatalf("tick = %v, want errSilent", err)
	}
	want := "lighthouse: o/r " + tickKey + ": config: params documents: must not be empty\n"
	if !strings.Contains(errOut.String(), want) {
		t.Errorf("errOut = %q, want it to contain %q", errOut.String(), want)
	}
	if rows := scheduledRows(t, openStore(t, db)); len(rows) != 0 {
		t.Errorf("scheduled runs = %+v, want none", rows)
	}
	// The reverse of an unknown job: a config error holds the tick state back, so the next tick
	// gathers and names the error again.
	if st := tickState(t, openStore(t, db), "o/r"); st.Fingerprint != "" {
		t.Errorf("tick state = %+v, want none saved while the config error stands", st)
	}
}

func TestMapPlannedIsRegisteredInTheTickBinary(t *testing.T) {
	// Guards the blank import of the job package in tick_scheduled.go.
	if j, ok := scheduled.Lookup("map-planned"); !ok || j.Name() != "map-planned" {
		t.Fatalf("Lookup(map-planned) = %v, %v; want the job", j, ok)
	}
}
