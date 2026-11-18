package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	periodreport "github.com/CorneeldH/lighthouse/internal/report"
)

// reportEnv is what `lighthouse report` takes from its surroundings, so a test can replace it.
type reportEnv struct {
	Getenv   func(string) string
	Now      func() time.Time
	RepoRoot func() (string, error)                  // the repo without --repo
	GhToken  func() string                           // the token of `gh auth token`; "" when there is none
	ForgeURL string                                  // the API root of a GitHub repo; "" is forge.DefaultBaseURL
	Open     func(forge.Config) (forge.Forge, error) // nil: forge.Open
}

func defaultReportEnv() reportEnv {
	return reportEnv{Getenv: os.Getenv, Now: time.Now, RepoRoot: config.RepoRoot, GhToken: ghAuthToken}
}

// reportDisabled is the one line of a repo whose report is off.
const reportDisabled = "lighthouse report: the report is off for this repo; set [report] enabled = true in lighthouse.toml to turn it on"

// newReportCommand is `lighthouse report day|week|month [--date YYYY-MM-DD] [--repo <path>]`.
func newReportCommand(env reportEnv) *cobra.Command {
	var date, repoFlag string
	cmd := &cobra.Command{
		Use:   "report day|week|month [--date YYYY-MM-DD] [--repo <path>]",
		Short: "Print or write the report of one day, week or month of a repo",
		Long: "Print or write the report of one day, week or month: merged PRs, closed issues, open PRs\n" +
			"with their state, and issues new on the roadmap. --date picks the period that holds that day,\n" +
			"in local time; without it the current one. The repo is --repo, else the repo root.\n" +
			"[report] in its lighthouse.toml turns it on and picks the sink: stdout, or a file\n" +
			"<dir>/<period>/<label>.md that is overwritten and never committed.\n\n" +
			"Exit: 0 written, or the report is off | 1 a config or forge error | 2 wrong usage.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageErr(fmt.Errorf("%s: expected one period (day|week|month), got %d arguments", cmd.CommandPath(), len(args)))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, err := periodreport.ParseKind(args[0])
			if err != nil {
				return usageErr(err)
			}
			at := env.Now()
			if date != "" {
				if at, err = time.ParseInLocation("2006-01-02", date, at.Location()); err != nil {
					return usageErr(fmt.Errorf("argument --date: not a date YYYY-MM-DD: %q", date))
				}
			}
			root := repoFlag
			if root == "" {
				if root, err = env.RepoRoot(); err != nil {
					return &exitError{code: 1, err: err}
				}
			}
			if err := runReportCmd(cmd.Context(), env, root, periodreport.NewPeriod(kind, at), cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
				return &exitError{code: 1, err: err}
			}
			return nil
		},
	}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageErr(err) })
	cmd.Flags().StringVar(&date, "date", "", "a day YYYY-MM-DD in the period; default today")
	cmd.Flags().StringVar(&repoFlag, "repo", "", "the path of the repo; default the repo root")
	return cmd
}

// runReportCmd reports period of the repo at root, or says on errOut that the report is off.
// Nothing reaches the sink unless Build succeeds.
func runReportCmd(ctx context.Context, env reportEnv, root string, period periodreport.Period, out, errOut io.Writer) error {
	cfg, err := config.Load(filepath.Join(root, "lighthouse.toml"))
	if err != nil {
		return err
	}
	enabled, err := config.Lookup(cfg, "report.enabled")
	if err != nil {
		return err
	}
	if enabled != true {
		_, err := fmt.Fprintln(errOut, reportDisabled)
		return err
	}
	repo, err := repoconfig.Load(root)
	if err != nil {
		return err
	}
	if repo.ReportErr != nil {
		return repo.ReportErr
	}
	o := newForgeOpener(env.Getenv, env.ForgeURL)
	o.ghToken = env.GhToken
	if env.Open != nil {
		o.open = env.Open
	}
	rf, err := o.forRepo(ctx, repo)
	if err != nil {
		return err
	}
	r, err := periodreport.Build(ctx, rf.Forge, repo.Slug, period, env.Now())
	if err != nil {
		return err
	}
	md := periodreport.Render(r, repo.Language)
	if repo.Report.Sink != "file" {
		_, err := io.WriteString(out, md)
		return err
	}
	return writeReportFile(filepath.Join(root, repo.Report.Dir, string(period.Kind), period.Label+".md"), md)
}

// writeReportFile replaces path with md through a temporary file in the same directory, so a
// reader never sees half a report.
func writeReportFile(path, md string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".report-*.md")
	if err != nil {
		return err
	}
	_, werr := tmp.WriteString(md)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
