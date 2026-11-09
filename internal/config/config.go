// Package config reads lighthouse.toml and host.toml. It is a faithful port of
// pack/core/lib/config.py: the same validation, the same messages, the same output. The
// fixtures under tests/pack/fixtures/config prove it, as pytest runs them against config.py
// and go test against this package.
//
// Paths: lighthouse.toml in the repo root (LIGHTHOUSE_REPO_ROOT, else git rev-parse
// --show-toplevel), overridden by LIGHTHOUSE_TOML; host.toml per hostconfig.Path.
package config

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

// Error is a configuration error: the text names the file and the key, and the CLI exits 2.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// ErrNotSet is wrapped by every error that means "this key has no value": rc 1.
var ErrNotSet = errors.New("not set")

func errf(format string, args ...any) error { return &Error{Msg: fmt.Sprintf(format, args...)} }

func notSet(key string) error { return fmt.Errorf("%s: %w", key, ErrNotSet) }

// ControllerStatusOption is required in the status options when the repo has a
// [controller] table.
const ControllerStatusOption = "Controller"

var requiredStatusOptions = []string{"To do", "In progress", "Done", "On hold"}

var languages = []string{"nl", "en"}

// forgeKinds are the values forge.kind may have.
var forgeKinds = []string{"github", "gitlab"}

// segment is the characters a path segment may hold (an owner, a GitLab group or a name). A
// quote or `<` never reaches a URL or a regex this way.
const segment = `[A-Za-z0-9._-]+`

// repoRE is the form of forge.repo per kind: GitHub owner/name, GitLab group/name with any
// number of subgroups between; repoForm names that form in an error.
var (
	repoRE = map[string]*regexp.Regexp{
		"github": regexp.MustCompile(`^` + segment + `/` + segment + `\z`),
		"gitlab": regexp.MustCompile(`^` + segment + `(?:/` + segment + `)+\z`),
	}
	repoForm = map[string]string{"github": "owner/name", "gitlab": "group/name or group/subgroup/.../name"}
)

// hostRE is a bare host name: no scheme, port or path.
var hostRE = regexp.MustCompile(`^[A-Za-z0-9.-]+\z`)

const (
	kStr  = "str"
	kInt  = "int"
	kStrs = "list[str]"
	kInts = "list[int]"
	kBool = "bool"
)

// schema lists the keys beyond [forge], [forge.board] and [project] language, with their
// type. The order is config.py's: with two bad keys the first one in this list is reported.
var schema = []struct{ key, kind string }{
	{"forge.default_branch", kStr},
	{"paths.specs", kStr},
	{"paths.plans", kStr},
	{"paths.tenets", kStr},
	{"paths.areas", kStr},
	{"paths.history", kStr},
	{"paths.labels", kStr},
	{"paths.search_dirs", kStrs},
	{"paths.mirror_exceptions", kStr},
	{"labels.blocked_issue", kStr},
	{"labels.blocked_external", kStr},
	{"labels.needs_shaping", kStr},
	{"labels.needs_decision", kStr},
	{"labels.needs_hands", kStr},
	{"labels.needs_testing", kStr},
	{"labels.location_prefix", kStr},
	{"labels.area_prefix", kStr},
	{"test_gate.command", kStr},
	{"test_gate.deadline_seconds", kInt},
	{"test_gate.slots", kInt},
	{"test_gate.slot_wait_seconds", kInt},
	{"test_gate.unchecked_exit_codes", kInts},
	{"test_gate.evidence_test_dir", kStr},
	{"ci.runs_on", kStr},
	{"ci.bot_client_id_var", kStr},
	{"ci.bot_private_key_secret", kStr},
	{"ci.bot_login", kStr},
	{"codex_usage.halt_pct_session", kInt},
	{"codex_usage.vraag_pct_week", kInt},
	{"codex_usage.app_server_timeout_seconds", kInt},
	{"codex_usage.review_pct_session", kInt},
	{"codex_usage.review_pct_week", kInt},
	{"worktree_audit.protected_refs", kStrs},
	{"worktree_audit.protected_paths", kStrs},
	{"project.instructions_word_ceiling", kInt},
	{"project.skill_prefix", kStr},
	{"project.milestone_prefix", kStr},
	{"python.source_dirs", kStrs},
	{"python.lint_dirs", kStrs},
	{"python.coverage_command", kStr},
	{"python.nightly_workflow", kStr},
	{"python.coverage_issue_labels", kStrs},
	{"shell.strict_dirs", kStrs},
	{"project.code_language", kStr},
	{"project.code_language_exempt", kStrs},
	{"usage_guard.resume", kBool},
	{"usage_guard.controller", kBool},
	{"project.code_language_paths", kStrs},
	{"code_language_llm.url", kStr},
	{"code_language_llm.models", kStrs},
	{"code_language_llm.key_env", kStr},
	{"code_language_llm.env_file", kStr},
	{"review.roborev", kStr},
}

