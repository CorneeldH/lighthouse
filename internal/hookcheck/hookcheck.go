// Package hookcheck is `lighthouse check-hooks`: does every hook lighthouse installed run on this
// machine? It checks in four steps, each with one line per finding:
//
//	1   Git Bash (Windows only): Claude Code runs a shell-form hook in Git Bash, and in
//	    PowerShell without it, where a `sh` hook blocks nothing.
//	2   CRLF: no file under .claude/hooks/lighthouse/ has `\r\n` line endings.
//	2b  registrations: every hook unit of the manifest is registered, and no settings file
//	    sets disableAllHooks.
//	3   smoke test: every registration under .claude/hooks/lighthouse/ runs once, the way
//	    Claude Code runs it, with a payload a healthy hook does nothing with.
//
// Output contract, as in internal/checkinstr: `ok    <text>` or `FAIL  <text>` with the remedy,
// exit 1 on at least one FAIL. When the repository cannot be measured at all (no root, no
// .claude/settings.json, one that does not read), `check-hooks: COULD NOT CHECK — <reason>` goes
// to stderr and the exit is 2.
package hookcheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/manifest"
	"github.com/CorneeldH/lighthouse/internal/orderedjson"
	"github.com/CorneeldH/lighthouse/internal/packs"
	"github.com/CorneeldH/lighthouse/internal/settings"
)

const (
	settingsPath      = ".claude/settings.json"
	localSettingsPath = ".claude/settings.local.json"
	gitBashVar        = "CLAUDE_CODE_GIT_BASH_PATH"
	upgradeRemedy     = "run `lighthouse init --upgrade`"
)

// Env is the machine the check runs on. Every field is a seam for the tests; DefaultEnv fills
// them from the real host.
type Env struct {
	Root, Home, GOOS string
	Getenv           func(string) string
	LookPath         func(string) (string, error)
	// Exists says whether a regular file is at the path.
	Exists func(string) bool
}

// DefaultEnv is the real host, for the repository at root.
func DefaultEnv(root string) Env {
	home, _ := os.UserHomeDir()
	return Env{
		Root:     root,
		Home:     home,
		GOOS:     runtime.GOOS,
		Getenv:   os.Getenv,
		LookPath: exec.LookPath,
		Exists: func(p string) bool {
			info, err := os.Stat(p)
			return err == nil && info.Mode().IsRegular()
		},
	}
}

// CouldNotCheck prints why the command as a whole cannot measure and returns its exit code, 2.
func CouldNotCheck(errOut io.Writer, reason string) int {
	_, _ = fmt.Fprintf(errOut, "check-hooks: COULD NOT CHECK — %s\n", reason)
	return 2
}

// report prints the outcome lines and counts the findings.
type report struct {
	out      io.Writer
	findings int
}

func (r *report) ok(msg string) { _, _ = fmt.Fprintf(r.out, "ok    %s\n", msg) }

func (r *report) fail(msg string) {
	_, _ = fmt.Fprintf(r.out, "FAIL  %s\n", msg)
	r.findings++
}

// registration is one command hook of .claude/settings.json.
type registration struct {
	Event, Matcher, Command string
	// Timeout is the `timeout` of the entry; 0 when it has none.
	Timeout time.Duration
}

// smokeStep is step 3; a test of the earlier steps replaces it.
var smokeStep = smoke

