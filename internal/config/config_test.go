package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestSchemaMatchesSnapshot pins schema, defaults and key lists to config-schema.json, which a
// pytest test pins to config.py. A key dropped or retyped here fails this test.
func TestSchemaMatchesSnapshot(t *testing.T) {
	raw, err := os.ReadFile("../../tests/pack/fixtures/config-schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Schema         [][2]string    `json:"schema"`
		Defaults       map[string]any `json:"defaults"`
		BaseKeys       []string       `json:"base_keys"`
		HostKeys       []string       `json:"host_keys"`
		OptionPrefixes []string       `json:"option_prefixes"`
	}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	var gotSchema [][2]string
	for _, s := range schema {
		gotSchema = append(gotSchema, [2]string{s.key, s.kind})
	}
	if !reflect.DeepEqual(gotSchema, want.Schema) {
		t.Errorf("schema differs from config-schema.json:\n got %v\nwant %v", gotSchema, want.Schema)
	}
	// Through JSON, so int64 and float64 compare the way the file spells them.
	var gotDefaults map[string]any
	enc, _ := json.Marshal(defaults)
	if err := json.Unmarshal(enc, &gotDefaults); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotDefaults, want.Defaults) {
		t.Errorf("defaults differ from config-schema.json:\n got %v\nwant %v", gotDefaults, want.Defaults)
	}
	if !reflect.DeepEqual(baseKeys, want.BaseKeys) {
		t.Errorf("baseKeys = %v, want %v", baseKeys, want.BaseKeys)
	}
	if !reflect.DeepEqual(hostKeys, want.HostKeys) {
		t.Errorf("hostKeys = %v, want %v", hostKeys, want.HostKeys)
	}
	if !reflect.DeepEqual(optionPrefixes, want.OptionPrefixes) {
		t.Errorf("optionPrefixes = %v, want %v", optionPrefixes, want.OptionPrefixes)
	}
}

func TestIsKnownKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"forge.repo", true},
		{"forge.owner", true},
		{"paths.specs", true},
		{"host.gate_slots", true},
		{"forge.board.project_owner", true},
		{"forge.board.status_options.In progress", true},
		{"forge.board.priority_options.a.b", true},
		{"forge.board.status_options.", false},
		{"forge.board.status_options", false},
		{"nope.nothing", false},
		{"host.bogus", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsKnownKey(tt.key); got != tt.want {
			t.Errorf("IsKnownKey(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

func TestDefaultHandsOutFreshLists(t *testing.T) {
	v, ok := Default("python.lint_dirs")
	if !ok {
		t.Fatal("python.lint_dirs has no default")
	}
	v.([]any)[0] = "changed"
	again, _ := Default("python.lint_dirs")
	if got := again.([]any); got[0] != "src" || got[1] != "tests" {
		t.Errorf("Default gave %v after a caller changed its copy, want [src tests]", got)
	}
	if _, ok := Default("paths.tenets"); ok {
		t.Error("paths.tenets has no default, Default says it has")
	}
	if v, _ := Default("test_gate.slots"); v != int64(2) {
		t.Errorf("Default(test_gate.slots) = %#v, want int64(2)", v)
	}
	if v, _ := Default("test_gate.on"); v != "commit" {
		t.Errorf("Default(test_gate.on) = %#v, want \"commit\"", v)
	}
}

func TestLookupErrorKinds(t *testing.T) {
	c, err := Parse([]byte("[project]\nlanguage = \"nl\"\n"), "lighthouse.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lookup(c, "forge.repo"); !errors.Is(err, ErrNotSet) {
		t.Errorf("Lookup(forge.repo) error = %v, want ErrNotSet", err)
	}
	if v, err := Lookup(c, "project.language"); err != nil || v != "nl" {
		t.Errorf("Lookup(project.language) = %v, %v; want nl", v, err)
	}
	_, err = Parse([]byte("[forge]\nkind = \"x\"\n"), "a//b/./lighthouse.toml")
	var cerr *Error
	if !errors.As(err, &cerr) || errors.Is(err, ErrNotSet) {
		t.Fatalf("Parse error = %v, want an *Error that is not ErrNotSet", err)
	}
	if want := "a/b/lighthouse.toml: key forge.kind must be 'github' or 'gitlab', not 'x'"; cerr.Msg != want {
		t.Errorf("Error.Msg = %q, want %q", cerr.Msg, want)
	}
}

// TestStatusOptionsWithoutOnHoldValid pins that On hold is not a required status option: a board
// with only To do, In progress and Done loads. It goes red when "On hold" returns to
// requiredStatusOptions.
func TestStatusOptionsWithoutOnHoldValid(t *testing.T) {
	src := `[forge]
kind = "github"
repo = "acme/widgets"
[forge.board]
project_number = 7
project_id = "PVT_x"
status_field_id = "PVTSSF_s"
[forge.board.status_options]
"To do" = "a1"
"In progress" = "a2"
Done = "a3"
`
	c, err := Parse([]byte(src), "lighthouse.toml")
	if err != nil {
		t.Fatalf("Parse without On hold: %v", err)
	}
	if _, ok := c.Board.StatusOptions["On hold"]; ok {
		t.Errorf("StatusOptions = %v, want no On hold", c.Board.StatusOptions)
	}
}

func TestParseFillsConfig(t *testing.T) {
	src := `[forge]
kind = "github"
repo = "acme/widgets"
[forge.board]
project_number = 7
project_id = "PVT_x"
status_field_id = "PVTSSF_s"
priority_field_id = "PVTSSF_p"
[forge.board.status_options]
"To do" = "a1"
"In progress" = "a2"
Done = "a3"
"On hold" = "a4"
[forge.board.priority_options]
High = "p1"
`
	c, err := Parse([]byte(src), "lighthouse.toml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != "github" || c.Repo != "acme/widgets" || c.Language != "en" {
		t.Errorf("Kind, Repo, Language = %q, %q, %q", c.Kind, c.Repo, c.Language)
	}
	want := Board{
		ProjectNumber: 7, ProjectID: "PVT_x", StatusFieldID: "PVTSSF_s", PriorityFieldID: "PVTSSF_p",
		StatusOptions:   map[string]string{"To do": "a1", "In progress": "a2", "Done": "a3", "On hold": "a4"},
		PriorityOptions: map[string]string{"High": "p1"},
	}
	if c.Board == nil || !reflect.DeepEqual(*c.Board, want) {
		t.Errorf("Board = %+v, want %+v", c.Board, want)
	}
}

// TestParseFillsGitLabHost pins Config.Host, which the fixtures cannot see: they read
// forge.host through the raw document.
func TestParseFillsGitLabHost(t *testing.T) {
	c, err := Parse([]byte("[forge]\nkind = \"gitlab\"\nrepo = \"g/s/n\"\nhost = \"git.example.org\"\n"), "lighthouse.toml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != "gitlab" || c.Repo != "g/s/n" || c.Host != "git.example.org" {
		t.Errorf("Kind, Repo, Host = %q, %q, %q", c.Kind, c.Repo, c.Host)
	}
}

func TestLines(t *testing.T) {
	tests := []struct {
		name    string
		in      any
		want    []string
		wantErr string
	}{
		{"string", "x", []string{"x"}, ""},
		{"bool", true, []string{"true"}, ""},
		{"list", []any{int64(1), "a", false, 2.5}, []string{"1", "a", "false", "2.5"}, ""},
		{"empty list", []any{}, []string{}, ""},
		{"table", map[string]any{"a": int64(1)}, nil, "k is a table, not a value"},
		{"list of lists", []any{[]any{int64(1)}}, nil, "k is a table, not a value"},
		{"array of tables", []map[string]any{{"a": int64(1)}}, nil, "k is a table, not a value"},
	}
	for _, tt := range tests {
		got, err := Lines("k", tt.in)
		if tt.wantErr != "" {
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("%s: error = %v, want %q", tt.name, err, tt.wantErr)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: Lines = %v, %v; want %v", tt.name, got, err, tt.want)
		}
	}
}

func TestPyRepr(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{"plain", "'plain'"},
		{"it's", `"it's"`},
		{`both ' and "`, `'both \' and "'`},
		{"tab\there\n", `'tab\there\n'`},
		{"back\\slash", `'back\\slash'`},
		{"\x00\x7f", `'\x00\x7f'`},
		{"é", "'é'"},
		{true, "True"},
		{int64(-3), "-3"},
		{1e20, "1e+20"},
		{[]any{"a", int64(1), false}, "['a', 1, False]"},
	}
	for _, tt := range tests {
		if got := pyRepr(tt.in); got != tt.want {
			t.Errorf("pyRepr(%#v) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestPyPath(t *testing.T) {
	tests := map[string]string{
		"lighthouse.toml": "lighthouse.toml",
		"./a//b/":         "a/b",
		"/abs/./x":        "/abs/x",
		"":                ".",
		"../up/host.toml": "../up/host.toml",
	}
	for in, want := range tests {
		if got := pyPath(in); got != want {
			t.Errorf("pyPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsLocal(t *testing.T) {
	if local, err := IsLocal("127.0.0.1"); err != nil || !local {
		t.Errorf("IsLocal(127.0.0.1) = %v, %v; want true", local, err)
	}
	local, err := IsLocal("192.0.2.1") // TEST-NET-1: on no interface
	if err != nil {
		t.Fatal(err)
	}
	if local {
		t.Skip("192.0.2.1 binds here: net.ipv4.ip_nonlocal_bind is set")
	}
}

func TestHostCheckNotLocal(t *testing.T) {
	if local, _ := IsLocal("192.0.2.1"); local {
		t.Skip("net.ipv4.ip_nonlocal_bind is set")
	}
	dir := t.TempDir()
	path := dir + "/host.toml"
	if err := os.WriteFile(path, []byte("[host]\nname = \"box\"\ntailscale_ip = \"192.0.2.1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_HOST_TOML", path)
	var out, errOut bytes.Buffer
	if rc := HostCheck(&out, &errOut); rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
	want := "config: host box (" + path + ") has tailscale_ip 192.0.2.1, but that address is not on this machine\n"
	if errOut.String() != want || out.Len() != 0 {
		t.Errorf("stderr = %q, stdout = %q; want %q and nothing", errOut.String(), out.String(), want)
	}
}

func TestHostCheckController(t *testing.T) {
	orig := isLocal
	t.Cleanup(func() { isLocal = orig })
	tests := []struct {
		name    string
		body    string
		local   bool
		wantRC  int
		wantOut string
		wantErr string // substring
	}{
		{"true without ip", "controller = true\n", false, 0, "host: box\n", ""},
		{"true with non-local ip", "controller = true\ntailscale_ip = \"192.0.2.1\"\n", false, 0, "host: box\n", ""},
		{"false with local ip", "controller = false\ntailscale_ip = \"192.0.2.1\"\n", true, 1, "",
			"has host.controller = false, so it is not the controller host"},
		{"absent with local ip", "tailscale_ip = \"192.0.2.1\"\n", true, 0, "host: box\n", ""},
		{"absent without ip", "", false, 1, "", "has no tailscale_ip, so it is not the always-on host"},
		{"string", "controller = \"yes\"\n", false, 2, "", "key host.controller must be true or false: 'yes'"},
		{"integer", "controller = 1\n", false, 2, "", "key host.controller must be true or false: 1"},
		{"true with invalid ip", "controller = true\ntailscale_ip = \"nope\"\n", false, 2, "",
			"key host.tailscale_ip must be an IPv4 address"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isLocal = func(string) (bool, error) { return tt.local, nil }
			path := t.TempDir() + "/host.toml"
			if err := os.WriteFile(path, []byte("[host]\nname = \"box\"\n"+tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("LIGHTHOUSE_HOST_TOML", path)
			var out, errOut bytes.Buffer
			if rc := HostCheck(&out, &errOut); rc != tt.wantRC {
				t.Errorf("rc = %d, want %d (stderr %q)", rc, tt.wantRC, errOut.String())
			}
			if out.String() != tt.wantOut {
				t.Errorf("stdout = %q, want %q", out.String(), tt.wantOut)
			}
			if !strings.Contains(errOut.String(), tt.wantErr) || (tt.wantErr == "" && errOut.Len() != 0) {
				t.Errorf("stderr = %q, want it to contain %q", errOut.String(), tt.wantErr)
			}
		})
	}
}

func TestRunWithoutKey(t *testing.T) {
	var out, errOut bytes.Buffer
	if rc := Run(nil, &out, &errOut); rc != 2 || !strings.HasPrefix(errOut.String(), "config: usage") {
		t.Errorf("rc = %d, stderr = %q; want 2 and a usage line", rc, errOut.String())
	}
}

func TestReportDefaults(t *testing.T) {
	if v, ok := Default("report.enabled"); !ok || v != false {
		t.Errorf("Default(report.enabled) = %#v, %v, want false", v, ok)
	}
	if v, ok := Default("report.sink"); !ok || v != "stdout" {
		t.Errorf("Default(report.sink) = %#v, %v, want \"stdout\"", v, ok)
	}
	if v, ok := Default("report.dir"); ok {
		t.Errorf("Default(report.dir) = %#v, want no default", v)
	}
}

func TestReportSinkRejectsOtherValues(t *testing.T) {
	_, err := Parse([]byte("[report]\nsink = \"wiki\"\n"), "lighthouse.toml")
	want := "lighthouse.toml: key report.sink must be 'stdout' or 'file', not 'wiki'"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v, want it to contain %q", err, want)
	}
}

func TestReportEnabledMustBeBool(t *testing.T) {
	_, err := Parse([]byte("[report]\nenabled = \"true\"\n"), "lighthouse.toml")
	want := "key report.enabled must be of type bool"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v, want it to contain %q", err, want)
	}
}

// runImportsCheck runs `lighthouse config imports.check` against a lighthouse.toml with body.
func runImportsCheck(t *testing.T, body string) (int, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/lighthouse.toml", []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("LIGHTHOUSE_REPO_ROOT", "")
	t.Setenv("LIGHTHOUSE_TOML", "lighthouse.toml")
	t.Setenv("LIGHTHOUSE_HOST_TOML", "host.toml")
	var out, errOut bytes.Buffer
	rc := Run([]string{"imports.check"}, &out, &errOut)
	if rc != 0 {
		t.Errorf("rc = %d, want 0 (stderr %q)", rc, errOut.String())
	}
	return rc, out.String()
}

func TestImportsCheckDefaultsToTrue(t *testing.T) {
	if _, got := runImportsCheck(t, ""); got != "true\n" {
		t.Errorf("stdout = %q, want %q", got, "true\n")
	}
}

func TestImportsCheckFalse(t *testing.T) {
	if _, got := runImportsCheck(t, "[imports]\ncheck = false\n"); got != "false\n" {
		t.Errorf("stdout = %q, want %q", got, "false\n")
	}
}

// runPRGateIssue runs `lighthouse config pr_gate.issue` against a lighthouse.toml with body.
func runPRGateIssue(t *testing.T, body string) (rc int, stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/lighthouse.toml", []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("LIGHTHOUSE_REPO_ROOT", "")
	t.Setenv("LIGHTHOUSE_TOML", "lighthouse.toml")
	t.Setenv("LIGHTHOUSE_HOST_TOML", "host.toml")
	var out, errOut bytes.Buffer
	rc = Run([]string{"pr_gate.issue"}, &out, &errOut)
	return rc, out.String(), errOut.String()
}

func TestPRGateIssue(t *testing.T) {
	tests := []struct {
		name       string
		toml       string
		wantRC     int
		wantOut    string
		wantStderr string
	}{
		{"no table is optional", "", 0, "optional\n", ""},
		{"required", "[pr_gate]\nissue = \"required\"\n", 0, "required\n", ""},
		{"evidence", "[pr_gate]\nissue = \"evidence\"\n", 0, "evidence\n", ""},
		{"unknown value is a config error", "[pr_gate]\nissue = \"strict\"\n", 2, "",
			"key pr_gate.issue must be 'optional', 'required' or 'evidence'"},
		{"capitals are a config error", "[pr_gate]\nissue = \"Required\"\n", 2, "",
			"key pr_gate.issue must be 'optional', 'required' or 'evidence'"},
		{"empty string is a config error", "[pr_gate]\nissue = \"\"\n", 2, "", "key pr_gate.issue is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc, out, errOut := runPRGateIssue(t, tt.toml)
			if rc != tt.wantRC || out != tt.wantOut || !strings.Contains(errOut, tt.wantStderr) {
				t.Errorf("rc=%d stdout=%q stderr=%q, want rc=%d stdout=%q stderr containing %q",
					rc, out, errOut, tt.wantRC, tt.wantOut, tt.wantStderr)
			}
		})
	}
}

const gitlabBoard = `[forge]
kind = "gitlab"
repo = "g/sub/p"
[forge.board.status_options]
"To do" = "To do"
"In progress" = "In progress"
Done = "Done"
"On hold" = "On hold"
[forge.board.priority_options]
P1 = "priority::P1"
`

func TestParseGitLabBoard(t *testing.T) {
	c, err := Parse([]byte(gitlabBoard), "lighthouse.toml")
	if err != nil {
		t.Fatal(err)
	}
	want := Board{
		StatusOptions:   map[string]string{"To do": "To do", "In progress": "In progress", "Done": "Done", "On hold": "On hold"},
		PriorityOptions: map[string]string{"P1": "priority::P1"},
	}
	if c.Board == nil || !reflect.DeepEqual(*c.Board, want) {
		t.Errorf("Board = %+v, want %+v", c.Board, want)
	}
}

// On GitHub two names may share an option ID: the reader does not look at it.
func TestParseGitHubBoardAllowsSharedOption(t *testing.T) {
	src := strings.NewReplacer(`kind = "gitlab"`, `kind = "github"`, "g/sub/p", "o/r", `"On hold" = "On hold"`, `"On hold" = "To do"`).Replace(gitlabBoard)
	src = strings.Replace(src, "[forge.board.status_options]", "[forge.board]\nproject_number = 3\nproject_id = \"P\"\nstatus_field_id = \"F\"\n[forge.board.status_options]", 1)
	if _, err := Parse([]byte(src), "lighthouse.toml"); err != nil {
		t.Errorf("Parse = %v, want no error", err)
	}
}

func TestSharedOption(t *testing.T) {
	cases := []struct {
		name          string
		options       map[string]string
		fold          bool
		first, second string
		ok            bool
	}{
		{"empty", map[string]string{}, true, "", "", false},
		{"distinct", map[string]string{"A": "x", "B": "y"}, true, "", "", false},
		{"same value", map[string]string{"B": "x", "A": "x", "C": "z"}, false, "A", "B", true},
		{"case differs, folded", map[string]string{"A": "To do", "B": "TO DO"}, true, "A", "B", true},
		{"case differs, exact", map[string]string{"A": "To do", "B": "TO DO"}, false, "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first, second, ok := SharedOption(tc.options, tc.fold)
			if first != tc.first || second != tc.second || ok != tc.ok {
				t.Errorf("SharedOption = %q, %q, %v; want %q, %q, %v", first, second, ok, tc.first, tc.second, tc.ok)
			}
		})
	}
}

// The GitLab example in docs/install.md is a lighthouse.toml a reader may copy: it must load,
// with and without a [controller] table, and map every status name the reader requires.
func TestInstallDocGitLabExampleParses(t *testing.T) {
	doc, err := os.ReadFile("../../docs/install.md")
	if err != nil {
		t.Fatal(err)
	}
	var example string
	for _, block := range strings.Split(string(doc), "```toml\n")[1:] {
		block, _, _ = strings.Cut(block, "```")
		if strings.Contains(block, `kind = "gitlab"`) {
			example = block
			break
		}
	}
	if example == "" {
		t.Fatal("docs/install.md has no toml block with kind = \"gitlab\"")
	}
	for _, src := range []string{example, example + "\n[controller]\n"} {
		c, err := Parse([]byte(src), "lighthouse.toml")
		if err != nil {
			t.Fatalf("Parse(install.md example) = %v\n%s", err, src)
		}
		want := map[string]string{"To do": "To do", "In progress": "In progress", "Done": "Done", "Controller": "Controller"}
		if c.Board == nil || !reflect.DeepEqual(c.Board.StatusOptions, want) {
			t.Errorf("status_options = %v, want %v", c.Board, want)
		}
	}
}

func TestParseBoardPerKind(t *testing.T) {
	githubBoard := strings.NewReplacer(`kind = "gitlab"`, `kind = "github"`, "g/sub/p", "o/r").Replace(gitlabBoard)
	githubBoard = strings.Replace(githubBoard, "[forge.board.status_options]", "[forge.board]\nproject_number = 3\n[forge.board.status_options]", 1)
	rows := []struct {
		name, src, want string
	}{
		{"gitlab project_number", strings.Replace(gitlabBoard, "[forge.board.status_options]", "[forge.board]\nproject_number = 3\n[forge.board.status_options]", 1),
			"lighthouse.toml: key forge.board.project_number is only valid with kind = 'github'"},
		{"gitlab project_id", strings.Replace(gitlabBoard, "[forge.board.status_options]", "[forge.board]\nproject_id = \"x\"\n[forge.board.status_options]", 1),
			"lighthouse.toml: key forge.board.project_id is only valid with kind = 'github'"},
		{"gitlab status_field_id", strings.Replace(gitlabBoard, "[forge.board.status_options]", "[forge.board]\nstatus_field_id = \"x\"\n[forge.board.status_options]", 1),
			"lighthouse.toml: key forge.board.status_field_id is only valid with kind = 'github'"},
		{"gitlab priority_field_id", strings.Replace(gitlabBoard, "[forge.board.status_options]", "[forge.board]\npriority_field_id = \"x\"\n[forge.board.status_options]", 1),
			"lighthouse.toml: key forge.board.priority_field_id is only valid with kind = 'github'"},
		{"gitlab project_owner", strings.Replace(gitlabBoard, "[forge.board.status_options]", "[forge.board]\nproject_owner = \"acme\"\n[forge.board.status_options]", 1),
			"lighthouse.toml: key forge.board.project_owner is only valid with kind = 'github'"},
		{"gitlab empty status name", strings.Replace(gitlabBoard, `Done = "Done"`, `Done = ""`, 1),
			"must be a non-empty string"},
		{"gitlab empty priority label", strings.Replace(gitlabBoard, `P1 = "priority::P1"`, `P1 = ""`, 1),
			"must be a non-empty string"},
		{"gitlab two statuses on one GitLab status", strings.Replace(gitlabBoard, `"On hold" = "On hold"`, `"On hold" = "to DO"`, 1),
			`lighthouse.toml: options forge.board.status_options.On hold and forge.board.status_options.To do map to the same GitLab status 'to DO'`},
		{"gitlab two priorities on one label", strings.Replace(gitlabBoard, `P1 = "priority::P1"`, "P1 = \"priority::P1\"\nP2 = \"priority::P1\"", 1),
			`lighthouse.toml: options forge.board.priority_options.P1 and forge.board.priority_options.P2 map to the same label 'priority::P1'`},
		{"github without project_id", githubBoard,
			"lighthouse.toml: missing key forge.board.project_id"},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.src), "lighthouse.toml")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Parse error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestAgentAuditDefaults pins the three [agent_audit] keys: an absent key gives its default
// through Lookup, and a set key overrides it.
func TestAgentAuditDefaults(t *testing.T) {
	c, err := Parse([]byte("[project]\nlanguage = \"nl\"\n"), "lighthouse.toml")
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"agent_audit.ignore":                []any{},
		"agent_audit.top":                   int64(10),
		"agent_audit.description_max_chars": int64(1024),
	} {
		got, err := Lookup(c, key)
		if err != nil {
			t.Errorf("Lookup(%s) error = %v", key, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Lookup(%s) = %#v, want %#v", key, got, want)
		}
	}
	set, err := Parse([]byte("[agent_audit]\nignore = [\"permission:Bash(x)\"]\ntop = 3\n"), "lighthouse.toml")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := Lookup(set, "agent_audit.top"); got != int64(3) {
		t.Errorf("Lookup(agent_audit.top) = %#v, want int64(3)", got)
	}
	if _, err := Parse([]byte("[agent_audit]\ntop = \"ten\"\n"), "lighthouse.toml"); err == nil {
		t.Error("agent_audit.top = \"ten\" parsed, want a type error")
	}
}

// TestKindOf fails when KindOf stops answering from schema (kind), from the known-key lists
// ("known") or for a key nobody knows ("unknown").
func TestKindOf(t *testing.T) {
	tests := map[string]string{
		"forge.default_branch":             "str",
		"paths.search_dirs":                "list[str]",
		"test_gate.deadline_seconds":       "int",
		"usage_guard.resume":               "bool",
		"forge.repo":                       "known",
		"forge.board.status_options.To do": "known",
		"no.such_key":                      "unknown",
		"forge.board.status_options.":      "unknown",
	}
	for key, want := range tests {
		if got := KindOf(key); got != want {
			t.Errorf("KindOf(%q) = %q, want %q", key, got, want)
		}
	}
}

// TestSchemaDump fails when a part of the dump goes missing or comes from another list than
// KindOf reads.
func TestSchemaDump(t *testing.T) {
	d := SchemaDump()
	schemaMap, ok := d["schema"].(map[string]string)
	if !ok || schemaMap["ci.runs_on"] != "str" || len(schemaMap) != len(schema) {
		t.Errorf("schema = %v", d["schema"])
	}
	if !reflect.DeepEqual(d["languages"], []string{"nl", "en"}) {
		t.Errorf("languages = %v", d["languages"])
	}
	for _, k := range []string{"defaults", "base_keys", "host_keys", "option_prefixes"} {
		if d[k] == nil {
			t.Errorf("missing %s", k)
		}
	}
}

func TestParseProjectOwner(t *testing.T) {
	base := "[forge]\nkind = \"github\"\nrepo = \"acme/widgets\"\n[forge.board]\nproject_number = 7\nproject_id = \"PVT_x\"\nstatus_field_id = \"PVTSSF_s\"\n%s[forge.board.status_options]\n\"To do\" = \"a1\"\n\"In progress\" = \"a2\"\nDone = \"a3\"\n\"On hold\" = \"a4\"\n"
	rows := []struct{ name, line, want, wantErr string }{
		{"set", "project_owner = \"acme\"\n", "acme", ""},
		{"absent", "", "", ""},
		{"not a string", "project_owner = 5\n", "", "lighthouse.toml: key forge.board.project_owner must be of type str"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			c, err := Parse([]byte(fmt.Sprintf(base, r.line)), "lighthouse.toml")
			if r.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), r.wantErr) {
					t.Fatalf("err = %v, want %q", err, r.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.Board.ProjectOwner != r.want {
				t.Errorf("ProjectOwner = %q, want %q", c.Board.ProjectOwner, r.want)
			}
		})
	}
}

// TestLevelAndComponentsParse fails when build stops filling Config.Level or Config.Components
// from the top-level `level` key and the [components] table, or when IsKnownKey/KindOf stop
// knowing them.
func TestLevelAndComponentsParse(t *testing.T) {
	src := "level = \"strict\"\n[components]\nmd-link-check = \"on\"\nusage-gate = \"log\"\nworktree-check = \"off\"\n"
	c, err := Parse([]byte(src), "lighthouse.toml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Level != "strict" {
		t.Errorf("Level = %q, want strict", c.Level)
	}
	want := map[string]string{"md-link-check": "on", "usage-gate": "log", "worktree-check": "off"}
	if !reflect.DeepEqual(c.Components, want) {
		t.Errorf("Components = %v, want %v", c.Components, want)
	}
	for _, key := range []string{"level", "components.md-link-check"} {
		if !IsKnownKey(key) {
			t.Errorf("IsKnownKey(%q) = false, want true", key)
		}
	}
	if IsKnownKey("components.") {
		t.Error("IsKnownKey(\"components.\") = true, want false")
	}
	if got := KindOf("level"); got != kStr {
		t.Errorf("KindOf(level) = %q, want str", got)
	}
	none, err := Parse([]byte("[project]\nlanguage = \"nl\"\n"), "lighthouse.toml")
	if err != nil {
		t.Fatal(err)
	}
	if none.Level != "" || len(none.Components) != 0 {
		t.Errorf("unset: Level = %q, Components = %v, want empty", none.Level, none.Components)
	}
}

// TestComponentsBadStandIsError fails when build stops checking that a component value is
// off, log or on, or when level or [components] stops being checked for shape.
func TestComponentsBadStandIsError(t *testing.T) {
	for name, src := range map[string]string{
		"bad stand":     "[components]\nmd-link-check = \"maybe\"\n",
		"not a string":  "[components]\nmd-link-check = 1\n",
		"level not str": "level = 3\n",
		"empty level":   "level = \"\"\n",
		"not a table":   "components = \"on\"\n",
	} {
		_, err := Parse([]byte(src), "lighthouse.toml")
		var cerr *Error
		if !errors.As(err, &cerr) {
			t.Errorf("%s: err = %v, want *Error", name, err)
		}
	}
	_, err := Parse([]byte("[components]\nmd-link-check = \"maybe\"\n"), "lighthouse.toml")
	if err == nil || !strings.Contains(err.Error(), "components.md-link-check") {
		t.Errorf("error = %v, want it to name components.md-link-check", err)
	}
}

// TestUnknownLevelNameIsNotAnError fails when build starts rejecting a level name: the names
// are checked against the pack's levels.toml elsewhere, not by config.Load.
func TestUnknownLevelNameIsNotAnError(t *testing.T) {
	c, err := Parse([]byte("level = \"bogus\"\n"), "lighthouse.toml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Level != "bogus" {
		t.Errorf("Level = %q, want bogus", c.Level)
	}
}

// TestUnknownComponentNameIsNotAnError fails when build starts rejecting a component name.
func TestUnknownComponentNameIsNotAnError(t *testing.T) {
	c, err := Parse([]byte("[components]\nno-such-thing = \"on\"\n"), "lighthouse.toml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Components["no-such-thing"] != "on" {
		t.Errorf("Components = %v, want no-such-thing = on", c.Components)
	}
}

// TestDefaultWorktreeRoot pins the default of [worktree] root.
func TestDefaultWorktreeRoot(t *testing.T) {
	v, ok := Default("worktree.root")
	if !ok || v != ".claude/worktrees" {
		t.Errorf("Default(worktree.root) = %v, %v; want .claude/worktrees", v, ok)
	}
}
