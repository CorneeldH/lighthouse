package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// read is one read command: it opens the forge, runs get and prints the result, filtered
// by --field. The result is rendered first and filtered after, so --field sees the JSON shape.
func (e *forgeEnv) read(cmd *cobra.Command, field string, get func(f forge.Forge) (any, error)) error {
	f, err := e.open(cmd.Context())
	if err != nil {
		return e.fail(cmd, err)
	}
	v, err := get(f)
	if err != nil {
		return e.fail(cmd, err)
	}
	text, err := forgeResult{value: v, field: field}.render()
	if err != nil {
		return e.fail(cmd, err)
	}
	_, err = fmt.Fprint(e.stdout(cmd), text)
	return err
}

func addFieldFlag(cmd *cobra.Command, field *string) {
	cmd.Flags().StringVar(field, "field", "", "print one field (dotted path) instead of the JSON")
}

// forgeNumber parses a number argument; a bad one is exit 2.
func forgeNumber(cmd *cobra.Command, s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, forgeUsage(cmd, fmt.Sprintf("invalid number %q", s))
	}
	return n, nil
}

func newForgeIssueCommand(env *forgeEnv) *cobra.Command {
	cmd := forgeGroup("issue", "Read and write issues")
	cmd.AddCommand(newForgeIssueViewCommand(env), newForgeIssueListCommand(env),
		newForgeIssueSearchCommand(env), newForgeIssueBlockersCommand(env))
	cmd.AddCommand(newForgeIssueWriteCommands(env)...)
	return cmd
}

func newForgeIssueViewCommand(env *forgeEnv) *cobra.Command {
	var field string
	var comments bool
	cmd := &cobra.Command{
		Use: "view <number> [--comments] [--field PATH]", Short: "Show an issue as JSON", Args: forgeArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := forgeNumber(cmd, args[0])
			if err != nil {
				return err
			}
			return env.read(cmd, field, func(f forge.Forge) (any, error) {
				issue, err := f.Issue(cmd.Context(), n)
				if err != nil {
					return nil, err
				}
				out := renderIssue(issue)
				if comments {
					cs, err := f.Comments(cmd.Context(), n)
					if err != nil {
						return nil, err
					}
					out["comments"] = renderComments(cs)
				}
				return out, nil
			})
		},
	}
	cmd.Flags().BoolVar(&comments, "comments", false, "add the comments")
	addFieldFlag(cmd, &field)
	return cmd
}

func newForgeIssueListCommand(env *forgeEnv) *cobra.Command {
	var field string
	var filter forge.IssueFilter
	cmd := &cobra.Command{
		Use: "list [--state S] [--label L] [--milestone TITLE] [--field PATH]", Short: "List issues as JSON", Args: forgeArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch filter.State {
			case "", "open", "closed", "all":
			default:
				return forgeUsage(cmd, fmt.Sprintf("--state %q is not open, closed or all", filter.State))
			}
			return env.read(cmd, field, func(f forge.Forge) (any, error) {
				issues, err := f.ListIssues(cmd.Context(), filter)
				if err != nil {
					return nil, err
				}
				out := make([]any, len(issues))
				for i, is := range issues {
					out[i] = renderIssue(is)
				}
				return out, nil
			})
		},
	}
	cmd.Flags().StringVar(&filter.State, "state", "", "open (default), closed or all")
	cmd.Flags().StringVar(&filter.Label, "label", "", "only issues with this label")
	cmd.Flags().StringVar(&filter.Milestone, "milestone", "", "only issues in the milestone with this title")
	addFieldFlag(cmd, &field)
	return cmd
}

func newForgeIssueSearchCommand(env *forgeEnv) *cobra.Command {
	var field string
	cmd := &cobra.Command{
		Use: "search <text> [--field PATH]", Short: "Search issues as JSON", Args: forgeArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return env.read(cmd, field, func(f forge.Forge) (any, error) {
				issues, err := f.SearchIssues(cmd.Context(), args[0])
				if err != nil {
					return nil, err
				}
				out := make([]any, len(issues))
				for i, is := range issues {
					out[i] = renderIssue(is)
				}
				return out, nil
			})
		},
	}
	addFieldFlag(cmd, &field)
	return cmd
}

