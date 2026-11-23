package lintcheck

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ShellChecker checks shellcheck: the pin SHELLCHECK_PIN of the installed shellcheck-bin.sh, an rc
// file at the root that turns no floor code off (no rc file is fine), and a CI step that runs
// shellcheck on the pinned version.
type ShellChecker struct{}

const (
	shellcheckLinter = "shellcheck"
	shellcheckHookSh = "shell-lint-check.sh"
	shellcheckLib    = ".lighthouse/scripts/lib/shellcheck-bin.sh"
)

// shellcheckRcFiles are the rc file names shellcheck looks for in a directory, in its own order.
var shellcheckRcFiles = []string{".shellcheckrc", "shellcheckrc"}

// Check implements Checker. dir is always "." for shell.
func (ShellChecker) Check(_ string, env Env) ([]Finding, []string, error) {
	floor, err := Floor(env.Root, shellcheckHookSh)
	if err != nil {
		return nil, nil, err
	}
	pin, err := shellcheckPin(env.Root)
	if err != nil {
		return nil, nil, err
	}
	steps, err := Steps(env.Root, env.ForgeKind)
	if err != nil {
		return nil, nil, err
	}
	var findings []Finding

	for _, name := range shellcheckRcFiles {
		p := filepath.Join(env.Root, name)
		if _, err := os.Stat(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, nil, UncheckedError(fmt.Sprintf("cannot read %s: %v", name, err))
		}
		off, err := rcDisabled(p, name, floor)
		if err != nil {
			return nil, nil, err
		}
		for _, d := range off {
			findings = append(findings, Finding{File: name, Line: d.line, Linter: shellcheckLinter, Msg: "floor rule " + d.code + " disabled"})
		}
		break // shellcheck reads the first rc file it finds
	}

	cfgNarrows := func(dir, cfgPath string) bool {
		rel := path.Join(dir, cfgPath)
		off, err := rcDisabled(filepath.Join(env.Root, filepath.FromSlash(rel)), rel, floor)
		return err != nil || len(off) > 0
	}
	calls := Calls(steps, shellcheckLinter, nil, floor, cfgNarrows)
	if c, ok := wiredCall(calls, shellcheckLinter); !ok {
		findings = append(findings, callFinding(c, env.ForgeKind))
	} else if f, ok := ciVersionFinding(calls, pin, shellcheckLinter, func(c Call) (string, Step, bool) {
		return shellcheckCIVersion(steps, c)
	}); !ok {
		findings = append(findings, f)
	}

	if len(findings) > 0 {
		return findings, nil, nil
	}
	return nil, []string{shellcheckLinter}, nil
}

var shellcheckPinLine = regexp.MustCompile(`(?m)^SHELLCHECK_PIN=["']?([^"'\s]+)["']?[ \t]*$`)

// shellcheckPin reads SHELLCHECK_PIN from the installed shellcheck-bin.sh; a missing lib, or one
// without the pin, is an UncheckedError.
func shellcheckPin(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(shellcheckLib)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", UncheckedError(shellcheckLib + " is not installed")
		}
		return "", UncheckedError(fmt.Sprintf("cannot read %s: %v", shellcheckLib, err))
	}
	m := shellcheckPinLine.FindSubmatch(data)
	if m == nil {
		return "", UncheckedError(shellcheckLib + " has no SHELLCHECK_PIN")
	}
	return string(m[1]), nil
}

// rcOff is a floor code an rc file turns off, with the line of the disable.
type rcOff struct {
	code string
	line int
}

// rcDisabled returns the floor codes the rc file at p turns off through `disable=`: a code, `all`
// or a range (SC2000-SC2999). A missing or unreadable file, or a value the check cannot read, is
// an UncheckedError; rel names the file in the message.
func rcDisabled(p, rel string, floor []string) ([]rcOff, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, UncheckedError(fmt.Sprintf("cannot read %s: %v", rel, err))
	}
	var out []rcOff
	done := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "disable" {
			continue
		}
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			covers, err := rcCovers(item)
			if err != nil {
				return nil, UncheckedError(fmt.Sprintf("%s:%d: %v", rel, n, err))
			}
			for _, code := range floor {
				if !done[code] && covers(code) {
					done[code] = true
					out = append(out, rcOff{code: code, line: n})
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, UncheckedError(fmt.Sprintf("cannot read %s: %v", rel, err))
	}
	// in floor order, as the other checkers report
	ordered := make([]rcOff, 0, len(out))
	for _, code := range floor {
		for _, o := range out {
			if o.code == code {
				ordered = append(ordered, o)
			}
		}
	}
	return ordered, nil
}

// rcCovers parses one item of a disable list into a test on a floor code.
func rcCovers(item string) (func(string) bool, error) {
	if strings.EqualFold(item, "all") {
		return func(string) bool { return true }, nil
	}
	lo, hi, isRange := strings.Cut(item, "-")
	a, err := scNumber(lo)
	if err != nil {
		return nil, err
	}
	b := a
	if isRange {
		if b, err = scNumber(hi); err != nil {
			return nil, err
		}
		if b < a {
			return nil, fmt.Errorf("disable range %s runs backwards", item)
		}
	}
	return func(code string) bool {
		n, err := scNumber(code)
		return err == nil && n >= a && n <= b
	}, nil
}

// scNumber reads SC2164 or 2164 as 2164.
func scNumber(s string) (int, error) {
	s = strings.TrimSpace(s)
	digits := strings.TrimPrefix(strings.ToUpper(s), "SC")
	n, err := strconv.Atoi(digits)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%q is no shellcheck code", s)
	}
	return n, nil
}

// shellcheckCIVersion finds the version a valid call runs: the pin itself for shellcheck-repo.sh,
// else the version of the nearest install in the same job up to the call.
func shellcheckCIVersion(steps []Step, c Call) (string, Step, bool) {
	for i := len(steps) - 1; i >= 0; i-- {
		s := steps[i]
		if s.IfFalse || s.File != c.Step.File || s.Job != c.Step.Job || s.Index > c.Step.Index || s.Run == "" {
			continue
		}
		if v, ok := runShellcheckVersion(s); ok {
			return v, s, true
		}
	}
	return "", c.Step, false
}

// runShellcheckVersion returns the last shellcheck version a run step provisions, outside echo and
// printf: shellcheck-repo.sh provisions the pin, an install names the version in a word that
// names shellcheck (a release URL, shellcheck=0.9.0-1).
func runShellcheckVersion(s Step) (string, bool) {
	sc, err := parseScript(s.Run)
	if err != nil {
		return "", false
	}
	var last string
	sc.walk(shellState{dir: workDir(s)}, func(v visit) {
		if isEcho(v.words) {
			return
		}
		if binName(stripWrappersOrSelf(v.words)[0]) == "shellcheck-repo.sh" {
			last = fromPinFile
			return
		}
		for _, w := range v.words {
			if !strings.Contains(strings.ToLower(w), "shellcheck") {
				continue
			}
			if m := versionNumber.FindString(w); m != "" {
				last = m
			}
		}
	})
	return last, last != ""
}
