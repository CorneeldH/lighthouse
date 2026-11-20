package checkpointscan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/procrun"
)

// gitTimeout bounds one git call; the original had none, so it is generous.
const gitTimeout = time.Hour

// git runs `git -C repo args…` and returns its stdout; a failure is a CouldNotCheck.
func git(repo string, stdin *string, args ...string) (string, error) {
	res, err := procrun.Exec(gitTimeout)(context.Background(), "", stdin, "git", append([]string{"-C", repo}, args...)...)
	if err != nil {
		var nf *procrun.NotFound
		if errors.As(err, &nf) {
			return "", CouldNotCheck{Msg: fmt.Sprintf("git did not start: %v", err)}
		}
		return "", CouldNotCheck{Msg: fmt.Sprintf("git %s: %v", args[0], err)}
	}
	if res.Code != 0 {
		return "", CouldNotCheck{Msg: fmt.Sprintf("git %s failed: %s", args[0], strings.TrimSpace(res.Stderr))}
	}
	return res.Stdout, nil
}
