package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/automemory"
)

// newMemoryCommand is `lighthouse memory`: tools around Claude Code's auto-memory.
func newMemoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Tools around Claude Code auto-memory",
	}
	cmd.AddCommand(newMemoryScanCommand())
	return cmd
}

// newMemoryScanCommand is `lighthouse memory scan`: it proposes a destination per auto-memory file
// of this repository. It only reads.
func newMemoryScanCommand() *cobra.Command {
	var format, configDir string
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Propose a destination for each auto-memory file of this repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := runMemoryScan(cmd.OutOrStdout(), format, configDir); err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "memory scan: %v\n", err)
				return &exitError{code: 2}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "output format: json or text")
	cmd.Flags().StringVar(&configDir, "config-dir", "", "Claude config directory (default CLAUDE_CONFIG_DIR, else ~/.claude)")
	return cmd
}

func runMemoryScan(out io.Writer, format, configDir string) error {
	if format != "json" && format != "text" {
		return fmt.Errorf("unknown format %q: want json or text", format)
	}
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		configDir = automemory.ConfigDir(os.Getenv, home)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	repo, err := automemory.MainCheckout(cwd)
	if err != nil {
		return err
	}
	res, err := automemory.Scan(configDir, repo)
	if err != nil {
		return err
	}
	if format == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	files := append([]automemory.Proposal(nil), res.Files...)
	sort.SliceStable(files, func(i, j int) bool { return files[i].Destination < files[j].Destination })
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "destination\ttype\tname\tpath")
	for _, f := range files {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", f.Destination, f.Type, f.Name, f.Path)
	}
	return tw.Flush()
}
