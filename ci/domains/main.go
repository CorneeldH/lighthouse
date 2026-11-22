// Command domains sorts changed paths into the domains web, pack-python and go, for the jobs of CI.
//
// CI runs `go run ./ci/domains classify --github-output "$GITHUB_OUTPUT"` with the NUL-separated
// paths of the change on stdin, and gets `web=`, `pack-python=` and `go=` as job outputs, in that
// order, on stdout and appended to the file of `--github-output`.
//
// Exit codes of the binary: 0 is ok, 2 is a usage error (no or unknown subcommand, a bad flag) or
// COULD NOT CHECK (stdin or the `--github-output` file cannot be read or written). `go run` turns
// any nonzero exit into 1 and prints `exit status N` on stderr.
//
// Differences from the Python original: an unwritable `--github-output` is exit 2 with a message,
// not a traceback; a path with bytes that are not UTF-8 is compared as raw bytes, which
// `surrogateescape` did too.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

const usageText = "usage: domains classify [--github-output FILE]"

func say(w io.Writer, a ...any) { _, _ = fmt.Fprintln(w, a...) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "classify" {
		say(stderr, usageText)
		return 2
	}
	fs := flag.NewFlagSet("classify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	githubOutput := fs.String("github-output", "", "append the lines to `FILE`")
	if err := fs.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		say(stderr, usageText)
		return 2
	}
	raw, err := io.ReadAll(stdin)
	if err != nil {
		say(stderr, "COULD NOT CHECK: cannot read stdin:", err)
		return 2
	}
	var paths []string
	for _, p := range bytes.Split(raw, []byte{0}) {
		if len(p) > 0 {
			paths = append(paths, string(p))
		}
	}
	touched := Classify(paths)
	var out bytes.Buffer
	for _, d := range domains {
		fmt.Fprintf(&out, "%s=%t\n", d, touched[d])
	}
	if *githubOutput != "" {
		f, err := os.OpenFile(*githubOutput, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			say(stderr, "COULD NOT CHECK: cannot write --github-output:", err)
			return 2
		}
		_, werr := f.Write(out.Bytes())
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			say(stderr, "COULD NOT CHECK: cannot write --github-output:", werr)
			return 2
		}
	}
	_, _ = stdout.Write(out.Bytes())
	return 0
}
