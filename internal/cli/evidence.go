package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/evidence"
)

// The usage lines of evidence.py's argparse, under the names of these commands.
const (
	evidenceUsage      = "lighthouse evidence [-h] {check,run} ..."
	evidenceCheckUsage = "lighthouse evidence check [-h] cell"
	evidenceRunUsage   = "lighthouse evidence run [-h] --issue ISSUE --body-file BODY_FILE --cwd CWD [--before-pr]"
)

// usageError reports a wrong command line as argparse does: the usage line and the error on
// stderr, exit 2.
func usageError(cmd *cobra.Command, usage, msg string) error {
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "usage: %s\n%s: error: %s\n", usage, cmd.CommandPath(), msg)
	return &exitError{code: 2, err: errSilent}
}

// evidenceExit turns the exit code of the evidence package into the error of a command.
func evidenceExit(code int) error {
	if code == 0 {
		return nil
	}
	return &exitError{code: code, err: errSilent}
}

// newEvidenceCommand is `lighthouse evidence check|run`, the twin of evidence.py's command line.
func newEvidenceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evidence {check,run}",
		Short: "Check a Command cell against the Evidence allowlist, or run the Evidence rows of a PR body",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError(cmd, evidenceUsage, "the following arguments are required: cmd")
			}
			return usageError(cmd, evidenceUsage, fmt.Sprintf("argument cmd: invalid choice: '%s' (choose from check, run)", args[0]))
		},
		RunE: func(*cobra.Command, []string) error { return nil },
	}
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		usage := evidenceUsage
		switch c.Name() {
		case "check":
			usage = evidenceCheckUsage
		case "run":
			usage = evidenceRunUsage
		}
		return usageError(c, usage, err.Error())
	})
	cmd.AddCommand(newEvidenceCheckCommand(), newEvidenceRunCommand())
	return cmd
}

// newEvidenceCheckCommand is `lighthouse evidence check <cell>`: exit 0 without output when the
// cell is allowed, exit 1 with the reason on stdout when not, exit 3 when the configuration
// cannot be read.
func newEvidenceCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "check <cell>",
		Short: "Check one Command cell against the allowlist",
		Args: func(cmd *cobra.Command, args []string) error {
			switch {
			case len(args) == 0:
				return usageError(cmd, evidenceCheckUsage, "the following arguments are required: cell")
			case len(args) > 1:
				return usageError(cmd, evidenceCheckUsage, "unrecognized arguments: "+strings.Join(args[1:], " "))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return evidenceExit(evidence.CheckCell(args[0], cmd.OutOrStdout()))
		},
	}
}

// newEvidenceRunCommand is `lighthouse evidence run --issue N --body-file PATH --cwd DIR
// [--before-pr]`: the Evidence rows as a table on stdout, with the exit codes of evidence.Run.
func newEvidenceRunCommand() *cobra.Command {
	var issue, bodyFile, cwd string
	var beforePR bool
	cmd := &cobra.Command{
		Use:   "run --issue N --body-file PATH --cwd DIR [--before-pr]",
		Short: "Run the Evidence rows of a PR body",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError(cmd, evidenceRunUsage, "unrecognized arguments: "+strings.Join(args, " "))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			var missing []string
			for _, name := range []string{"issue", "body-file", "cwd"} {
				if !cmd.Flags().Changed(name) {
					missing = append(missing, "--"+name)
				}
			}
			if len(missing) > 0 {
				return usageError(cmd, evidenceRunUsage, "the following arguments are required: "+strings.Join(missing, ", "))
			}
			nr, ok := pythonInt(issue)
			if !ok {
				return usageError(cmd, evidenceRunUsage, fmt.Sprintf("argument --issue: invalid int value: '%s'", issue))
			}
			return evidenceExit(evidence.Run(nr, bodyFile, cwd, beforePR, cmd.OutOrStdout()))
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&issue, "issue", "", "the issue number of the Evidence block")
	flags.StringVar(&bodyFile, "body-file", "", "the PR body")
	flags.StringVar(&cwd, "cwd", "", "the directory the rows run in")
	flags.BoolVar(&beforePR, "before-pr", false, "do not run a row whose Result waits on the PR")
	return cmd
}

// pythonInt parses s as Python's int(s) does for an ASCII number: surrounding whitespace, a sign,
// decimal digits with single underscores between them (Atoi refuses a second sign). A leading
// zero stays decimal, where strconv's base 0 would read "010" as 8. A value beyond int is
// refused, where Python accepts it.
func pythonInt(s string) (int, bool) {
	s = strings.TrimSpace(s)
	digits := strings.TrimLeft(s, "+-")
	if digits == "" || digits[0] == '_' || digits[len(digits)-1] == '_' || strings.Contains(digits, "__") {
		return 0, false
	}
	n, err := strconv.Atoi(s[:len(s)-len(digits)] + strings.ReplaceAll(digits, "_", ""))
	return n, err == nil
}
