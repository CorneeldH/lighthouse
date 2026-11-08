package dispatch

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// TestDecideResumedSubagent runs the decision rule on what ReadTranscript measures from a
// transcript in which SendMessage resumed a subagent that had already ended once (#299). While
// the resumed agent runs, the tick does not hand the run back on reason_no_handoff; once its
// second notification ends it, a session without handoff, question or PR is handed back as
// before. The input is the done-background-pending fixture with its transcript replaced, and
// the second case must equal done-no-handoff.
func TestDecideResumedSubagent(t *testing.T) {
	fixtures := decideFixtures(t)
	cases := []struct {
		transcript string
		want       []Action
	}{
		{"background-resumed", []Action{}},
		{"background-resumed-ended", fixtures["done-no-handoff"].Want},
	}
	for _, c := range cases {
		t.Run(c.transcript, func(t *testing.T) {
			tr, err := workplace.ReadTranscript(filepath.Join("..", "workplace", "testdata", "transcript", c.transcript), "s")
			if err != nil {
				t.Fatal(err)
			}
			in := fixtures["done-background-pending"].Input
			run := in.Repos[0].Runs[5]
			run.Transcript = &tr
			in.Repos[0].Runs = map[int64]RunState{5: run}
			got := Decide(in)
			if !reflect.DeepEqual(got, c.want) {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				wantJSON, _ := json.MarshalIndent(c.want, "", "  ")
				t.Errorf("Decide:\ngot  %s\nwant %s", gotJSON, wantJSON)
			}
		})
	}
}
