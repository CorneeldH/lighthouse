package cli

import (
	"context"
	"errors"
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

// selfupdateRun is replaceable so tests can observe the options the command passes.
var selfupdateRun = selfupdate.Run

func newChannelCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "channel",
		Short: "Print the stored self-update channel (release or dev)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := selfupdate.ChannelPath()
			if path == "" {
				return errors.New("channel: no home directory")
			}
			c, err := selfupdate.ReadChannel(path)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), c)
			return nil
		},
	}
}

func newSelfUpdateCommand() *cobra.Command {
	var unit, channel string
	cmd := &cobra.Command{
		Use:   "self-update",
		Short: "Replace this binary with the latest release, after verifying its checksum",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			chPath := selfupdate.ChannelPath()
			var ch selfupdate.Channel
			if cmd.Flags().Changed("channel") {
				c, err := selfupdate.ParseChannel(channel)
				if err != nil {
					return err
				}
				ch = c
				if chPath == "" {
					return errors.New("--channel: no home directory to store it in")
				}
				if err := selfupdate.WriteChannel(chPath, ch); err != nil {
					return err
				}
			} else {
				if chPath == "" {
					return errors.New("self-update: no home directory to read the channel from")
				}
				c, err := selfupdate.ReadChannel(chPath)
				if err != nil {
					return err
				}
				ch = c
			}
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
				Channel:      ch,
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
			res, err := selfupdateRun(ctx, opts)
			if err != nil {
				return err
			}
			reportSelfUpdate(cmd.OutOrStdout(), res, unit)
			return nil
		},
	}
	cmd.Flags().StringVar(&channel, "channel", "", "channel to follow and store: release or dev (not given: the stored one)")
	cmd.Flags().StringVar(&unit, "restart", "", "systemd user unit to try-restart after a replace (empty: no restart)")
	return cmd
}

// reportSelfUpdate writes the outcome of selfupdate.Run; the restart line only follows a replace.
func reportSelfUpdate(out io.Writer, r selfupdate.Result, unit string) {
	switch r.Outcome {
	case selfupdate.SkippedDev:
		_, _ = fmt.Fprintln(out, "self-update: skipped — dev build")
	case selfupdate.UpToDate:
		_, _ = fmt.Fprintf(out, "self-update: up to date (%s; newest on channel %s)\n", r.From, r.To)
	case selfupdate.Replaced:
		_, _ = fmt.Fprintf(out, "self-update: replaced %s with %s\n", r.From, r.To)
		if unit != "" {
			_, _ = fmt.Fprintf(out, "self-update: restarted %s\n", unit)
		}
	}
}
