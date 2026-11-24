package commitgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The deadline in the message is the configured one.
func TestDeadlineMessageNamesTheConfiguredSeconds(t *testing.T) {
	h := newHarness(t, "pre", testGate+"deadline_seconds = 30\n", "").reply(runReply{124, "slow\n", nil})
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: COULD NOT CHECK — the test command ran past the deadline of 30 s.\n"+
		"\n"+
		"Root: "+h.root+". An aborted run is not a green light.\n"+
		"\n"+
		"slow\n")
}

// How many slots a gate may take comes from the project and the host: the project asks, the host
// caps, and without a host value the ceiling is seven.
func TestSlotCountsFromTheProjectAndTheHost(t *testing.T) {
	busy := func(slots string) string {
		return "commit-test-gate: COULD NOT CHECK — all gate slots busy after 0 s (slots: " + slots + ")\n" +
			"\n" +
			"Other gates are running already. Try the commit again shortly.\n" +
			"Not being able to test is not a green light.\n"
	}
	for _, tc := range []struct {
		name       string
		gate, host string
		held       int
		wantRun    bool
		wantStderr string
	}{
		{"three slots take the third", "slots = 3\n", "", 2, true, ""},
		{"seven slots take the seventh", "slots = 7\n", "", 6, true, ""},
		{"a host above the project changes nothing", "", "gate_slots = 5\n", 2, false, busy("2")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, "pre", testGate+tc.gate, tc.host).reply(runReply{0, "3 passed\n", nil})
			for n := 1; n <= tc.held; n++ {
				hold(t, h.slots, n)
			}
			res := h.g.RunRoots(h.rootRow(), "PreToolUse")
			if tc.wantRun {
				if got, want := message(t, res, "PreToolUse"), plainNotes+"commit-test-gate: "+h.root+" — 3 passed"; got != want {
					t.Fatalf("message:\n%s\nwant:\n%s", got, want)
				}
				return
			}
			blocked(t, res, tc.wantStderr)
			if len(h.calls) != 0 {
				t.Fatalf("the command ran %d times", len(h.calls))
			}
		})
	}
}

// The memory wait gets what the slot wait left of the deadline, not the full slot wait: a gate
// that waited 1 s of a 3 s deadline for a slot waits 2 s for memory.
func TestMemoryWaitGetsWhatTheSlotWaitLeft(t *testing.T) {
	h := newHarness(t, "pre", "[test_gate]\ncommand = \"x\"\nslot_wait_seconds = 3\ndeadline_seconds = 3\n", "gate_min_mem_available_gb = 4\n")
	now := time.Unix(startEpoch, 0)
	h.g.Now = func() time.Time { return now }
	h.g.acquire = func(files []*os.File, _, _ time.Duration) (int, error) {
		now = now.Add(time.Second) // the slot wait takes 1 s
		return 1, nil
	}
	meminfo := filepath.Join(h.dir, "meminfo")
	writeMeminfo(t, meminfo, gibKB, 8*gibKB)
	h.g.Env = append(h.g.Env, "LIGHTHOUSE_GATE_MEMINFO="+meminfo)
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: COULD NOT CHECK — host memory below threshold after 2 s (MemAvailable 1.0 GiB < 4 GiB)\n"+
		"\n"+
		"The host has too little free memory for a gate run. Try the commit again shortly.\n"+
		"Not being able to test is not a green light.\n")
}

// A slot that cannot be taken still shows the notes gathered before it.
func TestSlotFailureShowsTheNotesGatheredBeforeIt(t *testing.T) {
	h := newHarness(t, "pre", testGate+"slots = 3\n", "gate_slots = 2\n")
	write(t, filepath.Join(h.dir, "file"), "")
	bad := filepath.Join(h.dir, "file", "slots")
	h.g.Env = append(h.g.Env, "LIGHTHOUSE_GATE_SLOT_DIR="+bad)
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: test_gate.slots = 3 above host gate_slots = 2, the gate uses 2\n"+
		"commit-test-gate: COULD NOT CHECK — cannot take a gate slot: lock dir "+bad+
		" cannot be created (mkdir "+filepath.Join(h.dir, "file")+": not a directory)\n"+
		"\n"+
		"Without a slot this run would go unchecked beside the other gates.\n"+
		"Not being able to test is not a green light.\n")
}

func TestReadOnDefaultsToCommit(t *testing.T) {
	h := newHarness(t, "pre", testGate, "")
	on, block := h.g.readOn()
	if block != nil || on != "commit" {
		t.Fatalf("readOn() = %q, %v; want commit and no block", on, block)
	}
}

func TestReadOnValues(t *testing.T) {
	for _, want := range []string{"commit", "pr", "both"} {
		t.Run(want, func(t *testing.T) {
			h := newHarness(t, "pre", testGate+"on = \""+want+"\"\n", "")
			on, block := h.g.readOn()
			if block != nil || on != want {
				t.Fatalf("readOn() = %q, %v; want %q and no block", on, block, want)
			}
		})
	}
}

func TestReadOnRejectsUnknown(t *testing.T) {
	h := newHarness(t, "pre", testGate+"on = \"always\"\n", "")
	_, block := h.g.readOn()
	if block == nil || block.Code != 2 {
		t.Fatalf("readOn() block = %v, want code 2", block)
	}
	if want := "test_gate.on = \"always\"; it must be commit, pr or both."; !strings.Contains(block.Stderr, want) {
		t.Fatalf("stderr = %q, want it to contain %q", block.Stderr, want)
	}
}
