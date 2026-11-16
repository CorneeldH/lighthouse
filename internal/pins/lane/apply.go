package lane

import (
	"context"
	"errors"
	"fmt"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// laneCloseComment is the comment the lane leaves when it closes an item itself.
const laneCloseComment = "De lane sluit dit item: de pin heeft het niet meer nodig."

// ItemContent is what the caller rendered for the item an Apply creates or updates. Body
// carries the marker. Branch is the head of a PR; Base its target branch.
type ItemContent struct {
	Title, Body string
	Branch      string
	Base        string
}

// Applier executes the actions Decide returned against the forge. It never merges, and
// creates no board item, milestone or closing reference: a lane item is an issue or a draft PR
// only.
type Applier struct {
	F forge.Forge
}

// Apply executes actions for the pin key in order. A failed action does not stop the next
// one, nor the caller's next pin: the errors are joined, each naming key.
func (a Applier) Apply(ctx context.Context, key string, actions []Action, content ItemContent) error {
	var errs []error
	for _, act := range actions {
		var err error
		switch act.Op {
		case Create:
			err = a.create(ctx, act, content)
		case Update:
			err = a.update(ctx, act, content)
		case CloseByLane:
			err = a.closeByLane(ctx, act.Item)
		default:
			err = fmt.Errorf("unknown action %d", act.Op)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("pin %s: %w", key, err))
		}
	}
	return errors.Join(errs...)
}

func (a Applier) create(ctx context.Context, act Action, c ItemContent) error {
	if act.Kind == KindPR {
		_, err := a.F.CreatePR(ctx, forge.NewPR{Title: c.Title, Body: c.Body, Head: c.Branch, Base: c.Base, Draft: true})
		return err
	}
	_, err := a.F.CreateIssue(ctx, c.Title, c.Body)
	return err
}

func (a Applier) update(ctx context.Context, act Action, c ItemContent) error {
	if act.Item.IsPR {
		return a.F.UpdatePRBody(ctx, act.Item.Number, c.Body)
	}
	return a.F.UpdateIssueBody(ctx, act.Item.Number, c.Body)
}

// closeByLane writes LaneClosedMarker into the body first, because FindByMarker and Decide only
// see the body, and then closes with a comment. A failed marker write leaves the item open.
func (a Applier) closeByLane(ctx context.Context, it forge.Item) error {
	body := it.Body + "\n" + LaneClosedMarker + "\n"
	if it.IsPR {
		if err := a.F.UpdatePRBody(ctx, it.Number, body); err != nil {
			return err
		}
		return a.F.ClosePR(ctx, it.Number, laneCloseComment)
	}
	if err := a.F.UpdateIssueBody(ctx, it.Number, body); err != nil {
		return err
	}
	return a.F.CloseIssue(ctx, it.Number, laneCloseComment)
}
