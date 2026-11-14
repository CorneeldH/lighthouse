package bashwrites

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/deadline"
)

// The dispatch of post: a gate per claimed file, its verdict, and the bundle that post reports.
// Port of `run_gate`, `_printable`, `_Outcome`, `_report_verdict`, `_cannot_left`, `_vanished` and
// `_dispatch` in the former pack/core/lib/bash_writes.py.

const (
	prefix = "bash-write-check:"
	// noticeToken in the first output line of a gate with rc 0 is a notice for the user and the
	// agent; any other output of a gate with rc 0 is dropped.
	noticeToken = "NOTICE"
	// underGatesEnv is set for every gate; code-language-check reads it and then skips layer 2.
	underGatesEnv = "LIGHTHOUSE_UNDER_BASH_WRITE_CHECK"
	// gateGrace is the wait between SIGTERM and SIGKILL of a gate past the deadline.
	gateGrace = 10 * time.Second
	// verdictStopped is the verdict of a gate a signal to post stopped: post itself is stopping.
	// A gate that itself ends with 128 + n is no signal but an ordinary non-verdict exit.
	verdictStopped = "stopped"
	// verdictMissing is the verdict of a gate that is not installed, because its language pack is
	// missing: no block, but a notice per file, and the file counts as seen afterwards.
	verdictMissing = "missing"
)

// signAfterGate are the gates that rewrite the file themselves: the verdict of the file holds for
// the version after the last such gate that ran. The other gates only read: without a rewriting
// gate the verdict holds for the version from just before the first gate, so a write during a gate
// stays new.
var signAfterGate = map[string]bool{"ruff-format-check.sh": true, "gofmt-check.sh": true}

// runCapture is deadline.RunCapture; a test swaps it.
var runCapture = deadline.RunCapture

// runGateFunc is runGate; a test swaps it.
var runGateFunc = runGate

// runGate runs gate on rel with a mock Edit-like payload
// `{"tool_name":"Bash","cwd":root,"tool_input":{"file_path":<abs>}}`, in root as working
// directory (ruff-format-check.sh looks for its .ruff-version from there) and with
// LIGHTHOUSE_UNDER_BASH_WRITE_CHECK=1. The verdict is Classify's, "stopped" when a signal to this
// process stopped the gate, or "missing" with a notice when the gate is not under repoRoot. A gate
// that exists but cannot start (EACCES) is an error, as the exception in the Python: Post turns it
// into `internal error in the dispatcher (<error>)`, COULD NOT CHECK.
func runGate(repoRoot, root, rel, gate string, until time.Time) (string, []byte, error) {
	path := GatePath(repoRoot, gate)
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return verdictMissing, []byte("no " + GatePacks[gate] + " check installed"), nil
	}
	argv := []string{path}
	if extra, ok := GateArgv[gate]; ok {
		argv = append(argv, extra[1:]...)
	}
	payload := marshalJSON(map[string]any{
		"tool_name":  "Bash",
		"cwd":        root,
		"tool_input": map[string]any{"file_path": filepath.Join(root, rel)},
	})
	code, output, err := runCapture(until, argv, deadline.Options{
		Grace: gateGrace,
		Dir:   root,
		Stdin: payload,
		Env:   append(os.Environ(), underGatesEnv+"=1"),
	})
	var stopped *deadline.Interrupted
	if errors.As(err, &stopped) {
		return verdictStopped, stopped.Output, nil
	}
	if err != nil {
		return "", nil, err
	}
	return Classify(code, output), output, nil
}

// printable is text with every byte that is not valid UTF-8 written as \xNN, the way Python
// prints a name from os.fsdecode with backslashreplace. Valid UTF-8 stays as it is.
func printable(text string) string {
	if utf8.ValidString(text) {
		return text
	}
	var b strings.Builder
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&b, `\x%02x`, text[0])
		} else {
			b.WriteString(text[:size])
		}
		text = text[size:]
	}
	return b.String()
}

// Outcome is what post reports: blocking parts (exit 2) and notes (exit 0). At exit 2 the bundle
// goes to stderr AND as additionalContext to stdout; at exit 0 only the latter, and with nothing
// to report nothing at all. A part noted as a notice also goes to the user, as a top-level
// systemMessage, at exit 0 and at exit 2.
type Outcome struct {
	parts    []string
	notices  []string
	blocking bool
}

func (o *Outcome) block(text string) {
	o.parts = append(o.parts, text)
	o.blocking = true
}

func (o *Outcome) note(text string, notice bool) {
	o.parts = append(o.parts, text)
	if notice {
		o.notices = append(o.notices, text)
	}
}

func (o *Outcome) cannot(what string) { o.block(prefix + " " + cannotCheck + ": " + what) }

