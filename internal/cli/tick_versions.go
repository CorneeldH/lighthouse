package cli

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/telemetry"
	"github.com/CorneeldH/lighthouse/internal/telemetry/collect"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// versionCheckEvery is how often the tick looks for version changes to propose.
const versionCheckEvery = time.Hour

// lastVersionCheckFile is the file under <state>/telemetry that holds the moment of the last
// look, successful or not.
const lastVersionCheckFile = "last-version-check"

// versionSpec is the design that says a version change is a trigger; the issue points to it.
const versionSpec = "docs/developer/planning/specs/2026-11-24-issue-388-explore-exploit-design.md"

// versionMarker is the full comment that marks the issue of a change; FindByMarker searches
// for all of it, through " -->", so a version that is the prefix of another does not match.
func versionMarker(c telemetry.VersionChange) string {
	parts := []string{string(c.Dimension)}
	if c.Series != "" {
		parts = append(parts, c.Series)
	}
	parts = append(parts, "→"+c.New)
	return "<!-- lighthouse-version-change: " + strings.Join(parts, " ") + " -->"
}

// proposeVersionChanges opens, in the proposals repo of tel, one issue per latest version
// change of each series, unless an issue (open or closed) already carries its marker. It runs
// at most once an hour; the moment is written also after a failed look, so a log line shows
// at most once an hour. No proposals_repo, or one that is no repo of [dispatch], is one log
// line and no failure; a forge that did not open this tick is a failure. A failing change gets
// a log line and the others go on. failed reports a failure.
func proposeVersionChanges(ctx context.Context, env tickEnv, db *sql.DB, tel hostconfig.Telemetry,
	repos []repoconfig.Repo, ticked map[string]forge.Forge, errOut io.Writer) (failed bool) {
	stateDir, err := collect.StateDir(env.Getenv)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "lighthouse: version change: %s\n", oneLine(err))
		return true
	}
	now := env.now()
	if last := readTimeFile(stateDir, lastVersionCheckFile); !last.IsZero() && now.Sub(last) < versionCheckEvery {
		return false
	}
	defer func() {
		if err := workplace.WriteFileAtomic(filepath.Join(stateDir, "telemetry", lastVersionCheckFile),
			[]byte(now.UTC().Format(time.RFC3339Nano)+"\n")); err != nil {
			_, _ = fmt.Fprintf(errOut, "lighthouse: version change: %s: %s\n", lastVersionCheckFile, oneLine(err))
			failed = true
		}
	}()

	if tel.ProposalsRepo == "" {
		_, _ = fmt.Fprintln(errOut, "lighthouse: version change: no [telemetry] proposals_repo in host.toml: no proposals")
		return false
	}
	i := slices.IndexFunc(repos, func(r repoconfig.Repo) bool { return r.Slug == tel.ProposalsRepo })
	if i < 0 {
		_, _ = fmt.Fprintf(errOut, "lighthouse: version change: proposals_repo %s is no repo of [dispatch]: no proposals\n", tel.ProposalsRepo)
		return false
	}
	f, ok := ticked[tel.ProposalsRepo]
	if !ok {
		_, _ = fmt.Fprintf(errOut, "lighthouse: version change: %s has no forge this tick\n", tel.ProposalsRepo)
		return true
	}
	changes, err := telemetry.VersionChanges(ctx, db)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "lighthouse: version change: %s\n", oneLine(err))
		return true
	}
	language := repos[i].Language
	for _, c := range telemetry.LatestProposals(changes) {
		marker := versionMarker(c)
		found, err := f.FindByMarker(ctx, marker)
		if err == nil && len(found) == 0 {
			title, body := versionIssueText(language, c, marker)
			_, err = f.CreateIssue(ctx, forge.NewIssue{Title: title, Body: body})
		}
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "lighthouse: version change: %s: %s\n", marker, oneLine(err))
			failed = true
		}
	}
	return failed
}

// versionIssueText is the title and body of the issue of c in language ("nl", else English).
func versionIssueText(language string, c telemetry.VersionChange, marker string) (title, body string) {
	what := string(c.Dimension)
	if c.Series != "" {
		what += " " + c.Series
	}
	change := fmt.Sprintf("%s %s → %s", what, c.Old, c.New)
	first := "-"
	switch {
	case c.RunID.Valid:
		first = fmt.Sprintf("run %d", c.RunID.Int64)
	case c.SessionID.Valid:
		first = fmt.Sprintf("session %d", c.SessionID.Int64)
	}
	at := c.At.UTC().Format(time.RFC3339)
	components := strings.Join(telemetry.ComponentsFor(c.Dimension), ", ")
	if language == "nl" {
		first = strings.Replace(first, "session", "sessie", 1)
		title = "Versiewissel: " + change
		body = fmt.Sprintf("Versiewissel gezien: %s.\n\n"+
			"- Eerste %s met de nieuwe versie, gestart op %s.\n"+
			"- Componenten die erdoor kunnen veranderen: %s.\n"+
			"- Reden: een versiewissel is een trigger volgens #388, zie [de spec](%s).\n\n"+
			"Lighthouse start de meting niet zelf: beslis hier of de componenten opnieuw gemeten worden.\n\n%s\n",
			change, first, at, components, versionSpec, marker)
		return title, body
	}
	title = "Version change: " + change
	body = fmt.Sprintf("Version change seen: %s.\n\n"+
		"- First %s with the new version, started at %s.\n"+
		"- Components it can influence: %s.\n"+
		"- Reason: a version change is a trigger according to #388, see [the spec](%s).\n\n"+
		"Lighthouse does not start the measurement itself: decide here whether the components are measured again.\n\n%s\n",
		change, first, at, components, versionSpec, marker)
	return title, body
}
