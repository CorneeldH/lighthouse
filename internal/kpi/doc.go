// Package kpi measures how well the lighthouse-start-issue and lighthouse-fix-pr skills
// behave, from the Claude Code session transcripts on disk. It is a Go port of the two
// Python measurement scripts it replaced: the output is
// byte-identical to Python's json.dumps(table, indent=2, ensure_ascii=False).
//
// Deliberate deviations from the Python, none of which occurs in real transcripts:
//
//   - Invalid UTF-8 becomes one U+FFFD per strings.ToValidUTF8 run, not one per invalid
//     byte sequence.
//   - A JSON line that is not an object is dropped (Python crashes on it).
//   - A whole number written as 35.0 counts as an integer for totalToolUseCount.
//   - A timestamp yields a duration only when it is RFC 3339 or a naive
//     2006-01-02T15:04:05[.ffffff]; Python's fromisoformat accepts more forms.
//   - A non-string Bash command counts as "".
//   - Go's regexp \b is ASCII-only, so a non-ASCII letter or digit next to a match
//     differs from Python 3 (see the fix_pr rows in classify.json).
//   - An empty --dir falls back to the default directory (Python: the cwd).
//   - An unknown or abbreviated flag ends in exit code 1 through cobra (argparse: 2), and
//     --sin is not --since. An unknown metric prints its own text, not argparse's; the exit
//     code is the same, 2.
//   - Go rejects NaN and Infinity in a transcript line (Python's json accepts them), and
//     turns a lone surrogate escape into U+FFFD.
//   - An empty git stderr prints Go's own error text (for example "exit status 128") where
//     Python prints nothing; a missing git binary is exit code 2 (Python crashes with 1).
//   - A JSON true is not accepted as an integer count (Python takes it as 1).
//   - The gate metric also needs the scout_skipped heading to load; Python needs only
//     gate_line.
//   - A truthy tool input that is not an object counts as {} (Python crashes on it).
package kpi
