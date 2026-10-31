package workplace

import (
	"fmt"
	"time"
)

// Busy returns the first session in the list that makes the workplace busy.
//
// ownKeys holds the session keys of this issue's runs (runs.SessionKeys);
// open holds the session ids of its open runs. Own is decided by key, not by
// session_id: a resume run shares the session_id of the earlier run, so a late
// agent of a resume that never got a key would otherwise pass for the earlier
// run's session. activity is the last transcript event per session key,
// measured by the caller; nil or a missing key falls back to UpdatedAt, which
// is the forge state when nothing better is measurable.
//
// A session of an open run is busy unless it is done; open is checked first.
// A session whose key belongs to a run of the issue does not count. Any other
// session is foreign: busy when it is working, or when it is waiting and its
// last activity is unknown or younger than maxWait.
func Busy(sessions []Session, ownKeys, open map[string]bool, activity map[string]time.Time, now time.Time, maxWait time.Duration) (Session, bool) {
	for _, s := range sessions {
		switch {
		case open[s.SessionID]:
			if s.Stand() != Done {
				return s, true
			}
		case ownKeys[s.Key]:
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
	since := "unknown"
	if !s.UpdatedAt.IsZero() {
		since = s.UpdatedAt.Local().Format("15:04")
	}
	return fmt.Sprintf("workplace busy: session %s, %s since %s", s.Key, s.State, since)
}

// ForeignLive returns the first session whose key is not in ownKeys and that
// is not done, also a quiet one: cleanup must not remove the workspace under it.
func ForeignLive(sessions []Session, ownKeys map[string]bool) (Session, bool) {
	for _, s := range sessions {
		if !ownKeys[s.Key] && s.Stand() != Done {
			return s, true
		}
	}
	return Session{}, false
}
