package workplace

import (
	"fmt"
	"time"
)

// Own maps the session key of each run of the issue to that run's session_id,
// as runs.SessionKeys returns it.
type Own map[string]string

// Has reports whether s is a session of the issue's own runs: its key is
// recorded, and its forge session_id is empty or that of the recording run.
// Ownership goes by key because a resume run shares the session_id of the
// earlier run. The session_id check catches a key adopted while its
// session_id was still empty (a re-read after a spawn) that forge later fills
// in with another agent's id: that session is foreign.
func (o Own) Has(s Session) bool {
	sid, ok := o[s.Key]
	return ok && (s.SessionID == "" || s.SessionID == sid)
}

// Busy returns the first session in the list that makes the workplace busy.
//
// own comes from runs.SessionKeys; open holds the session ids of the issue's
// open runs. activity is the last transcript event per session key, measured
// by the caller; nil or a missing key falls back to UpdatedAt, which is the
// forge state when nothing better is measurable.
//
// A session of an open run is busy unless it is done; open is checked first.
// A session that own.Has does not count. Any other session is foreign: busy
// when it is working, or when it is waiting and its last activity is unknown
// or younger than maxWait.
func Busy(sessions []Session, own Own, open map[string]bool, activity map[string]time.Time, now time.Time, maxWait time.Duration) (Session, bool) {
	for _, s := range sessions {
		switch {
		case open[s.SessionID]:
			if s.Stand() != Done {
				return s, true
			}
		case own.Has(s):
			// A closed run of this issue: not a reason to wait.
		default:
			if foreignBusy(s, activity, now, maxWait) {
				return s, true
			}
		}
	}
	return Session{}, false
}

func foreignBusy(s Session, activity map[string]time.Time, now time.Time, maxWait time.Duration) bool {
	switch s.Stand() {
	case Working:
		return true
	case Waiting:
		last := s.UpdatedAt
		if t, ok := activity[s.Key]; ok {
			last = t
		}
		return last.IsZero() || now.Sub(last) < maxWait
	default:
		return false
	}
}

// BusyReason explains why Busy returned s.
func BusyReason(s Session) string {
	state, since := s.State, "unknown"
	if state == "" {
		state = "unknown"
	}
	if !s.UpdatedAt.IsZero() {
		since = s.UpdatedAt.Local().Format("15:04")
	}
	return fmt.Sprintf("workplace busy: session %s, %s since %s", s.Key, state, since)
}

// ForeignLive returns the first session that own.Has rejects and that is not
// done, also a quiet one: cleanup must not remove the workspace under it.
func ForeignLive(sessions []Session, own Own) (Session, bool) {
	for _, s := range sessions {
		if !own.Has(s) && s.Stand() != Done {
			return s, true
		}
	}
	return Session{}, false
}
