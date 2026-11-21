package cli

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// write is one write command: it opens the forge and runs do. A write error is a forge
// error (exit 1), except a title that fits two milestones, which is a usage error (exit 2).
func (e *forgeEnv) write(cmd *cobra.Command, do func(f forge.Forge) error) error {
	f, err := e.open(cmd.Context())
	if err != nil {
		return e.fail(cmd, err)
	}
	if err := do(f); err != nil {
		return e.fail(cmd, translateWriteError(err))
	}
	return nil
}

// translateWriteError gives the milestone errors of the forge their exit code: an
// *forge.AmbiguousMilestone is exit 2 (its message names every number), a
// *forge.GroupMilestone is exit 1 with the way out, `lighthouse forge api`.
func translateWriteError(err error) error {
	var amb *forge.AmbiguousMilestone
	if errors.As(err, &amb) {
		return &exitError{code: forgeExitUsage, err: err}
	}
	var grp *forge.GroupMilestone
	if errors.As(err, &grp) {
		return fmt.Errorf("milestone %d belongs to a group, which the project API cannot edit; use `lighthouse forge api`", grp.Number)
	}
	return err
}

// readBodyFile reads the file named by flag; the flag is required and the file must hold text.
// There is no stdin: a missing flag, a missing file and an empty file are exit 2.
func readBodyFile(cmd *cobra.Command, flag string) (string, error) {
	path, _ := cmd.Flags().GetString(flag)
	if path == "" {
		return "", forgeUsage(cmd, "--"+flag+" is required")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", forgeUsage(cmd, fmt.Sprintf("--%s: %v", flag, err))
	}
	if strings.TrimSpace(string(b)) == "" {
		return "", forgeUsage(cmd, fmt.Sprintf("--%s %s is empty", flag, path))
	}
	return string(b), nil
}

func requireTitle(cmd *cobra.Command, title string) error {
	if strings.TrimSpace(title) == "" {
		return forgeUsage(cmd, "--title is required")
	}
	return nil
}

// changeWord is what the forge calls a pull request: "MR" on GitLab, "PR" otherwise.
func changeWord(f forge.Forge) string {
	if f.Kind() == "gitlab" {
		return "MR"
	}
	return "PR"
}

func newForgeIssueWriteCommands(env *forgeEnv) []*cobra.Command {
	return []*cobra.Command{
		newForgeIssueCreateCommand(env), newForgeIssueEditCommand(env), newForgeIssueCommentCommand(env),
		newForgeIssueBlockCommand(env), newForgeIssueStatusCommand(env),
	}
}

func newForgeIssueCreateCommand(env *forgeEnv) *cobra.Command {
	var in forge.NewIssue
	cmd := &cobra.Command{
		Use: "create --title T --body-file F [--label L]... [--milestone TITLE]", Short: "Create an issue; prints its number and URL",
		Args: forgeArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireTitle(cmd, in.Title); err != nil {
				return err
			}
			body, err := readBodyFile(cmd, "body-file")
			if err != nil {
				return err
			}
			in.Body = body
			return env.write(cmd, func(f forge.Forge) error {
				is, err := f.CreateIssue(cmd.Context(), in)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintf(env.stdout(cmd), "%d\n%s\n", is.Number, is.URL)
				return err
			})
		},
	}
	cmd.Flags().StringVar(&in.Title, "title", "", "the title (required)")
	cmd.Flags().String("body-file", "", "file with the body (required)")
	cmd.Flags().StringArrayVar(&in.Labels, "label", nil, "a label; repeatable")
	cmd.Flags().StringVar(&in.Milestone, "milestone", "", "the title of the milestone")
	return cmd
}

