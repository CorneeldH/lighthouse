package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/orderedjson"
	"github.com/CorneeldH/lighthouse/internal/roborev"
)

// newRoborevGateCommand is `lighthouse roborev-gate`, the port of roborev_gate.py: the gate of
// the local roborev review that write-pr runs before `gh pr create`.
func newRoborevGateCommand(budget roborev.Budget) *cobra.Command {
	cmd := scriptCommand(&cobra.Command{
		Use:   "roborev-gate",
		Short: "Gate of the local roborev review before a PR is created",
		Long: "Exit: 0 ok | 2 wrong usage or a refused record (`roborev_gate: <error>`) | 3\n" +
			"`COULD NOT CHECK: <reason>`: no git repo or a config error.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usageErr(fmt.Errorf("%s: a subcommand is required: gate, fixed, body-line, classify or record", cmd.CommandPath()))
		},
	})
	cmd.AddCommand(
		roborevSub("gate", "Print the verdict as JSON: off, done, skip, classify or run",
			func(_ *cobra.Command, root string, _ []string) (string, error) {
				out, err := roborev.Gate(root, budget)
				return compactJSON(out, err)
			}),
		roborevSub("fixed", "Print the findings that fix commits of this branch name, as JSON",
			func(_ *cobra.Command, root string, _ []string) (string, error) {
				fixed, err := roborev.FixedFindings(root)
				if err != nil {
					return "", err
				}
				return compactJSON(roborev.FixedJSON(fixed), nil)
			}),
		roborevSub("body-line", "Print the line the PR body carries",
			func(_ *cobra.Command, root string, _ []string) (string, error) { return roborev.BodyLine(root) }),
		newRoborevClassify(),
		newRoborevRecord(),
	)
	return cmd
}

// roborevSub is a subcommand without arguments: run gets the repo root and returns the line to
// print. A failure to find the repo is `COULD NOT CHECK`, as for the script.
func roborevSub(name, short string, run func(*cobra.Command, string, []string) (string, error)) *cobra.Command {
	return scriptCommand(&cobra.Command{
		Use: name, Short: short,
		RunE: func(cmd *cobra.Command, args []string) error { return roborevRun(cmd, args, run) },
	})
}

func roborevRun(cmd *cobra.Command, args []string, run func(*cobra.Command, string, []string) (string, error)) error {
	root, err := config.RepoRoot()
	if err != nil {
		return roborevFail(cmd, &roborev.CheckError{Msg: err.Error()})
	}
	line, err := run(cmd, root, args)
	if err != nil {
		return roborevFail(cmd, err)
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), line)
	return err
}

// roborevFail prints the error as the script does: exit 3 for a check that could not run, 2 for
// a refusal.
func roborevFail(cmd *cobra.Command, err error) error {
	var check *roborev.CheckError
	if errors.As(err, &check) {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "COULD NOT CHECK: %s\n", check.Msg)
		return &exitError{code: 3, err: errSilent}
	}
	var value *roborev.ValueError
	if errors.As(err, &value) {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "roborev_gate: %s\n", value.Msg)
		return &exitError{code: 2, err: errSilent}
	}
	return err
}

func newRoborevClassify() *cobra.Command {
	cmd := scriptCommand(&cobra.Command{
		Use:   "classify [<path>]",
		Short: "Classify raw review output as a verdict; without a path, the raw file of this branch",
		RunE: func(cmd *cobra.Command, args []string) error {
			return roborevRun(cmd, args, func(_ *cobra.Command, root string, args []string) (string, error) {
				source := ""
				if len(args) == 1 {
					source = args[0]
				} else {
					_, raw, err := roborev.StatePaths(root)
					if err != nil {
						return "", err
					}
					source = raw
				}
				text, err := os.ReadFile(source)
				if err != nil {
					return "", &roborev.CheckError{Msg: fmt.Sprintf("%s: unreadable: %v", source, err)}
				}
				result := roborev.Classify(string(text))
				if result.Verdict == "skip" && len(args) == 0 {
					reason := result.Reason
					if _, err := roborev.Record(root, roborev.RecordOptions{Skip: &reason}); err != nil {
						return "", err
					}
				}
				return compactJSON(result.JSON(), nil)
			})
		},
	})
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			return usageErr(fmt.Errorf("%s: unrecognized arguments: %v", cmd.CommandPath(), args[1:]))
		}
		return nil
	}
	return cmd
}

