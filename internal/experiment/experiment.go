// Package experiment decides whether a trajectory (repo + issue) takes part in an
// explore/exploit experiment. It is pure code: no database, no clock of its own.
package experiment

import (
	"crypto/sha256"
	"encoding/binary"
	"strconv"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// Declared is the experiment a repo declares, with the repo that declares it.
type Declared struct {
	Repo string
	repoconfig.Experiment
}

// Decision is the assignment of one trajectory.
type Decision struct {
	Assignment   string // "standard" or "variant:<component>=<choice>"
	ExperimentID string // "" for standard
	Note         string // non-empty: why no assignment (conflict); the caller logs it
}

const standard = "standard"

// Merge reduces the declared experiments to at most one. The same id in several repos is one
// experiment and must be equal in every field. Experiments with different ids on different
// components, or on the same component, conflict: no experiment and a note.
func Merge(all []Declared) (active *repoconfig.Experiment, note string) {
	byID := map[string]repoconfig.Experiment{}
	var first *repoconfig.Experiment
	for _, d := range all {
		e := d.Experiment
		if prev, ok := byID[e.ID]; ok {
			if prev != e {
				return nil, "experiment " + strconv.Quote(e.ID) + " is declared differently across repos: no assignment"
			}
			continue
		}
		byID[e.ID] = e
		if first == nil {
			first = &e
			continue
		}
		if first.Component != e.Component {
			return nil, "experiments on different components: no assignment"
		}
		return nil, "more than one experiment on component " + strconv.Quote(e.Component) + ": no assignment"
	}
	return first, ""
}

// Hash maps repo and issue to a stable value in [0,1): the top 53 bits of the first 8 bytes of
// sha256(repo + "#" + issue), so the result is exact in a float64 and can never round to 1.
func Hash(repo string, issue int) float64 {
	sum := sha256.Sum256([]byte(repo + "#" + strconv.Itoa(issue)))
	return float64(binary.BigEndian.Uint64(sum[:8])>>11) / (1 << 53)
}

// Decide assigns a trajectory. Without an experiment, before its start, once variantCount has
// reached the sample, or when Hash is not strictly below Share, it is standard.
func Decide(exp *repoconfig.Experiment, note string, repo string, issue int, variantCount int, now time.Time) Decision {
	return DecideWith(Hash, exp, note, repo, issue, variantCount, now)
}

// DecideWith is Decide with the hash function given, so a test can fix the draw.
func DecideWith(hash func(string, int) float64, exp *repoconfig.Experiment, note string, repo string, issue int, variantCount int, now time.Time) Decision {
	std := Decision{Assignment: standard, Note: note}
	if exp == nil || now.Before(exp.Start) || variantCount >= exp.Sample || !(hash(repo, issue) < exp.Share) {
		return std
	}
	return Decision{Assignment: exp.Assignment(), ExperimentID: exp.ID, Note: note}
}
