package initcmd

import (
	"io"
	"os"
	"os/exec"
)

// runLabelScript runs the installed .lighthouse/scripts/create_labels.py in root. It never
// reads stdin and tells gh not to prompt; a non-zero exit comes back as an error.
func runLabelScript(root string, create bool, out, errw io.Writer) error {
	args := []string{".lighthouse/scripts/create_labels.py"}
	if create {
		args = append(args, "--create")
	}
	cmd := exec.Command("python3", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1")
	cmd.Stdout, cmd.Stderr = out, errw
	return cmd.Run()
}
