package initcmd

import (
	"fmt"
	"io"
	"regexp"
	"strings"
)

// gateRewrites are the only texts init --upgrade rewrites in [test_gate] command.
var gateRewrites = []struct{ from, to string }{
	{"python3 .lighthouse/scripts/gate_go.py", "lighthouse gate go"},
	{"python3 .lighthouse/scripts/gate_node.py", "lighthouse gate node"},
}

// gateRef matches a path into the installed scripts or lib directory.
var gateRef = regexp.MustCompile(`\.lighthouse/(?:scripts|lib)/[A-Za-z0-9_./-]*[A-Za-z0-9_/-]`)

const staleLib = "lighthouse_lib"

// migrateGateCommand rewrites the value of [test_gate] command in old and leaves the rest
// of the file as it is. before and after are the old and new value (empty when nothing
// changed). stale lists what the value, after the rewrite, still references that
// installed says lighthouse does not install: a path, or lighthouse_lib.
func migrateGateCommand(old []byte, installed func(rel string) bool) (out []byte, before, after string, stale []string) {
	lines := strings.SplitAfter(string(old), "\n")
	inGate := false
	var values []string
	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "[") {
			name, _, _ := strings.Cut(t, "#")
			inGate = strings.TrimSpace(name) == "[test_gate]"
			continue
		}
		if !inGate {
			continue
		}
		key, rest, ok := strings.Cut(lines[i], "=")
		if !ok || strings.TrimSpace(key) != "command" {
			continue
		}
		end := valueEnd(lines, i, rest)
		block := strings.Join(lines[i:end+1], "")
		head, raw, _ := strings.Cut(block, "=")
		rewritten := raw
		for _, r := range gateRewrites {
			rewritten = strings.ReplaceAll(rewritten, r.from, r.to)
		}
		if rewritten != raw {
			lines[i] = head + "=" + rewritten
			for j := i + 1; j <= end; j++ {
				lines[j] = ""
			}
			before, after = strings.TrimSpace(raw), strings.TrimSpace(rewritten)
		}
		values = append(values, rewritten)
		i = end
	}
	out = []byte(strings.Join(lines, ""))
	joined := strings.Join(values, "\n")
	for _, p := range gateRef.FindAllString(joined, -1) {
		if !installed(p) {
			stale = append(stale, p)
		}
	}
	for _, p := range gateRef.Split(joined, -1) {
		if strings.Contains(p, staleLib) {
			stale = append(stale, staleLib)
			break
		}
	}
	return out, before, after, stale
}

// reportGateCommand tells what migrateGateCommand did.
func reportGateCommand(w io.Writer, before, after string, stale []string) {
	if before != after {
		_, _ = fmt.Fprintf(w, "lighthouse.toml: [test_gate] command rewritten\n  old: %s\n  new: %s\n", before, after)
	}
	for _, s := range stale {
		if s == staleLib {
			_, _ = fmt.Fprintln(w, "warning: lighthouse.toml: [test_gate] command uses lighthouse_lib, which lighthouse is phasing out; move it to a lighthouse command")
			continue
		}
		_, _ = fmt.Fprintf(w, "warning: lighthouse.toml: [test_gate] command calls %s, which lighthouse no longer installs; the test gate breaks until you change it\n", s)
	}
}

// valueEnd is the index of the last line of the value that starts on lines[i], whose
// text after the equals sign is rest: later lines only for a multi-line string.
func valueEnd(lines []string, i int, rest string) int {
	q := strings.TrimSpace(rest)
	if !strings.HasPrefix(q, `"""`) && !strings.HasPrefix(q, `'''`) {
		return i
	}
	delim := q[:3]
	if strings.Contains(q[3:], delim) {
		return i
	}
	for j := i + 1; j < len(lines); j++ {
		if strings.Contains(lines[j], delim) {
			return j
		}
	}
	return len(lines) - 1
}