func newForgeIssueEditCommand(env *forgeEnv) *cobra.Command {
	var title, milestone string
	var addLabels, removeLabels []string
	cmd := &cobra.Command{
		Use:   "edit <number> [--title T] [--body-file F] [--milestone TITLE] [--add-label L]... [--remove-label L]...",
		Short: "Edit an issue; an empty --milestone removes the milestone", Args: forgeArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := forgeNumber(cmd, args[0])
			if err != nil {
				return err
			}
			var e forge.IssueEdit
			flags := cmd.Flags()
			if flags.Changed("title") {
				if err := requireTitle(cmd, title); err != nil {
					return err
				}
				e.Title = &title
			}
			if flags.Changed("body-file") {
				body, err := readBodyFile(cmd, "body-file")
				if err != nil {
					return err
				}
				e.Body = &body
			}
			if flags.Changed("milestone") {
				e.Milestone = &milestone
			}
			e.AddLabels, e.RemoveLabels = addLabels, removeLabels
			if e.Title == nil && e.Body == nil && e.Milestone == nil && len(addLabels) == 0 && len(removeLabels) == 0 {
				return forgeUsage(cmd, "nothing to change: give --title, --body-file, --milestone, --add-label or --remove-label")
			}
			return env.write(cmd, func(f forge.Forge) error {
				_, err := f.EditIssue(cmd.Context(), n, e)
				return err
			})
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "the new title")
	cmd.Flags().String("body-file", "", "file with the new body")
	cmd.Flags().StringVar(&milestone, "milestone", "", "the title of the milestone; empty removes it")
	cmd.Flags().StringArrayVar(&addLabels, "add-label", nil, "a label to add; repeatable")
	cmd.Flags().StringArrayVar(&removeLabels, "remove-label", nil, "a label to remove; repeatable")
	return cmd
}

// newForgeCommentCommand is `comment <number> --body-file F`; post sends it (issue or change).
func newForgeCommentCommand(env *forgeEnv, short string, post func(f forge.Forge, cmd *cobra.Command, n int, body string) (string, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use: "comment <number> --body-file F", Short: short, Args: forgeArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := forgeNumber(cmd, args[0])
			if err != nil {
				return err
			}
			body, err := readBodyFile(cmd, "body-file")
			if err != nil {
				return err
			}
			return env.write(cmd, func(f forge.Forge) error {
				url, err := post(f, cmd, n, body)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(env.stdout(cmd), url)
				return err
			})
		},
	}
	cmd.Flags().String("body-file", "", "file with the comment (required)")
	return cmd
}

func newForgeIssueCommentCommand(env *forgeEnv) *cobra.Command {
	return newForgeCommentCommand(env, "Comment on an issue; prints the comment URL",
		func(f forge.Forge, cmd *cobra.Command, n int, body string) (string, error) {
			c, err := f.CreateComment(cmd.Context(), n, body)
			return c.URL, err
		})
}

func newForgeIssueBlockCommand(env *forgeEnv) *cobra.Command {
	var by int
	cmd := &cobra.Command{
		Use: "block <number> --by <blocker>", Short: "Make issue <blocker> block issue <number>", Args: forgeArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := forgeNumber(cmd, args[0])
			if err != nil {
				return err
			}
			if by < 1 {
				return forgeUsage(cmd, "--by <blocker number> is required")
			}
			return env.write(cmd, func(f forge.Forge) error { return f.AddBlocker(cmd.Context(), n, by) })
		},
	}
	cmd.Flags().IntVar(&by, "by", 0, "the number of the blocking issue (required)")
	return cmd
}

