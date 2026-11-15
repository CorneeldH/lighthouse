package forge

import (
	"context"
	"errors"
	"fmt"
)

// The methods below are not written yet; each later task moves its own out of this file, and
// the file goes when the last one is gone. Until then they fail with errors.ErrUnsupported.

func notYet(name string) error {
	return fmt.Errorf("gitlab: %s not implemented yet: %w", name, errors.ErrUnsupported)
}

func (g *GitLab) MergePR(context.Context, int, string) error { return notYet("MergePR") }
func (g *GitLab) OpenPRs(context.Context) ([]OpenPR, error)  { return nil, notYet("OpenPRs") }
func (g *GitLab) BranchPRs(context.Context, string) ([]PR, error) {
	return nil, notYet("BranchPRs")
}
func (g *GitLab) BoardItems(context.Context) ([]BoardItem, error) { return nil, notYet("BoardItems") }
func (g *GitLab) IssueItem(context.Context, int) (BoardItem, bool, error) {
	return BoardItem{}, false, notYet("IssueItem")
}
func (g *GitLab) AddToBoard(context.Context, int) (BoardItem, error) {
	return BoardItem{}, notYet("AddToBoard")
}
func (g *GitLab) SetStatus(context.Context, BoardItem, string) error { return notYet("SetStatus") }
