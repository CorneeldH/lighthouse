package initcmd

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// isolatedEnv is the environment for a python3 call that must read only the repository in
// root: the caller's PYTHON* settings and LIGHTHOUSE_* pointers are dropped, then the
// repository's own are set.
func isolatedEnv(root string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PYTHON") || strings.HasPrefix(kv, "LIGHTHOUSE_TOML=") ||
			strings.HasPrefix(kv, "LIGHTHOUSE_REPO_ROOT=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"LIGHTHOUSE_REPO_ROOT="+root,
		"LIGHTHOUSE_TOML="+filepath.Join(root, "lighthouse.toml"),
	)
}

// runLabelScript runs the installed .lighthouse/scripts/create_labels.py in root. It never
// reads stdin and tells gh not to prompt; a non-zero exit comes back as an error.
func runLabelScript(root string, create bool, out, errw io.Writer) error {
	args := []string{".lighthouse/scripts/create_labels.py"}
	if create {
		args = append(args, "--create")
	}
	cmd := exec.Command("python3", args...)
	cmd.Dir = root
	cmd.Env = append(isolatedEnv(root), "GH_PROMPT_DISABLED=1")
	cmd.Stdout, cmd.Stderr = out, errw
	return cmd.Run()
}