// defaults is the value of an absent key. A key in schema without a default is optional: not
// set. Lists are []any; Default hands out a fresh copy.
var defaults = map[string]any{
	"forge.default_branch":                   "main",
	"paths.specs":                            "docs/developer/planning/specs",
	"paths.plans":                            "docs/developer/planning/plans",
	"paths.search_dirs":                      []any{"."},
	"labels.blocked_issue":                   "blocked-issue",
	"labels.blocked_external":                "blocked-external",
	"labels.needs_shaping":                   "needs-shaping",
	"labels.needs_decision":                  "needs-decision",
	"labels.needs_hands":                     "needs-hands",
	"labels.needs_testing":                   "needs-testing",
	"test_gate.deadline_seconds":             int64(270),
	"test_gate.slots":                        int64(2),
	"test_gate.slot_wait_seconds":            int64(180),
	"test_gate.unchecked_exit_codes":         []any{},
	"test_gate.evidence_test_dir":            "tests/",
	"ci.runs_on":                             "ubuntu-latest",
	"codex_usage.halt_pct_session":           int64(85),
	"codex_usage.vraag_pct_week":             int64(80),
	"codex_usage.app_server_timeout_seconds": int64(20),
	"codex_usage.review_pct_session":         int64(90),
	"codex_usage.review_pct_week":            int64(95),
	"worktree_audit.protected_refs":          []any{},
	"worktree_audit.protected_paths":         []any{},
	"project.instructions_word_ceiling":      int64(3000),
	"python.source_dirs":                     []any{"src"},
	"python.lint_dirs":                       []any{"src", "tests"},
	"python.coverage_command":                "uv run pytest -q",
	"python.coverage_issue_labels":           []any{},
	"shell.strict_dirs":                      []any{".claude/hooks"},
	"usage_guard.resume":                     false,
	"usage_guard.controller":                 false,
	"review.roborev":                         "off",
}

// mayBeEmpty are the keys whose empty value is a setting: `[test_gate] command = ""` turns the
// test gate off.
var mayBeEmpty = map[string]bool{"test_gate.command": true}

var baseKeys = []string{
	"forge.kind",
	"forge.repo",
	"forge.owner",
	"forge.name",
	"forge.host",
	"forge.board.project_number",
	"forge.board.project_id",
	"forge.board.status_field_id",
	"forge.board.priority_field_id",
	"project.language",
}

var optionPrefixes = []string{"forge.board.status_options.", "forge.board.priority_options."}

// Default returns a fresh copy of the default of key, and false when the key has none.
func Default(key string) (any, bool) {
	d, ok := defaults[key]
	if !ok {
		return nil, false
	}
	if list, isList := d.([]any); isList {
		return append([]any{}, list...), true
	}
	return d, true
}

