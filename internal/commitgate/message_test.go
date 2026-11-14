package commitgate

import (
	"encoding/json"
	"os/exec"
	"testing"
)

func TestShellQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "''"},
		{"uv run pytest", "'uv run pytest'"},
		{"it's", `'it'\''s'`},
		{"a\nb", "'a\nb'"},
		{"echo 'x'\nexit 3", `'echo '\''x'\''` + "\nexit 3'"},
		// $(…) in shell_quote drops trailing newlines.
		{"x\n\n", "'x'"},
		{"/tmp/a b", "'/tmp/a b'"},
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The quoted text, read back by bash, is the text itself: what post_untested pastes runs the
// command unchanged.
func TestShellQuoteRoundTripsThroughBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	for _, s := range []string{"echo 'x'\nexit 3", `a"b$c\d` + "`e`", "it's a 'test'"} {
		out, err := exec.Command(bash, "-c", "printf %s "+shellQuote(s)).Output()
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != s {
			t.Errorf("bash read back %q, want %q", out, s)
		}
	}
}

func TestEmitWithoutNotesIsSilent(t *testing.T) {
	g := &Gate{Mode: "pre"}
	res := g.Emit("PreToolUse")
	if res.Code != 0 || res.Output != nil || res.Stderr != "" {
		t.Fatalf("Emit = %+v, want a silent pass", res)
	}
}

func TestEmitCarriesTheNotesForUserAndModel(t *testing.T) {
	g := &Gate{Mode: "post"}
	g.Note("one")
	g.Note("two")
	res := g.Emit("PostToolUseFailure")
	got, err := json.Marshal(res.Output)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"hookSpecificOutput":{"additionalContext":"one\ntwo","hookEventName":"PostToolUseFailure"},"systemMessage":"one\ntwo"}`
	if res.Code != 0 || string(got) != want || res.Stderr != "" {
		t.Fatalf("Emit = %d %s %q, want 0 %s", res.Code, got, res.Stderr, want)
	}
}

func TestBlockInPreKeepsThePendingNotes(t *testing.T) {
	g := &Gate{Mode: "pre"}
	g.Note("commit-test-gate: earlier note")
	res := g.Block("something broke.")
	want := "commit-test-gate: earlier note\n" +
		"commit-test-gate: COULD NOT CHECK — something broke.\n" +
		"\n" +
		"This command does contain a 'git commit'. 'Could not test' is not a\n" +
		"green light, so this does not pass silently.\n"
	if res.Code != 2 || res.Stderr != want || res.Output != nil {
		t.Fatalf("Block = %d %v\n%s\nwant\n%s", res.Code, res.Output, res.Stderr, want)
	}
}

func TestBlockInPostWithoutRowsSaysCheckGitLog(t *testing.T) {
	g := &Gate{Mode: "post"}
	res := g.Block("could not read the recorded HEAD: boom")
	want := "commit-test-gate: COULD NOT CHECK — could not read the recorded HEAD: boom\n" +
		"\n" +
		"'Could not test' is not a green light.\n" +
		"\n" +
		"A commit of this call may be untested. Do not repeat the commit to retry:\n" +
		"that gives 'nothing to commit' and tests nothing. Check 'git log', then run\n" +
		"[test_gate] command by hand on the files of that commit.\n"
	if res.Code != 2 || res.Stderr != want {
		t.Fatalf("Block stderr:\n%s\nwant\n%s", res.Stderr, want)
	}
}

func TestPostUntestedNamesTheRowsNotDoneWithoutACommand(t *testing.T) {
	g := &Gate{Mode: "post", rows: []Row{{"/r/a", "b1", "h1"}, {"", "x", "y"}, {"/r/b c", "b2", "h2"}}, done: 1}
	res := g.Block("x")
	want := "commit-test-gate: COULD NOT CHECK — x\n" +
		"\n" +
		"'Could not test' is not a green light.\n" +
		"\n" +
		"The commit is made but not tested. Do not repeat the commit to retry: that\n" +
		"gives 'nothing to commit' and tests nothing. Test it by hand instead:\n" +
		"\n" +
		"  Untested: b2..h2 in /r/b c\n" +
		`  cd '/r/b c' && f=$(mktemp) && t=$(mktemp -d) && git -c core.quotePath=false diff --name-only --no-renames b2 h2 > "$f" && LIGHTHOUSE_GATE_BASE=b2 LIGHTHOUSE_GATE_HEAD=h2 LIGHTHOUSE_GATE_FILES="$f" LIGHTHOUSE_GATE_TMP="$t" bash -c '<[test_gate] command>'` + "\n"
	if res.Stderr != want {
		t.Fatalf("Block stderr:\n%s\nwant\n%s", res.Stderr, want)
	}
}
