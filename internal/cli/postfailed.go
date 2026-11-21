package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
)

// stateDirEnv is the state directory of the controller, which every launch carries.
const stateDirEnv = "LIGHTHOUSE_STATE_DIR"

// notePostFailed writes the post-failed note of a controller run; outside one it does nothing.
// A controller run has a UUID in LIGHTHOUSE_CONTROLLER_RUN and a LIGHTHOUSE_STATE_DIR. A failed
// write adds one line to stderr and never changes what the failed post reports.
func notePostFailed(getenv func(string) string, now time.Time, stderr io.Writer, issue int, command, body string, postErr error) {
	session, stateDir := getenv(askSessionEnv), getenv(stateDirEnv)
	if !askSessionUUID.MatchString(session) || stateDir == "" {
		return
	}
	text := ""
	if postErr != nil {
		text = postErr.Error()
	}
	err := dispatch.WritePostFailed(stateDir, session, dispatch.PostFailed{
		At: now, Issue: issue, Command: command, Error: text, Body: body,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "lighthouse: post-failed note: COULD NOT CHECK — %v\n", err)
	}
}
