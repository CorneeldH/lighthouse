package nodeserve

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"

	"github.com/danielgtaylor/huma/v2"

	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/usage"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// The usage routes carry the tick's usage guard to the node. The hook on the node and its
// markers stay as on self: serve writes PAUSE and the alarm mirror where that hook reads them,
// and answers and removes the markers the hook wrote.

type usageInput struct {
	Body nodeapi.UsageRequest
}

type markersOutput struct{ Body []park.Marker }

type markerInput struct {
	Session string `path:"session"`
	Key     string `query:"key"`
}

func (h *handler) registerUsage() {
	huma.Put(h.api, nodeapi.PathUsage, h.putUsage, func(op *huma.Operation) {
		op.OperationID = "put-usage"
		op.DefaultStatus = http.StatusNoContent
	})
	huma.Get(h.api, nodeapi.PathMarkers, h.markers, func(op *huma.Operation) { op.OperationID = "get-markers" })
	huma.Delete(h.api, nodeapi.PathMarker, h.deleteMarker, func(op *huma.Operation) {
		op.OperationID = "delete-marker"
		op.DefaultStatus = http.StatusNoContent
	})
}

// putUsage writes the raw bytes of the tick's PAUSE and alarm mirror atomically on the node,
// or removes the file for nil. The validity (until, reset) is in the bytes themselves.
func (h *handler) putUsage(_ context.Context, in *usageInput) (*struct{}, error) {
	pause, mirror, err := h.usagePaths()
	if err != nil {
		return nil, err
	}
	if err := putOrRemove(pause, in.Body.Pause); err != nil {
		return nil, internal("pause flag %s: %v", pause, err)
	}
	if err := putOrRemove(mirror, in.Body.Alarm); err != nil {
		return nil, internal("alarm mirror %s: %v", mirror, err)
	}
	return nil, nil
}

// usagePaths are PAUSE and the alarm mirror on the node: from Deps, else from the environment
// of serve as on self.
func (h *handler) usagePaths() (pause, mirror string, err error) {
	pause, mirror = h.d.PauseFile, h.d.AlarmMirror
	if pause == "" {
		if pause, err = park.PauseFile(os.Getenv); err != nil {
			return "", "", internal("no place for the pause flag: %v", err)
		}
	}
	if mirror == "" {
		p, err := usage.DefaultPaths()
		if err != nil || p.Mirror == "" {
			return "", "", internal("no place for the alarm mirror: %v", err)
		}
		mirror = p.Mirror
	}
	return pause, mirror, nil
}

func putOrRemove(path string, content *string) error {
	if content != nil {
		return workplace.WriteFileAtomic(path, []byte(*content))
	}
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// markerDir is the node's marker directory: from Deps, else park.MarkerDir as on self.
func (h *handler) markerDir() (string, error) {
	if h.d.MarkerDir != "" {
		return h.d.MarkerDir, nil
	}
	dir, err := park.MarkerDir(os.Getenv)
	if err != nil {
		return "", internal("no place for the resume markers: %v", err)
	}
	return dir, nil
}

// markers answers the valid resume markers of the node, an empty list without any.
func (h *handler) markers(context.Context, *struct{}) (*markersOutput, error) {
	dir, err := h.markerDir()
	if err != nil {
		return nil, err
	}
	list, err := park.ReadMarkers(dir)
	if err != nil {
		return nil, internal("markers in %s: %v", dir, err)
	}
	if list == nil {
		list = []park.Marker{}
	}
	return &markersOutput{Body: list}, nil
}

// deleteMarker is park.RemoveMarker on the node: a marker whose key differs stays, since the
// guard parked the session again; a missing marker is no error.
func (h *handler) deleteMarker(_ context.Context, in *markerInput) (*struct{}, error) {
	if err := checkSession(in.Session); err != nil {
		return nil, err
	}
	if in.Key == "" {
		return nil, badRequest("delete marker %s without key", in.Session)
	}
	dir, err := h.markerDir()
	if err != nil {
		return nil, err
	}
	if err := park.RemoveMarker(dir, in.Session, in.Key); err != nil {
		return nil, internal("remove marker %s: %v", in.Session, err)
	}
	return nil, nil
}
