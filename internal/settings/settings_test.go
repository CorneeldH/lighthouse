package settings

import (
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
	"github.com/CorneeldH/lighthouse/internal/packs"
)

const lhCmd = `"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/g.sh`

func parse(t *testing.T, s string) *orderedjson.Object {
	t.Helper()
	v, err := orderedjson.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return v.(*orderedjson.Object)
}

func compact(t *testing.T, o *orderedjson.Object) string {
	t.Helper()
	b, err := orderedjson.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	// Compact, in file order: strip the whitespace of the pretty form outside strings.
	var sb strings.Builder
	inString := false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c == '"' && (i == 0 || b[i-1] != '\\') {
			inString = !inString
		}
		if !inString && (c == ' ' || c == '\n') {
			continue
		}
		sb.WriteByte(c)
	}
	return sb.String()
}

func reg(t *testing.T, event, matcher, entry string) packs.Registration {
	t.Helper()
	return packs.Registration{Event: event, Matcher: matcher, Entry: parse(t, entry)}
}

func lh(t *testing.T, timeout string) packs.Registration {
	return reg(t, "PreToolUse", "Bash", `{"type":"command","command":"\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/lighthouse/g.sh","timeout":`+timeout+`}`)
}

const lhEntry15 = `{"type":"command","command":"\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/lighthouse/g.sh","timeout":15}`

func apply(t *testing.T, doc string, want ...packs.Registration) string {
	t.Helper()
	o := parse(t, doc)
	if err := Apply(o, want); err != nil {
		t.Fatal(err)
	}
	return compact(t, o)
}

func TestApplyAppendsToExistingGroup(t *testing.T) {
	doc := `{"permissions":{"allow":[]},"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"x.sh"}]}]},"env":{"A":"1"}}`
	want := `{"permissions":{"allow":[]},"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"x.sh"},` + lhEntry15 + `]}]},"env":{"A":"1"}}`
	if got := apply(t, doc, lh(t, "15")); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestApplyNewGroupAtEnd(t *testing.T) {
	doc := `{"hooks":{"PreToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"e.sh"}]}]}}`
	want := `{"hooks":{"PreToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"e.sh"}]},{"matcher":"Bash","hooks":[` + lhEntry15 + `]}]}}`
	if got := apply(t, doc, lh(t, "15")); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestApplyNewEventAndHooks(t *testing.T) {
	if got, want := apply(t, `{"model":"x"}`, lh(t, "15")), `{"model":"x","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[`+lhEntry15+`]}]}}`; got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	doc := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"s.sh"}]}]}}`
	want := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"s.sh"}]}],"PreToolUse":[{"matcher":"Bash","hooks":[` + lhEntry15 + `]}]}}`
	if got := apply(t, doc, lh(t, "15")); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestApplyReplacesInPlace(t *testing.T) {
	doc := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"a.sh"},{"type":"command","command":"\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/lighthouse/g.sh","timeout":5},{"type":"command","command":"b.sh"}]}]}}`
	want := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"a.sh"},` + lhEntry15 + `,{"type":"command","command":"b.sh"}]}]}}`
	if got := apply(t, doc, lh(t, "15")); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestApplyRemovesDropped(t *testing.T) {
	doc := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"a.sh"},` + lhEntry15 + `]},{"matcher":"Edit","hooks":[` + lhEntry15 + `]},{"matcher":"Read","hooks":[]}],"Stop":[{"hooks":[` + lhEntry15 + `]}]}}`
	want := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"a.sh"}]},{"matcher":"Read","hooks":[]}]}}`
	if got := apply(t, doc); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	doc := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"a.sh"}]}]}}`
	once := apply(t, doc, lh(t, "15"))
	if twice := apply(t, once, lh(t, "15")); twice != once {
		t.Fatalf("second apply changed\n%s\n%s", once, twice)
	}
}

func TestApplyNoMatcher(t *testing.T) {
	doc := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"s.sh"}]}]}}`
	r := reg(t, "Stop", "", `{"type":"command","command":"\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/lighthouse/g.sh"}`)
	want := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"s.sh"},{"type":"command","command":"\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/lighthouse/g.sh"}]}]}}`
	if got := apply(t, doc, r); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if got := apply(t, `{}`, r); strings.Contains(got, "matcher") {
		t.Fatalf("new group got a matcher: %s", got)
	}
}

func TestRejectsShape(t *testing.T) {
	cases := map[string]string{
		"hooks as array":          `{"hooks":[]}`,
		"event as object":         `{"hooks":{"PreToolUse":{}}}`,
		"group not object":        `{"hooks":{"PreToolUse":[1]}}`,
		"group without hooks":     `{"hooks":{"PreToolUse":[{"matcher":"Bash"}]}}`,
		"matcher not string":      `{"hooks":{"PreToolUse":[{"matcher":1,"hooks":[]}]}}`,
		"entry not object":        `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":["x"]}]}}`,
		"command hook no command": `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command"}]}]}}`,
		"command not string":      `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":1}]}]}}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			o := parse(t, doc)
			err := Apply(o, []packs.Registration{lh(t, "15")})
			if err == nil || !strings.Contains(err.Error(), "hooks") {
				t.Fatalf("err = %v", err)
			}
			if _, err := Registrations(o); err == nil {
				t.Fatal("Registrations accepted it")
			}
		})
	}
}

func TestPromptHookWithoutCommandIsFine(t *testing.T) {
	doc := `{"hooks":{"Stop":[{"hooks":[{"type":"prompt","prompt":"check"}]}]}}`
	if got := apply(t, doc); got != doc {
		t.Fatalf("got %s", got)
	}
}

func TestRegistrationsAndCommands(t *testing.T) {
	o := parse(t, `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"a.sh"},`+lhEntry15+`]}],"Stop":[{"hooks":[{"type":"prompt","prompt":"p"}]}]}}`)
	regs, err := Registrations(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 3 || regs[1].Event != "PreToolUse" || regs[1].Matcher != "Bash" || regs[2].Event != "Stop" || regs[2].Matcher != "" {
		t.Fatalf("regs = %+v", regs)
	}
	cmds, err := Commands(o)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cmds, "|") != "a.sh|"+lhCmd {
		t.Fatalf("cmds = %v", cmds)
	}
}
