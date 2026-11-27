package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/datadir"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/review/compare"
	"github.com/CorneeldH/lighthouse/internal/store"
	telreport "github.com/CorneeldH/lighthouse/internal/telemetry/report"
)

func newCompareReviewersCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "compare-reviewers",
		Short: "Measure what the second reviewer adds to the first (comparison 1 of #573)",
		Long: "Reads the paired Codex review records of the data directory and the recovery after merge\n" +
			"of their trajectories in lighthouse.db, and prints a markdown report: records kept and\n" +
			"dropped, findings per source, the 90% test with its Wilson interval, and a table per\n" +
			"feature class. A class with fewer than 10 trajectories per group gets `geen conclusie`.\n" +
			"Opens the database read-only (no migration, no file is created); it changes no record and no model, skill or harness.\n\n" +
			"Exit 0 measured | 1 read or database error | 2 wrong usage.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return usageErr(fmt.Errorf("%s: takes no arguments, got %d", cmd.CommandPath(), len(args)))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			root, err := datadir.Dir(os.Getenv)
			if err != nil {
				return err
			}
			kept, dropped, err := compare.Select(root, servedRepos(hostconfig.Path()))
			if err != nil {
				return err
			}
			dbPath, err := store.DefaultPath()
			if err != nil {
				return err
			}
			st, err := store.OpenReadOnly(ctx, dbPath)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return fmt.Errorf("no database at %s: run `lighthouse review-index` first: %w", dbPath, err)
				}
				return err
			}
			defer func() { _ = st.Close() }()
			ds, err := telreport.Load(ctx, st.DB, time.Now())
			if err != nil {
				return err
			}
			ts := make([]*telreport.Trajectory, len(ds.Trajectories))
			for i := range ds.Trajectories {
				ts[i] = &ds.Trajectories[i]
			}
			if len(ds.Missing) > 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Let op: tabellen ontbreken in de database (%s); het herstel na merge is niet gemeten.\n\n", strings.Join(ds.Missing, ", "))
			}
			printComparison(cmd.OutOrStdout(), kept, countMissingTrajectories(kept, ts), dropped, compare.Tally(kept), compare.ByClass(kept, ts))
			return nil
		},
	}
}

func pct(x float64) string { return fmt.Sprintf("%.0f%%", x*100) }

func printCapture(w io.Writer, name string, c compare.Capture) {
	if !c.Defined {
		_, _ = fmt.Fprintf(w, "- %s: niet gedefinieerd (geen overgenomen BLOCKER- of MAJOR-bevindingen).\n", name)
		return
	}
	verdict := "onder de 90%"
	if c.Low >= 0.9 {
		verdict = "90% gehaald"
	} else if c.High >= 0.9 {
		verdict = "90% niet uitgesloten, niet bevestigd"
	}
	_, _ = fmt.Fprintf(w, "- %s: %d van %d overgenomen BLOCKER/MAJOR (%s), Wilson 95%%-interval %s tot %s: %s.\n",
		name, c.Caught, c.Adopted, pct(float64(c.Caught)/float64(c.Adopted)), pct(c.Low), pct(c.High), verdict)
}

