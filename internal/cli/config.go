package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// newConfigCommand is `lighthouse config <key>|host-check`, with `--json` for a value of any
// shape. Messages are written here and the exit code is returned silently, so execute adds no
// `lighthouse: ` prefix.
func newConfigCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "config <key>|host-check",
		Short: "Print one lighthouse.toml or host.toml value",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var code int
			if asJSON && len(args) == 1 && args[0] != "host-check" {
				code = runConfigJSON(args[0], cmd.OutOrStdout(), cmd.ErrOrStderr())
			} else {
				code = config.Run(args, cmd.OutOrStdout(), cmd.ErrOrStderr())
			}
			if code == 0 {
				return nil
			}
			return &exitError{code: code, err: errSilent}
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the value as JSON; a table is allowed")
	return cmd
}

// runConfigJSON prints the value of key as one JSON document, with the exit codes of config.Run.
func runConfigJSON(key string, out, errOut io.Writer) int {
	value, err := lookupValue(key)
	if err == nil {
		var data []byte
		if data, err = json.Marshal(value); err == nil {
			_, _ = fmt.Fprintf(out, "%s\n", data)
			return 0
		}
	}
	if errors.Is(err, config.ErrNotSet) {
		_, _ = fmt.Fprintf(errOut, "config: %s is not set\n", key)
		return 1
	}
	_, _ = fmt.Fprintf(errOut, "config: %v\n", err)
	return 2
}

// lookupValue resolves key; a host.* key never loads lighthouse.toml.
func lookupValue(key string) (any, error) {
	if strings.HasPrefix(key, "host.") {
		return config.HostValue(key)
	}
	path, err := config.Path()
	if err != nil {
		return nil, err
	}
	c, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	return config.Lookup(c, key)
}