func newRoborevRecord() *cobra.Command {
	var skip, done, decisions, out, raw, rng string
	cmd := scriptCommand(&cobra.Command{
		Use:   "record --skip <reason> | --done <sha> --decisions <file> [--out <file>] [--raw <file>] [--range <base>..<head>]",
		Short: "Write the state file: a skip with its reason, or done with one decision per finding",
		RunE: func(cmd *cobra.Command, args []string) error {
			return roborevRun(cmd, args, func(cmd *cobra.Command, root string, _ []string) (string, error) {
				flags := cmd.Flags()
				o := roborev.RecordOptions{Out: out, Raw: raw}
				if flags.Changed("skip") {
					o.Skip = &skip
				}
				if flags.Changed("done") {
					o.Sha = &done
				}
				if flags.Changed("range") {
					o.Range = &rng
				}
				if (o.Skip == nil) == (o.Sha == nil) {
					return "", &roborev.ValueError{Msg: "record needs --skip <reason> or --done <sha> --decisions <file>"}
				}
				if o.Sha != nil {
					if decisions == "" {
						return "", &roborev.ValueError{Msg: "--done needs --decisions <file>"}
					}
					list, err := readDecisions(decisions)
					if err != nil {
						return "", err
					}
					o.Decisions = list
				}
				return roborev.Record(root, o)
			})
		},
	})
	f := cmd.Flags()
	f.StringVar(&skip, "skip", "", "record a skip with this reason")
	f.StringVar(&done, "done", "", "record a done review with this sha")
	f.StringVar(&decisions, "decisions", "", "JSON list with one decision per finding")
	f.StringVar(&out, "out", "", "write here instead of the state file of the branch")
	f.StringVar(&raw, "raw", "", "raw review to check the decisions against")
	f.StringVar(&rng, "range", "", "the reviewed range <base>..<head> as gate printed it")
	return cmd
}

func readDecisions(path string) ([]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &roborev.ValueError{Msg: fmt.Sprintf("%s: unreadable: %v", path, err)}
	}
	parsed, err := orderedjson.Parse(data)
	if err != nil {
		return nil, &roborev.ValueError{Msg: fmt.Sprintf("%s: unreadable: %v", path, err)}
	}
	list, ok := parsed.([]any)
	if !ok {
		return nil, &roborev.ValueError{Msg: fmt.Sprintf("%s: not a JSON list", path)}
	}
	return list, nil
}

// compactJSON is the one-line JSON the script prints, with the key order of the object.
func compactJSON(v any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := writeCompact(&buf, v); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func writeCompact(w *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case *orderedjson.Object:
		w.WriteByte('{')
		for i, k := range t.Keys() {
			if i > 0 {
				w.WriteString(", ")
			}
			if err := writeCompact(w, k); err != nil {
				return err
			}
			w.WriteString(": ")
			e, _ := t.Get(k)
			if err := writeCompact(w, e); err != nil {
				return err
			}
		}
		w.WriteByte('}')
	case []any:
		w.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				w.WriteString(", ")
			}
			if err := writeCompact(w, e); err != nil {
				return err
			}
		}
		w.WriteByte(']')
	case json.Number:
		w.WriteString(t.String())
	default:
		var tmp bytes.Buffer
		enc := json.NewEncoder(&tmp)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err != nil {
			return err
		}
		w.Write(bytes.TrimSuffix(tmp.Bytes(), []byte("\n")))
	}
	return nil
}

// newRoborevCommentCommand is `lighthouse roborev-comment`, the port of roborev_comment.py: the
// PR comment of the roborev review, the same for write-pr (lokaal) and fix-pr (ci).
func newRoborevCommentCommand() *cobra.Command {
	cmd := scriptCommand(&cobra.Command{
		Use:   "roborev-comment",
		Short: "The PR comment of the roborev review",
		Long:  "Exit: 0 ok | 2 wrong usage, a state that is not `done`, or a comment that cannot fit (`roborev_comment: <error>`).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usageErr(fmt.Errorf("%s: a subcommand is required: render", cmd.CommandPath()))
		},
	})
	cmd.AddCommand(newRoborevCommentRender())
	return cmd
}

func newRoborevCommentRender() *cobra.Command {
	var pr int
	var source, state, raw string
	cmd := scriptCommand(&cobra.Command{
		Use:   "render --pr <nr> --bron lokaal|ci --state <path> --raw <path>",
		Short: "Print the comment: marker, log block, table and raw output",
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, name := range []string{"pr", "bron", "state", "raw"} {
				if !cmd.Flags().Changed(name) {
					return usageErr(fmt.Errorf("%s: the following arguments are required: --%s", cmd.CommandPath(), name))
				}
			}
			if source != "lokaal" && source != "ci" {
				return usageErr(fmt.Errorf("%s: --bron must be lokaal or ci, not %q", cmd.CommandPath(), source))
			}
			text, err := renderComment(pr, source, state, raw)
			if err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "roborev_comment: %v\n", err)
				return &exitError{code: 2, err: errSilent}
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), text)
			return err
		},
	})
	f := cmd.Flags()
	f.IntVar(&pr, "pr", 0, "number of the PR")
	f.StringVar(&source, "bron", "", "lokaal or ci")
	f.StringVar(&state, "state", "", "state file of the review")
	f.StringVar(&raw, "raw", "", "raw roborev output")
	return cmd
}

func renderComment(pr int, source, statePath, rawPath string) (string, error) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return "", err
	}
	parsed, err := orderedjson.Parse(data)
	if err != nil {
		return "", err
	}
	state, ok := parsed.(*orderedjson.Object)
	if !ok {
		return "", errors.New("state is not a done review: nothing to render")
	}
	if outcome, _ := state.Get("uitkomst"); outcome != "done" {
		return "", errors.New("state is not a done review: nothing to render")
	}
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		return "", err
	}
	return roborev.Render(state, string(raw), pr, source)
}
