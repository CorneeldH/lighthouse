//go:build !windows

package prbody

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
	"github.com/CorneeldH/lighthouse/internal/gitinvocations"
)

// defaultFixtureRepo is `forge.repo` of a case without a `repo` key (DEFAULT_FIXTURE_REPO).
const defaultFixtureRepo = "Acme/widgets"

// fixtureCase is one case of tests/pack/fixtures/pr_body_gate/cases.json; the format is described
// in tests/pack/test_pr_body_gate_fixtures.py.
type fixtureCase struct {
	Name    string            `json:"name"`
	Kind    string            `json:"kind"`
	Body    string            `json:"body"`
	BodyB64 *string           `json:"body_b64"`
	Nr      int               `json:"nr"`
	Counts  map[string][2]any `json:"counts"`
	Command string            `json:"command"`
	CWD     *string           `json:"cwd"`
	Setup   []map[string]any  `json:"setup"`
	Value   string            `json:"value"`
	Repo    json.RawMessage   `json:"repo"`
	Expect  json.RawMessage   `json:"expect"`
}

// fixtureRepo is `forge.repo` of the case: absent is Acme/widgets, null is none ("").
func fixtureRepo(t *testing.T, c fixtureCase) string {
	t.Helper()
	if len(c.Repo) == 0 {
		return defaultFixtureRepo
	}
	var repo *string
	mustJSON(t, c.Repo, &repo)
	if repo == nil {
		return ""
	}
	return *repo
}

// writeFixtureConfig is write_fixture_config of tests/pack/_testkit.py: a lighthouse.toml with
// forge.repo (none for "") and the headings table the AC count reads, under root, which becomes
// the repo root.
func writeFixtureConfig(t *testing.T, root, repo string) {
	t.Helper()
	toml := "[project]\nlanguage = \"en\"\n\n[forge]\nkind = \"github\"\n"
	if repo != "" {
		toml += "repo = \"" + repo + "\"\n"
	}
	if err := os.WriteFile(filepath.Join(root, "lighthouse.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	headings, err := os.ReadFile("../../pack/core/headings.toml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".lighthouse"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".lighthouse", "headings.toml"), headings, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_REPO_ROOT", root)
	t.Setenv("LIGHTHOUSE_TOML", "")
	t.Setenv("LIGHTHOUSE_HEADINGS", "")
	t.Setenv("GH_REPO", "")
}

func mustJSON(t *testing.T, raw json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("expect %s: %v", raw, err)
	}
}

// orEmpty makes nil and an empty list the same, as they are for a Python list.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func eq(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\n got  %#v\n want %#v", what, got, want)
	}
}

// fillJSON replaces the placeholders of the case in a raw expectation.
func fillJSON(t *testing.T, raw json.RawMessage, root string) json.RawMessage {
	t.Helper()
	var v any
	mustJSON(t, raw, &v)
	var fill func(any) any
	fill = func(v any) any {
		switch x := v.(type) {
		case string:
			return fixturesetup.Fill(t, x, root)
		case []any:
			for i := range x {
				x[i] = fill(x[i])
			}
		case map[string]any:
			for k := range x {
				x[k] = fill(x[k])
			}
		}
		return v
	}
	out, err := json.Marshal(fill(v))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// checkBodyCase runs one check_body case. A body_b64 goes through a file, read the way
// `--body-file` is read.
func checkBodyCase(t *testing.T, c fixtureCase, repo string) map[string][]string {
	t.Helper()
	body := c.Body
	if c.BodyB64 != nil {
		raw, err := base64.StdEncoding.DecodeString(*c.BodyB64)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "body.md")
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		var problem string
		if body, problem = readBodyFile(path); problem != "" {
			t.Fatal(problem)
		}
	}
	acCount := func(nr int) (int, bool, string) {
		value, ok := c.Counts[strconv.Itoa(nr)]
		if !ok {
			t.Fatalf("ac_count called for #%d", nr)
		}
		cause, _ := value[1].(string)
		if value[0] == nil {
			return 0, false, cause
		}
		return int(value[0].(float64)), true, cause
	}
	v := CheckBody(body, repo, acCount)
	return map[string][]string{"blockers": orEmpty(v.Blockers), "notices": orEmpty(v.Notices)}
}

