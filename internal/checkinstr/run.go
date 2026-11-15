// Package checkinstr is `lighthouse check-instructions`: the generic deterministic checks over
// the instruction layer of a repo, 1, 2, 3, 4, 5, 7, 8, 10, 11 and 14. The numbers come from
// the source; 6, 9, 12 and 13 are project-specific and are not in here.
//
// Output contract, three outcomes and not two:
//
//	ok       the check measured and found nothing
//	FAIL     the check measured and found something -> exit 1
//	SKIPPED  the check could not measure here. Exit 0, but loud: the absence of a signal is
//	         not a negative result.
//
// If the command as a whole cannot measure (no repo root, broken lighthouse.toml), then
// `check-instructions: COULD NOT CHECK — <reason>` on stderr and exit 2.
package checkinstr

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/config"
)

const (
	ceilingKey = "project.instructions_word_ceiling"
	mirrorKey  = "paths.mirror_exceptions"
)

// CouldNotCheck prints why the command as a whole cannot measure and returns its exit code, 2.
func CouldNotCheck(errOut io.Writer, reason string) int {
	_, _ = fmt.Fprintf(errOut, "check-instructions: COULD NOT CHECK — %s\n", reason)
	return 2
}

// repo is what every check reads: the paths and the two config values.
type repo struct {
	root         string
	claudeSkills string
	agentsSkills string
	personal     string
	ceiling      int
	// mirrorExceptions is the absolute path of `[paths] mirror_exceptions`, "" when not set.
	mirrorExceptions string
}

// report prints the outcome lines and counts the findings.
type report struct {
	out      io.Writer
	findings int
}

func (r *report) ok(msg string) { _, _ = fmt.Fprintf(r.out, "ok    %s\n", msg) }

func (r *report) skip(msg string) { _, _ = fmt.Fprintf(r.out, "SKIPPED  %s\n", msg) }

func (r *report) fail(msg string) {
	_, _ = fmt.Fprintf(r.out, "FAIL  %s\n", msg)
	r.findings++
}

// line prints text as is: the detail under a FAIL.
func (r *report) line(text string) { _, _ = io.WriteString(r.out, text) }

// Run runs checks 1, 2, 3, 11, 4, 5, 7, 8, 14 and 10, in that order, on the repo at root.
// personal is the directory of the personal skills (check 5). Exit 1 on at least one FAIL,
// 2 when the repo cannot be measured at all.
func Run(root, personal string, out, errOut io.Writer) int {
	rp, code := open(root, personal, errOut)
	if code != 0 {
		return code
	}
	r := &report{out: out}
	checkDuplicateName(rp, r)
	checkByteIdentical(rp, r)
	checkMirror(rp, r)
	checkMirrorContent(rp, r, errOut)
	checkOverlap(rp, r)
	checkShadow(rp, r)
	checkScoping(rp, r)
	checkPairing(rp, r)
	checkHookNames(rp, r)
	checkSize(rp, r)
	if r.findings > 0 {
		_, _ = fmt.Fprintf(out, "\n%d finding(s).\n", r.findings)
		return 1
	}
	return 0
}

// PrintMirrorExceptions prints the current mirror differences in the form of
// `[paths] mirror_exceptions` (exit 0). When git could not compare a pair it prints what it
// had, then `SKIPPED  diff could not compare a skill pair` on stderr, and exits 4.
func PrintMirrorExceptions(root string, out, errOut io.Writer) int {
	rp, code := open(root, "", errOut)
	if code != 0 {
		return code
	}
	diffs, err := mirrorDifferences(rp, errOut)
	_, _ = io.WriteString(out, diffs)
	if err != nil {
		_, _ = io.WriteString(errOut, "SKIPPED  diff could not compare a skill pair\n")
		return 4
	}
	return 0
}

// open checks the root and reads both config values before any output: every outcome after a
// broken config would rest on a guess.
func open(root, personal string, errOut io.Writer) (*repo, int) {
	if !isDir(root) {
		return nil, CouldNotCheck(errOut, fmt.Sprintf("repository root '%s' is not a directory", root))
	}
	rp := &repo{
		root:         root,
		claudeSkills: root + "/.claude/skills",
		agentsSkills: root + "/.agents/skills",
		personal:     personal,
	}
	path := os.Getenv("LIGHTHOUSE_TOML")
	if path == "" {
		path = strings.TrimRight(root, "/") + "/lighthouse.toml"
	}
	c, err := config.Load(path)
	if err != nil {
		return nil, CouldNotCheck(errOut, "config: "+err.Error())
	}
	ceiling, _, err := configValue(c, ceilingKey)
	if err != nil {
		return nil, CouldNotCheck(errOut, "config: "+err.Error())
	}
	if rp.ceiling, err = strconv.Atoi(ceiling); err != nil {
		return nil, CouldNotCheck(errOut, fmt.Sprintf("config: %s is %q, not a whole number", ceilingKey, ceiling))
	}
	mirror, set, err := configValue(c, mirrorKey)
	if err != nil {
		return nil, CouldNotCheck(errOut, "config: "+err.Error())
	}
	if set {
		rp.mirrorExceptions = exceptionsPath(root, mirror)
	}
	return rp, 0
}

// exceptionsPath resolves the configured exceptions file against root. An absolute value
// (a leading slash, as in the script, or a native absolute path such as C:\…) stays as is; a
// relative one is joined with "/" so it appears in messages as the script printed it.
func exceptionsPath(root, value string) string {
	if strings.HasPrefix(value, "/") || filepath.IsAbs(value) {
		return value
	}
	return root + "/" + value
}

// configValue is the value of key as `lighthouse config <key>` prints it, its lines joined;
// set is false when the key is not set.
func configValue(c *config.Config, key string) (value string, set bool, err error) {
	v, err := config.Lookup(c, key)
	if errors.Is(err, config.ErrNotSet) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	lines, err := config.Lines(key, v)
	if err != nil {
		return "", false, err
	}
	return strings.Join(lines, "\n"), true, nil
}

// isFile is `test -f`: a regular file, a symlink followed.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// isDir is `test -d`: a directory, a symlink followed.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
