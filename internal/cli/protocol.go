package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/protocol"
)

// headerJSON is the output of `protocol parse-header`; the keys are a contract with skills.
type headerJSON struct {
	Kind    string            `json:"kind"`
	Fields  map[string]string `json:"fields"`
	Session *string           `json:"session"`
}

// newProtocolCommand is `lighthouse protocol`, the shell-facing part of the protocol package.
func newProtocolCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "protocol",
		Short: "Read and print parts of the lighthouse protocol",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "robot",
		Short: "Print the robot that opens every protocol comment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), protocol.Robot)
			return err
		},
	}, &cobra.Command{
		Use:   "parse-header",
		Short: "Read a comment body on stdin; print its kind, fields and session as JSON, or null",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return err
			}
			var out any
			if header, ok := protocol.ParseHeader(string(body)); ok {
				result := headerJSON{Kind: string(header.Kind), Fields: header.Fields}
				if header.Session != "" {
					result.Session = &header.Session
				}
				out = result
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetEscapeHTML(false)
			return enc.Encode(out)
		},
	})
	return cmd
}
