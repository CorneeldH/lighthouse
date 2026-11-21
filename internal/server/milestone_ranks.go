package server

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/priorities"
)

// RankChange moves one milestone from rank From (nil: it has no rank) to rank To.
type RankChange struct {
	Number int  `json:"number" minimum:"1"`
	From   *int `json:"from" minimum:"1" nullable:"true"`
	To     int  `json:"to" minimum:"1" maximum:"1000000"`
}

// RanksRequest is the list of rank changes in one repo, as the page computed them.
type RanksRequest struct {
	Repo    string       `json:"repo"`
	Changes []RankChange `json:"changes" minItems:"1" maxItems:"200"`
}

// RanksResult names the milestones written, in the order written; never nil.
type RanksResult struct {
	Written []int `json:"written"`
}

type ranksInput struct {
	Body RanksRequest
}

// Resolve refuses a body that is not application/json, which huma would otherwise assume
// when Content-Type is missing, and a request whose Origin is not the Host it was sent to.
func (in *ranksInput) Resolve(ctx huma.Context) []error {
	var errs []error
	mt, _, err := mime.ParseMediaType(ctx.Header("Content-Type"))
	if err != nil || mt != "application/json" {
		errs = append(errs, huma.Error415UnsupportedMediaType("body must be application/json"))
	}
	r := &http.Request{Host: ctx.Host(), Header: http.Header{}}
	r.Header.Set("Origin", ctx.Header("Origin"))
	if !sameOrigin(r) {
		errs = append(errs, huma.Error403Forbidden("Origin does not match Host"))
	}
	return errs
}

type ranksOutput struct {
	Body RanksResult
}

// registerMilestoneRanks adds the endpoint that writes the `Priority: N` line of milestones
// on the forge of one repo. It reads every milestone fresh and writes nothing when one rank
// differs from its From (409); it patches the cache after every write and on a 409, so the
// page shows what the forge holds without waiting for a build.
func registerMilestoneRanks(api huma.API, forges map[string]forge.Forge,
	patch func(func([]priorities.Repo) []priorities.Repo) bool) {
	huma.Register(api, huma.Operation{
		OperationID: "post-milestone-ranks",
		Method:      http.MethodPost,
		Path:        "/api/priorities/milestone-ranks",
		Errors:      []int{403, 404, 409, 415, 502, 503},
	}, func(ctx context.Context, in *ranksInput) (*ranksOutput, error) {
		req := in.Body
		f, ok := forges[req.Repo]
		if !ok {
			return nil, huma.Error404NotFound(fmt.Sprintf("unknown repo %q", req.Repo))
		}
		fresh, err := readFresh(ctx, f, req.Changes)
		if err != nil {
			return nil, forgeError(err)
		}
		if stale := staleNumbers(fresh, req.Changes); len(stale) > 0 {
			patch(func(repos []priorities.Repo) []priorities.Repo {
				for _, c := range req.Changes {
					repos = priorities.WithDescription(repos, req.Repo, c.Number, fresh[c.Number])
				}
				return repos
			})
			return nil, huma.Error409Conflict(fmt.Sprintf("rank changed on the forge for milestones %v", stale))
		}
		written := []int{}
		for _, c := range req.Changes {
			desc := priorities.SetRank(fresh[c.Number], c.To)
			m, err := f.EditMilestone(ctx, c.Number, nil, &desc)
			if err != nil {
				return nil, writeError(err, c.Number, written)
			}
			patch(func(repos []priorities.Repo) []priorities.Repo {
				return priorities.WithDescription(repos, req.Repo, c.Number, m.Description)
			})
			written = append(written, c.Number)
		}
		return &ranksOutput{Body: RanksResult{Written: written}}, nil
	})
}

// readFresh reads the description of every milestone in changes from the forge.
func readFresh(ctx context.Context, f forge.Forge, changes []RankChange) (map[int]string, error) {
	fresh := make(map[int]string, len(changes))
	for _, c := range changes {
		m, err := f.MilestoneByNumber(ctx, c.Number)
		if err != nil {
			return nil, fmt.Errorf("read milestone %d: %w", c.Number, err)
		}
		fresh[c.Number] = m.Description
	}
	return fresh, nil
}

// staleNumbers lists the milestones whose rank in fresh is not their From.
func staleNumbers(fresh map[int]string, changes []RankChange) []int {
	var stale []int
	for _, c := range changes {
		rank, ok := priorities.ParseRank(fresh[c.Number])
		if c.From == nil && !ok {
			continue
		}
		if c.From != nil && ok && rank == *c.From {
			continue
		}
		stale = append(stale, c.Number)
	}
	return stale
}

// writeError maps a failed write as forgeError does and names the milestones already written.
func writeError(err error, number int, written []int) error {
	status := http.StatusBadGateway
	if errors.Is(err, forge.ErrNotConfigured) {
		status = http.StatusServiceUnavailable
	}
	nums := make([]string, len(written))
	for i, n := range written {
		nums[i] = fmt.Sprint(n)
	}
	return huma.NewError(status, fmt.Sprintf("write milestone %d: %v; written=[%s]", number, err, strings.Join(nums, ",")))
}
