package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/park"
)

// pauseEnv is what pause reads from the machine: the environment, the path of host.toml and
// the clock. Only tests set Now.
type pauseEnv struct {
	Getenv   func(string) string
	HostToml string
	Now      func() time.Time // nil is time.Now
}

func newPauseCommand() *cobra.Command {
	var until string
	cmd := &cobra.Command{
		Use:   "pause [off]",
		Short: "Pause the dispatcher, or lift the pause",
		Long: "Write the pause flag: the dispatcher starts nothing until the given time. Without --until the\n" +
			"pause lasts until the start of [dispatch] night; at night --until is required. --until HH:MM is\n" +
			"the next such wall-clock time, within 24 hours. \"pause off\" removes the flag.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			off := false
			if len(args) == 1 {
				if args[0] != "off" {
					return fmt.Errorf("unknown argument %q: the only argument is off", args[0])
				}
				off = true
			}
			env := pauseEnv{Getenv: os.Getenv, HostToml: hostconfig.Path()}
			return pause(env, until, off, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&until, "until", "", "end of the pause as HH:MM (default: the start of night)")
	return cmd
}

// pause writes the pause flag until the time that until names, or removes it when off.
func pause(env pauseEnv, until string, off bool, out io.Writer) error {
	path, err := park.PauseFile(env.Getenv)
	if err != nil {
		return err
	}
	if off {
		if until != "" {
			return errors.New("pause off takes no --until")
		}
		removed, err := park.ClearPause(path)
		if err != nil {
			return err
		}
		if !removed {
			_, err = fmt.Fprintln(out, "lighthouse: no pause")
			return err
		}
		_, err = fmt.Fprintln(out, "lighthouse: pause off")
		return err
	}
	cfg, err := hostconfig.LoadDispatch(env.HostToml)
	if err != nil {
		return err
	}
	now := time.Now()
	if env.Now != nil {
		now = env.Now()
	}
	end, err := park.Until(now, until, cfg.Night)
	if err != nil {
		return err
	}
	if err := park.WritePause(path, end); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "lighthouse: paused until %s\n", end.Format(time.RFC3339))
	return err
}
