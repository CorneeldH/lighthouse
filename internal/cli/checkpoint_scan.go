package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/checkpointscan"
	"github.com/CorneeldH/lighthouse/internal/config"
)

const ckPrefix = checkpointscan.Prefix

// ckUsage is a usage error of checkpoint-scan: a COULD NOT CHECK line and exit 2, never
// cobra's 1 (the hook reads 1 as findings).
func ckUsage(cmd *cobra.Command, msg string) error {
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s: COULD NOT CHECK — %s\n", ckPrefix, msg)
	return &exitError{code: 2, err: errSilent}
}

// ckRun runs fn and makes its result the exit code of cmd: any error, a panic included, is a
// COULD NOT CHECK line and exit 2, since the hook reads 1 as findings.
func ckRun(cmd *cobra.Command, fn func(out, errOut io.Writer) (int, error)) (err error) {
	errOut := cmd.ErrOrStderr()
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(errOut, "%s: COULD NOT CHECK — panic: %v\n", ckPrefix, r)
			err = &exitError{code: 2, err: errSilent}
		}
	}()
	code, ferr := fn(cmd.OutOrStdout(), errOut)
	if ferr != nil {
		_, _ = fmt.Fprintf(errOut, "%s: COULD NOT CHECK — %v\n", ckPrefix, ferr)
		return &exitError{code: 2, err: errSilent}
	}
	if code != 0 {
		return &exitError{code: code}
	}
	return nil
}

// ckArgs validates the positional arguments of one subcommand; none may follow a `--`.
func ckArgs(check func(args []string) string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if cmd.ArgsLenAtDash() >= 0 {
			return ckUsage(cmd, cmd.Name()+" takes no command after --")
		}
		if msg := check(args); msg != "" {
			return ckUsage(cmd, msg)
		}
		return nil
	}
}

func ckExactly(n int, what string) cobra.PositionalArgs {
	return ckArgs(func(args []string) string {
		if len(args) != n {
			return fmt.Sprintf("expected %s, got %d arguments", what, len(args))
		}
		return ""
	})
}

func ckCommand(cmd *cobra.Command) *cobra.Command {
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error { return ckUsage(c, err.Error()) })
	return cmd
}

// newCheckpointScanCommand is the hidden `lighthouse checkpoint-scan`, the port of
// the former Python tools/hooks/checkpoint_scan.py. Exit 0 clean, 1 findings (or held refs), 2 COULD NOT CHECK.
func newCheckpointScanCommand() *cobra.Command {
	cmd := ckCommand(&cobra.Command{
		Use:    "checkpoint-scan",
		Short:  "Scan the checkpoint blobs a remote misses (for the pre-push hook)",
		Hidden: true,
		Long: "Exit: 0 clean | 1 findings or held refs | 2 COULD NOT CHECK (every error, an\n" +
			"unexpected one and a usage error included: the hook reads 1 as findings).",
		Args: ckArgs(func(args []string) string {
			if len(args) > 0 {
				return fmt.Sprintf("unknown subcommand %q", args[0])
			}
			return ""
		}),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ckUsage(cmd, "a subcommand is required (scan, push, entire-remote, remote-exclusion, run-bounded)")
		},
	})
	cmd.AddCommand(newCkScanCommand(), newCkPushCommand(), newCkEntireRemoteCommand(),
		newCkRemoteExclusionCommand(), newCkRunBoundedCommand())
	return cmd
}

var refSHA = regexp.MustCompile(`\A(.+)=([0-9a-f]{40}|[0-9a-f]{64})\z`)

// parseRefSHAs reads `<ref>=<sha>` arguments (sha1 or sha256); an all-zero sha is a deletion,
// which sends no objects, and is left out.
func parseRefSHAs(args []string) (map[string]string, error) {
	refs := map[string]string{}
	for _, arg := range args {
		hit := refSHA.FindStringSubmatch(arg)
		if hit == nil {
			return nil, fmt.Errorf("not <ref>=<sha>: %s", arg)
		}
		if strings.Trim(hit[2], "0") != "" {
			refs[hit[1]] = hit[2]
		}
	}
	return refs, nil
}

// defaultGitleaksConfig is `<repo-root>/.gitleaks.toml`.
func defaultGitleaksConfig() (string, error) {
	root, err := config.RepoRoot()
	if err != nil {
		return "", checkpointscan.CouldNotCheck{Msg: err.Error()}
	}
	return filepath.Join(root, ".gitleaks.toml"), nil
}

