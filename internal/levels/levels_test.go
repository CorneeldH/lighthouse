package levels

import (
	"strings"
	"testing"
)

const corePack = `
order = ["light", "standard", "strict"]
base = ["core-base"]
on = ["always-on"]

[light.components]
md-link-check = "off"
usage-gate = "log"

[light.flags]
strict-x = false

[standard.components]
md-link-check = "on"

[owners]
"hooks/a.sh" = "md-link-check"
"hooks/b.sh" = "base"

[calls]
"skill:x" = ["skill:y"]
`

func mustParse(t *testing.T, s string) File {
	t.Helper()
	f, err := Parse([]byte(s))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}

func TestParseReadsAllSections(t *testing.T) {
	f := mustParse(t, corePack)
	if got := strings.Join(f.Order, ","); got != "light,standard,strict" {
		t.Errorf("Order = %q", got)
	}
	if f.Levels["light"].Components["usage-gate"] != StandLog {
		t.Errorf("usage-gate = %q", f.Levels["light"].Components["usage-gate"])
	}
	if f.Levels["light"].Flags["strict-x"] {
		t.Errorf("strict-x should be false")
	}
	if f.Owners["hooks/b.sh"] != "base" || f.Calls["skill:x"][0] != "skill:y" {
		t.Errorf("owners/calls not read: %v %v", f.Owners, f.Calls)
	}
	if len(f.Base) != 1 || len(f.On) != 1 {
		t.Errorf("base/on = %v / %v", f.Base, f.On)
	}
}

func TestParseRejectsBadStand(t *testing.T) {
	_, err := Parse([]byte("[light.components]\nx = \"maybe\"\n"))
	if err == nil {
		t.Fatal("want error for stand maybe")
	}
}

func TestResolveOverrideWins(t *testing.T) {
	r, err := Resolve([]File{mustParse(t, corePack)}, "light", map[string]Stand{"md-link-check": StandOn})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Stand("md-link-check"); got != StandOn {
		t.Errorf("Stand = %q, want on", got)
	}
	if got := r.Stand("usage-gate"); got != StandLog {
		t.Errorf("usage-gate = %q, want log", got)
	}
}

func TestResolveUnlistedIsOn(t *testing.T) {
	r, err := Resolve([]File{mustParse(t, corePack)}, "light", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Stand("never-heard-of"); got != StandOn {
		t.Errorf("Stand = %q, want on", got)
	}
	if got := r.Stand("md-link-check"); got != StandOff {
		t.Errorf("md-link-check = %q, want off", got)
	}
}

func TestResolveUnknownLevel(t *testing.T) {
	_, err := Resolve([]File{mustParse(t, corePack)}, "nope", nil)
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{"light", "standard"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name level %q", err, want)
		}
	}
}

func TestResolveMergesPacks(t *testing.T) {
	other := mustParse(t, "[light.components]\ngo-lint-gate = \"off\"\n[light.flags]\nf = true\n")
	r, err := Resolve([]File{mustParse(t, corePack), other}, "light", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stand("go-lint-gate") != StandOff || r.Stand("md-link-check") != StandOff {
		t.Errorf("components not merged: %v", r.Components)
	}
	if !r.Flags["f"] {
		t.Errorf("flags not merged: %v", r.Flags)
	}
	if r.Level != "light" {
		t.Errorf("Level = %q", r.Level)
	}
}

func TestNamesIncludesBaseOnAndLevels(t *testing.T) {
	r, err := Resolve([]File{mustParse(t, corePack)}, "standard", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "always-on,core-base,md-link-check,usage-gate"
	if got := strings.Join(r.Names(), ","); got != want {
		t.Errorf("Names = %q, want %q", got, want)
	}
}

func TestCoreGates(t *testing.T) {
	if got := strings.Join(CoreGates, ","); got != "main-commit-gate,worktree-check,pr-create-gate" {
		t.Errorf("CoreGates = %q", got)
	}
}
