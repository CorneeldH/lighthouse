package pusher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Class is when a pushed workflow change can first run: only after the merge, or before it.
type Class int

const (
	// PostMerge: every changed workflow runs only on schedule or on a push to the default branch.
	PostMerge Class = iota
	// PreMerge: some changed workflow can run before the merge; it needs the security review.
	PreMerge
)

func (c Class) String() string {
	if c == PostMerge {
		return "post-merge"
	}
	return "pre-merge"
}

// The steps after the floor; Red.Step is one of them.
const (
	StepPolicy = "policy" // reading the workflows in the clone
	StepLint   = "lint"   // actionlint and zizmor
)

// The rules of the policy; Violation.Rule is one of them.
const (
	RuleUnpinned    = "unpinned"    // an external uses: not on a 40-hex SHA or a docker digest
	RulePermissions = "permissions" // no explicit workflow permissions, or write-all
	RuleTrigger     = "trigger"     // pull_request_target or workflow_run
	RuleParse       = "parse"       // a workflow the policy cannot read
)

// Violation is one broken policy rule in one workflow file.
type Violation struct{ File, Rule, Why string }

// workflowDir holds the files GitHub runs as workflows; only its direct children count.
const workflowDir = ".github/workflows"

// isWorkflow reports whether a changed path is a workflow GitHub would run.
func isWorkflow(p string) bool {
	ext := strings.ToLower(path.Ext(p))
	return path.Dir(p) == workflowDir && (ext == ".yml" || ext == ".yaml")
}

// treeEntry is one line of ls-tree: a mode, an object type and id, and a size.
type treeEntry struct {
	mode, typ, obj string
	size           int64
}

// workflowEntries lists the direct children of .github/workflows at rev in the clone, by path.
// The changed paths are never passed to git: they come from the branch and are untrusted.
func workflowEntries(ctx context.Context, clone, rev string) (map[string]treeEntry, error) {
	if !fullSHA.MatchString(rev) {
		return nil, fmt.Errorf("revision %q is not a sha", rev)
	}
	out, err := runGit(ctx, clone, []string{"GIT_LITERAL_PATHSPECS=1"},
		"ls-tree", "-l", "-z", "--full-tree", rev, "--", workflowDir+"/")
	if err != nil {
		return nil, err
	}
	entries := map[string]treeEntry{}
	for _, rec := range strings.Split(out, "\x00") {
		if rec == "" {
			continue
		}
		meta, p, ok := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 4 {
			return nil, fmt.Errorf("unreadable ls-tree line %q", rec)
		}
		size, _ := strconv.ParseInt(fields[3], 10, 64) // "-" for a tree or submodule
		entries[p] = treeEntry{mode: fields[0], typ: fields[1], obj: fields[2], size: size}
	}
	return entries, nil
}

// readWorkflow reads the blob of a workflow entry from the clone; a symlink, a submodule or an
// oversized file is a reason, not content.
func readWorkflow(ctx context.Context, clone string, e treeEntry) ([]byte, string, error) {
	if e.typ != "blob" || (e.mode != "100644" && e.mode != "100755") {
		return nil, fmt.Sprintf("is a %s with mode %s, not a regular file", e.typ, e.mode), nil
	}
	if e.size > maxRead {
		return nil, fmt.Sprintf("is larger than %d bytes", maxRead), nil
	}
	out, err := runGit(ctx, clone, nil, "cat-file", "blob", e.obj)
	if err != nil {
		return nil, "", err
	}
	return []byte(out), "", nil
}

// Policy checks every added or changed workflow of c at c.SHA in the clone against the rules of
// spec §3 and classifies the change by trigger. A deleted workflow does not count. Red means the
// policy could not read the clone; a broken rule is a Violation.
func Policy(ctx context.Context, req Request, c Checked) (Class, []Violation, *Red) {
	entries, err := workflowEntries(ctx, req.CloneDir, c.SHA)
	if err != nil {
		return PreMerge, nil, &Red{StepPolicy, "COULD NOT CHECK: " + err.Error()}
	}
	class := PostMerge
	violations := []Violation{}
	for _, p := range c.Files {
		if !isWorkflow(p) {
			continue
		}
		e, ok := entries[p]
		if !ok {
			continue // deleted
		}
		text, reason, err := readWorkflow(ctx, req.CloneDir, e)
		if err != nil {
			return PreMerge, nil, &Red{StepPolicy, "COULD NOT CHECK: " + err.Error()}
		}
		var found []Violation
		post := false
		if reason != "" {
			found = []Violation{{p, RuleParse, p + " " + reason}}
		} else {
			found, post = checkWorkflow(p, text, req.Default)
		}
		violations = append(violations, found...)
		if !post {
			class = PreMerge
		}
	}
	return class, violations, nil
}

// checkWorkflow applies the rules to one workflow and reports whether it runs only post-merge.
// A workflow it cannot read yields one RuleParse violation and is pre-merge.
func checkWorkflow(file string, text []byte, defaultBranch string) ([]Violation, bool) {
	top, err := parseWorkflow(text)
	if err != nil {
		return []Violation{{file, RuleParse, err.Error()}}, false
	}
	w := &workflowCheck{file: file}
	evs, ok := triggers(top)
	if !ok {
		w.add(RuleParse, "`on` is neither a string, a list of strings nor a map")
	}
	for _, ev := range evs {
		if ev.name == "pull_request_target" || ev.name == "workflow_run" {
			w.add(RuleTrigger, "trigger "+ev.name+" runs with the secrets of the default branch")
		}
	}
	perms, present := top["permissions"]
	switch {
	case !present:
		w.add(RulePermissions, "no explicit permissions at workflow level")
	case !permissionsOK(perms):
		w.add(RulePermissions, fmt.Sprintf("workflow permissions %v: want a map or read-all", perms))
	}
	w.jobs(top["jobs"])
	post := ok && !w.parseFailed && postMerge(evs, defaultBranch)
	return w.found, post
}

