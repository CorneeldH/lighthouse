package packs

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/pack"
)

// Classification: fails when the node pack loses its imports gate (the Stop hook, its command
// or its timeout), the line in its instruction block that names `lighthouse imports check`, or
// the requirement that names npx for that command. TestLanguagePacks counts the units; this
// pins what they say.
func TestNodePackImportsGate(t *testing.T) {
	sub, err := fs.Sub(pack.Files, "node")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Load(sub, "node", "dev")
	if err != nil {
		t.Fatal(err)
	}
	var hooks []Unit
	var block string
	for _, u := range p.Units {
		switch u.Kind {
		case KindHook:
			hooks = append(hooks, u)
		case KindBlock:
			block = string(u.Content)
		}
	}
	if len(hooks) != 1 {
		t.Fatalf("%d hooks, want 1", len(hooks))
	}
	content := string(hooks[0].Content)
	if hooks[0].Hook == nil || hooks[0].Hook.Event != "Stop" {
		t.Errorf("registration %+v, want the Stop event", hooks[0].Hook)
	}
	for _, want := range []string{"node-imports-gate.sh", `"timeout":120`} {
		if !strings.Contains(content, want) {
			t.Errorf("hook entry lacks %q:\n%s", want, content)
		}
	}
	if !strings.Contains(block, "lighthouse imports check") {
		t.Errorf("the instruction block does not name `lighthouse imports check`:\n%s", block)
	}
	var npx []string
	for _, r := range p.Requires {
		if r.Tool == "npx" {
			npx = r.NeededBy
		}
	}
	if strings.Join(npx, ",") != "lighthouse imports check" {
		t.Errorf("npx is needed by %v, want [lighthouse imports check]", npx)
	}
}
