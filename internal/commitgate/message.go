package commitgate

import "strings"

// HookName opens every line the gate writes.
const HookName = "commit-test-gate"

// Result is the outcome of the gate; it maps one to one onto hook.Result.
type Result struct {
	Code   int
	Output map[string]any
	Stderr string
}

// shellQuote single-quotes s for bash, so a pasted line runs it unchanged. Like the `$(…)` of
// shell_quote it drops trailing newlines.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(strings.TrimRight(s, "\n"), "'", `'\''`) + "'"
}

// Note adds one line to the notes of the gate (add_message).
func (g *Gate) Note(line string) { g.msgs = append(g.msgs, line) }

// pending are the notes so far, before a block ends the gate on stderr: a block emits no JSON,
// so without this they would be lost.
func (g *Gate) pending(b *strings.Builder) {
	if len(g.msgs) > 0 {
		b.WriteString(strings.Join(g.msgs, "\n") + "\n")
	}
}

// Emit ends the gate with a pass, with the notes as JSON when there are any. event is the hook
// event the JSON answers. additionalContext reaches the model, systemMessage the user: a silent
// gate must not look like a satisfied gate.
func (g *Gate) Emit(event string) Result {
	if len(g.msgs) == 0 {
		return Result{}
	}
	m := strings.Join(g.msgs, "\n")
	return Result{Output: map[string]any{
		"systemMessage":      m,
		"hookSpecificOutput": map[string]any{"hookEventName": event, "additionalContext": m},
	}}
}

func blockResult(b *strings.Builder) Result { return Result{Code: 2, Stderr: b.String()} }

// Block ends the gate with COULD NOT CHECK: reason, after the notes so far.
func (g *Gate) Block(reason string) Result {
	var b strings.Builder
	g.pending(&b)
	b.WriteString(HookName + ": COULD NOT CHECK — " + reason + "\n\n")
	if g.Mode == "post" {
		b.WriteString("'Could not test' is not a green light.\n")
		g.postUntested(&b)
	} else {
		b.WriteString("This command does contain a 'git commit'. 'Could not test' is not a\n")
		b.WriteString("green light, so this does not pass silently.\n")
	}
	return blockResult(&b)
}

// slotFailed blocks on a slot that could not be taken.
func (g *Gate) slotFailed(reason string) Result {
	var b strings.Builder
	g.pending(&b)
	b.WriteString(HookName + ": COULD NOT CHECK — cannot take a gate slot: " + reason + "\n\n")
	b.WriteString("Without a slot this run would go unchecked beside the other gates.\n")
	b.WriteString("Not being able to test is not a green light.\n")
	g.postUntested(&b)
	return blockResult(&b)
}

// postUntested, in post only and under a COULD NOT CHECK: the commit is already there, and
// repeating it gives "nothing to commit", so HEAD does not move and post stays silent, which
// reads as green. So it names every commit not tested yet and the line that tests it by hand.
func (g *Gate) postUntested(b *strings.Builder) {
	if g.Mode != "post" {
		return
	}
	b.WriteString("\n")
	if len(g.rows) == 0 {
		b.WriteString("A commit of this call may be untested. Do not repeat the commit to retry:\n")
		b.WriteString("that gives 'nothing to commit' and tests nothing. Check 'git log', then run\n")
		b.WriteString("[test_gate] command by hand on the files of that commit.\n")
		return
	}
	command := "'<[test_gate] command>'"
	if g.command != "" {
		command = shellQuote(g.command)
	}
	b.WriteString("The commit is made but not tested. Do not repeat the commit to retry: that\n")
	b.WriteString("gives 'nothing to commit' and tests nothing. Test it by hand instead:\n")
	n := 0
	for _, r := range g.rows {
		if r.Root == "" {
			continue
		}
		n++
		if n <= g.done {
			continue
		}
		b.WriteString("\n")
		b.WriteString("  Untested: " + r.Base + ".." + r.Head + " in " + r.Root + "\n")
		b.WriteString("  cd " + shellQuote(r.Root) + ` && f=$(mktemp) && t=$(mktemp -d) && git -c core.quotePath=false diff --name-only --no-renames ` +
			r.Base + " " + r.Head + ` > "$f" && LIGHTHOUSE_GATE_BASE=` + r.Base + " LIGHTHOUSE_GATE_HEAD=" + r.Head +
			` LIGHTHOUSE_GATE_FILES="$f" LIGHTHOUSE_GATE_TMP="$t" bash -c ` + command + "\n")
	}
}
