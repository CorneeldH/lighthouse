package commitgate

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/bashwrites"
	"github.com/CorneeldH/lighthouse/internal/manifest"
)

// importsDeadline bounds one `lighthouse imports check` run; past it the step could not check.
const importsDeadline = 120 * time.Second

// findingLine is a finding as `lighthouse imports check` prints it:
// `<file>:<line>: <ID> <import> — <reason>`.
var findingLine = regexp.MustCompile(`^.+:\d+: \S+ .+ — .+$`)

// runImportsDefault runs the check as a subprocess of this very binary, in root: in a dev
// checkout run.sh runs the gate with the binary built from HEAD, while PATH may hold an older
// one without `imports`.
func (g *Gate) runImportsDefault(root string, files []string, limit time.Duration) (stdout, stderr string, code int, err error) {
	executable := g.Executable
	if executable == nil {
		executable = os.Executable
	}
	exe, err := executable()
	if err != nil {
		return "", "", 0, err
	}
	argv := append([]string{exe, "imports", "check"}, flagged(files)...)
	if g.Exec != nil {
		return g.Exec(root, g.Env, argv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = root, g.Env
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	cmd.WaitDelay = time.Second
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return "", "", 0, errors.New("the check ran past its deadline of " + limit.String())
	}
	var exit *exec.ExitError
	if errors.As(runErr, &exit) {
		return out.String(), errOut.String(), exit.ExitCode(), nil
	}
	return out.String(), errOut.String(), 0, runErr
}

// flagged is files as --files flags.
func flagged(files []string) []string {
	var out []string
	for _, f := range files {
		out = append(out, "--files", f)
	}
	return out
}

// importsFiles are the files the check looks at in one row: the staged files in a pre row, the
// files of the commit in a post row. Paths that no longer exist (deletions) are dropped: there
// is nothing to import in them.
func importsFiles(row Row) ([]string, error) {
	var files []string
	if row.Base != "" {
		changed, err := ChangedFiles(row.Root, row.Base, row.Head)
		if err != nil {
			return nil, err
		}
		files = changed
	} else {
		out, err := bashwrites.Git(row.Root, time.Now().Add(gitDeadline),
			"diff", "--cached", "--name-only", "--no-renames", "-z")
		if err != nil {
			return nil, err
		}
		for _, p := range strings.Split(string(out), "\x00") {
			if p != "" {
				files = append(files, p)
			}
		}
	}
	var present []string
	for _, f := range files {
		if _, err := os.Lstat(filepath.Join(row.Root, f)); err == nil {
			present = append(present, f)
		}
	}
	return present, nil
}

// runImports is the import step of the gate: for every row root with installed packs it runs
// `lighthouse imports check` on the files of the commit. It does not depend on
// the test command, and only [imports] check = false turns it off. stop is true when the gate
// ends with res. The clock of the gate starts at the first run: a run gets at most what is left of
// MaxDeadline since then, so the hook timeout never fires first (fail-open).
func (g *Gate) runImports(rows []Row) (stop bool, res Result) {
	enabled, checked := true, false
	for _, row := range rows {
		if row.Root == "" {
			continue
		}
		if _, err := manifest.Read(row.Root); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return true, g.Block("the pack manifest of " + row.Root + " cannot be read: " + err.Error())
		}
		if !checked {
			value, set, block := g.readConfig("imports.check")
			if block != nil {
				return true, *block
			}
			enabled, checked = !set || value != "false", true
		}
		if !enabled {
			return false, Result{}
		}
		g.startUnix()
		files, err := importsFiles(row)
		if err != nil {
			return true, g.Block("could not list the files to check for imports in " + row.Root + ": " + err.Error())
		}
		if len(files) == 0 {
			if row.Base == "" {
				// `git commit -a` and `git add -A && git commit` in one call stage nothing yet
				// when this hook runs: known limitation, no workaround.
				g.Note(HookName + ": imports not checked: nothing staged yet")
			}
			continue
		}
		run := g.ImportsCheck
		if run == nil {
			run = g.runImportsDefault
		}
		limit := min(importsDeadline, time.Unix(g.startUnix()+MaxDeadline, 0).Sub(g.now()))
		if limit <= 0 {
			return true, g.Block("the import check could not run in " + row.Root + ": the deadline of the gate is used up")
		}
		stdout, stderr, code, err := run(row.Root, files, limit)
		if res, stop := g.importsVerdict(row.Root, stdout, stderr, code, err); stop {
			return true, res
		}
	}
	return false, Result{}
}

// importsVerdict reads one run. Exit 0 is green; exit 1 with a finding line is red; everything
// else (exit 3, exit 1 without a finding, a binary without `imports`, a run that failed) is
// COULD NOT CHECK, never a pass.
func (g *Gate) importsVerdict(root, stdout, stderr string, code int, err error) (Result, bool) {
	if err != nil {
		return g.Block("the import check could not run in " + root + ": " + err.Error()), true
	}
	if code == 0 {
		return Result{}, false
	}
	var findings []string
	for _, line := range strings.Split(stdout, "\n") {
		if findingLine.MatchString(line) {
			findings = append(findings, line)
		}
	}
	if code != 1 || len(findings) == 0 {
		detail := captured([]byte(stderr))
		if detail == "" {
			detail = captured([]byte(stdout))
		}
		return g.Block("the import check could not check " + root + " (exit " + strconv.Itoa(code) + "): " + detail), true
	}
	var b strings.Builder
	g.pending(&b)
	if g.Mode == "post" {
		b.WriteString(HookName + ": imports not declared in a manifest, on the commit you just made. Declare them in a new commit.\n\n")
	} else {
		b.WriteString(HookName + ": imports not declared in a manifest. Declare them before committing.\n\n")
	}
	b.WriteString("Root: " + root + "\n")
	for _, f := range findings {
		b.WriteString(f + "\n")
	}
	b.WriteString("\n[imports] check = false in lighthouse.toml turns this check off.\n")
	return blockResult(&b), true
}