// statusNames are the valid statuses: the keys of [forge.board] status_options, sorted. nil
// when the repo has no board config or the seam is absent: the forge then reports itself.
func (e *forgeEnv) statusNames() []string {
	if e.repo == nil {
		return nil
	}
	r, err := e.repo()
	if err != nil || r.Board == nil {
		return nil
	}
	names := make([]string, 0, len(r.Board.StatusOptions))
	for name := range r.Board.StatusOptions {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func newForgeIssueStatusCommand(env *forgeEnv) *cobra.Command {
	return &cobra.Command{
		Use: "status <number> [<status>]", Short: "Read an issue's board status, or move it to one (adding it to the board first)", Args: forgeArgsRange(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := forgeNumber(cmd, args[0])
			if err != nil {
				return err
			}
			if len(args) == 1 {
				return env.readIssueStatus(cmd, n)
			}
			status := args[1]
			if names := env.statusNames(); names != nil && !slices.Contains(names, status) {
				return forgeUsage(cmd, fmt.Sprintf("unknown status %q; valid: %s", status, strings.Join(names, ", ")))
			}
			return env.write(cmd, func(f forge.Forge) error {
				item, ok, err := f.IssueItem(cmd.Context(), n)
				if err != nil {
					return err
				}
				if !ok {
					if item, err = f.AddToBoard(cmd.Context(), n); err != nil {
						return err
					}
				}
				return f.SetStatus(cmd.Context(), item, status)
			})
		},
	}
}

// readIssueStatus prints {"board", "on_board", "status"}. Without [forge.board] there is no
// board to read: it says so without opening the forge.
func (e *forgeEnv) readIssueStatus(cmd *cobra.Command, n int) error {
	noBoard := e.repo == nil
	if !noBoard {
		r, err := e.loadRepo(cmd)
		if err != nil {
			return err
		}
		noBoard = r.Board == nil
	}
	if noBoard {
		return e.printStatus(cmd, map[string]any{"board": false, "on_board": false, "status": nil})
	}
	return e.read(cmd, "", func(f forge.Forge) (any, error) {
		item, ok, err := f.IssueItem(cmd.Context(), n)
		if err != nil {
			return nil, err
		}
		var status any
		if item.Status != "" {
			status = item.Status
		}
		return map[string]any{"board": true, "on_board": ok, "status": status}, nil
	})
}

func (e *forgeEnv) printStatus(cmd *cobra.Command, v map[string]any) error {
	text, err := forgeResult{value: v}.render()
	if err != nil {
		return e.fail(cmd, err)
	}
	_, err = fmt.Fprint(e.stdout(cmd), text)
	return err
}

func newForgePRWriteCommands(env *forgeEnv) []*cobra.Command {
	return []*cobra.Command{newForgePRCreateCommand(env),
		newForgeCommentCommand(env, "Comment on a pull request (merge request); prints the comment URL",
			func(f forge.Forge, cmd *cobra.Command, n int, body string) (string, error) {
				c, err := f.CreateChangeComment(cmd.Context(), n, body)
				return c.URL, err
			})}
}

func newForgePRCreateCommand(env *forgeEnv) *cobra.Command {
	var in forge.NewPR
	cmd := &cobra.Command{
		Use: "create --title T --body-file F [--head BRANCH] [--base BRANCH] [--milestone TITLE]", Short: "Create a pull request (merge request); prints its number and URL",
		Args: forgeArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireTitle(cmd, in.Title); err != nil {
				return err
			}
			body, err := readBodyFile(cmd, "body-file")
			if err != nil {
				return err
			}
			in.Body = body
			if in.Head == "" {
				if in.Head, err = env.currentBranch(cmd); err != nil {
					return err
				}
			}
			if in.Base == "" {
				r, err := env.loadRepo(cmd)
				if err != nil {
					return err
				}
				in.Base = r.DefaultBranch
			}
			return env.write(cmd, func(f forge.Forge) error {
				c, createErr := f.CreateChange(cmd.Context(), in)
				if createErr != nil && c.Number == 0 {
					return createErr
				}
				// A change that exists is reported even when a later step failed.
				_, _ = fmt.Fprintf(env.stderr(cmd), "created %s #%d: %s -> %s\n", changeWord(f), c.Number, in.Head, in.Base)
				if _, err := fmt.Fprintf(env.stdout(cmd), "%d\n%s\n", c.Number, c.URL); err != nil {
					return err
				}
				return createErr
			})
		},
	}
	cmd.Flags().StringVar(&in.Title, "title", "", "the title (required)")
	cmd.Flags().String("body-file", "", "file with the body (required)")
	cmd.Flags().StringVar(&in.Head, "head", "", "the source branch (default: the current branch)")
	cmd.Flags().StringVar(&in.Base, "base", "", "the target branch (default: the forge's default branch)")
	cmd.Flags().StringVar(&in.Milestone, "milestone", "", "the title of the milestone")
	return cmd
}

// currentBranch is the branch to open a change from. A detached HEAD has none: exit 2.
func (e *forgeEnv) currentBranch(cmd *cobra.Command) (string, error) {
	if e.branch == nil {
		return "", forgeUsage(cmd, "no --head given and the current branch is unknown")
	}
	b, err := e.branch()
	if err != nil || b == "" || b == "HEAD" {
		return "", forgeUsage(cmd, "no --head given and there is no current branch (detached HEAD?)")
	}
	return b, nil
}

// loadRepo reads lighthouse.toml through the env seam; a failure is a missing configuration, exit 2.
func (e *forgeEnv) loadRepo(cmd *cobra.Command) (repoconfig.Repo, error) {
	if e.repo == nil {
		return repoconfig.Repo{}, forgeUsage(cmd, "no --base given and lighthouse.toml is not available")
	}
	r, err := e.repo()
	if err != nil {
		return repoconfig.Repo{}, e.fail(cmd, &exitError{code: forgeExitUsage, err: err})
	}
	return r, nil
}

func newForgeMilestoneWriteCommands(env *forgeEnv) []*cobra.Command {
	var createTitle string
	create := &cobra.Command{
		Use: "create --title T [--description-file F]", Short: "Create a milestone; prints its number", Args: forgeArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireTitle(cmd, createTitle); err != nil {
				return err
			}
			desc := ""
			if cmd.Flags().Changed("description-file") {
				var err error
				if desc, err = readBodyFile(cmd, "description-file"); err != nil {
					return err
				}
			}
			return env.write(cmd, func(f forge.Forge) error {
				m, err := f.CreateMilestone(cmd.Context(), createTitle, desc)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintf(env.stdout(cmd), "%d\n", m.Number)
				return err
			})
		},
	}
	create.Flags().StringVar(&createTitle, "title", "", "the title (required)")
	create.Flags().String("description-file", "", "file with the description")

	var newTitle string
	edit := &cobra.Command{
		Use: "edit <title> [--title T] [--description-file F]", Short: "Edit the milestone with this title", Args: forgeArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var title, desc *string
			if cmd.Flags().Changed("title") {
				if err := requireTitle(cmd, newTitle); err != nil {
					return err
				}
				title = &newTitle
			}
			if cmd.Flags().Changed("description-file") {
				d, err := readBodyFile(cmd, "description-file")
				if err != nil {
					return err
				}
				desc = &d
			}
			if title == nil && desc == nil {
				return forgeUsage(cmd, "nothing to change: give --title or --description-file")
			}
			return env.write(cmd, func(f forge.Forge) error {
				m, err := milestoneByTitle(cmd, f, args[0])
				if err != nil {
					return err
				}
				_, err = f.EditMilestone(cmd.Context(), m.Number, title, desc)
				return err
			})
		},
	}
	edit.Flags().StringVar(&newTitle, "title", "", "the new title")
	edit.Flags().String("description-file", "", "file with the new description")
	return []*cobra.Command{create, edit}
}

var labelColorRE = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)

// newForgeLabelCreateCommand is `label create <name> --color <hex>`: the label is created as
// written, the colour is six hex digits (a leading "#" is dropped; the forge adds what it needs).
// A label that exists already is no error. It is also the command `init --labels` prints.
func newForgeLabelCreateCommand(env *forgeEnv) *cobra.Command {
	var color string
	cmd := &cobra.Command{
		Use: "create <name> --color <hex>", Short: "Create a label; a label that exists already is no error",
		Args: forgeArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hex := strings.TrimPrefix(color, "#")
			if !labelColorRE.MatchString(hex) {
				return forgeUsage(cmd, "--color must be six hex digits, got "+strconv.Quote(color))
			}
			return env.write(cmd, func(f forge.Forge) error {
				if err := f.CreateLabel(cmd.Context(), args[0], hex); err != nil {
					return err
				}
				_, err := fmt.Fprintf(env.stdout(cmd), "created %s\n", args[0])
				return err
			})
		},
	}
	cmd.Flags().StringVar(&color, "color", "", "the colour, six hex digits (required)")
	return cmd
}
