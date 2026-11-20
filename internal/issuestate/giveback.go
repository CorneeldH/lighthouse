package issuestate

import (
	"context"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// ToDo is the board status an issue is given back to.
const ToDo = "To do"

// Returned is what Return did: the label edit, and on the board the status before (From,
// EffectiveStatus) when the issue is on it.
type Returned struct {
	Label   LabelEdit
	From    string
	OnBoard bool
}

// Return gives issue nr back to a human: Apply with wantKey, then, when board is true, the
// status To do unless it is already To do. An issue that is not on the board gives OnBoard
// false and no error. The board Priority stays as it is.
func Return(ctx context.Context, f forge.Forge, nr int, wantKey string, l repoconfig.Labels, board bool) (Returned, error) {
	var r Returned
	var err error
	if r.Label, err = Apply(ctx, f, nr, wantKey, l); err != nil || !board {
		return r, err
	}
	it, ok, err := f.IssueItem(ctx, nr)
	if err != nil || !ok {
		return r, err
	}
	r.OnBoard, r.From = true, it.EffectiveStatus()
	if r.From == ToDo {
		return r, nil
	}
	return r, f.SetStatus(ctx, it, ToDo)
}
