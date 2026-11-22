// Command loop_roadmap checks that the roadmap layer of the loop map matches the issues labelled `loop`.
//
// The label marks an issue that adds or changes a step, a handoff or a route of the loop (see
// docs/developer/labels.md); explaining the controller is not one. Two modes:
//
// `go run ./ci/loop_roadmap` (main mode) checks one direction: every planned step or handoff in
// docs/site/data/loop.json names an open issue with the label. The other direction, an open issue
// with the label that belongs on the map as a planned item, is carried by the scheduled job
// `map-planned` (it adds the item, or asks a question); this command no longer flags it.
//
// `go run ./ci/loop_roadmap --pr <n>` (PR mode) checks that a PR closing a `loop` issue also
// updates the map: no closed issue is still planned on it, and the map file is touched. A PR that
// touches the map or a Go file of this command also gets the main mode check, so a planned item
// naming a wrong or closed issue, or a broken command, is red in the PR and not only later on
// `main` (the anti-goal of the spec). That takes precedence over "a PR that closes no loop issue
// is green without further check", which is about the closing-issue check only.
//
// Both read GitHub through `gh`, the repository from GITHUB_REPOSITORY (default
// CorneeldH/lighthouse), and the map from the working directory, which is the repository root.
// Exit 0 is ok, 1 is a verdict, 2 is COULD NOT CHECK (gh failed or the map does not parse) or a
// usage error. `go run` turns any nonzero exit into 1 and prints `exit status N` on stderr.
//
// `--help` prints the usage and exits 0.
//
// Differences from the Python original: a reply without closingIssuesReferences is exit 2 with a
// message; the script that counts as touched is any `ci/loop_roadmap/*.go`.
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
	os.Exit(run(os.Args[1:], os.Getenv, ghCLI{}, ".", os.Stdout, os.Stderr))
}

const (
	usageText = "usage: loop_roadmap [--pr N]"
	scriptDir = "ci/loop_roadmap/"
)

func say(w io.Writer, a ...any) { _, _ = fmt.Fprintln(w, a...) }

func scriptTouched(touched map[string]bool) bool {
	for p := range touched {
		if strings.HasPrefix(p, scriptDir) && strings.HasSuffix(p, ".go") && !strings.Contains(p[len(scriptDir):], "/") {
			return true
		}
	}
	return false
}

func run(args []string, env func(string) string, gh ghClient, root string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("loop_roadmap", flag.ContinueOnError)
	fs.SetOutput(stderr)
	prFlag := fs.Int("pr", 0, "check this PR instead of the planned items")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		say(stderr, usageText)
		return 2
	}
	prMode := false
	fs.Visit(func(f *flag.Flag) { prMode = prMode || f.Name == "pr" })
	repo := env("GITHUB_REPOSITORY")
	if repo == "" {
		repo = "CorneeldH/lighthouse"
	}
	found, ok, err := check(prMode, *prFlag, repo, root, gh)
	if err != nil {
		say(stderr, "COULD NOT CHECK:", err)
		return 2
	}
	for _, line := range found {
		say(stdout, line)
	}
	if len(found) == 0 {
		say(stdout, ok)
		return 0
	}
	return 1
}

func check(prMode bool, pr int, repo, root string, gh ghClient) (found []string, ok string, err error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(mapRel)))
	if err != nil {
		return nil, "", err
	}
	chain, err := parseChain(data)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", mapRel, err)
	}
	if !prMode {
		issues, err := gh.OpenLoopIssues(repo)
		if err != nil {
			return nil, "", err
		}
		return Problems(chain, issues), fmt.Sprintf("loop roadmap: %d open %s issues, planned items all open", len(issues), label), nil
	}
	closed, err := gh.ClosedLoopIssues(repo, pr)
	if err != nil {
		return nil, "", err
	}
	touched, err := gh.TouchedFiles(repo, pr)
	if err != nil {
		return nil, "", err
	}
	if len(closed) > 0 {
		found = PRProblems(chain, closed, touched)
	}
	if touched[mapRel] || scriptTouched(touched) {
		// The issues this PR closes are still open while it runs and are no longer on the map.
		open, err := gh.OpenLoopIssues(repo)
		if err != nil {
			return nil, "", err
		}
		for nr := range closed {
			delete(open, nr)
		}
		found = append(found, Problems(chain, open)...)
	}
	ok = fmt.Sprintf("loop roadmap: PR #%d closes %d loop issues, map in step", pr, len(closed))
	if len(closed) == 0 {
		ok = fmt.Sprintf("loop roadmap: PR #%d closes no loop issue", pr)
	}
	return found, ok, nil
}
