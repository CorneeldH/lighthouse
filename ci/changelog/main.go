// Command changelog parses changelog fragments in `changelog.d/*.md` and checks that a PR adds
// one.
//
// A fragment is a markdown file with a one-line front matter (`type: <t>`) and the text of the
// entry. The release workflow merges the fragments into CHANGELOG.md; see docs/releasing.md.
//
// Calls, all through `go run ./ci/changelog <command>`:
//
//   - `check --base B --head H` runs on every PR. It needs at least one fragment added in
//     `B...H` and refuses any fragment that does not parse.
//   - `release --version vX.Y.Z [--date YYYY-MM-DD]` writes the section into CHANGELOG.md, removes
//     the fragments and prints the tag. Nothing releasable, or a version that has a tag, is exit 1.
//   - `released` prints the fragment names an earlier `chore(release): vX.Y.Z` commit removed.
//   - `next --kind K [--branch B]` prints the next dev, rc, test, minor, major or patch tag.
//   - `sync --tag T --source DIR` copies the section of T into --root and removes its fragments.
//   - `reconcile --branch B --now T --pr-open BOOL --pr-closed-by-hand BOOL` prints the action for a
//     release branch: none, tag-rc, open-release-pr, close-release-pr, open-patch-pr, release-tip or recover.
//   - `split --kind minor|major` prints `split B RC`, `merge B RC` or `skip REASON`.
//   - `milestone-release --description T` prints yes when T has a line `Release: ja`, else no.
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
// The line splitting of CHANGELOG.md breaks only on `\r\n`, `\r` and `\n`; unlike Python's
// `str.splitlines` it does not break on `\v`, `\f`, `\x1c` to `\x1e`, `\x85`, U+2028 or U+2029.
//
// A value in a message is quoted with single quotes, as the Python script did. For a value with
// a `'` or a non-printable character (a BOM) this differs from Python's `repr`: Go neither
// switches to double quotes nor escapes. The OS detail after `cannot read: ` is Go's, not Python's.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// say and sayf write a message. A failed write to stdout or stderr has nowhere to be reported.
func say(w io.Writer, a ...any) { _, _ = fmt.Fprintln(w, a...) }

