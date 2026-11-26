package repoconfig

import (
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func decodeExperiments(t *testing.T, src string) ([]Experiment, error) {
	t.Helper()
	var raw rawFile
	md, err := toml.Decode(src, &raw)
	if err != nil {
		t.Fatal(err)
	}
	return experimentsFrom(raw.Experiment, md)
}

func TestExperimentsFrom(t *testing.T) {
	const valid = "[[experiment]]\nid = \"e1\"\ncomponent = \"plan_review\"\nchoice = \"opus\"\nshare = 0.1\nsample = 20\nstart = \"2026-12-01\"\n"
	block := func(id, component, choice, share, sample, start string) string {
		s := "[[experiment]]\n"
		for _, kv := range [][2]string{{"id", id}, {"component", component}, {"choice", choice}, {"share", share}, {"sample", sample}, {"start", start}} {
			if kv[1] != "" {
				s += kv[0] + " = " + kv[1] + "\n"
			}
		}
		return s
	}
	tests := []struct {
		name string
		src  string
		want []string // every substring must be in the error; none = valid
	}{
		{"valid", valid, nil},
		{"share above cap", block(`"e"`, `"plan_review"`, `"opus"`, "0.21", "5", `"2026-12-01"`), []string{"share"}},
		{"share zero", block(`"e"`, `"plan_review"`, `"opus"`, "0", "5", `"2026-12-01"`), []string{"share"}},
		{"component not assignable", block(`"e"`, `"harness"`, `"opus"`, "0.1", "5", `"2026-12-01"`), []string{"cannot be assigned yet"}},
		{"choice outside set", block(`"e"`, `"plan_review"`, `"gpt"`, "0.1", "5", `"2026-12-01"`), []string{"choice"}},
		{"missing id", block("", `"plan_review"`, `"opus"`, "0.1", "5", `"2026-12-01"`), []string{"id is required"}},
		{"missing start", block(`"e"`, `"plan_review"`, `"opus"`, "0.1", "5", ""), []string{"start"}},
		{"missing sample", block(`"e"`, `"plan_review"`, `"opus"`, "0.1", "", `"2026-12-01"`), []string{"sample"}},
		{"bad start", block(`"e"`, `"plan_review"`, `"opus"`, "0.1", "5", `"1 dec"`), []string{"start"}},
		{"spec_review bad choice", block(`"e"`, `"spec_review"`, `"gpt"`, "0.1", "5", `"2026-12-01"`), []string{"choice"}},
		{"share NaN", block(`"e"`, `"plan_review"`, `"opus"`, "nan", "5", `"2026-12-01"`), []string{"share"}},
		{"blank component", block(`"e"`, `""`, `"opus"`, "0.1", "5", `"2026-12-01"`), []string{"component is required"}},
		{"sample zero", block(`"e"`, `"plan_review"`, `"opus"`, "0.1", "0", `"2026-12-01"`), []string{"sample must be at least 1"}},
		{"sample negative", block(`"e"`, `"plan_review"`, `"opus"`, "0.1", "-3", `"2026-12-01"`), []string{"sample must be at least 1"}},
		{"percent in id", block(`"50%d"`, `"harness"`, `"opus"`, "0.1", "5", `"2026-12-01"`), []string{`"50%d"`}},
		{"duplicate id", valid + valid, []string{"duplicate id"}},
		{"unknown key", valid + "bogus = 1\n", []string{"unknown key experiment.bogus"}},
		{"collects all errors", block(`"e"`, `"harness"`, `"x"`, "0.5", "", ""), []string{"cannot be assigned yet", "share", "sample", "start"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeExperiments(t, tt.src)
			if len(tt.want) == 0 {
				if err != nil {
					t.Fatal(err)
				}
				e := got[0]
				if e.ID != "e1" || e.Component != "plan_review" || e.Choice != "opus" || e.Share != 0.1 || e.Sample != 20 ||
					!e.Start.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.Local)) {
					t.Errorf("got %+v", e)
				}
				if a := e.Assignment(); a != "variant:plan_review=opus" {
					t.Errorf("Assignment = %q", a)
				}
				return
			}
			if err == nil {
				t.Fatal("want an error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
		})
	}
}

func TestLoadExperiments(t *testing.T) {
	r, err := Load(write(t, full+"[[experiment]]\nid = \"e1\"\ncomponent = \"plan_review\"\nchoice = \"codex\"\nshare = 0.2\nsample = 3\nstart = \"2026-12-01\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Experiments) != 1 || r.Experiments[0].Choice != "codex" {
		t.Errorf("experiments: %+v", r.Experiments)
	}
}

func TestExperimentsFromSpecReview(t *testing.T) {
	got, err := decodeExperiments(t, "[[experiment]]\nid = \"e\"\ncomponent = \"spec_review\"\nchoice = \"codex\"\nshare = 0.1\nsample = 5\nstart = \"2026-12-01\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if a := got[0].Assignment(); a != "variant:spec_review=codex" {
		t.Errorf("Assignment = %q", a)
	}
}

func TestParseAssignment(t *testing.T) {
	cases := []struct {
		in, component, choice string
		ok                    bool
	}{
		{"variant:plan_review=codex", "plan_review", "codex", true},
		{"variant:spec_review=opus", "spec_review", "opus", true},
		{"variant:foo", "", "", false},
		{"variant:=codex", "", "", false},
		{"variant:plan_review=", "", "", false},
		{"variant:code_review=codex", "", "", false},
		{"variant:plan_review=gpt", "", "", false},
		{"variant:plan_review=codex=x", "", "", false},
		{"standard", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		component, choice, ok := ParseAssignment(c.in)
		if component != c.component || choice != c.choice || ok != c.ok {
			t.Errorf("ParseAssignment(%q) = %q, %q, %v; want %q, %q, %v", c.in, component, choice, ok, c.component, c.choice, c.ok)
		}
	}
}
