package lintcheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// GoChecker checks golangci-lint: the pin in .golangci-version, a version 2 config that turns
// every floor linter on, and a CI step that runs `golangci-lint run` on the pinned version.
type GoChecker struct{}

const (
	golangciLinter  = "golangci-lint"
	golangciPinFile = ".golangci-version"
	golangciHookSh  = "go-lint-gate.sh"
)

// golangciConfigFiles are the config files golangci-lint looks for, in its own order.
var golangciConfigFiles = []string{".golangci.yml", ".golangci.yaml", ".golangci.toml", ".golangci.json"}

// golangciStandard is the set golangci-lint v2 runs with `linters.default: standard`.
var golangciStandard = []string{"errcheck", "govet", "ineffassign", "staticcheck", "unused"}

// Check implements Checker. dir is always "." for Go.
func (GoChecker) Check(_ string, env Env) ([]Finding, []string, error) {
	floor, err := Floor(env.Root, golangciHookSh)
	if err != nil {
		return nil, nil, err
	}
	steps, err := Steps(env.Root, env.ForgeKind)
	if err != nil {
		return nil, nil, err
	}
	var findings []Finding
	add := func(file string, msg string) {
		findings = append(findings, Finding{File: file, Linter: golangciLinter, Msg: msg})
	}

	pin, err := readPin(env.Root, golangciPinFile)
	if err != nil {
		return nil, nil, err
	}
	if pin == "" {
		add(golangciPinFile, "pin missing")
	}

	cfgFile, cfg, err := findGolangciConfig(env.Root)
	if err != nil {
		return nil, nil, err
	}
	if cfgFile == "" {
		add(golangciConfigFiles[0], "config missing")
	} else {
		for _, msg := range cfg.floorGaps(floor) {
			add(cfgFile, msg)
		}
	}

	cfgNarrows := func(dir, cfgPath string) bool {
		rel := path.Join(dir, cfgPath)
		c, err := loadGolangciConfig(filepath.Join(env.Root, filepath.FromSlash(rel)), rel)
		return err != nil || len(c.floorGaps(floor)) > 0
	}
	calls := Calls(steps, golangciLinter, nil, floor, cfgNarrows)
	if c, ok := wiredCall(calls, golangciLinter); !ok {
		findings = append(findings, callFinding(c, env.ForgeKind))
	} else if f, ok := ciVersionFinding(calls, pin, golangciLinter, func(c Call) (string, Step, bool) {
		return golangciCIVersion(steps, c)
	}); !ok {
		findings = append(findings, f)
	}

	if len(findings) > 0 {
		return findings, nil, nil
	}
	return nil, []string{golangciLinter}, nil
}

// golangciConfig is the part of a golangci-lint config that selects linters. Exclusions are path
// scope and do not count.
type golangciConfig struct {
	version          string
	defaultSet       string
	enable, disable  []string
	hasDefaultSetKey bool
}

// floorGaps returns a message per way the config misses the floor: not version 2 (and nothing
// else), a floor linter in disable, one that neither the default set nor enable turns on, and
// without linters.default one message for the linters enable leaves out.
func (c golangciConfig) floorGaps(floor []string) []string {
	if c.version != "2" {
		return []string{"config not version 2"}
	}
	var gaps []string
	missingDefault := false
	for _, l := range floor {
		switch {
		case contains(c.disable, l):
			gaps = append(gaps, "floor rule "+l+" disabled")
		case contains(c.enable, l), c.defaultSet == "all", c.defaultSet == "standard" && contains(golangciStandard, l):
		case !c.hasDefaultSetKey:
			missingDefault = true
		default:
			gaps = append(gaps, "floor rule "+l+" disabled")
		}
	}
	if missingDefault {
		gaps = append([]string{"linters.default missing"}, gaps...)
	}
	return gaps
}

// findGolangciConfig returns the first config file at the root, with its selection; "" when there
// is none.
func findGolangciConfig(root string) (string, golangciConfig, error) {
	for _, name := range golangciConfigFiles {
		p := filepath.Join(root, name)
		if _, err := os.Stat(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return "", golangciConfig{}, UncheckedError(fmt.Sprintf("cannot read %s: %v", name, err))
		}
		c, err := loadGolangciConfig(p, name)
		return name, c, err
	}
	return "", golangciConfig{}, nil
}