func printComparison(w io.Writer, kept []compare.Record, missingTraj int, d compare.Dropped, res compare.Result, classes []compare.Class) {
	totalDropped := d.Fallback + d.OtherMode + d.NoCodexVersion + d.NotPaired + d.NotFinished + d.Unreadable
	_, _ = fmt.Fprintf(w, "# Vergelijking 1: wat voegt de tweede reviewer toe\n\n")
	_, _ = fmt.Fprintf(w, "Geselecteerd: %d records. Afgevallen: %d.\n\n", len(kept), totalDropped)
	_, _ = fmt.Fprintf(w, "Gekoppelde records zonder traject in de database (tellen voor bevindingen, niet voor herstel): %d.\n\n", missingTraj)
	_, _ = fmt.Fprintf(w, "| Reden van afvallen | Aantal |\n|---|---|\n")
	for _, r := range []struct {
		name string
		n    int
	}{
		{"fallback (review_mode fallback)", d.Fallback},
		{"andere review_mode", d.OtherMode},
		{"niet gepaard", d.NotPaired},
		{"zonder reviewer.codex_version", d.NoCodexVersion},
		{"nieuwste poging niet afgerond", d.NotFinished},
		{"onleesbaar", d.Unreadable},
	} {
		_, _ = fmt.Fprintf(w, "| %s | %d |\n", r.name, r.n)
	}

	_, _ = fmt.Fprintf(w, "\n## Bevindingen per bron\n\n")
	_, _ = fmt.Fprintf(w, "Ernst per bron zelf (ronde 1). `fallback` is Opus; de rij `both` telt de ernst van Codex.\n\n")
	_, _ = fmt.Fprintf(w, "| Bron | Bevindingen | BLOCKER | MAJOR | MINOR | Overgenomen | Afgewezen | Open |\n|---|---|---|---|---|---|---|---|\n")
	for _, s := range []string{"codex", "fallback", "both"} {
		c := res.Sources[s]
		_, _ = fmt.Fprintf(w, "| %s | %d | %d | %d | %d | %d | %d | %d |\n", s, c.N,
			c.BySource["BLOCKER"], c.BySource["MAJOR"], c.BySource["MINOR"], c.Adopted, c.Rejected, c.LeftOpen)
	}

	_, _ = fmt.Fprintf(w, "\n## Toets: alleen Codex vangt minstens 90%%\n\n")
	_, _ = fmt.Fprintf(w, "Noemer: overgenomen bevindingen die BLOCKER of MAJOR zijn voor minstens een reviewer.\n\n")
	printCapture(w, "Alleen Codex", res.CodexOnlyBM)
	printCapture(w, "Alleen Opus", res.OpusOnlyBM)

	_, _ = fmt.Fprintf(w, "\n## Per kenmerkklasse\n\n")
	_, _ = fmt.Fprintf(w, "| Kenmerk | Klasse | Trajecten | Bevindingen | Alleen Codex vangt | Herstel na merge | Conclusie |\n|---|---|---|---|---|---|---|\n")
	for _, c := range classes {
		findings := 0
		for _, s := range c.Result.Sources {
			findings += s.N
		}
		catch := "niet gedefinieerd"
		if cc := c.Result.CodexOnlyBM; cc.Defined {
			catch = fmt.Sprintf("%d van %d", cc.Caught, cc.Adopted)
		}
		concl := "geen conclusie"
		if c.Conclusion {
			concl = "gemeten"
		}
		recov := "niet gemeten"
		if c.Measured > 0 {
			recov = fmt.Sprintf("%d van %d gemeten: %d hersteld, %d open", c.Measured, c.Trajectories, c.Recovered, c.Open)
		}
		_, _ = fmt.Fprintf(w, "| %s | %s | %d | %d | %s | %s | %s |\n",
			c.Key, c.Value, c.Trajectories, findings, catch, recov, concl)
	}
	if len(classes) == 0 {
		_, _ = fmt.Fprintf(w, "| - | - | 0 | 0 | niet gedefinieerd | niet gemeten | geen conclusie |\n")
	}

	_, _ = fmt.Fprintf(w, "\n## Fallbacks en niet-toegepaste indelingen\n\n")
	_, _ = fmt.Fprintf(w, "- Fallbacks (afgevallen): %d.\n", d.Fallback)
	notApplied := map[string]int{}
	for _, r := range kept {
		if r.AssignmentApplied != nil && !*r.AssignmentApplied {
			name := r.Assignment
			if name == "" {
				name = "onbekend"
			}
			notApplied[name]++
		}
	}
	if len(notApplied) == 0 {
		_, _ = fmt.Fprintf(w, "- Niet-toegepaste indelingen (assignment_applied: false): 0.\n")
		return
	}
	names := make([]string, 0, len(notApplied))
	for n := range notApplied {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		_, _ = fmt.Fprintf(w, "- Niet-toegepaste indeling `%s` (assignment_applied: false): %d.\n", n, notApplied[n])
	}
}

// countMissingTrajectories counts the linked records whose trajectory is not in the database.
func countMissingTrajectories(kept []compare.Record, ts []*telreport.Trajectory) int {
	have := map[string]bool{}
	for _, t := range ts {
		have[fmt.Sprintf("%s#%d", t.Repo, t.Issue)] = true
	}
	n := 0
	for _, r := range kept {
		if r.Linked && !have[fmt.Sprintf("%s#%d", r.Repo, r.Issue)] {
			n++
		}
	}
	return n
}
