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
)

// NodeChecker checks eslint for a Node root (Expo included): an exact eslint version in
// devDependencies, a config file in the root that makes a choice, and a CI step that runs eslint
// in the root after an install from the lockfile. Node has no floor.
type NodeChecker struct{}

const eslintLinter = "eslint"

// eslintConfigFiles are the config files the check accepts in a Node root, flat config first.
var eslintConfigFiles = []string{
	"eslint.config.js", "eslint.config.mjs", "eslint.config.cjs", "eslint.config.ts",
	"eslint.config.mts", "eslint.config.cts",
	".eslintrc.js", ".eslintrc.cjs", ".eslintrc.yaml", ".eslintrc.yml", ".eslintrc.json", ".eslintrc",
}

// Check implements Checker. dir is the Node root, relative to the repo root.
func (NodeChecker) Check(dir string, env Env) ([]Finding, []string, error) {
	dir = path.Clean(filepath.ToSlash(dir))
	pkg, err := readPackageJSON(env.Root, dir)
	if err != nil {
		return nil, nil, err
	}
	steps, err := Steps(env.Root, env.ForgeKind)
	if err != nil {
		return nil, nil, err
	}
	var findings []Finding
	add := func(file, msg string) {
		findings = append(findings, Finding{File: file, Linter: eslintLinter, Msg: msg})
	}

	pkgFile := path.Join(dir, "package.json")
	if v, ok := pkg.DevDependencies[eslintLinter]; !ok {
		add(pkgFile, "pin missing in devDependencies")
	} else if !exactVersion(v) {
		add(pkgFile, "devDependencies eslint "+v+" is not an exact version")
	}

	cfgFile, err := findEslintConfig(env.Root, dir)
	if err != nil {
		return nil, nil, err
	}
	if cfgFile == "" {
		add(path.Join(dir, eslintConfigFiles[0]), "config missing")
	} else if choice, err := eslintConfigChoice(env.Root, cfgFile); err != nil {
		return nil, nil, err
	} else if !choice {
		add(cfgFile, "config makes no choice (no rules, extends or preset)")
	}

	pkgJSON := func(d string) map[string]string {
		p, err := readPackageJSON(env.Root, d)
		if err != nil {
			return nil
		}
		return p.Scripts
	}
	cfgNarrows := func(d, cfgPath string) bool {
		choice, err := eslintConfigChoice(env.Root, path.Join(d, cfgPath))
		return err != nil || !choice
	}
	// only a call in this root lints it; the lockfile install is part of the verdict (Unpinned)
	var calls []Call
	for _, c := range Calls(steps, eslintLinter, pkgJSON, nil, cfgNarrows) {
		if c.Dir == dir {
			calls = append(calls, c)
		}
	}
	if c, ok := wiredCall(calls, eslintLinter); !ok {
		findings = append(findings, callFinding(c, env.ForgeKind))
	}

	if len(findings) > 0 {
		return findings, nil, nil
	}
	return nil, []string{eslintLinter}, nil
}

// packageJSON is the part of a package.json the check reads.
type packageJSON struct {
	Scripts         map[string]string `json:"scripts"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// readPackageJSON reads the package.json in dir; a missing or invalid file is an UncheckedError.
func readPackageJSON(root, dir string) (packageJSON, error) {
	rel := path.Join(dir, "package.json")
	var p packageJSON
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return p, UncheckedError(fmt.Sprintf("cannot read %s: %v", rel, err))
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, UncheckedError(fmt.Sprintf("cannot read %s: %v", rel, err))
	}
	return p, nil
}

// exactVersionPattern is a full version without a range: 10.13.0, 1.0.0-rc.1, 1.2.3+build.5.
var exactVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// exactVersion reports whether a dependency version names one version: no ^, ~, range, tag,
// alias or path.
func exactVersion(v string) bool { return exactVersionPattern.MatchString(v) }

// findEslintConfig returns the first config file in dir (repo-relative), "" when there is none.
func findEslintConfig(root, dir string) (string, error) {
	for _, name := range eslintConfigFiles {
		rel := path.Join(dir, name)
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return "", UncheckedError(fmt.Sprintf("cannot read %s: %v", rel, err))
		}
		return rel, nil
	}
	return "", nil
}

var (
	blockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	// lineComment is // to the end of the line, except after a colon (a URL in a string).
	lineComment = regexp.MustCompile(`(?m)(^|[^:])//.*$`)
	hashComment = regexp.MustCompile(`(?m)(^|\s)#.*$`)
	// eslintChoice are the textual signs of a choice: a rules key, extends, a shared config
	// (configs.) or a preset package (eslint-config-…, eslint-config-expo included).
	eslintChoice = []*regexp.Regexp{
		regexp.MustCompile(`(?:^|[^\w$.-])["']?rules["']?\s*:`),
		regexp.MustCompile(`\bextends\b`),
		regexp.MustCompile(`\bconfigs\.`),
		regexp.MustCompile(`eslint-config-`),
	}
)

// eslintConfigChoice reports whether the config file rel (repo-relative) shows a sign of a choice
// outside comments. It does not parse JavaScript: a flat config chooses a preset in too many ways.
// A file that cannot be read is an UncheckedError.
func eslintConfigChoice(root, rel string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return false, UncheckedError(fmt.Sprintf("cannot read %s: %v", rel, err))
	}
	text := blockComment.ReplaceAllString(string(data), "")
	text = lineComment.ReplaceAllString(text, "$1")
	switch path.Ext(rel) {
	case ".yaml", ".yml", "":
		text = hashComment.ReplaceAllString(text, "$1")
	}
	for _, re := range eslintChoice {
		if re.MatchString(text) {
			return true, nil
		}
	}
	return false, nil
}
