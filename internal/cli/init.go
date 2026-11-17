package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/agentaudit"
	"github.com/CorneeldH/lighthouse/internal/initcmd"
)

// auditSummary is the audit init runs after it wrote; a test replaces it.
var auditSummary = agentaudit.Summary

// auditAfterInit shows the agent-audit summary of root. It never changes the exit code of
// init: whatever cannot be measured ends as an `agent-audit: COULD NOT CHECK` line on errOut.
func auditAfterInit(root string, out, errOut io.Writer) {
	o, err := agentAuditOptions()
	if err != nil {
		agentaudit.CouldNotCheck(errOut, err)
		return
	}
	env, err := agentaudit.DefaultEnv(root)
	if err != nil {
		agentaudit.CouldNotCheck(errOut, err)
		return
	}
	var msg bytes.Buffer
	code := auditSummary(env, o, out, &msg)
	_, _ = errOut.Write(msg.Bytes())
	if code != 0 && msg.Len() == 0 {
		agentaudit.CouldNotCheck(errOut, fmt.Errorf("the audit ended with exit code %d", code))
	}
}

func newInitCommand() *cobra.Command {
	var o initcmd.Options
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Install the lighthouse packages into this repository",
		Long: "Install the lighthouse packages into this repository. Shows a diff per file and\n" +
			"writes only after approval, or directly with --yes. --upgrade replaces what an\n" +
			"earlier init wrote; a locally changed unit is refused unless --force is given.\n\n" +
			"init installs core plus every language package that fits the repository. Add\n" +
			"or leave out a package with --pack and --no-pack, also later:\n" +
			"  lighthouse init --upgrade --pack python\n" +
			"  lighthouse init --upgrade --no-pack shell\n\n" +
			"--labels proposes a label taxonomy, [labels] and [paths] labels in the same diff,\n" +
			"under the same single approval. --create-labels, only with --labels, then creates\n" +
			"the missing labels on the GitHub remote and asks nothing: the flag is the approval,\n" +
			"also with nothing to apply (No changes.): it still creates the missing labels.\n\n" +
			"After the first init, and with --upgrade --audit, init shows a short agent-audit summary\n" +
			"(see `lighthouse agent-audit`). It changes nothing and never fails init.\n\n" +
			"A remote on a self-hosted GitLab is recognised with --forge-host <host>.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			o.Dir = dir
			o.In = cmd.InOrStdin()
			o.Out = cmd.OutOrStdout()
			o.Err = cmd.ErrOrStderr()
			o.Audit = auditAfterInit
			if initcmd.Run(o) != 0 {
				return errSilent
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&o.Upgrade, "upgrade", false, "replace what an earlier init installed")
	cmd.Flags().BoolVar(&o.AuditOnUpgrade, "audit", false, "with --upgrade: also run the agent audit; init runs it after the first init anyway")
	cmd.Flags().BoolVar(&o.Force, "force", false, "overwrite locally changed units")
	cmd.Flags().BoolVar(&o.Yes, "yes", false, "approve every change without asking")
	cmd.Flags().BoolVar(&o.Labels, "labels", false, "propose a label taxonomy, [labels] and [paths] labels")
	cmd.Flags().BoolVar(&o.CreateLabels, "create-labels", false, "with --labels: create the missing labels on the remote; this flag is the approval")
	cmd.Flags().StringArrayVar(&o.AddPacks, "pack", nil, "add a package (repeatable)")
	cmd.Flags().StringArrayVar(&o.DropPacks, "no-pack", nil, "leave a package out (repeatable)")
	cmd.Flags().StringArrayVar(&o.ForgeHosts, "forge-host", nil, "host of a self-hosted GitLab (repeatable)")
	return cmd
}

// errSilent makes the command fail without printing again; initcmd already reported.
var errSilent = errors.New("")