func newCkScanCommand() *cobra.Command {
	var remote, cfg string
	var full, keepGoing bool
	cmd := ckCommand(&cobra.Command{
		Use:   "scan --remote <remote> [--full] [--keep-going] [--config <file>] <ref>=<sha>...",
		Short: "Scan the blobs a remote misses, per unique blob",
		Args: ckArgs(func(args []string) string {
			if len(args) == 0 {
				return "scan needs at least one <ref>=<sha>"
			}
			return ""
		}),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("remote") {
				return ckUsage(cmd, "scan needs --remote")
			}
			refs, err := parseRefSHAs(args)
			if err != nil {
				return ckUsage(cmd, err.Error())
			}
			return ckRun(cmd, func(out, errOut io.Writer) (int, error) {
				c := cfg
				if !cmd.Flags().Changed("config") {
					var err error
					if c, err = defaultGitleaksConfig(); err != nil {
						return 0, err
					}
				}
				return runScan(remote, c, full, keepGoing, refs, out, errOut)
			})
		},
	})
	cmd.Flags().StringVar(&remote, "remote", "", "the remote the push goes to")
	cmd.Flags().BoolVar(&full, "full", false, "ignore the ledger (still write it)")
	cmd.Flags().BoolVar(&keepGoing, "keep-going", false, "do not stop at the findings cap, and report peak RSS per batch (the measurement)")
	cmd.Flags().StringVar(&cfg, "config", "", "the gitleaks config (default <repo-root>/.gitleaks.toml)")
	return cmd
}

func runScan(remote, cfg string, full, keepGoing bool, refs map[string]string, out, errOut io.Writer) (int, error) {
	limits, err := checkpointscan.LoadLimits()
	if err != nil {
		return 0, err
	}
	repo, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	if info, err := os.Stat(cfg); err != nil || !info.Mode().IsRegular() {
		return 0, checkpointscan.CouldNotCheck{Msg: fmt.Sprintf("cannot find %s; scans must use the same config as CI", cfg)}
	}
	ledger, err := checkpointscan.LedgerPath(repo, cfg)
	if err != nil {
		return 0, err
	}
	all, err := checkpointscan.MissingBlobs(repo, refs, remote)
	if err != nil {
		return 0, err
	}
	blobs, err := checkpointscan.UnscannedBlobs(all, ledger, full)
	if err != nil {
		return 0, err
	}
	if !full {
		volume := 0
		for _, b := range blobs {
			volume += b.Size
		}
		limit, err := checkpointscan.EnvBytes(checkpointscan.VolumeLimitEnv, checkpointscan.HookVolumeLimit)
		if err != nil {
			return 0, err
		}
		if volume > limit {
			_, _ = fmt.Fprintf(errOut, "pre-push: BLOCKED — %d checkpoint blobs (%.0f MB) are not yet scanned. "+
				"A first push after a bulk import ('entire import') is heavy; run: lighthouse checkpoint-scan push %s\n",
				len(blobs), float64(volume)/(1<<20), remote)
			return 2, nil
		}
	}
	result, err := checkpointscan.ScanBlobs(repo, blobs, checkpointscan.ScanOptions{
		Ledger: ledger, StopOnCap: !keepGoing, ReportRSS: keepGoing, Config: cfg, Limits: limits, Stderr: errOut,
	})
	if err != nil {
		return 0, err
	}
	seen := map[string]struct{}{}
	for _, f := range result.Findings {
		for place := range blobs[f.Blob].Places {
			seen[fmt.Sprintf("%s  %s  %s  %s", place.Ref, place.Path, f.Rule, f.Match)] = struct{}{}
		}
	}
	lines := make([]string, 0, len(seen))
	for line := range seen {
		lines = append(lines, line)
	}
	sort.Strings(lines)
	for _, line := range lines {
		_, _ = fmt.Fprintln(out, line)
	}
	// An aborted scan holds the findings of earlier batches only: never exit 1 for it.
	if result.Aborted != "" {
		return 2, nil
	}
	if len(result.Findings) > 0 {
		return 1, nil
	}
	return 0, nil
}