// loadGolangciConfig reads a config by its extension: .toml, .json, else YAML. A file that cannot
// be read or parsed, or a selection key of the wrong type, is an UncheckedError.
func loadGolangciConfig(p, rel string) (golangciConfig, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return golangciConfig{}, UncheckedError(fmt.Sprintf("cannot read golangci-lint config %s: %v", rel, err))
	}
	var doc map[string]any
	switch strings.ToLower(filepath.Ext(p)) {
	case ".toml":
		err = toml.Unmarshal(data, &doc)
	case ".json":
		err = json.Unmarshal(data, &doc)
	default:
		err = yaml.Unmarshal(data, &doc)
	}
	if err != nil {
		return golangciConfig{}, UncheckedError(fmt.Sprintf("cannot read golangci-lint config %s: %v", rel, err))
	}
	var c golangciConfig
	if v, ok := doc["version"]; ok && v != nil {
		c.version = strings.TrimSpace(fmt.Sprint(v))
	}
	section, _ := doc["linters"].(map[string]any)
	if d, ok := section["default"]; ok {
		s, ok := d.(string)
		if !ok {
			return c, UncheckedError(fmt.Sprintf("golangci-lint config %s: linters.default is not a string", rel))
		}
		c.defaultSet, c.hasDefaultSetKey = strings.TrimSpace(s), true
	}
	for key, dst := range map[string]*[]string{"enable": &c.enable, "disable": &c.disable} {
		v, ok := section[key]
		if !ok || v == nil {
			continue
		}
		list, ok := stringList(v)
		if !ok {
			return c, UncheckedError(fmt.Sprintf("golangci-lint config %s: linters.%s is not a list of linters", rel, key))
		}
		*dst = list
	}
	return c, nil
}

var (
	// golangciVersioned is a module path of golangci-lint with a version: …/golangci-lint@v2.15.0.
	golangciVersioned = regexp.MustCompile(`golangci-lint[^@]*@(.+)$`)
	versionWord       = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)
	catPin            = regexp.MustCompile(`^\$\(\s*cat\s+(?:\./)?(\S+)\s*\)$`)
)

// golangciCIVersion finds the version a valid call runs: the action's version input, else the last
// pinned install in the nearest step of the same job up to the call.
func golangciCIVersion(steps []Step, c Call) (string, Step, bool) {
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
			if actionName(s.Uses) == golangciAction && versionInput(s.With["version"]) {
				return strings.TrimSpace(s.With["version"]), s, true
			}
			continue
		}
		if v, ok := runGolangciVersion(s); ok {
			return v, s, true
		}
	}
	return "", c.Step, false
}

// runGolangciVersion returns the last golangci-lint version a run step installs, outside echo and
// printf: `go install …/golangci-lint@<v>` (or go run), or the version argument of the install
// script in a pipeline that fetches it.
func runGolangciVersion(s Step) (string, bool) {
	sc, err := parseScript(s.Run)
	if err != nil {
		return "", false
	}
	var last string
	sc.walk(shellState{dir: workDir(s)}, func(v visit) {
		if isEcho(v.words) {
			return
		}
		for _, w := range v.words {
			if m := golangciVersioned.FindStringSubmatch(w); m != nil && m[1] != "latest" {
				if r, ok := resolveVersion(m[1], s, golangciPinFile); ok {
					last = r
				}
			}
		}
		var pipe []string
		for _, cmd := range sc.lists[v.pos.list].pipes[v.pos.pipe].cmds {
			pipe = append(pipe, texts(cmd.words)...)
		}
		p := strings.Join(pipe, " ")
		if !strings.Contains(p, "golangci") || !strings.Contains(p, "install.sh") || v.pos.cmd == 0 {
			return // the version is an argument of the shell that runs the script
		}
		for _, w := range v.words[1:] {
			if r, ok := resolveVersion(w, s, golangciPinFile); ok {
				last = r
			}
		}
	})
	return last, last != ""
}

// resolveVersion reads a version word: $(cat <pinFile>) is the pin itself, a $VAR is read from the
// step's env, and anything else must look like a version.
func resolveVersion(w string, s Step, pinFile string) (string, bool) {
	w = strings.TrimSpace(w)
	if m := catPin.FindStringSubmatch(w); m != nil {
		return fromPinFile, path.Clean(m[1]) == pinFile
	}
	if m := shellVar.FindStringSubmatch(w); m != nil {
		val, ok := s.Env[m[1]]
		return strings.TrimSpace(val), ok && strings.TrimSpace(val) != ""
	}
	return w, versionWord.MatchString(w)
}