// Run checks the repository at env.Root: steps 1, 2, 2b and 3, in that order.
func Run(env Env, out, errOut io.Writer) int {
	if info, err := os.Stat(env.Root); err != nil || !info.IsDir() {
		return CouldNotCheck(errOut, fmt.Sprintf("repository root '%s' is not a directory", env.Root))
	}
	doc, err := readJSON(filepath.Join(env.Root, filepath.FromSlash(settingsPath)))
	if err != nil {
		return CouldNotCheck(errOut, err.Error())
	}
	regs, err := settings.Registrations(doc)
	if err != nil {
		return CouldNotCheck(errOut, err.Error())
	}
	all := make([]registration, 0, len(regs))
	for _, reg := range regs {
		all = append(all, toRegistration(reg))
	}

	r := &report{out: out}
	bash := ""
	if env.GOOS == "windows" {
		bash = checkGitBash(env, r)
	}
	checkCRLF(env.Root, r)
	checkRegistered(env.Root, all, r)
	checkDisabled(env, doc, r)
	smokeStep(env, all, bash, r)
	if r.findings > 0 {
		_, _ = fmt.Fprintf(out, "\n%d finding(s).\n", r.findings)
		return 1
	}
	return 0
}

// readJSON reads one settings file as an object. The error names the path.
func readJSON(path string) (*orderedjson.Object, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s does not exist", path)
		}
		return nil, fmt.Errorf("%s cannot be read: %v", path, err)
	}
	v, err := orderedjson.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %v", path, err)
	}
	obj, ok := v.(*orderedjson.Object)
	if !ok {
		return nil, fmt.Errorf("%s is not a JSON object", path)
	}
	return obj, nil
}

func toRegistration(reg packs.Registration) registration {
	out := registration{Event: reg.Event, Matcher: reg.Matcher}
	if c, ok := reg.Entry.Get("command"); ok {
		out.Command, _ = c.(string)
	}
	if t, ok := reg.Entry.Get("timeout"); ok {
		if n, ok := t.(json.Number); ok {
			if secs, err := n.Float64(); err == nil && secs > 0 {
				out.Timeout = time.Duration(secs * float64(time.Second))
			}
		}
	}
	return out
}

// checkGitBash is step 1; it returns the Git Bash it found, "" when there is none.
func checkGitBash(env Env, r *report) string {
	bash, err := FindGitBash(env)
	if err != nil {
		r.fail(fmt.Sprintf("Git Bash not found (%v): Claude Code then runs the hooks under PowerShell, and they block nothing — install Git for Windows, or set %s to its bash.exe, and start a new session", err, gitBashVar))
		return ""
	}
	if set := env.Getenv(gitBashVar); set != "" && set != bash {
		r.ok(fmt.Sprintf("Git Bash: %s (%s=%s is no bash or sh that exists, so Claude Code ignores it)", bash, gitBashVar, set))
		return bash
	}
	r.ok("Git Bash: " + bash)
	return bash
}

