package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/agentaudit"
	"github.com/CorneeldH/lighthouse/internal/config"
)

// newAgentAuditCommand is `lighthouse agent-audit`. The hook register it reads for
// Env.HookNames is filled by the blank import of internal/hooks in hook.go.
func newAgentAuditCommand() *cobra.Command {
	var repoOnly, strict bool
	var only []string
	cmd := scriptCommand(&cobra.Command{
		Use:   "agent-audit [--repo-only] [--only <part>]... [--strict]",
		Short: "Audit all agentic configuration: sources, conflicts, overlap, surface. Changes nothing.",
		Long: "Audit all agentic configuration of this repository, its user and its enabled plugins.\n" +
			"Changes nothing.\n\n" +
			"Sections, in this order: Bronnen (a status per source), Conflicten, Overlap, kandidaten,\n" +
			"Oppervlak (the always-loaded skills, commands and agents) and Inventaris. Every finding has a\n" +
			"stable key; `[agent_audit] ignore` in lighthouse.toml hides it. `[agent_audit] top` is the\n" +
			"number of largest posts, `description_max_chars` the description length reported as too long.\n\n" +
			"--repo-only skips the user config and the plugins; --only shows only inventory, conflicts,\n" +
			"overlap or surface (repeatable; Bronnen is always shown).\n\n" +
			"Exit:\n" +
			"  0  measured; without --strict also with findings or a source that could not be checked\n" +
			"  1  --strict and a shown conflict or overlap candidate that is not in ignore\n" +
			"  2  the audit as a whole could not measure (no repository root, a broken lighthouse.toml;\n" +
			"     `agent-audit: COULD NOT CHECK — <reason>` on stderr), --strict and a source or hook\n" +
			"     that could not be checked (managed settings excepted: they are never read), or wrong usage",
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, part := range only {
				if !slices.Contains(agentaudit.Parts, part) {
					return usageErr(fmt.Errorf("argument --only: invalid choice: '%s' (choose from %s)",
						part, strings.Join(agentaudit.Parts, ", ")))
				}
			}
			errOut := cmd.ErrOrStderr()
			root, err := config.RepoRoot()
			if err != nil {
				return &exitError{code: agentaudit.CouldNotCheck(errOut, err)}
			}
			o, err := agentAuditOptions()
			if err != nil {
				return &exitError{code: agentaudit.CouldNotCheck(errOut, err)}
			}
			env, err := agentaudit.DefaultEnv(root)
			if err != nil {
				return &exitError{code: agentaudit.CouldNotCheck(errOut, err)}
			}
			env.RepoOnly = repoOnly
			o.Only, o.Strict = only, strict
			if code := agentaudit.Run(env, o, cmd.OutOrStdout(), errOut); code != 0 {
				return &exitError{code: code}
			}
			return nil
		},
	})
	cmd.Flags().BoolVar(&repoOnly, "repo-only", false, "read only the repository: no user config, no plugins")
	cmd.Flags().StringArrayVar(&only, "only", nil, "show only this part: inventory, conflicts, overlap or surface (repeatable)")
	cmd.Flags().BoolVar(&strict, "strict", false, "exit 1 on a finding outside ignore, 2 on a source or hook that could not be checked")
	return cmd
}

// agentAuditOptions are the [agent_audit] keys of lighthouse.toml; a missing file gives the
// defaults, a broken one an error.
func agentAuditOptions() (agentaudit.Options, error) {
	var o agentaudit.Options
	path, err := config.Path()
	if err != nil {
		return o, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
			return o, err
		}
		if cfg, err = config.Parse(nil, path); err != nil {
			return o, err
		}
	}
	ignore, err := config.Lookup(cfg, "agent_audit.ignore")
	if err != nil {
		return o, err
	}
	list, ok := ignore.([]any)
	if !ok {
		return o, fmt.Errorf("agent_audit.ignore is %T, not a list", ignore)
	}
	for _, k := range list {
		key, ok := k.(string)
		if !ok {
			return o, fmt.Errorf("agent_audit.ignore holds %T, not a string", k)
		}
		o.Ignore = append(o.Ignore, key)
	}
	top, err := config.Lookup(cfg, "agent_audit.top")
	if err != nil {
		return o, err
	}
	maxDesc, err := config.Lookup(cfg, "agent_audit.description_max_chars")
	if err != nil {
		return o, err
	}
	t, okTop := top.(int64)
	m, okMax := maxDesc.(int64)
	if !okTop || !okMax {
		return o, fmt.Errorf("agent_audit.top and agent_audit.description_max_chars must be integers, not %T and %T", top, maxDesc)
	}
	o.Top, o.MaxDesc = int(t), int(m)
	return o, nil
}
