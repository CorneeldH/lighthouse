package repoconfig

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/internal/cron"
)

// Schedule is one [[schedule]] block. Params stay untyped: the job validates them after Load.
type Schedule struct {
	Name, Job, Cron string
	// Weeks and Anchor are set together or both zero: only the weeks that are a multiple of
	// Weeks after the week of Anchor count. Anchor is midnight local time.
	Weeks  int
	Anchor time.Time
	// Profile is the profile of the run; the [controller] profile when the block has none.
	Profile string
	Params  map[string]any
}

type rawSchedule struct {
	Name    string         `toml:"name"`
	Job     string         `toml:"job"`
	Cron    string         `toml:"cron"`
	Weeks   *int           `toml:"weeks"`
	Anchor  string         `toml:"anchor"`
	Profile string         `toml:"profile"`
	Params  map[string]any `toml:"params"`
}

// scheduleJobs are the jobs a schedule can name.
var scheduleJobs = []string{"map-refresh", "reflect"}

// scheduleName is a branch-name-safe key.
var scheduleName = regexp.MustCompile(`^[a-z0-9-]+$`)

// schedulesFrom validates the [[schedule]] blocks and fills Profile. It reports every problem,
// naming the block by its position and, when it has one, its name.
func schedulesFrom(raws []rawSchedule, md toml.MetaData, c *Controller) ([]Schedule, error) {
	var errs []error
	for _, k := range md.Undecoded() {
		if key := k.String(); key == "schedule" || strings.HasPrefix(key, "schedule.") {
			errs = append(errs, fmt.Errorf("unknown key %s", key))
		}
	}
	defaultProfile := "software"
	if c != nil {
		defaultProfile = c.Profile
	}
	var out []Schedule
	seen := map[string]bool{}
	for i, r := range raws {
		at := fmt.Sprintf("schedule[%d]", i)
		if r.Name != "" {
			at = fmt.Sprintf("schedule[%d] %q", i, r.Name)
		}
		fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(at+": "+format, a...)) }
		switch {
		case r.Name == "":
			fail("name is required")
		case !scheduleName.MatchString(r.Name):
			fail("name %q must match [a-z0-9-]+", r.Name)
		case seen[r.Name]:
			fail("duplicate name %q", r.Name)
		}
		seen[r.Name] = true
		if !slices.Contains(scheduleJobs, r.Job) {
			fail("job %q is not one of %s", r.Job, strings.Join(scheduleJobs, ", "))
		}
		if _, err := cron.Parse(r.Cron); err != nil {
			fail("cron %q: %v", r.Cron, err)
		}
		s := Schedule{Name: r.Name, Job: r.Job, Cron: r.Cron, Profile: r.Profile, Params: r.Params}
		if s.Profile == "" {
			s.Profile = defaultProfile
		}
		switch {
		case r.Weeks != nil && r.Anchor == "":
			fail("weeks needs anchor")
		case r.Weeks == nil && r.Anchor != "":
			fail("anchor needs weeks")
		case r.Weeks != nil && *r.Weeks < 1:
			fail("weeks must be at least 1")
		case r.Weeks != nil:
			a, err := time.ParseInLocation(time.DateOnly, r.Anchor, time.Local)
			if err != nil {
				fail("anchor %q is not a date YYYY-MM-DD", r.Anchor)
				break
			}
			s.Weeks, s.Anchor = *r.Weeks, a
		}
		out = append(out, s)
	}
	return out, errors.Join(errs...)
}