// FindGitBash finds the Git Bash Claude Code runs the hooks in on Windows. Only meaningful at
// GOOS "windows": the paths are Windows paths.
//
// The order, per the Claude Code docs read on 2026-11-22 (hooks, env-vars, setup):
//
//  1. CLAUDE_CODE_GIT_BASH_PATH, when the file exists and is named bash.exe, sh.exe, bash or sh.
//     Otherwise Claude Code (since v2.1.219) ignores the variable and detects as if it were
//     unset; so does this function, and the error then names the ignored path.
//  2. Git for Windows through git.exe on the PATH: the bin\bash.exe of its installation.
//  3. The standard paths of Git for Windows.
//
// The docs do not give the order of 2 and 3 or the exact paths; that part follows the spec and
// is not confirmed.
func FindGitBash(env Env) (string, error) {
	ignored := ""
	if p := env.Getenv(gitBashVar); p != "" {
		if shellName(p) && env.Exists(p) {
			return p, nil
		}
		ignored = p
	}
	if git, err := env.LookPath("git.exe"); err == nil && git != "" {
		// git.exe lives in <install>\cmd, <install>\bin or <install>\mingw64\bin.
		dir := winDir(git)
		for range 3 {
			if candidate := dir + `\bin\bash.exe`; env.Exists(candidate) {
				return candidate, nil
			}
			parent := winDir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	for _, p := range []string{`C:\Program Files\Git\bin\bash.exe`, `C:\Program Files (x86)\Git\bin\bash.exe`} {
		if env.Exists(p) {
			return p, nil
		}
	}
	if ignored != "" {
		return "", fmt.Errorf("%s=%s does not exist or is no bash or sh, and no other Git Bash was found", gitBashVar, ignored)
	}
	return "", errors.New("no Git Bash through git.exe on the PATH or in the standard paths")
}

// shellName says whether the file name of a Windows path is one Claude Code accepts.
func shellName(p string) bool {
	name := strings.ToLower(p[strings.LastIndexAny(p, `\/`)+1:])
	return slices.Contains([]string{"bash.exe", "sh.exe", "bash", "sh"}, name)
}

// winDir is the parent of a Windows path, on any host; a path without a separator is its own.
func winDir(p string) string {
	i := strings.LastIndexAny(p, `\/`)
	if i <= 0 {
		return p
	}
	return p[:i]
}

// checkCRLF is step 2: every file under the hook directory, not only *.sh. A `blocking` with
// CRLF matches no line in run.sh, and then every hook lets everything through.
func checkCRLF(root string, r *report) {
	dir := filepath.Join(root, filepath.FromSlash(packs.HookDir))
	var crlf []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte("\r\n")) {
			rel, _ := filepath.Rel(root, path)
			crlf = append(crlf, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		r.fail(fmt.Sprintf("%s cannot be read (%v) — %s", packs.HookDir, err, upgradeRemedy))
		return
	}
	for _, rel := range crlf {
		r.fail(fmt.Sprintf("%s has CRLF line endings, so the hook does not run or lets everything through — %s, which writes it with LF and sets .gitattributes", rel, upgradeRemedy))
	}
	if len(crlf) == 0 {
		r.ok("no CRLF line endings under " + packs.HookDir)
	}
}

// checkRegistered is the first half of step 2b: every hook unit of the manifest has its
// registration in .claude/settings.json.
func checkRegistered(root string, regs []registration, r *report) {
	m, err := manifest.Read(root)
	if err != nil {
		r.fail(fmt.Sprintf("%v — %s", err, upgradeRemedy))
		return
	}
	have := map[string]bool{}
	for _, reg := range regs {
		have[packs.HookID(reg.Event, reg.Matcher, reg.Command)] = true
	}
	missing := 0
	for _, u := range m.Units {
		if u.Kind != packs.KindHook || have[u.ID] {
			continue
		}
		missing++
		r.fail(fmt.Sprintf("hook not registered in %s: %s — %s", settingsPath, u.ID, upgradeRemedy))
	}
	if missing == 0 {
		r.ok("every hook in " + manifest.Path + " is registered in " + settingsPath)
	}
}

// checkDisabled is the second half of step 2b: disableAllHooks in the project, local or user
// settings turns every hook off. Managed settings are not read.
func checkDisabled(env Env, project *orderedjson.Object, r *report) {
	type source struct {
		path string
		doc  *orderedjson.Object
	}
	sources := []source{{path: filepath.Join(env.Root, filepath.FromSlash(settingsPath)), doc: project}}
	others := []string{filepath.Join(env.Root, filepath.FromSlash(localSettingsPath))}
	if env.Home != "" {
		others = append(others, filepath.Join(env.Home, ".claude", "settings.json"))
	}
	for _, p := range others {
		if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		doc, err := readJSON(p)
		if err != nil {
			r.fail(fmt.Sprintf("%v, so whether it sets disableAllHooks is unknown — fix the file", err))
			continue
		}
		sources = append(sources, source{path: p, doc: doc})
	}
	disabled := false
	for _, s := range sources {
		if v, ok := s.doc.Get("disableAllHooks"); ok && v == true {
			disabled = true
			r.fail(fmt.Sprintf("disableAllHooks is true in %s, so no hook runs — remove that line", s.path))
		}
	}
	if !disabled {
		r.ok("no settings file sets disableAllHooks")
	}
}