// IsKnownKey reports whether key is one this reader knows, so a skill may name it.
func IsKnownKey(key string) bool {
	if contains(baseKeys, key) || contains(hostKeys, key) {
		return true
	}
	for _, s := range schema {
		if s.key == key {
			return true
		}
	}
	for _, p := range optionPrefixes {
		if strings.HasPrefix(key, p) && len(key) > len(p) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// Board is the optional [forge.board] table.
type Board struct {
	ProjectNumber   int
	ProjectID       string
	StatusFieldID   string
	PriorityFieldID string // "" when absent
	StatusOptions   map[string]string
	PriorityOptions map[string]string
}

// Config is one validated lighthouse.toml. Raw holds the decoded document: string, int64,
// float64, bool, time.Time, []any, []map[string]any and map[string]any.
type Config struct {
	Kind     string // "" when absent
	Repo     string // "" when absent
	Host     string // "" when absent; set only with kind gitlab
	Language string
	Board    *Board
	Raw      map[string]any
	Path     string

	// order is the position of the first key at or below each table path, so a walk picks
	// the first of several matching tables in document order, like Python's dict.
	order map[string]int
}

// owner is everything in Repo before the last slash (group/sub for a GitLab subgroup repo),
// and false without a repo.
func (c *Config) owner() (string, bool) {
	i := strings.LastIndex(c.Repo, "/")
	return c.Repo[:max(i, 0)], c.Repo != "" && i >= 0
}

// name is everything in Repo after the last slash, and false without a repo.
func (c *Config) name() (string, bool) {
	i := strings.LastIndex(c.Repo, "/")
	return c.Repo[i+1:], c.Repo != "" && i >= 0
}

// RepoRoot is LIGHTHOUSE_REPO_ROOT, else the top level of the git checkout around the cwd.
func RepoRoot() (string, error) {
	if override := os.Getenv("LIGHTHOUSE_REPO_ROOT"); override != "" {
		return override, nil
	}
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return "", errf("repository root not found: git did not run: %v", err)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = "git rev-parse failed"
		}
		return "", errf("repository root not found: %s", msg)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", errf("repository root not found: git rev-parse failed")
	}
	return root, nil
}

// Path is LIGHTHOUSE_TOML, else lighthouse.toml in the repo root.
func Path() (string, error) {
	if override := os.Getenv("LIGHTHOUSE_TOML"); override != "" {
		return override, nil
	}
	root, err := RepoRoot()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(root, "/") + "/lighthouse.toml", nil
}

// pyPath renders path like str(pathlib.Path(path)): no empty or "." components, no trailing
// slash, "." for nothing.
func pyPath(path string) string {
	var parts []string
	for _, p := range strings.Split(path, "/") {
		if p != "" && p != "." {
			parts = append(parts, p)
		}
	}
	joined := strings.Join(parts, "/")
	if strings.HasPrefix(path, "/") {
		return "/" + joined
	}
	if joined == "" {
		return "."
	}
	return joined
}

// readTOML decodes the file at path. A missing file gives the Error with text missing.
func readTOML(path, missing string) (map[string]any, toml.MetaData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, toml.MetaData{}, errf("%s", missing)
		}
		return nil, toml.MetaData{}, errf("%s: unreadable or not valid TOML: %v", pyPath(path), err)
	}
	return decode(data, path)
}

func decode(data []byte, path string) (map[string]any, toml.MetaData, error) {
	if !utf8.Valid(data) {
		return nil, toml.MetaData{}, errf("%s: unreadable or not valid TOML: invalid UTF-8", pyPath(path))
	}
	raw := map[string]any{}
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return nil, md, errf("%s: unreadable or not valid TOML: %v", pyPath(path), err)
	}
	return raw, md, nil
}

// get returns the key of table, which must be of the given kind ("str", "int" or "dict"); a
// string must not be empty.
func get(table map[string]any, key, kind, where, path string) (any, error) {
	value, ok := table[key]
	if !ok {
		return nil, errf("%s: missing key %s%s", path, where, key)
	}
	good := false
	switch kind {
	case kStr:
		_, good = value.(string)
	case kInt:
		_, good = value.(int64)
	case "dict":
		_, good = value.(map[string]any)
	}
	if !good {
		return nil, errf("%s: key %s%s must be of type %s", path, where, key, kind)
	}
	if s, isStr := value.(string); isStr && s == "" {
		return nil, errf("%s: key %s%s is empty", path, where, key)
	}
	return value, nil
}

