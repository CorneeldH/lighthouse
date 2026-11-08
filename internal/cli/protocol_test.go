package cli

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"strings"
	"testing"
)

type headerFixture struct {
	Name    string            `json:"name"`
	Body    string            `json:"body"`
	Kind    *string           `json:"kind"`
	Fields  map[string]string `json:"fields"`
	Session *string           `json:"session"`
}

type headerOutput struct {
	Kind    string            `json:"kind"`
	Fields  map[string]string `json:"fields"`
	Session *string           `json:"session"`
}

func runProtocol(t *testing.T, stdin string, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	root := NewRoot(strings.NewReader(stdin), &o, &e)
	root.SetArgs(append([]string{"protocol"}, args...))
	code = execute(root, &e)
	return code, o.String(), e.String()
}

// Classification: cli-output change; the regression surface is the JSON that parse-header prints
// for a body. The fixture comes from protocol.py, so legacy keys and the session footer agree.
func TestProtocolParseHeaderFixtures(t *testing.T) {
	raw, err := os.ReadFile("../../tests/pack/fixtures/protocol/headers.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []headerFixture
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("fixture has no cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			code, out, errOut := runProtocol(t, c.Body, "parse-header")
			if code != 0 || errOut != "" {
				t.Fatalf("code = %d, stderr = %q, want 0 and empty", code, errOut)
			}
			if c.Kind == nil {
				if out != "null\n" {
					t.Fatalf("stdout = %q, want \"null\\n\"", out)
				}
				return
			}
			var got headerOutput
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("stdout %q is not JSON: %v", out, err)
			}
			if got.Kind != *c.Kind {
				t.Errorf("kind = %q, want %q", got.Kind, *c.Kind)
			}
			if got.Fields == nil {
				t.Errorf("fields is null, want an object")
			}
			if c.Fields != nil && !maps.Equal(got.Fields, c.Fields) {
				t.Errorf("fields = %v, want %v", got.Fields, c.Fields)
			}
			switch {
			case c.Session == nil || *c.Session == "":
				if got.Session != nil {
					t.Errorf("session = %q, want null", *got.Session)
				}
			case got.Session == nil || *got.Session != *c.Session:
				t.Errorf("session = %v, want %q", got.Session, *c.Session)
			}
		})
	}
}

func TestProtocolParseHeaderExactOutput(t *testing.T) {
	_, out, _ := runProtocol(t, "🤖 Vraag\nreden: x", "parse-header")
	want := `{"kind":"question","fields":{"reason":"x"},"session":null}` + "\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestProtocolRobot(t *testing.T) {
	code, out, errOut := runProtocol(t, "", "robot")
	if code != 0 || out != "🤖\n" || errOut != "" {
		t.Errorf("got code %d, stdout %q, stderr %q; want 0, \"🤖\\n\", empty", code, out, errOut)
	}
}