func newForgeIssueBlockersCommand(env *forgeEnv) *cobra.Command {
	var field string
	cmd := &cobra.Command{
		Use: "blockers <number> [--field PATH]", Short: "List the issues that block an issue", Args: forgeArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := forgeNumber(cmd, args[0])
			if err != nil {
				return err
			}
			return env.read(cmd, field, func(f forge.Forge) (any, error) {
				bs, err := f.BlockedBy(cmd.Context(), n)
				if err != nil {
					return nil, err
				}
				out := make([]any, len(bs))
				for i, b := range bs {
					out[i] = renderBlocker(b)
				}
				return out, nil
			})
		},
	}
	addFieldFlag(cmd, &field)
	return cmd
}

func newForgePRCommand(env *forgeEnv) *cobra.Command {
	cmd := forgeGroup("pr", "Read and write pull requests (merge requests)")
	var field string
	view := &cobra.Command{
		Use: "view <number> [--field PATH]", Short: "Show a pull request as JSON", Args: forgeArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := forgeNumber(cmd, args[0])
			if err != nil {
				return err
			}
			return env.read(cmd, field, func(f forge.Forge) (any, error) {
				c, err := f.Change(cmd.Context(), n)
				if err != nil {
					return nil, err
				}
				return renderChange(c), nil
			})
		},
	}
	addFieldFlag(view, &field)
	cmd.AddCommand(view)
	cmd.AddCommand(newForgePRWriteCommands(env)...)
	return cmd
}

func newForgeMilestoneCommand(env *forgeEnv) *cobra.Command {
	cmd := forgeGroup("milestone", "Read and write milestones")
	var listField, listState, viewField string
	list := &cobra.Command{
		Use: "list [--state S] [--field PATH]", Short: "List milestones as JSON", Args: forgeArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch listState {
			case "", "open", "closed", "all":
			default:
				return forgeUsage(cmd, fmt.Sprintf("--state %q is not open, closed or all", listState))
			}
			return env.read(cmd, listField, func(f forge.Forge) (any, error) {
				ms, err := f.Milestones(cmd.Context(), listState)
				if err != nil {
					return nil, err
				}
				out := make([]any, len(ms))
				for i, m := range ms {
					out[i] = renderMilestone(m)
				}
				return out, nil
			})
		},
	}
	list.Flags().StringVar(&listState, "state", "", "open (default), closed or all")
	addFieldFlag(list, &listField)
	view := &cobra.Command{
		Use: "view <title> [--field PATH]", Short: "Show the milestone with this title as JSON", Args: forgeArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return env.read(cmd, viewField, func(f forge.Forge) (any, error) {
				m, err := milestoneByTitle(cmd, f, args[0])
				if err != nil {
					return nil, err
				}
				return renderMilestone(m), nil
			})
		},
	}
	addFieldFlag(view, &viewField)
	cmd.AddCommand(list, view)
	cmd.AddCommand(newForgeMilestoneWriteCommands(env)...)
	return cmd
}

// milestoneByTitle finds the one milestone, open or closed, with title. A title that fits two
// milestones is a usage error (exit 2) naming both; none is a forge error.
func milestoneByTitle(cmd *cobra.Command, f forge.Forge, title string) (forge.Milestone, error) {
	ms, err := f.Milestones(cmd.Context(), "all")
	if err != nil {
		return forge.Milestone{}, err
	}
	var hits []forge.Milestone
	for _, m := range ms {
		if m.Title == title {
			hits = append(hits, m)
		}
	}
	switch len(hits) {
	case 0:
		return forge.Milestone{}, fmt.Errorf("%w: %q", forge.ErrMilestoneNotFound, title)
	case 1:
		return hits[0], nil
	}
	nums := make([]string, len(hits))
	for i, m := range hits {
		nums[i] = strconv.Itoa(m.Number)
	}
	return forge.Milestone{}, &exitError{code: forgeExitUsage,
		err: errors.New("milestone title " + strconv.Quote(title) + " matches " + strings.Join(nums, " and ") + "; use the api command")}
}

func newForgeLabelCommand(env *forgeEnv) *cobra.Command {
	cmd := forgeGroup("label", "Read labels")
	var field string
	list := &cobra.Command{
		Use: "list [--field PATH]", Short: "List the label names as JSON", Args: forgeArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return env.read(cmd, field, func(f forge.Forge) (any, error) {
				names, err := f.Labels(cmd.Context())
				if err != nil {
					return nil, err
				}
				return renderLabels(names), nil
			})
		},
	}
	addFieldFlag(list, &field)
	cmd.AddCommand(list)
	return cmd
}
