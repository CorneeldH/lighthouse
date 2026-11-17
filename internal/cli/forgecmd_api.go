package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// newForgeAPICommand is `lighthouse forge api <path> [-X method] [-f name=text]… [-F name=value]…`:
// the raw API of the forge, for what no other subcommand covers. The flags are parsed by hand:
// only -X, -f and -F exist, with no long form, which cobra cannot express.
func newForgeAPICommand(env *forgeEnv) *cobra.Command {
	return &cobra.Command{
		Use:                "api <path> [-X method] [-f name=text]... [-F name=value]...",
		Short:              "Call the forge API; prints the response body as it came",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
				return cmd.Help()
			}
			req, err := parseForgeAPIArgs(cmd, args)
			if err != nil {
				return err
			}
			f, err := env.open(cmd.Context())
			if err != nil {
				return env.fail(cmd, err)
			}
			out, err := f.API(cmd.Context(), req)
			if err != nil {
				return env.fail(cmd, err)
			}
			_, err = env.stdout(cmd).Write(out)
			return err
		},
	}
}

// parseForgeAPIArgs reads one path and the flags -X, -f and -F, each as "-f value" or
// "-fvalue". Any other flag, a missing value, a field without "=" or a second path is exit 2.
func parseForgeAPIArgs(cmd *cobra.Command, args []string) (forge.APIRequest, error) {
	var req forge.APIRequest
	var path string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			if path != "" {
				return req, forgeUsage(cmd, fmt.Sprintf("accepts 1 path, got %q and %q", path, a))
			}
			path = a
			continue
		}
		if len(a) < 2 || !strings.ContainsRune("XfF", rune(a[1])) || strings.HasPrefix(a, "--") {
			return req, forgeUsage(cmd, fmt.Sprintf("unknown flag: %s", a))
		}
		val := a[2:]
		if val == "" {
			if i+1 >= len(args) {
				return req, forgeUsage(cmd, fmt.Sprintf("flag needs an argument: %s", a))
			}
			i++
			val = args[i]
		}
		if a[1] == 'X' {
			req.Method = val
			continue
		}
		name, value, ok := strings.Cut(val, "=")
		if !ok || name == "" {
			return req, forgeUsage(cmd, fmt.Sprintf("field %q is not name=value", val))
		}
		req.Fields = append(req.Fields, forge.APIField{Name: name, Value: value, Typed: a[1] == 'F'})
	}
	if path == "" {
		return req, forgeUsage(cmd, "a path is required")
	}
	req.Path = path
	return req, nil
}
