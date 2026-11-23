package lintcheck

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Step is one step of a CI job. File is relative to the repo root, slash-separated; Job is the job
// name and Index the position of the step in that job (0-based). A step counts as an earlier
// install only with the same File and Job and a lower Index.
type Step struct {
	File               string
	Job                string
	Index              int
	Line               int
	Run                string
	Uses               string
	With               map[string]string
	Env                map[string]string // workflow, job and step env merged; the step wins
	WorkDir            string            // step, else job defaults, else workflow defaults
	Shell              string            // same precedence as WorkDir
	RunsOn             string            // empty when absent or not a plain string
	ContinueOnError    string
	JobContinueOnError string
	IfFalse            bool // the literal `if: false`
	GitLab             bool
	AllowFailure       bool
	Manual             bool
}

const workflowsDir = ".github/workflows"

// triggers are the events that run a workflow on a change.
var triggers = map[string]bool{"push": true, "pull_request": true, "pull_request_target": true, "merge_group": true}

func isGitLab(forgeKind string) bool { return forgeKind == "gitlab" }

func workflowFiles(root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(workflowsDir)))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		ext := strings.ToLower(path.Ext(e.Name()))
		if !e.IsDir() && (ext == ".yml" || ext == ".yaml") {
			out = append(out, path.Join(workflowsDir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// ciFilesPresent reports whether the repository has any CI file of the forge.
func ciFilesPresent(root, forgeKind string) bool {
	if isGitLab(forgeKind) {
		_, err := os.Stat(filepath.Join(root, ".gitlab-ci.yml"))
		return err == nil
	}
	return len(workflowFiles(root)) > 0
}

type workflow struct {
	file string
	top  *yaml.Node // the mapping
	on   map[string]bool
}

// Steps returns the steps of every workflow that runs on a change, in job order: a workflow with a
// push or pull_request trigger, plus the local reusable workflows it calls one level deep. Invalid
// YAML is an UncheckedError; no CI files is an empty list.
func Steps(root, forgeKind string) ([]Step, error) {
	if isGitLab(forgeKind) {
		return nil, UncheckedError("GitLab CI is not supported yet")
	}
	loaded := map[string]*workflow{}
	var order []string
	for _, f := range workflowFiles(root) {
		w, err := loadWorkflow(root, f)
		if err != nil {
			return nil, err
		}
		loaded[f] = w
		order = append(order, f)
	}
	var files []string
	seen := map[string]bool{}
	add := func(f string) {
		if !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	for _, f := range order {
		w := loaded[f]
		if !w.runsOnChange() {
			continue
		}
		add(f)
		for _, job := range jobs(w.top) {
			if u := scalar(mapGet(job.node, "uses")); strings.HasPrefix(u, "./") {
				if c, ok := loaded[path.Clean(u)]; ok && c.on["workflow_call"] {
					add(c.file)
				}
			}
		}
	}
	var out []Step
	for _, f := range files {
		out = append(out, loaded[f].steps()...)
	}
	return out, nil
}

func loadWorkflow(root, file string) (*workflow, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
	if err != nil {
		return nil, UncheckedError(fmt.Sprintf("cannot read %s: %v", file, err))
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, UncheckedError(fmt.Sprintf("%s is not valid YAML: %v", file, err))
	}
	w := &workflow{file: file, on: map[string]bool{}}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return w, nil // empty or not a workflow: no steps
	}
	w.top = doc.Content[0]
	on := mapGet(w.top, "on")
	if on == nil {
		on = mapGet(w.top, "true") // a YAML 1.1 reader may turn the key on into a boolean
	}
	if on != nil {
		switch on.Kind {
		case yaml.ScalarNode:
			w.on[on.Value] = true
		case yaml.SequenceNode:
			for _, n := range on.Content {
				w.on[n.Value] = true
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(on.Content); i += 2 {
				w.on[on.Content[i].Value] = true
			}
		}
	}
	return w, nil
}

func (w *workflow) runsOnChange() bool {
	for ev := range w.on {
		if triggers[ev] {
			return true
		}
	}
	return false
}

type jobNode struct {
	name string
	node *yaml.Node
}

func jobs(top *yaml.Node) []jobNode {
	m := mapGet(top, "jobs")
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	var out []jobNode
	for i := 0; i+1 < len(m.Content); i += 2 {
		out = append(out, jobNode{m.Content[i].Value, m.Content[i+1]})
	}
	return out
}

func (w *workflow) steps() []Step {
	if w.top == nil {
		return nil
	}
	wfEnv := stringMap(mapGet(w.top, "env"))
	wfDir, wfShell := runDefaults(w.top)
	var out []Step
	for _, j := range jobs(w.top) {
		seq := mapGet(j.node, "steps")
		if seq == nil || seq.Kind != yaml.SequenceNode {
			continue
		}
		jobDir, jobShell := runDefaults(j.node)
		jobEnv := stringMap(mapGet(j.node, "env"))
		runsOn := scalar(mapGet(j.node, "runs-on"))
		jobCoE := scalar(mapGet(j.node, "continue-on-error"))
		for i, sn := range seq.Content {
			s := Step{
				File: w.file, Job: j.name, Index: i, Line: sn.Line,
				Run: scalar(mapGet(sn, "run")), Uses: scalar(mapGet(sn, "uses")),
				With:    stringMap(mapGet(sn, "with")),
				Env:     mergeMaps(wfEnv, jobEnv, stringMap(mapGet(sn, "env"))),
				WorkDir: firstNonEmpty(scalar(mapGet(sn, "working-directory")), jobDir, wfDir),
				Shell:   firstNonEmpty(scalar(mapGet(sn, "shell")), jobShell, wfShell),
				RunsOn:  runsOn, ContinueOnError: scalar(mapGet(sn, "continue-on-error")),
				JobContinueOnError: jobCoE,
			}
			cond := strings.TrimSpace(scalar(mapGet(sn, "if")))
			s.IfFalse = cond == "false" || strings.ReplaceAll(cond, " ", "") == "${{false}}"
			out = append(out, s)
		}
	}
	return out
}

func runDefaults(n *yaml.Node) (dir, shell string) {
	run := mapGet(mapGet(n, "defaults"), "run")
	return scalar(mapGet(run, "working-directory")), scalar(mapGet(run, "shell"))
}

// mapGet returns the value of key in a mapping node, or nil.
func mapGet(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func scalar(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func stringMap(n *yaml.Node) map[string]string {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	m := map[string]string{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		m[n.Content[i].Value] = scalar(n.Content[i+1])
	}
	return m
}

func mergeMaps(maps ...map[string]string) map[string]string {
	var out map[string]string
	for _, m := range maps {
		for k, v := range m {
			if out == nil {
				out = map[string]string{}
			}
			out[k] = v
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
