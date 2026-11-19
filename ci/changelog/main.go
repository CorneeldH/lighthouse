// Command changelog parses changelog fragments in `changelog.d/*.md` and checks that a PR adds
// one.
//
// A fragment is a markdown file with a one-line front matter (`type: <t>`) and the text of the
// entry. The release workflow merges the fragments into CHANGELOG.md; see docs/releasing.md.
//
// Three calls, all through `go run ./ci/changelog <command>`:
//
//   - `check --base B --head H` runs on every PR. It needs at least one fragment added in
//     `B...H` and refuses any fragment that does not parse.
//   - `release --event E [--bump auto|patch|minor|major] [--date YYYY-MM-DD]` decides the bump
//     from the open fragments, writes the section into CHANGELOG.md, removes the fragments and
//     prints the new tag (an empty line when there is nothing to release).
//   - `notes <tag>` prints the CHANGELOG.md section of a tag.
//
// Every call takes `--root` (default `.`). The positional tag of `notes` may stand before or
// after it.
//
// Exit codes of the binary: 0 is ok, 1 is a verdict (no new fragment, a broken fragment, a
// release that must not happen, no section for the tag), 2 is COULD NOT CHECK (git failed, no
// release tag) or a usage error. `go run` turns any nonzero exit into 1 and prints
// `exit status N` on stderr; no workflow tells 1 from 2.
//
// A value in a message is quoted with single quotes, as the Python script did; a value that
// holds a `'` is not escaped. The OS detail after `cannot read: ` is Go's, not Python's.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// say and sayf write a message. A failed write to stdout or stderr has nowhere to be reported.
func say(w io.Writer, a ...any) { _, _ = fmt.Fprintln(w, a...) }

func sayf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usageText = "usage: changelog <check|release|notes> [flags]"

// choice is a flag value limited to a fixed set.
type choice struct {
	name    string
	value   string
	allowed []string
}

func (c *choice) String() string { return c.value }

func (c *choice) Set(v string) error {
	if !contains(c.allowed, v) {
		return fmt.Errorf("invalid value '%s' for --%s (choose from %s)", v, c.name, strings.Join(c.allowed, ", "))
	}
	c.value = v
	return nil
}

// parseFlags parses args into fs. A positional argument is returned in order; `--root` may come
// after one. Errors and help text go to stderr.
func parseFlags(fs *flag.FlagSet, args []string) (positional []string, code int, done bool) {
	for {
		if err := fs.Parse(args); err != nil {
			if err == flag.ErrHelp {
				return nil, 0, true
			}
			return nil, 2, true
		}
		if fs.NArg() == 0 {
			return positional, 0, false
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		say(stderr, usageText)
		return 2
	}
	fs := flag.NewFlagSet("changelog "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root")
	usageError := func(msg string) int {
		sayf(stderr, "changelog %s: %s\n", args[0], msg)
		return 2
	}
	switch args[0] {
	case "check":
		base := fs.String("base", "", "base revision")
		head := fs.String("head", "", "head revision")
		pos, code, done := parseFlags(fs, args[1:])
		if done {
			return code
		}
		if len(pos) > 0 {
			return usageError("unexpected argument '" + pos[0] + "'")
		}
		if *base == "" || *head == "" {
			return usageError("--base and --head are required")
		}
		return runCheck(*root, *base, *head, stderr)
	case "release":
		event := &choice{name: "event", allowed: []string{"push", "schedule", "workflow_dispatch"}}
		bump := &choice{name: "bump", value: "auto", allowed: append([]string{"auto"}, bumps...)}
		fs.Var(event, "event", "push, schedule or workflow_dispatch")
		fs.Var(bump, "bump", "auto, patch, minor or major")
		date := fs.String("date", time.Now().Format("2006-01-02"), "release date")
		pos, code, done := parseFlags(fs, args[1:])
		if done {
			return code
		}
		if len(pos) > 0 {
			return usageError("unexpected argument '" + pos[0] + "'")
		}
		if event.value == "" {
			return usageError("--event is required")
		}
		return runRelease(*root, event.value, bump.value, *date, stdout, stderr)
	case "notes":
		pos, code, done := parseFlags(fs, args[1:])
		if done {
			return code
		}
		if len(pos) != 1 {
			return usageError("exactly one <tag> is required")
		}
		return runNotes(*root, pos[0], stdout, stderr)
	}
	sayf(stderr, "changelog: unknown command '%s'\n%s\n", args[0], usageText)
	return 2
}

func runCheck(root, base, head string, stderr io.Writer) int {
	added, err := addedFragments(root, base, head)
	if err != nil {
		sayf(stderr, "COULD NOT CHECK: %v\n", err)
		return 2
	}
	if _, err := readFragments(filepath.Join(root, fragmentDir)); err != nil {
		say(stderr, err)
		return 1
	}
	if len(added) == 0 {
		sayf(stderr, "no new fragment in %s/: add one (see docs/releasing.md), "+
			"type `internal` if the change needs no changelog entry\n", fragmentDir)
		return 1
	}
	return 0
}

func runRelease(root, event, bump, date string, stdout, stderr io.Writer) int {
	cmd := exec.Command("git", "-C", root, "tag", "--list", "v*")
	var gitErr strings.Builder
	cmd.Stderr = &gitErr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(gitErr.String())
		if detail == "" {
			detail = err.Error()
		}
		sayf(stderr, "COULD NOT CHECK: git tag failed: %s\n", detail)
		return 2
	}
	tag, err := release(root, event, bump, date, strings.Fields(string(out)))
	switch err.(type) {
	case nil:
		say(stdout, tag)
		return 0
	case *releaseError, *fragmentError:
		say(stderr, err)
		return 1
	}
	sayf(stderr, "COULD NOT CHECK: %v\n", err)
	return 2
}

func runNotes(root, tag string, stdout, stderr io.Writer) int {
	p := filepath.Join(root, "CHANGELOG.md")
	text, err := os.ReadFile(p)
	if err != nil {
		sayf(stderr, "%s: cannot read: %v\n", p, err)
		return 1
	}
	body, ok := extractSection(string(text), tag)
	if !ok {
		sayf(stderr, "no section for %s in %s\n", tag, p)
		return 1
	}
	say(stdout, body)
	return 0
}
