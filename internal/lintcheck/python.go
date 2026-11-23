package lintcheck

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// PythonChecker checks ruff: the pin in .ruff-version, a ruff config that turns every floor rule
// on, and a CI step that runs `ruff check` on the pinned version.
type PythonChecker struct{}

const (
	ruffLinter  = "ruff"
	ruffPinFile = ".ruff-version"
	ruffHookSh  = "ruff-format-check.sh"
)

// ruffConfigFiles are the config files ruff looks for in a directory, in its own order.
var ruffConfigFiles = []string{".ruff.toml", "ruff.toml", "pyproject.toml"}

// Check implements Checker. dir is always "." for Python.
func (PythonChecker) Check(_ string, env Env) ([]Finding, []string, error) {
	floor, err := Floor(env.Root, ruffHookSh)
	if err != nil {
		return nil, nil, err
	}
	steps, err := Steps(env.Root, env.ForgeKind)
	if err != nil {
		return nil, nil, err
	}
	var findings []Finding
	add := func(file string, line int, msg string) {
		findings = append(findings, Finding{File: file, Line: line, Linter: ruffLinter, Msg: msg})
	}

	pin, err := readPin(env.Root, ruffPinFile)
	if err != nil {
		return nil, nil, err
	}
	if pin == "" {
		add(ruffPinFile, 0, "pin missing")
	}

	cfg, layers, err := findRuffConfig(env.Root, ruffConfigFiles)
	if err != nil {
		return nil, nil, err
	}
	if cfg == "" {
		add("ruff.toml", 0, "config missing")
	} else {
		for _, code := range floor {
			if !layersSelect(layers, code) {
				add(cfg, 0, "floor rule "+code+" disabled")
			}
		}
	}

	cfgNarrows := func(dir, cfgPath string) bool {
		_, ls, err := findRuffConfig(env.Root, []string{path.Join(dir, cfgPath)})
		if err != nil || ls == nil {
			return true
		}
		for _, code := range floor {
			if !layersSelect(ls, code) {
				return true
			}
		}
		return false
	}
	calls := Calls(steps, ruffLinter, nil, floor, cfgNarrows)
	if c, ok := wiredCall(calls, ruffLinter); !ok {
		findings = append(findings, callFinding(c, env.ForgeKind))
	} else if f, ok := ruffVersionFinding(steps, calls, pin); !ok {
		findings = append(findings, f)
	}

	if len(findings) > 0 {
		return findings, nil, nil
	}
	return nil, []string{ruffLinter}, nil
}

// readPin returns the trimmed content of a pin file, "" when it is missing or empty.
func readPin(root, name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", UncheckedError(fmt.Sprintf("cannot read %s: %v", name, err))
	}
	return strings.TrimSpace(string(data)), nil
}

// callFinding is the finding for a linter whose CI calls are not wired: the reported call's step,
// or the CI location when no step runs the linter.
func callFinding(c Call, forgeKind string) Finding {
	if c.Verdict == NotFound {
		file := workflowsDir
		if isGitLab(forgeKind) {
			file = gitlabFile
		}
		return Finding{File: file, Linter: c.Linter, Msg: c.Reason}
	}
	return Finding{File: c.Step.File, Line: c.Step.Line, Linter: c.Linter, Msg: c.Reason}
}

// ruffVersionFinding compares the CI version of the valid calls with the pin. One valid call on the
// pin is enough; otherwise the first mismatch is reported at the step that sets the version. A
// missing pin has its own finding, so no comparison is made.
func ruffVersionFinding(steps []Step, calls []Call, pin string) (Finding, bool) {
	if pin == "" {
		return Finding{}, true
	}
	var first *Finding
	for _, c := range calls {
		if c.Verdict != Valid {
			continue
		}
		v, at, found := ruffCIVersion(steps, c)
		if found && (v == ruffFromPinFile || trimV(v) == trimV(pin)) {
			return Finding{}, true
		}
		if first == nil {
			msg := "CI version not pinned"
			if found {
				msg = fmt.Sprintf("CI version %s, pin %s", v, pin)
			}
			first = &Finding{File: at.File, Line: at.Line, Linter: ruffLinter, Msg: msg}
		}
	}
	if first == nil {
		return Finding{}, true
	}
	return *first, false
}