// Render is the exit code, the JSON object for stdout (nil when there is nothing to report) and
// the text for stderr (the bundle plus a newline, only when blocking).
func (o *Outcome) Render(event string) (int, map[string]any, string) {
	if len(o.parts) == 0 {
		return 0, nil, ""
	}
	text := printable(strings.Join(o.parts, "\n\n"))
	output := map[string]any{
		"hookSpecificOutput": map[string]any{"hookEventName": event, "additionalContext": text},
	}
	if len(o.notices) > 0 {
		output["systemMessage"] = printable(strings.Join(o.notices, "\n\n"))
	}
	if o.blocking {
		return 2, output, text + "\n"
	}
	return 0, output, ""
}

// reportVerdict puts one gate in the bundle: `== <label> (<gate label>) ==` plus its output. label
// is the repo-relative path, or the absolute path when post covers more than one root: two roots
// can have the same relative path.
func reportVerdict(o *Outcome, label, gate, verdict string, output []byte) {
	name := GateLabel(gate)
	text := strings.TrimRight(decodeReplace(output), "\n")
	switch verdict {
	case "ok":
		first, _, _ := strings.Cut(text, "\n")
		if strings.Contains(first, noticeToken) {
			o.note("== "+label+" ("+name+") ==\n"+text, true)
		}
		return
	case verdictMissing:
		o.note(prefix+" "+label+" not checked — "+string(output), false)
		return
	}
	lines := []string{"== " + label + " (" + name + ") =="}
	if verdict == "unchecked" {
		lines = append(lines, prefix+" "+cannotCheck+": "+label+" — "+name+" gave no verdict")
	}
	if text != "" {
		lines = append(lines, text)
	}
	o.block(strings.Join(lines, "\n"))
}

// cannotLeft is one COULD NOT CHECK with why and every remaining absolute path.
func cannotLeft(o *Outcome, why string, left []claimed) {
	lines := make([]string, 0, len(left))
	for _, c := range left {
		lines = append(lines, "  "+filepath.Join(c.view.root, c.rel))
	}
	o.cannot(why + ", not checked:\n" + strings.Join(lines, "\n"))
}

// vanished handles a claimed candidate that is no longer on disk: no gate, no verdict. If the
// path is in HEAD it is COULD NOT CHECK and stays in pending; otherwise it is deleted and drops out
// (also from pending). If HEAD cannot be read, it counts as "in HEAD": loud, never silent.
func vanished(state, key string, view rootView, rel string, o *Outcome, until time.Time) error {
	present, err := inHead(view.root, view.head, []string{rel}, until)
	isIn := err != nil || present[rel]
	err = withLedger(state, func(l *Ledger) error {
		if l.Unreadable {
			return nil
		}
		r := recordFor(l, view)
		r.dropClaim(rel, key)
		if !isIn {
			r.dropPending(rel)
		}
		l.Roots[view.root] = r.encode()
		return nil
	})
	if err != nil {
		return err
	}
	if isIn {
		o.cannot(filepath.Join(view.root, rel) + " (vanished before the gate; in HEAD, not on disk)")
	}
	return nil
}

// dispatch runs the gates sequentially, outside the lock, in sorted order, until the deadline.
// Sorted plus updating seen at once means every post makes progress, and the next post continues
// where this one stopped. A candidate that vanished between pool and gate does not go to the gate
// (see vanished). If a signal stops a gate, the dispatch stops: the rest is COULD NOT CHECK, as
// after the deadline.
func dispatch(files []claimed, state, key, repoRoot string, until time.Time, o *Outcome, absolute bool) error {
	for index, c := range files {
		// Per file, not per gate: a file whose first gate started gets all its gates (each is
		// bounded by the deadline itself), so every post judges at least one whole file.
		if !now().Before(until) {
			cannotLeft(o, "deadline passed", files[index:])
			return nil
		}
		path := filepath.Join(c.view.root, c.rel)
		before, ok := signatureOf(path)
		if !ok {
			if err := vanished(state, key, c.view, c.rel, o, until); err != nil {
				return err
			}
			continue
		}
		sig := &before
		combined := "ok"
		label := c.rel
		if absolute {
			label = path
		}
		for _, gate := range Gates[splitExt(c.rel)] {
			verdict, output, err := runGateFunc(repoRoot, c.view.root, c.rel, gate, until)
			if err != nil {
				return err
			}
			if verdict == verdictStopped {
				if err := recordVerdict(state, key, c.view, c.rel, "unchecked", nil); err != nil {
					return err
				}
				cannotLeft(o, "signal received", files[index:])
				return nil
			}
			if signAfterGate[gate] && verdict != verdictMissing {
				sig = nil
				if after, ok := signatureOf(path); ok {
					sig = &after
				}
			}
			if combined != "unchecked" {
				combined = verdict
			}
			reportVerdict(o, label, gate, verdict, output)
		}
		if err := recordVerdict(state, key, c.view, c.rel, combined, sig); err != nil {
			return err
		}
	}
	return nil
}