// parseWorkflow decodes exactly one YAML document whose top level is a map. Anchors, aliases and
// merge keys are resolved by the decoder; a duplicate key is an error.
func parseWorkflow(text []byte) (top map[string]any, err error) {
	defer func() {
		if r := recover(); r != nil {
			top, err = nil, fmt.Errorf("YAML parser failed: %v", r)
		}
	}()
	dec := yaml.NewDecoder(bytes.NewReader(text))
	var doc any
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the workflow is empty")
		}
		return nil, fmt.Errorf("not valid YAML: %w", err)
	}
	var more any
	if err := dec.Decode(&more); !errors.Is(err, io.EOF) {
		return nil, errors.New("more than one YAML document")
	}
	top, ok := asMap(doc)
	if !ok {
		return nil, errors.New("the top level is not a map")
	}
	return top, nil
}

// asMap normalises a decoded YAML map to string keys; a non-string key becomes its text, and
// two keys with the same text make it unreadable.
func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			key := fmt.Sprint(k)
			if _, dup := out[key]; dup {
				return nil, false
			}
			out[key] = val
		}
		return out, true
	}
	return nil, false
}

// event is one trigger and its configuration (nil in the string and list forms).
type event struct {
	name string
	cfg  any
}

// triggers reads `on` as a string, a list or a map. YAML 1.1 reads a bare `on` as true, so a
// `true` key counts as well; both are joined.
func triggers(top map[string]any) ([]event, bool) {
	var evs []event
	for _, key := range []string{"on", "true"} {
		v, present := top[key]
		if !present {
			continue
		}
		switch x := v.(type) {
		case nil:
		case string:
			evs = append(evs, event{name: x})
		case []any:
			for _, item := range x {
				s, ok := item.(string)
				if !ok {
					return nil, false
				}
				evs = append(evs, event{name: s})
			}
		default:
			m, ok := asMap(x)
			if !ok {
				return nil, false
			}
			for _, name := range sortedKeys(m) {
				evs = append(evs, event{name, m[name]})
			}
		}
	}
	return evs, true
}

// postMerge: at least one trigger, and every trigger is schedule or a push to exactly the
// default branch with no other filter (tags, tags-ignore, paths, branches-ignore).
func postMerge(evs []event, defaultBranch string) bool {
	if len(evs) == 0 {
		return false
	}
	for _, ev := range evs {
		switch ev.name {
		case "schedule":
		case "push":
			m, ok := asMap(ev.cfg)
			if !ok || len(m) != 1 {
				return false
			}
			branches, ok := m["branches"].([]any)
			if !ok || len(branches) != 1 || branches[0] != defaultBranch {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// permissionsOK accepts a permissions map or read-all; write-all, null, an expression or any
// other string is not explicit enough.
func permissionsOK(v any) bool {
	if s, ok := v.(string); ok {
		return s == "read-all"
	}
	if v == nil {
		return false
	}
	_, ok := asMap(v)
	return ok
}

// workflowCheck collects the violations of one workflow file.
type workflowCheck struct {
	file        string
	found       []Violation
	parseFailed bool
}

func (w *workflowCheck) add(rule, why string) {
	if rule == RuleParse {
		w.parseFailed = true
	}
	w.found = append(w.found, Violation{w.file, rule, why})
}

// jobs checks the permissions and every uses: of the jobs: the reusable workflow of a job and
// the action of each step.
func (w *workflowCheck) jobs(v any) {
	if v == nil {
		return
	}
	jobs, ok := asMap(v)
	if !ok {
		w.add(RuleParse, "`jobs` is not a map")
		return
	}
	for _, id := range sortedKeys(jobs) {
		job, ok := asMap(jobs[id])
		if !ok {
			w.add(RuleParse, "job "+id+" is not a map")
			continue
		}
		if p, present := job["permissions"]; present && !permissionsOK(p) {
			w.add(RulePermissions, fmt.Sprintf("job %s permissions %v: want a map or read-all", id, p))
		}
		if u, present := job["uses"]; present {
			w.uses("job "+id, u)
		}
		w.steps(id, job["steps"])
	}
}

func (w *workflowCheck) steps(job string, v any) {
	if v == nil {
		return
	}
	steps, ok := v.([]any)
	if !ok {
		w.add(RuleParse, "steps of job "+job+" are not a list")
		return
	}
	for i, s := range steps {
		step, ok := asMap(s)
		if !ok {
			w.add(RuleParse, fmt.Sprintf("step %d of job %s is not a map", i, job))
			continue
		}
		if u, present := step["uses"]; present {
			w.uses(fmt.Sprintf("step %d of job %s", i, job), u)
		}
	}
}

var (
	pinnedAction = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(/[^@\s]+)?@[0-9a-f]{40}$`)
	pinnedDocker = regexp.MustCompile(`^docker://[^@\s]+@sha256:[0-9a-f]{64}$`)
)

// uses accepts a local ./ path, a docker image by digest, or an external action or reusable
// workflow on a full commit SHA.
func (w *workflowCheck) uses(where string, v any) {
	s, ok := v.(string)
	switch {
	case !ok:
		w.add(RuleUnpinned, fmt.Sprintf("uses of %s is not a string", where))
	case strings.HasPrefix(s, "./"), pinnedDocker.MatchString(s), pinnedAction.MatchString(s):
	default:
		w.add(RuleUnpinned, fmt.Sprintf("uses %q of %s is not pinned on a 40-hex SHA", s, where))
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
