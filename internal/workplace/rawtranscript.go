package workplace

import "context"

// RawTranscripts is an optional interface next to Workplace: the whole transcript files of a
// session, for a reader that parses the lines itself (the telemetry collector). Every
// FileDelta has From 0 and holds the whole file in Data; the main transcript comes first.
// Errors are those of Transcripts.Transcript: ErrNotMeasurable, ErrGone, ErrUnanswered and an
// error that wraps fs.ErrNotExist for a session without a transcript.
type RawTranscripts interface {
	RawTranscript(ctx context.Context, workspaceID, sessionID string) ([]FileDelta, error)
}
