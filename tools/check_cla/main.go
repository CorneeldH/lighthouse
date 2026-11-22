// Command check_cla is the CLA gate: every identity in a pull request must be covered by the
// signature register.
//
// Why this exists: without a CLA, what the project receives from an outside contributor is only
// a licence under the repository's own terms, with no right to sublicense or relicense. From the
// first uncovered merge on, every licence change needs the consent of every contributor,
// permanently. One forgotten pull request cannot be undone; hence a gate, not an agreement.
//
// It runs as `go run ./tools/check_cla --base B --head H` on a fork pull request; the module has
// no dependency the job has to install beyond what `go run` fetches.
//
// The register comes from the working tree, not from the base branch. Otherwise signing in the
// same pull request cannot work by definition, and that is exactly the route that keeps the
// threshold low. The price is that the register itself has to be guarded: checkAppendOnly
// compares it with the copy on the base branch, so a removed or changed signature, a silently
// withdrawn grant, turns red.
//
// Exit codes: 0 every identity is covered, 1 a signature is missing, 2 not measurable (an empty
// range, an unreadable or invalid register, a register that was not appended to, git failed). A
// `go run` turns any nonzero exit into 1; the report in the job summary carries the difference.
//
// Differences from the Python script this replaces, all on input no valid register has: a date
// with a trailing newline or non-ASCII digits is refused (`regexp` `\d` is ASCII, `$` does not
// match before a final newline); a register that is not valid UTF-8 or not a JSON object is
// unreadable (exit 2) instead of a crash.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check_cla", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "repository root")
	base := fs.String("base", "origin/main", "base revision")
	head := fs.String("head", "HEAD", "head revision")
	register := fs.String("register", "", "signature register (default: <repo>/.github/cla-signatures.json)")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "check_cla: unexpected argument %s\n", pyRepr(fs.Arg(0)))
		return 2
	}
	registerPath := *register
	if registerPath == "" {
		registerPath = filepath.Join(*repo, ".github", "cla-signatures.json")
	}

	// An empty `--base` or `--head` makes the range `git log ..`, that is `HEAD..HEAD`, so
	// empty, so "zero identities, all covered" and exit 0. A green run that measured nothing is
	// exactly the outcome this gate must not be able to give.
	if *base == "" || *head == "" {
		emit(stdout, stderr, "## CLA: no range to measure\n\n"+
			fmt.Sprintf("`--base` is %s and `--head` is %s; without a range a green outcome says nothing.\n",
				pyRepr(*base), pyRepr(*head)))
		return 2
	}

	reg, err := loadRegister(registerPath)
	if err != nil {
		emit(stdout, stderr, fmt.Sprintf("## CLA: the register is unreadable\n\n```\n%v\n```\n", err))
		return 2
	}

	if errs := validateRegister(reg); len(errs) > 0 {
		emit(stdout, stderr, "## CLA: the register is invalid\n\n"+bullets(errs)+"\n")
		return 2
	}

	errs, err := checkAppendOnly(*repo, *base, registerPath, reg)
	if err == nil && len(errs) > 0 {
		emit(stdout, stderr, "## CLA: the register was changed, not appended to\n\n"+
			"A signature that disappears or changes is a silently withdrawn grant. "+
			"Only additions are allowed.\n\n"+bullets(errs)+"\n")
		return 2
	}
	var identities []Identity
	if err == nil {
		identities, err = collectIdentities(*repo, *base, *head)
	}
	if err != nil {
		emit(stdout, stderr, fmt.Sprintf("## CLA: git failed\n\n```\n%v\n```\n", err))
		return 2
	}

	if missing := unsigned(identities, reg); len(missing) > 0 {
		url, err := repoURL(*repo)
		if err != nil {
			emit(stdout, stderr, fmt.Sprintf("## CLA: lighthouse.toml is unreadable\n\n```\n%v\n```\n", err))
			return 2
		}
		emit(stdout, stderr, renderReport(missing, url))
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "CLA: %d identities in %s..%s, all covered.\n", len(identities), *base, *head)
	return 0
}

func bullets(items []string) string {
	return "- " + strings.Join(items, "\n- ")
}
