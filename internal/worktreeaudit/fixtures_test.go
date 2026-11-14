package worktreeaudit

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// The data fixtures of tests/pack/fixtures/worktree_audit, which test_worktree_audit_cases.py
// held against worktree_audit.py. Every render runs in UTC, as the Python runner sets TZ=UTC.

func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

const fixtureDir = "../../tests/pack/fixtures/worktree_audit/"

// paia is the protect list of the fixtures without a config of their own.
const paia = "[worktree_audit]\nprotected_refs = [\"entire/*\"]\nprotected_paths = [\".port-registry\"]\n"

func load[T any](t *testing.T, name string) []T {
	t.Helper()
	data, err := os.ReadFile(fixtureDir + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []T
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s: no cases", name)
	}
	return cases
}

func policyOf(t *testing.T, text *string) Policy {
	t.Helper()
	body := paia
	if text != nil {
		body = *text
	}
	cfg, err := config.Parse([]byte(body), "lighthouse.toml")
	if err != nil {
		t.Fatal(err)
	}
	p, err := PolicyFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func aliveOf(t *testing.T, name string) Alive {
	t.Helper()
	switch name {
	case "never":
		return func(int) *bool { return ptr(false) }
	case "always":
		return func(int) *bool { return ptr(true) }
	case "unmeasurable":
		return func(int) *bool { return nil }
	}
	t.Fatalf("unknown alive %q", name)
	return nil
}

// fixtureRow is a fixture row over the defaults of the dataclass.
type fixtureRow struct{ Row }

func (r *fixtureRow) UnmarshalJSON(data []byte) error {
	row := NewRow("")
	if err := json.Unmarshal(data, &row); err != nil {
		return err
	}
	r.Row = row
	return nil
}

func rowsOf(raw []fixtureRow) []Row {
	out := make([]Row, len(raw))
	for i, r := range raw {
		out[i] = r.Row
	}
	return out
}

type verdictJSON struct {
	Rank      int      `json:"rank"`
	Icon      string   `json:"icon"`
	Text      string   `json:"text"`
	Modifiers []string `json:"modifiers"`
	Safe      bool     `json:"safe"`
	Render    string   `json:"render"`
}

func asJSON(v Verdict) verdictJSON {
	mods := v.Modifiers
	if mods == nil {
		mods = []string{}
	}
	return verdictJSON{v.Rank, v.Icon, v.Text, mods, v.Safe(), v.Render()}
}

func TestJudgeCases(t *testing.T) {
	type judgeCase struct {
		Name   string      `json:"name"`
		Config *string     `json:"config"`
		Row    fixtureRow  `json:"row"`
		Alive  string      `json:"alive"`
		Expect verdictJSON `json:"expect"`
	}
	for _, c := range load[judgeCase](t, "judge") {
		t.Run(c.Name, func(t *testing.T) {
			got := asJSON(policyOf(t, c.Config).Judge(c.Row.Row, aliveOf(t, c.Alive)))
			if !reflect.DeepEqual(got, c.Expect) {
				t.Errorf("verdict\n got %+v\nwant %+v", got, c.Expect)
			}
		})
	}
}

// normalized makes an absent and an empty dir_entries the same, as the Python's default ().
func normalized(rows []Row) []Row {
	out := make([]Row, len(rows))
	for i, r := range rows {
		if r.DirEntries == nil {
			r.DirEntries = []string{}
		}
		out[i] = r
	}
	return out
}

func TestRowsCases(t *testing.T) {
	type rowsInput struct {
		Worktrees      []WorktreeInfo         `json:"worktrees"`
		LocalBranches  []string               `json:"local_branches"`
		RemoteBranches []string               `json:"remote_branches"`
		Pulls          map[string]PullInfo    `json:"pulls"`
		Probes         map[string]BranchProbe `json:"probes"`
		Sessions       map[string]SessionInfo `json:"sessions"`
		Transcripts    map[string][2]any      `json:"transcripts"`
		DetachedDirty  map[string]*bool       `json:"detached_dirty"`
		Orphans        []OrphanDir            `json:"orphans"`
		Ports          map[string]string      `json:"ports"`
		PRLookupOK     bool                   `json:"pr_lookup_ok"`
	}
	type rowsCase struct {
		Name       string    `json:"name"`
		Input      rowsInput `json:"input"`
		Alive      string    `json:"alive"`
		JudgeAlive string    `json:"judge_alive"`
		Expect     struct {
			Rows     []Row         `json:"rows"`
			Verdicts []verdictJSON `json:"verdicts"`
		} `json:"expect"`
	}
	policy := policyOf(t, nil)
	for _, c := range load[rowsCase](t, "rows") {
		t.Run(c.Name, func(t *testing.T) {
			in := Inputs{
				Worktrees: c.Input.Worktrees, LocalBranches: c.Input.LocalBranches, RemoteBranches: c.Input.RemoteBranches,
				Pulls: c.Input.Pulls, Probes: c.Input.Probes, Sessions: c.Input.Sessions,
				Transcripts: map[string]Transcript{}, DetachedDirty: c.Input.DetachedDirty, Orphans: c.Input.Orphans,
				Ports: c.Input.Ports, PRLookupOK: c.Input.PRLookupOK,
			}
			for path, tr := range c.Input.Transcripts {
				in.Transcripts[path] = Transcript{MTime: tr[0].(float64), SessionID: tr[1].(string)}
			}
			got := BuildRows(in, aliveOf(t, c.Alive))
			if !reflect.DeepEqual(normalized(got), normalized(c.Expect.Rows)) {
				g, _ := json.Marshal(got)
				w, _ := json.Marshal(c.Expect.Rows)
				t.Fatalf("rows\n got %s\nwant %s", g, w)
			}
			var verdicts []verdictJSON
			for _, r := range got {
				verdicts = append(verdicts, asJSON(policy.Judge(r, aliveOf(t, c.JudgeAlive))))
			}
			if !reflect.DeepEqual(verdicts, c.Expect.Verdicts) {
				t.Errorf("verdicts\n got %+v\nwant %+v", verdicts, c.Expect.Verdicts)
			}
		})
	}
}

type parseCase struct {
	Name   string          `json:"name"`
	Fn     string          `json:"fn"`
	Input  json.RawMessage `json:"input"`
	Expect json.RawMessage `json:"expect"`
}

func decode[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return v
}

// parse runs one parse case and returns the result and the expectation in the same Go type.
func parse(t *testing.T, c parseCase) (got, want any) {
	t.Helper()
	switch c.Fn {
	case "parse_worktrees":
		in := decode[struct{ Porcelain string }](t, c.Input)
		got := ParseWorktrees(in.Porcelain)
		if got == nil {
			got = []WorktreeInfo{}
		}
		return got, decode[[]WorktreeInfo](t, c.Expect)
	case "parse_branches":
		in := decode[struct {
			Text   string
			Remote bool
		}](t, c.Input)
		got := ParseBranches(in.Text, in.Remote)
		if got == nil {
			got = []string{}
		}
		return got, decode[[]string](t, c.Expect)
	case "parse_pulls":
		return ParsePulls(decode[struct{ Payload []any }](t, c.Input).Payload), decode[map[string]PullInfo](t, c.Expect)
	case "parse_port_registry":
		return ParsePortRegistry(decode[struct{ Text string }](t, c.Input).Text), decode[map[string]string](t, c.Expect)
	case "parse_sessions":
		return ParseSessions(decode[struct{ Records []any }](t, c.Input).Records), decode[map[string]SessionInfo](t, c.Expect)
	case "encode_project_dir":
		return EncodeProjectDir(decode[struct{ Path string }](t, c.Input).Path), decode[string](t, c.Expect)
	case "epoch":
		return epoch(decode[struct{ Stamp any }](t, c.Input).Stamp), decode[*float64](t, c.Expect)
	case "tip_shape":
		at, root := parseTipShape(decode[struct{ Output *string }](t, c.Input).Output)
		return [2]any{at, root}, func() [2]any {
			w := decode[[2]*json.RawMessage](t, c.Expect)
			return [2]any{decode[*float64](t, rawOrNull(w[0])), decode[*bool](t, rawOrNull(w[1]))}
		}()
	case "parse_issue_pr_heads":
		in := decode[struct {
			Payload any
			Slug    string
			Nr      int
		}](t, c.Input)
		heads, ok := ParseIssuePRHeads(in.Payload, in.Slug, in.Nr)
		if !ok {
			heads = nil
		}
		return heads, decode[[]string](t, c.Expect)
	case "issue_branches":
		in := decode[struct {
			Rows    []fixtureRow
			Nr      int
			PRHeads []string `json:"pr_heads"`
		}](t, c.Input)
		return IssueBranches(rowsOf(in.Rows), in.Nr, in.PRHeads), decode[[]string](t, c.Expect)
	case "assert_readonly":
		err := assertReadonly(decode[struct{ Argv []string }](t, c.Input).Argv)
		got := map[string]any{"ok": true}
		if err != nil {
			got = map[string]any{"error": err.Error()}
		}
		return got, decode[map[string]any](t, c.Expect)
	}
	t.Fatalf("unknown fn %q", c.Fn)
	return nil, nil
}

func rawOrNull(r *json.RawMessage) json.RawMessage {
	if r == nil {
		return json.RawMessage("null")
	}
	return *r
}

func TestParseCases(t *testing.T) {
	for _, c := range load[parseCase](t, "parse") {
		t.Run(c.Name, func(t *testing.T) {
			got, want := parse(t, c)
			if !reflect.DeepEqual(got, want) {
				g, _ := json.Marshal(got)
				w, _ := json.Marshal(want)
				t.Errorf("%s\n got %s\nwant %s", c.Fn, g, w)
			}
		})
	}
}

func TestRenderCases(t *testing.T) {
	type renderCase struct {
		Name    string       `json:"name"`
		Fn      string       `json:"fn"`
		Config  *string      `json:"config"`
		Rows    []fixtureRow `json:"rows"`
		Alive   string       `json:"alive"`
		Branch  string       `json:"branch"`
		Cwd     string       `json:"cwd"`
		Nr      int          `json:"nr"`
		PRHeads *[]string    `json:"pr_heads"`
		Expect  struct {
			Text string `json:"text"`
			OK   *bool  `json:"ok"`
		} `json:"expect"`
	}
	for _, c := range load[renderCase](t, "render") {
		t.Run(c.Name, func(t *testing.T) {
			v := View{Policy: policyOf(t, c.Config), Alive: aliveOf(t, c.Alive)}
			rows := rowsOf(c.Rows)
			var text string
			var ok *bool
			switch c.Fn {
			case "cleanup":
				text = v.Cleanup(rows)
			case "sessions":
				text = v.Sessions(rows)
			case "origin":
				text = v.Origin(rows)
			case "kpi":
				text = v.KPI(rows)
			case "only":
				var yes bool
				text, yes = v.Only(rows, c.Branch, c.Cwd)
				ok = &yes
			case "issue":
				var heads []string
				if c.PRHeads != nil {
					heads = *c.PRHeads
				}
				text = v.Issue(rows, c.Nr, heads, c.PRHeads != nil, c.Cwd)
			default:
				t.Fatalf("unknown fn %q", c.Fn)
			}
			if text != c.Expect.Text {
				t.Errorf("text\n got %q\nwant %q", text, c.Expect.Text)
			}
			if !reflect.DeepEqual(ok, c.Expect.OK) {
				t.Errorf("ok = %v, want %v", deref(ok), deref(c.Expect.OK))
			}
		})
	}
}

func deref(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}