// foundCreate is a PrCreate as the fixture writes it.
type foundCreate struct {
	CWD         *string `json:"cwd"`
	Repo        *string `json:"repo"`
	Body        *string `json:"body"`
	BodyFile    *string `json:"body_file"`
	Problem     *string `json:"problem"`
	RepoProblem *string `json:"repo_problem"`
}

func orNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func findCase(t *testing.T, c fixtureCase, root string, expect json.RawMessage) {
	t.Helper()
	fixturesetup.Build(t, root, c.Setup)
	cwd := ""
	if c.CWD != nil {
		cwd = fixturesetup.Fill(t, *c.CWD, root)
	}
	found, err := FindPrCreates(c.Command, cwd)
	var wantText string
	if json.Unmarshal(expect, &wantText) == nil {
		eq(t, "unparseable", errors.Is(err, gitinvocations.ErrUnparseable), wantText == "unparseable")
		return
	}
	if err != nil {
		t.Fatalf("FindPrCreates: %v", err)
	}
	var want []foundCreate
	mustJSON(t, expect, &want)
	got := []foundCreate{}
	for _, pc := range found {
		got = append(got, foundCreate{pc.Cwd, pc.Repo, pc.Body, pc.BodyFile, orNil(pc.Problem), orNil(pc.RepoProblem)})
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(orEmpty(want))
	eq(t, "creates", string(gotJSON), string(wantJSON))
}

// decisionNames maps the English decisions to the Dutch words of the fixtures.
var decisionNames = map[string]string{"check": "toetsen", "skip": "overslaan", "block": "blokkeren"}

func TestFixtures(t *testing.T) {
	raw, err := os.ReadFile("../../tests/pack/fixtures/pr_body_gate/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []fixtureCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			root := t.TempDir()
			repo := fixtureRepo(t, c)
			writeFixtureConfig(t, root, repo)
			expect := fillJSON(t, c.Expect, root)
			switch c.Kind {
			case "closes", "refs", "refs_targets":
				var want []int
				mustJSON(t, expect, &want)
				var got []int
				switch c.Kind {
				case "closes":
					got = ClosesTargets(c.Body, repo)
				case "refs":
					got = RefsIssues(c.Body)
				default:
					got = RefsTargets(c.Body)
				}
				eq(t, c.Kind, orEmpty(got), orEmpty(want))
			case "no_issue":
				var want bool
				mustJSON(t, expect, &want)
				eq(t, "no_issue", NoIssue(c.Body), want)
			case "evidence_rows":
				rows, reason := EvidenceRows(c.Body, c.Nr)
				var wantReason struct{ Reason *string }
				if json.Unmarshal(expect, &wantReason) == nil && wantReason.Reason != nil {
					eq(t, "reason", reason, *wantReason.Reason)
					eq(t, "rows", len(rows), 0)
					return
				}
				var want [][3]string
				mustJSON(t, expect, &want)
				got := [][3]string{}
				for _, r := range rows {
					got = append(got, [3]string{r.AC, r.Command, r.Result})
				}
				eq(t, "reason", reason, "")
				eq(t, "rows", got, orEmpty(want))
			case "count_acs":
				var want *int
				mustJSON(t, expect, &want)
				n, ok := CountACs(c.Body)
				var got *int
				if ok {
					got = &n
				}
				eq(t, "count", got, want)
			case "check_body":
				var want map[string][]string
				mustJSON(t, expect, &want)
				want["blockers"], want["notices"] = orEmpty(want["blockers"]), orEmpty(want["notices"])
				eq(t, "verdict", checkBodyCase(t, c, repo), want)
			case "find":
				findCase(t, c, root, expect)
			case "normalize_repo":
				var want *string
				mustJSON(t, expect, &want)
				got, ok := NormalizeRepo(c.Value)
				eq(t, "normalized", orNilIf(got, ok), want)
			case "scope_repo":
				var want [2]string
				mustJSON(t, expect, &want)
				value := c.Value
				decision, reason := InScope(context.Background(), PrCreate{Repo: &value}, root, repo)
				eq(t, "scope", [2]string{decisionNames[decision], reason}, want)
			default:
				t.Fatalf("unknown kind %s", c.Kind)
			}
		})
	}
}

func orNilIf(s string, ok bool) *string {
	if !ok {
		return nil
	}
	return &s
}
