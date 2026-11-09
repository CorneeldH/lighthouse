package config

import (
	"bytes"
	"encoding/json"
	"errors"
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
	if local, err := isLocal("127.0.0.1"); err != nil || !local {
		t.Errorf("isLocal(127.0.0.1) = %v, %v; want true", local, err)
	}
	local, err := isLocal("192.0.2.1") // TEST-NET-1: on no interface
	if err != nil {
		t.Fatal(err)
	}
	if local {
		t.Skip("192.0.2.1 binds here: net.ipv4.ip_nonlocal_bind is set")
	}
}

func TestHostCheckNotLocal(t *testing.T) {
	if local, _ := isLocal("192.0.2.1"); local {
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

func TestRunWithoutKey(t *testing.T) {
	var out, errOut bytes.Buffer
	if rc := Run(nil, &out, &errOut); rc != 2 || !strings.HasPrefix(errOut.String(), "config: usage") {
		t.Errorf("rc = %d, stderr = %q; want 2 and a usage line", rc, errOut.String())
	}
}
