package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/selfupdate"
	"github.com/CorneeldH/lighthouse/internal/version"
)

// selfUpdateTimeout bounds the whole update: the HTTP client in selfupdate has no timeout of its own.
const selfUpdateTimeout = 5 * time.Minute

func newSelfUpdateCommand() *cobra.Command {
	var unit string
	cmd := &cobra.Command{
		Use:   "self-update",
		Short: "Replace this binary with the latest release, after verifying its checksum",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			exe, err = filepath.EvalSymlinks(exe)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), selfUpdateTimeout)
			defer cancel()
			opts := selfupdate.Options{
				Current:      version.String(),
				Target:       exe,
				GOOS:         runtime.GOOS,
				GOARCH:       runtime.GOARCH,
				Repo:         "CorneeldH/lighthouse",
				APIBase:      "https://api.github.com",
				DownloadBase: "https://github.com",
				Token:        os.Getenv("GH_TOKEN"),
			}
			if unit != "" {
				opts.Restart = func() error {
					out, err := exec.CommandContext(ctx, "systemctl", "--user", "try-restart", unit).CombinedOutput()
					if err != nil {
						return fmt.Errorf("systemctl --user try-restart %s: %w: %s", unit, err, strings.TrimSpace(string(out)))
					}
					return nil
				}
			}
			res, err := selfupdate.Run(ctx, opts)
			if err != nil {
				return err
			}
			reportSelfUpdate(cmd.OutOrStdout(), res, unit)
			return nil
		},
	}
	cmd.Flags().StringVar(&unit, "restart", "", "systemd user unit to try-restart after a replace (empty: no restart)")
	return cmd
}

// reportSelfUpdate writes the outcome of selfupdate.Run; the restart line only follows a replace.
func reportSelfUpdate(out io.Writer, r selfupdate.Result, unit string) {
	switch r.Outcome {
	case selfupdate.SkippedDev:
		_, _ = fmt.Fprintln(out, "self-update: skipped — dev build")
	case selfupdate.UpToDate:
		_, _ = fmt.Fprintf(out, "self-update: up to date (%s)\n", r.To)
	case selfupdate.Replaced:
		_, _ = fmt.Fprintf(out, "self-update: replaced %s with %s\n", r.From, r.To)
		if unit != "" {
			_, _ = fmt.Fprintf(out, "self-update: restarted %s\n", unit)
		}
	}
}
