package importcheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
)

// PythonChecker checks a Python project with deptry, run in the project's venv.
type PythonChecker struct {
	// SourceDirs are relative to the project; empty switches the check off, silently.
	SourceDirs []string
	// MainCheckout is the main checkout whose .venv is the fallback for a fresh worktree; empty
	// means only the project's own .venv counts.
	MainCheckout string
}

// deptryReasons are the blocking rules; DEP002 (unused) and DEP004 (dev dependency in
// production code) are left out on purpose.
var deptryReasons = map[string]string{
	"DEP001": "module is imported but missing from the dependency definitions",
	"DEP003": "module is imported but only a transitive dependency",
}

// Check runs deptry on the existing source dirs of the project.
func (c PythonChecker) Check(p Project, env Env) ([]Finding, error) {
	unchecked := func(format string, a ...any) error {
		return UncheckedError(fmt.Sprintf(format, a...))
	}
	if len(c.SourceDirs) == 0 {
		return nil, nil
	}
	dir := filepath.Join(env.Root, filepath.FromSlash(p.Dir))
	var sources []string
	for _, s := range c.SourceDirs {
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(s))); err == nil && info.IsDir() {
			sources = append(sources, s)
		}
	}
	if len(sources) == 0 {
		_, _ = fmt.Fprintf(env.Stderr, "not checked: none of the source dirs %v exists in %s\n", c.SourceDirs, p.Dir)
		return nil, nil
	}
	if _, err := env.LookPath("uv"); err != nil {
		return nil, unchecked("uv not found on PATH, needed in %s: %v", p.Dir, err)
	}
	venv, ok := findVenv(dir, c.MainCheckout)
	if !ok {
		return nil, unchecked("no .venv in %s or the main checkout; run uv sync first.", p.Dir)
	}
	tmp, err := os.CreateTemp("", "deptry-*.json")
	if err != nil {
		return nil, unchecked("cannot create the deptry output file for %s: %v", p.Dir, err)
	}
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmp.Name()) }()

	argv := append([]string{"uv", "run", "--frozen", "--no-sync", "--with", "deptry==" + DeptryVersion, "deptry"}, sources...)
	argv = append(argv, "--json-output", tmp.Name())
	_, stderr, code, err := env.Exec(dir, argv, []string{"VIRTUAL_ENV=" + venv, "UV_PROJECT_ENVIRONMENT=" + venv})
	if err != nil {
		return nil, unchecked("deptry could not start in %s: %v", p.Dir, err)
	}
	// deptry exits 0 (clean) or 1 (violations); anything else is a failure to run.
	if code != 0 && code != 1 {
		return nil, unchecked("deptry failed in %s: exit %d: %s", p.Dir, code, string(stderr))
	}
	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		return nil, unchecked("cannot read the deptry output of %s: %v", p.Dir, err)
	}
	findings, err := parseDeptry(data)
	if err != nil {
		return nil, unchecked("cannot read the deptry output of %s: %v", p.Dir, err)
	}
	for i := range findings {
		findings[i].File = path.Join(p.Dir, findings[i].File)
	}
	return findings, nil
}

type deptryViolation struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
	Module   string `json:"module"`
	Location struct {
		File string `json:"file"`
		Line int    `json:"line"`
	} `json:"location"`
}

// parseDeptry reads the JSON of `deptry --json-output` into the blocking findings. File stays
// as deptry reports it, relative to its working directory; the column is dropped.
func parseDeptry(out []byte) ([]Finding, error) {
	var violations []deptryViolation
	if err := json.Unmarshal(out, &violations); err != nil {
		return nil, err
	}
	var findings []Finding
	for _, v := range violations {
		reason, blocking := deptryReasons[v.Error.Code]
		if !blocking {
			continue
		}
		findings = append(findings, Finding{
			File: filepath.ToSlash(v.Location.File), Line: v.Location.Line,
			ID: v.Error.Code, Import: v.Module, Reason: reason,
		})
	}
	return findings, nil
}

// findVenv returns the project's own .venv, else the one of the main checkout.
func findVenv(projectDir, mainCheckout string) (string, bool) {
	candidates := []string{filepath.Join(projectDir, ".venv")}
	if mainCheckout != "" {
		candidates = append(candidates, filepath.Join(mainCheckout, ".venv"))
	}
	for _, v := range candidates {
		if info, err := os.Stat(v); err == nil && info.IsDir() {
			return v, true
		}
	}
	return "", false
}
