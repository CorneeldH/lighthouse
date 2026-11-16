package nodeserve

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

type transcriptInput struct {
	ID      string `path:"id"`
	Session string `path:"session"`
	Profile string `query:"profile"`
	From    string `query:"from"`
}

type transcriptOutput struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

func (h *handler) registerTranscript() {
	huma.Get(h.api, nodeapi.PathTranscript, h.transcript, func(op *huma.Operation) { op.OperationID = "get-transcript" })
}

// transcript answers the transcript files of the session from the offsets the tick holds, as
// a tar whose members carry nodeapi.PAXSize and nodeapi.PAXFrom. It reads them where the
// adapter of the node reads them for the profile (the devcontainer with software, the host
// otherwise) and measures nothing: the tick runs ReadTranscript on its mirror.
func (h *handler) transcript(ctx context.Context, in *transcriptInput) (*transcriptOutput, error) {
	if err := checkWorkspaceID(in.ID); err != nil {
		return nil, err
	}
	if !workplace.SafeSession.MatchString(in.Session) {
		return nil, badRequest("session id %q is not safe", in.Session)
	}
	if !h.d.Profiles[in.Profile] {
		return nil, badRequest("profile %q is not a profile of this node", in.Profile)
	}
	from, err := parseFrom(in.From)
	if err != nil {
		return nil, err
	}
	if _, err := h.resolve(ctx, in.ID); err != nil {
		return nil, err
	}
	a := *h.adapter // a copy per request: the profile is the run's, not the node's
	a.Profile = in.Profile
	deltas, err := a.TranscriptDelta(ctx, in.ID, in.Session, from)
	switch {
	case errors.Is(err, workplace.ErrGone):
		return nil, &nodeapi.Problem{Status: http.StatusNotFound, Code: nodeapi.CodeWorkspaceGone, Detail: err.Error()}
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, workplace.ErrNotMeasurable):
		return nil, &nodeapi.Problem{Status: http.StatusNotFound, Code: nodeapi.CodeNoTranscript, Detail: err.Error()}
	case err != nil:
		return nil, internal("transcript of %s in %s: %v", in.Session, in.ID, err)
	}
	body, err := tarDeltas(deltas)
	if err != nil {
		return nil, internal("transcript of %s in %s: %v", in.Session, in.ID, err)
	}
	return &transcriptOutput{ContentType: "application/x-tar", Body: body}, nil
}

// parseFrom reads the query from: "<file>:<offset>,…", each offset a non-negative integer. The
// file is split off at the last colon; whether it is a file of the session decides ReadDelta.
func parseFrom(raw string) (map[string]int64, error) {
	from := map[string]int64{}
	if raw == "" {
		return from, nil
	}
	for _, part := range strings.Split(raw, ",") {
		i := strings.LastIndex(part, ":")
		if i <= 0 {
			return nil, badRequest("from %q: %q is not <file>:<offset>", raw, part)
		}
		off, err := strconv.ParseInt(part[i+1:], 10, 64)
		if err != nil || off < 0 {
			return nil, badRequest("from %q: offset of %q is not a non-negative integer", raw, part[:i])
		}
		from[part[:i]] = off
	}
	return from, nil
}

// tarDeltas is the tar of the deltas, one member per file with the PAX records of its size and
// offset.
func tarDeltas(deltas []workplace.FileDelta) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, d := range deltas {
		hdr := &tar.Header{
			Name: d.Name, Mode: 0o600, Size: int64(len(d.Data)), Typeflag: tar.TypeReg, Format: tar.FormatPAX,
			PAXRecords: map[string]string{
				nodeapi.PAXSize: strconv.FormatInt(d.Size, 10),
				nodeapi.PAXFrom: strconv.FormatInt(d.From, 10),
			},
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(d.Data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
