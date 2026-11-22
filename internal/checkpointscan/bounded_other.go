//go:build !unix

package checkpointscan

import (
	"errors"
	"os"
	"os/exec"
)

// There is no systemd scope, no nice and no wait4 here: the command runs as it is, no
// ceiling is recognised and no peak RSS is measured.

func probeCeiling() string { return "no systemd on this platform" }

func unboundedCommand(_ string, argv []string) []string { return argv }

func execWait(argv []string, cwd string, out *os.File) (int, *int, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = cwd, out, out
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil, nil
	}
	return 0, nil, err
}