func sayf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usageText = "usage: changelog <check|release|released|next|sync|reconcile|split|milestone-release|notes> [flags]"

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
	name := "changelog"
	if len(args) > 0 {
		name += " " + args[0]
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root")
	// usageError prints the message, then the usage, as a flag parse error does; exit 2.
	usageError := func(msg string) int {
		sayf(stderr, "%s: %s\n", name, msg)
		say(stderr, usageText)
		fs.Usage()
		return 2
	}
	if len(args) == 0 {
		return usageError("a command is required")
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
		version := fs.String("version", "", "version to release, vX.Y.Z")
		date := fs.String("date", time.Now().Format("2006-01-02"), "release date")
		pos, code, done := parseFlags(fs, args[1:])
		if done {
			return code
		}
		if len(pos) > 0 {
			return usageError("unexpected argument '" + pos[0] + "'")
		}
		if *version == "" {
			return usageError("--version is required")
		}
		if _, ok := parseTag(*version); !ok {
			return usageError("invalid --version '" + *version + "' (want vX.Y.Z)")
		}
		return runRelease(*root, *version, *date, stdout, stderr)
	case "next":
		kind := fs.String("kind", "", "dev, rc, test, minor, major or patch")
		branch := fs.String("branch", "", "release branch, release/vX.Y (rc and patch)")
		pos, code, done := parseFlags(fs, args[1:])
		if done {
			return code
		}
		if len(pos) > 0 {
			return usageError("unexpected argument '" + pos[0] + "'")
		}
		if !contains(nextKind, *kind) {
			return usageError("--kind is required: " + strings.Join(nextKind, ", "))
		}
		tags, err := allTags(*root)
		if err == nil {
			var branches []string
			if branches, err = releaseBranches(*root); err == nil {
				var tag string
				tag, err = nextTag(*kind, *branch, tags, branches)
				if err == nil {
					say(stdout, tag)
					return 0
				}
				if _, ok := err.(*releaseError); ok {
					say(stderr, err)
					return 1
				}
				return usageError(err.Error())
			}
		}
		sayf(stderr, "COULD NOT CHECK: %v\n", err)
		return 2
	case "sync":
		tag := fs.String("tag", "", "release tag, vX.Y.Z")
		source := fs.String("source", "", "checkout of the tag")
		pos, code, done := parseFlags(fs, args[1:])
		if done {
			return code
		}
		if len(pos) > 0 {
			return usageError("unexpected argument '" + pos[0] + "'")
		}
		if _, ok := parseTag(*tag); !ok || *source == "" {
			return usageError("--tag vX.Y.Z and --source are required")
		}
		err := syncTag(*root, *source, *tag, stderr)
		switch err.(type) {
		case nil:
			return 0
		case *releaseError:
			say(stderr, err)
			return 1
		}
		sayf(stderr, "COULD NOT CHECK: %v\n", err)
		return 2
	case "reconcile":
		branch := fs.String("branch", "", "release branch, release/vX.Y")
		nowFlag := fs.String("now", "", "the time to judge by, RFC 3339 (default: now)")
		prOpen := fs.String("pr-open", "false", "true when a release PR is open")
		closedByHand := fs.String("pr-closed-by-hand", "false", "true when the last release PR was closed by hand")
		pos, code, done := parseFlags(fs, args[1:])
		if done {
			return code
		}
		if len(pos) > 0 {
			return usageError("unexpected argument '" + pos[0] + "'")
		}
		if branchRe.FindString(*branch) == "" {
			return usageError("--branch release/vX.Y is required")
		}
		now := time.Now()
		if *nowFlag != "" {
			var err error
			if now, err = time.Parse(time.RFC3339, *nowFlag); err != nil {
				return usageError("invalid --now '" + *nowFlag + "' (want RFC 3339)")
			}
		}
		open, errOpen := strconv.ParseBool(*prOpen)
		hand, errHand := strconv.ParseBool(*closedByHand)
		if errOpen != nil || errHand != nil {
			return usageError("--pr-open and --pr-closed-by-hand take true or false")
		}
		state, err := loadState(*root, *branch)
		if err != nil {
			sayf(stderr, "COULD NOT CHECK: %v\n", err)
			return 2
		}
		state.ReleasePROpen, state.ReleasePRClosedByHand = open, hand
		say(stdout, decide(state, now))
		return 0
	case "split":
		kind := fs.String("kind", "", "minor or major")
		pos, code, done := parseFlags(fs, args[1:])
		if done {
			return code
		}
		if len(pos) > 0 {
			return usageError("unexpected argument '" + pos[0] + "'")
		}
		if *kind != "minor" && *kind != "major" {
			return usageError("--kind minor or major is required")
		}
		verdict, err := decideSplit(*root, *kind)
		switch err.(type) {
		case nil:
			say(stdout, verdict)
			return 0
		case *releaseError, *fragmentError:
			say(stderr, err)
			return 1
		}
		sayf(stderr, "COULD NOT CHECK: %v\n", err)
		return 2
	case "milestone-release":
		description := fs.String("description", "", "the milestone description")
		pos, code, done := parseFlags(fs, args[1:])
		if done {
			return code
		}
		if len(pos) > 0 {
			return usageError("unexpected argument '" + pos[0] + "'")
		}
		if milestoneRelease(*description) {
			say(stdout, "yes")
		} else {
			say(stdout, "no")
		}
		return 0
	case "released":
		pos, code, done := parseFlags(fs, args[1:])
		if done {
			return code
		}
		if len(pos) > 0 {
			return usageError("unexpected argument '" + pos[0] + "'")
		}
		released, err := releasedFragments(*root)
		if err != nil {
			sayf(stderr, "COULD NOT CHECK: %v\n", err)
			return 2
		}
		names := make([]string, 0, len(released))
		for n := range released {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			say(stdout, n)
		}
		return 0
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
	return usageError("unknown command '" + args[0] + "'")
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

func runRelease(root, version, date string, stdout, stderr io.Writer) int {
	tags, err := releaseTags(root)
	if err != nil {
		sayf(stderr, "COULD NOT CHECK: %v\n", err)
		return 2
	}
	released, err := releasedFragments(root)
	if err != nil {
		sayf(stderr, "COULD NOT CHECK: %v\n", err)
		return 2
	}
	tag, err := release(root, version, date, tags, released, stderr)
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