func newCkPushCommand() *cobra.Command {
	var full, reportRSS bool
	cmd := ckCommand(&cobra.Command{
		Use:   "push <remote> [--full] [--report-rss]",
		Short: "Scan the checkpoints a remote misses, push the clean ones",
		Args:  ckExactly(1, "one <remote>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return ckRun(cmd, func(out, errOut io.Writer) (int, error) {
				limits, err := checkpointscan.LoadLimits()
				if err != nil {
					return 0, err
				}
				repo, err := os.Getwd()
				if err != nil {
					return 0, err
				}
				// The hook's config: one ledger for both, so the hook finds what push scanned.
				cfg, err := defaultGitleaksConfig()
				if err != nil {
					return 0, err
				}
				return checkpointscan.Push(repo, args[0], checkpointscan.PushOptions{
					Full: full, ReportRSS: reportRSS, Config: cfg, Limits: limits,
				}, out, errOut), nil
			})
		},
	})
	cmd.Flags().BoolVar(&full, "full", false, "ignore the ledger (still write it)")
	cmd.Flags().BoolVar(&reportRSS, "report-rss", false, "report gitleaks' peak RSS per batch (for a timer's journal)")
	return cmd
}

func newCkEntireRemoteCommand() *cobra.Command {
	var toplevel string
	cmd := ckCommand(&cobra.Command{
		Use:   "entire-remote --toplevel <dir> <remote> <url>",
		Short: "For the hook: whether entire may push by itself, and whether <remote> is the checkpoint remote",
		Args:  ckExactly(2, "<remote> <url>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("toplevel") {
				return ckUsage(cmd, "entire-remote needs --toplevel")
			}
			return ckRun(cmd, func(out, _ io.Writer) (int, error) {
				settings, err := checkpointscan.ReadEntireSettings(toplevel)
				if err != nil {
					return 0, err
				}
				yesNo := func(b bool) string {
					if b {
						return "yes"
					}
					return "no"
				}
				_, _ = fmt.Fprintln(out, yesNo(settings.SelfPush))
				_, _ = fmt.Fprintln(out, yesNo(checkpointscan.IsCheckpointRemote(settings, args[0], args[1])))
				return 0, nil
			})
		},
	})
	cmd.Flags().StringVar(&toplevel, "toplevel", "", "the work tree holding .entire/")
	return cmd
}

func newCkRemoteExclusionCommand() *cobra.Command {
	return ckCommand(&cobra.Command{
		Use:   "remote-exclusion <remote>",
		Short: "For the hook: the rev-list arguments that leave out what <remote> already has",
		Args:  ckExactly(1, "one <remote>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return ckRun(cmd, func(out, _ io.Writer) (int, error) {
				repo, err := os.Getwd()
				if err != nil {
					return 0, err
				}
				exclusion, err := checkpointscan.RemoteExclusion(repo, args[0])
				if err != nil {
					return 0, err
				}
				_, _ = fmt.Fprintln(out, strings.Join(exclusion, " "))
				return 0, nil
			})
		},
	})
}

func newCkRunBoundedCommand() *cobra.Command {
	return ckCommand(&cobra.Command{
		Use:   "run-bounded -- <argv...>",
		Short: "Run one command bounded: a memory ceiling, a CPU quota, an OOM score and a nice level",
		// Everything after the first `--` is the command, untouched.
		Args: func(cmd *cobra.Command, args []string) error {
			dash := cmd.ArgsLenAtDash()
			switch {
			case dash > 0:
				return ckUsage(cmd, fmt.Sprintf("run-bounded: unrecognized arguments: %v", args[:dash]))
			case dash < 0 || len(args) == 0:
				return ckUsage(cmd, "run-bounded needs a command after --")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return ckRun(cmd, func(out, errOut io.Writer) (int, error) {
				limits, err := checkpointscan.LoadLimits()
				if err != nil {
					return 0, err
				}
				name := filepath.Base(args[0])
				run, err := checkpointscan.RunBounded(args, limits, "")
				if err != nil {
					return 0, err
				}
				_, _ = io.WriteString(out, run.Output)
				if run.CeilingHit {
					_, _ = fmt.Fprintf(errOut, "%s: ABORTED at memory ceiling (%s); raise host.scan_memory_max\n", ckPrefix, limits.MemoryMax)
					return 2, nil
				}
				if run.ReturnCode == 0 || run.ReturnCode == 1 {
					return run.ReturnCode, nil
				}
				return 0, errors.New(name + " exited " + fmt.Sprint(run.ReturnCode))
			})
		},
	})
}