func getTable(table map[string]any, key, where, path string) (map[string]any, error) {
	v, err := get(table, key, "dict", where, path)
	if err != nil {
		return nil, err
	}
	return v.(map[string]any), nil
}

func getString(table map[string]any, key, where, path string) (string, error) {
	v, err := get(table, key, kStr, where, path)
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func getInt(table map[string]any, key, where, path string) (int, error) {
	v, err := get(table, key, kInt, where, path)
	if err != nil {
		return 0, err
	}
	return int(v.(int64)), nil
}

// options checks that every option of table is a non-empty string, reporting the first bad
// one in document order.
func (c *Config) options(table map[string]any, tablePath []string, where, path string) (map[string]string, error) {
	out := make(map[string]string, len(table))
	for _, name := range c.ordered(table, tablePath) {
		id, ok := table[name].(string)
		if !ok || id == "" {
			return nil, errf("%s: option %s%s must be a non-empty string", path, where, name)
		}
		out[name] = id
	}
	return out, nil
}

func (c *Config) board(board map[string]any, needsController bool, path string) (*Board, error) {
	const where = "forge.board."
	boardPath := []string{"forge", "board"}
	rawStatus, err := getTable(board, "status_options", where, path)
	if err != nil {
		return nil, err
	}
	status, err := c.options(rawStatus, append(boardPath, "status_options"), where+"status_options.", path)
	if err != nil {
		return nil, err
	}
	required := requiredStatusOptions
	if needsController {
		required = append(append([]string{}, required...), ControllerStatusOption)
	}
	for _, name := range required {
		if _, ok := status[name]; !ok {
			return nil, errf("%s: missing status option %s in forge.board.status_options", path, pyStrRepr(name))
		}
	}
	b := &Board{StatusOptions: status}
	if _, ok := board["priority_field_id"]; ok {
		if b.PriorityFieldID, err = getString(board, "priority_field_id", where, path); err != nil {
			return nil, err
		}
	}
	priority := map[string]any{}
	if v, ok := board["priority_options"]; ok {
		if priority, ok = v.(map[string]any); !ok {
			return nil, errf("%s: key forge.board.priority_options must be a table", path)
		}
	}
	if b.ProjectNumber, err = getInt(board, "project_number", where, path); err != nil {
		return nil, err
	}
	if b.ProjectID, err = getString(board, "project_id", where, path); err != nil {
		return nil, err
	}
	if b.StatusFieldID, err = getString(board, "status_field_id", where, path); err != nil {
		return nil, err
	}
	b.PriorityOptions, err = c.options(priority, append(boardPath, "priority_options"), where+"priority_options.", path)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func checkType(value any, kind string) bool {
	switch kind {
	case kStr:
		_, ok := value.(string)
		return ok
	case kInt:
		_, ok := value.(int64)
		return ok
	case kBool:
		_, ok := value.(bool)
		return ok
	}
	list, ok := value.([]any)
	if !ok {
		return false
	}
	for _, e := range list {
		switch kind {
		case kStrs:
			if s, isStr := e.(string); !isStr || s == "" {
				return false
			}
		default:
			if _, isInt := e.(int64); !isInt {
				return false
			}
		}
	}
	return true
}

func checkSchema(data map[string]any, path string) error {
	tables := map[string]bool{}
	for _, s := range schema {
		table, _, _ := strings.Cut(s.key, ".")
		tables[table] = true
	}
	names := make([]string, 0, len(tables))
	for t := range tables {
		names = append(names, t)
	}
	sort.Strings(names)
	for _, table := range names {
		if v, ok := data[table]; ok {
			if _, isTable := v.(map[string]any); !isTable {
				return errf("%s: key %s must be a table", path, table)
			}
		}
	}
	for _, s := range schema {
		table, name, _ := strings.Cut(s.key, ".")
		t, ok := data[table].(map[string]any)
		if !ok {
			continue
		}
		value, ok := t[name]
		if !ok {
			continue
		}
		if !checkType(value, s.kind) {
			return errf("%s: key %s must be of type %s", path, s.key, s.kind)
		}
		if str, isStr := value.(string); isStr && str == "" && !mayBeEmpty[s.key] {
			return errf("%s: key %s is empty", path, s.key)
		}
	}
	return nil
}

// Load reads and validates one lighthouse.toml.
func Load(path string) (*Config, error) {
	data, md, err := readTOML(path, pyPath(path)+": file is missing")
	if err != nil {
		return nil, err
	}
	return build(data, md, path)
}

// Parse validates the lighthouse.toml text in data; path names the file in messages.
func Parse(data []byte, path string) (*Config, error) {
	raw, md, err := decode(data, path)
	if err != nil {
		return nil, err
	}
	return build(raw, md, path)
}

func build(data map[string]any, md toml.MetaData, rawPath string) (*Config, error) {
	path := pyPath(rawPath)
	c := &Config{Language: "en", Raw: data, Path: rawPath, order: documentOrder(md)}

	if _, ok := data["forge"]; ok {
		forge, err := getTable(data, "forge", "", path)
		if err != nil {
			return nil, err
		}
		// kind, repo and host are optional too: a repo without a remote may hold only
		// default_branch here.
		if _, ok := forge["kind"]; ok {
			if c.Kind, err = getString(forge, "kind", "forge.", path); err != nil {
				return nil, err
			}
			if !contains(forgeKinds, c.Kind) {
				return nil, errf("%s: key forge.kind must be 'github' or 'gitlab', not %s", path, pyStrRepr(c.Kind))
			}
		}
		// Without a kind the repo is a GitHub one.
		repoKind := cmp.Or(c.Kind, "github")
		if _, ok := forge["repo"]; ok {
			if c.Repo, err = getString(forge, "repo", "forge.", path); err != nil {
				return nil, err
			}
			if !repoRE[repoKind].MatchString(c.Repo) {
				return nil, errf("%s: key forge.repo must have the form %s: %s", path, repoForm[repoKind], pyStrRepr(c.Repo))
			}
		}
		if _, ok := forge["host"]; ok {
			if c.Host, err = getString(forge, "host", "forge.", path); err != nil {
				return nil, err
			}
			if c.Kind != "gitlab" {
				return nil, errf("%s: key forge.host is only valid with kind = 'gitlab'", path)
			}
			if !hostRE.MatchString(c.Host) {
				return nil, errf("%s: key forge.host must be a bare host name, not %s", path, pyStrRepr(c.Host))
			}
		}
		if _, ok := forge["board"]; ok {
			board, err := getTable(forge, "board", "forge.", path)
			if err != nil {
				return nil, err
			}
			_, needsController := data["controller"]
			if c.Board, err = c.board(board, needsController, path); err != nil {
				return nil, err
			}
		}
	}

	if _, ok := data["project"]; ok {
		project, err := getTable(data, "project", "", path)
		if err != nil {
			return nil, err
		}
		if v, ok := project["language"]; ok {
			value, isStr := v.(string)
			if !isStr || !contains(languages, value) {
				return nil, errf("%s: key project.language must be one of %s, not %s",
					path, strings.Join(languages, ", "), pyRepr(v))
			}
			c.Language = value
		}
		if v, ok := project["code_language"]; ok && v != "en" {
			return nil, errf("%s: key project.code_language must be en, not %s", path, pyRepr(v))
		}
	}

	if err := checkSchema(data, path); err != nil {
		return nil, err
	}

	if review, ok := data["review"].(map[string]any); ok {
		if v, ok := review["roborev"]; ok && v != "local" && v != "off" {
			return nil, errf("%s: key review.roborev must be 'local' or 'off', not %s", path, pyRepr(v))
		}
	}
	return c, nil
}

// documentOrder maps each table path to the index of the first key at or below it.
func documentOrder(md toml.MetaData) map[string]int {
	order := map[string]int{}
	for i, key := range md.Keys() {
		for n := 1; n <= len(key); n++ {
			id := strings.Join(key[:n], "\x00")
			if _, seen := order[id]; !seen {
				order[id] = i
			}
		}
	}
	return order
}

func sortedNames(m map[string]any) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ordered lists the names of table, which sits at tablePath, in document order.
func (c *Config) ordered(table map[string]any, tablePath []string) []string {
	names := sortedNames(table)
	index := func(name string) int {
		id := strings.Join(append(append([]string{}, tablePath...), name), "\x00")
		if i, ok := c.order[id]; ok {
			return i
		}
		return len(c.order)
	}
	sort.SliceStable(names, func(a, b int) bool { return index(names[a]) < index(names[b]) })
	return names
}

// walk follows a dotted key. Per level an exact key wins; otherwise the first table, in
// document order, whose name plus "." starts the rest. Option names may hold spaces or dots.
func (c *Config) walk(table map[string]any, tablePath []string, key string) (any, bool) {
	if v, ok := table[key]; ok {
		return v, true
	}
	for _, name := range c.ordered(table, tablePath) {
		sub, isTable := table[name].(map[string]any)
		if isTable && strings.HasPrefix(key, name+".") {
			return c.walk(sub, append(append([]string{}, tablePath...), name), key[len(name)+1:])
		}
	}
	return nil, false
}

// Lookup returns the value of key, or its default; an error wrapping ErrNotSet when it is not
// set, an *Error when a host.toml is invalid.
func Lookup(c *Config, key string) (any, error) {
	switch {
	case key == "forge.owner" || key == "forge.name":
		get := c.owner
		if key == "forge.name" {
			get = c.name
		}
		if v, ok := get(); ok {
			return v, nil
		}
		return nil, notSet(key)
	case key == "project.language":
		return c.Language, nil
	case strings.HasPrefix(key, "host."):
		return HostValue(key)
	}
	if v, ok := c.walk(c.Raw, nil, key); ok {
		return v, nil
	}
	if d, ok := Default(key); ok {
		return d, nil
	}
	return nil, notSet(key)
}

func scalar(key string, v any) (string, error) {
	switch x := v.(type) {
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case string:
		return x, nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case int:
		return strconv.Itoa(x), nil
	case float64:
		return FormatFloat(x), nil
	case *big.Int:
		return x.String(), nil
	case time.Time, map[string]any, []any, []map[string]any:
		return "", errf("%s is a table, not a value", key)
	}
	return "", errf("%s is a table, not a value", key)
}

// Lines is the output of a value: one line for a scalar, one per element for a list.
func Lines(key string, v any) ([]string, error) {
	var elements []any
	switch x := v.(type) {
	case []any:
		elements = x
	case []map[string]any:
		for _, e := range x {
			elements = append(elements, e)
		}
	default:
		elements = []any{v}
	}
	lines := make([]string, 0, len(elements))
	for _, e := range elements {
		line, err := scalar(key, e)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// Run is `lighthouse config <key>` and `lighthouse config host-check`: rc 0 with the value on
// out, rc 1 when the key is not set, rc 2 on a configuration error.
func Run(args []string, out, errOut io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(errOut, "config: usage: lighthouse config <key>|host-check")
		return 2
	}
	key := args[0]
	if key == "host-check" {
		return HostCheck(out, errOut)
	}
	lines, err := lookupLines(key)
	if err != nil {
		if errors.Is(err, ErrNotSet) {
			_, _ = fmt.Fprintf(errOut, "config: %s is not set\n", key)
			return 1
		}
		_, _ = fmt.Fprintf(errOut, "config: %v\n", err)
		return 2
	}
	for _, line := range lines {
		_, _ = fmt.Fprintln(out, line)
	}
	return 0
}

// lookupLines resolves key; a host.* key never loads lighthouse.toml.
func lookupLines(key string) ([]string, error) {
	var value any
	var err error
	if strings.HasPrefix(key, "host.") {
		value, err = HostValue(key)
	} else {
		var path string
		if path, err = Path(); err != nil {
			return nil, err
		}
		var c *Config
		if c, err = Load(path); err != nil {
			return nil, err
		}
		value, err = Lookup(c, key)
	}
	if err != nil {
		return nil, err
	}
	return Lines(key, value)
}