func trimV(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "v") }

// ruffFromPinFile stands for a CI version read from .ruff-version itself.
const ruffFromPinFile = "$(cat .ruff-version)"

var (
	ruffVersioned = regexp.MustCompile(`(?:^|[^A-Za-z0-9_-])ruff(?:@|==)(\$\(\s*cat\s+(?:\./)?\.ruff-version\s*\)|\S+)`)
	shellVar      = regexp.MustCompile(`^\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?$`)
)

// ruffCIVersion finds the version a valid call runs: the action's version input, else the last
// pinned install in the nearest step of the same job up to the call. A $VAR version is read from
// the step's env.
func ruffCIVersion(steps []Step, c Call) (string, Step, bool) {
	if c.Step.Uses != "" {
		v := c.Step.With["version"]
		return strings.TrimSpace(v), c.Step, versionInput(v)
	}
	for i := len(steps) - 1; i >= 0; i-- {
		s := steps[i]
		if s.IfFalse || s.File != c.Step.File || s.Job != c.Step.Job || s.Index > c.Step.Index {
			continue
		}
		if s.Uses != "" {
			if actionName(s.Uses) == ruffAction && versionInput(s.With["version"]) {
				return strings.TrimSpace(s.With["version"]), s, true
			}
			continue
		}
		if v, ok := runRuffVersion(s); ok {
			return v, s, true
		}
	}
	return "", c.Step, false
}

// runRuffVersion returns the last ruff version a run step installs, outside echo and printf.
func runRuffVersion(s Step) (string, bool) {
	sc, err := parseScript(s.Run)
	if err != nil {
		return "", false
	}
	var last string
	sc.walk(shellState{dir: workDir(s)}, func(v visit) {
		if isEcho(v.words) {
			return
		}
		for _, m := range ruffVersioned.FindAllStringSubmatch(strings.Join(v.words, " "), -1) {
			if m[1] != "latest" {
				last = m[1]
			}
		}
	})
	if last == "" {
		return "", false
	}
	if strings.HasPrefix(last, "$(") {
		return ruffFromPinFile, true
	}
	if m := shellVar.FindStringSubmatch(last); m != nil {
		if val, ok := s.Env[m[1]]; ok {
			return strings.TrimSpace(val), true
		}
	}
	return last, true
}

// ruffLayer is the selection of one config file; layers apply from the base of an extend chain
// to the file itself.
type ruffLayer struct {
	hasSelect bool
	selects   []string // select and extend-select
	ignores   []string // ignore and extend-ignore; per-file ignores are path scope and do not count
}

// ruffEffective reports whether code is on in ruff's own sense in the first of cfgPaths (relative
// to root) that is a ruff config: selected by select or extend-select (exact, by prefix or ALL)
// and not ignored, the most specific selector winning and ignore winning a tie. extend is
// followed. No config is false; an unreadable config, a missing extend target or an extend cycle
// is an UncheckedError.
func ruffEffective(cfgPaths []string, root string, code string) (bool, error) {
	_, layers, err := findRuffConfig(root, cfgPaths)
	if err != nil {
		return false, err
	}
	return layersSelect(layers, code), nil
}

// findRuffConfig returns the first of cands (relative to root) that exists and holds ruff
// settings, with its layers; "" when there is none.
func findRuffConfig(root string, cands []string) (string, []ruffLayer, error) {
	for _, c := range cands {
		p := filepath.Join(root, filepath.FromSlash(c))
		if _, err := os.Stat(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return "", nil, UncheckedError(fmt.Sprintf("cannot read %s: %v", c, err))
		}
		layers, isRuff, err := loadRuff(root, p, map[string]bool{})
		if err != nil {
			return "", nil, err
		}
		if isRuff {
			return path.Clean(filepath.ToSlash(c)), layers, nil
		}
	}
	return "", nil, nil
}

