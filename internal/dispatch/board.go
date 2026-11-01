package dispatch

import (
	"context"
	"fmt"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// StatusSetter sets a single-select field of a project item; *forge.Client is one.
type StatusSetter interface {
	SetStatus(ctx context.Context, projectID, itemID, fieldID, optionID string) error
}

// NewBoard is the Board of b: a status name becomes the option ID of [forge.board]
// status_options, on b's project and Status field.
func NewBoard(c StatusSetter, b repoconfig.Board) Board {
	return board{c: c, b: b}
}

type board struct {
	c StatusSetter
	b repoconfig.Board
}

func (bd board) SetStatus(ctx context.Context, itemID, status string) error {
	option, ok := bd.b.StatusOptions[status]
	if !ok {
		return fmt.Errorf("status %q is not in [forge.board] status_options", status)
	}
	return bd.c.SetStatus(ctx, bd.b.ProjectID, itemID, bd.b.StatusFieldID, option)
}