// loadRuff reads one config file and the files it extends. isRuff is false for a pyproject.toml
// without [tool.ruff].
func loadRuff(root, p string, seen map[string]bool) (layers []ruffLayer, isRuff bool, err error) {
	rel := p
	if r, rerr := filepath.Rel(root, p); rerr == nil {
		rel = filepath.ToSlash(r)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return nil, false, UncheckedError(fmt.Sprintf("ruff config %s: %v", rel, err))
	}
	if seen[abs] {
		return nil, false, UncheckedError(fmt.Sprintf("ruff config %s: extend cycle", rel))
	}
	seen[abs] = true
	var doc map[string]any
	if _, err := toml.DecodeFile(abs, &doc); err != nil {
		return nil, false, UncheckedError(fmt.Sprintf("cannot read ruff config %s: %v", rel, err))
	}
	settings := doc
	if filepath.Base(abs) == "pyproject.toml" {
		tool, _ := doc["tool"].(map[string]any)
		settings, _ = tool["ruff"].(map[string]any)
		if settings == nil {
			return nil, false, nil
		}
	}
	if ext, ok := settings["extend"]; ok {
		target, ok := ext.(string)
		if !ok {
			return nil, false, UncheckedError(fmt.Sprintf("ruff config %s: extend is not a path", rel))
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(abs), target)
		}
		if _, err := os.Stat(target); err != nil {
			return nil, false, UncheckedError(fmt.Sprintf("ruff config %s: cannot read extend target %s", rel, ext))
		}
		if layers, _, err = loadRuff(root, target, seen); err != nil {
			return nil, false, err
		}
	}
	layer, err := ruffSelection(settings, rel)
	if err != nil {
		return nil, false, err
	}
	return append(layers, layer), true, nil
}

// ruffSelection reads the selection keys; a key under [lint] wins over the deprecated top-level
// one.
func ruffSelection(settings map[string]any, rel string) (ruffLayer, error) {
	lint, _ := settings["lint"].(map[string]any)
	get := func(key string) any {
		if v, ok := lint[key]; ok {
			return v
		}
		return settings[key]
	}
	var l ruffLayer
	var errs []string
	list := func(key string) []string {
		v := get(key)
		if v == nil {
			return nil
		}
		out, ok := stringList(v)
		if !ok {
			errs = append(errs, key)
		}
		return out
	}
	if get("select") != nil {
		l.hasSelect = true
	}
	l.selects = append(list("select"), list("extend-select")...)
	l.ignores = append(list("ignore"), list("extend-ignore")...)
	if len(errs) > 0 {
		return l, UncheckedError(fmt.Sprintf("ruff config %s: %s is not a list of codes", rel, errs[0]))
	}
	return l, nil
}

func stringList(v any) ([]string, bool) {
	items, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, ok := it.(string)
		if !ok {
			return nil, false
		}
		out = append(out, strings.TrimSpace(s))
	}
	return out, true
}

// layersSelect applies the layers in order, as ruff does: a select resets the selection, the most
// specific selector covering code decides within a layer, and ignore wins a tie. A default
// selection does not count, so the start is off.
func layersSelect(layers []ruffLayer, code string) bool {
	on := false
	for _, l := range layers {
		if l.hasSelect {
			on = false
		}
		sel, ign := specificity(l.selects, code), specificity(l.ignores, code)
		switch {
		case sel < 0 && ign < 0:
		case ign >= sel:
			on = false
		default:
			on = true
		}
	}
	return on
}

// specificity is the highest specificity of a selector in list that covers code: 0 for ALL, else
// its length; -1 when none covers it.
func specificity(list []string, code string) int {
	best := -1
	for _, s := range list {
		if s == "" || !ruffCovers(s, code) {
			continue
		}
		n := len(s)
		if s == "ALL" {
			n = 0
		}
		if n > best {
			best = n
		}
	}
	return best
}
