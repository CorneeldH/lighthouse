// Package labels creates the labels of the taxonomy file on the forge (GitHub or GitLab), or
// prints the commands that would.
//
// The taxonomy file is `[paths] labels` (default projectconfig.DefaultLabelsPath). Every Markdown
// table with a label column (`Label` or `Area`) and a colour column (`Color`, `Colour` or
// `Kleur`, case does not matter) counts. A label is between backticks, a colour is six hex
// characters, also between backticks. Lines that do not start with `|` are skipped, so an
// example row in an HTML comment does not count.
//
// Norms: never --force, a label that exists on the forge stays as it is (name compared without
// regard to case, a name is never changed); every forge outcome (no forge, no token, no rights,
// a failed create) returns nil and prints what could not be done as a
// `lighthouse forge label create` line, which works on both forges. A missing or empty taxonomy file or a broken
// config is an error whose text is already on errw.
package labels

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/projectconfig"
)

// Label is one row of the taxonomy: a name and an upper-case six-hex colour.
type Label struct{ Name, Color string }

var (
	colorRE = regexp.MustCompile("^`([0-9A-Fa-f]{6})`$")
	nameRE  = regexp.MustCompile("^`([^`]+)`$")

	nameHeaders  = []string{"label", "area"}
	colorHeaders = []string{"color", "colour", "kleur"}
)

func cells(line string) []string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

func indexOf(lowered, headers []string) int {
	for _, h := range headers {
		for i, c := range lowered {
			if c == h {
				return i
			}
		}
	}
	return -1
}

// ParseTaxonomy returns the labels of every table with a label column and a colour column,
// in document order; headers match case-insensitively and colours come back upper-case.
func ParseTaxonomy(text string) []Label {
	var found []Label
	nameI, colorI := -1, -1
	inTable := false
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "|") {
			inTable = false
			continue
		}
		cs := cells(line)
		if !inTable {
			lowered := make([]string, len(cs))
			for i, c := range cs {
				lowered[i] = strings.ToLower(c)
			}
			n, c := indexOf(lowered, nameHeaders), indexOf(lowered, colorHeaders)
			if n >= 0 && c >= 0 {
				nameI, colorI, inTable = n, c, true
			}
			continue
		}
		if strings.Trim(strings.Join(cs, ""), "-: ") == "" {
			continue
		}
		if max(nameI, colorI) >= len(cs) {
			continue
		}
		name, color := nameRE.FindStringSubmatch(cs[nameI]), colorRE.FindStringSubmatch(cs[colorI])
		if name != nil && color != nil {
			found = append(found, Label{name[1], strings.ToUpper(color[1])})
		}
	}
	return found
}

// createArgs is the `lighthouse forge label create` command line of l, program name included.
func createArgs(l Label) []string {
	return []string{"lighthouse", "forge", "label", "create", l.Name, "--color", l.Color}
}

var safeRE = regexp.MustCompile(`^[A-Za-z0-9@%+=:,./_-]+$`)

// shellJoin quotes args the way Python's shlex.join does, so a line can be pasted.
func shellJoin(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		if safeRE.MatchString(a) {
			q[i] = a
		} else {
			q[i] = "'" + strings.ReplaceAll(a, "'", `'"'"'`) + "'"
		}
	}
	return strings.Join(q, " ")
}

// Sync reads the taxonomy file of the repository in root and creates the missing labels on
// the forge f (create), or prints the `lighthouse forge label create` lines of the missing ones.
// A nil f means the forge cannot be opened (no token, no forge.repo): every label is then
// missing and printed. A returned error has already been written to errw.
func Sync(root string, create bool, f forge.Forge, out, errw io.Writer) error {
	fail := func(format string, a ...any) error {
		msg := fmt.Sprintf(format, a...)
		_, _ = fmt.Fprintf(errw, "labels: ERROR — %s\n", msg)
		return errors.New(msg)
	}
	cfg, err := config.Load(filepath.Join(root, "lighthouse.toml"))
	if err != nil {
		return fail("%v", err)
	}
	rel := projectconfig.DefaultLabelsPath
	if v, err := config.Lookup(cfg, "paths.labels"); err == nil {
		if s, ok := v.(string); ok {
			rel = s
		}
	}
	taxonomy := filepath.Join(root, rel)
	data, err := os.ReadFile(taxonomy)
	if err != nil {
		return fail("cannot read %s: %v", taxonomy, err)
	}
	wanted := ParseTaxonomy(string(data))
	if len(wanted) == 0 {
		return fail("no labels found in %s", taxonomy)
	}

	repo := cfg.Repo // forge.repo
	var remote []string
	remoteKnown := false
	reason := "no forge.repo configured"
	if repo != "" {
		reason = "no forge to open: is a token set?"
	}
	if f != nil {
		remote, err = f.Labels(context.Background())
		remoteKnown = err == nil
		if err != nil {
			reason = err.Error()
		}
	}
	missing := wanted
	if remoteKnown {
		have := map[string]bool{}
		for _, n := range remote {
			have[strings.ToLower(n)] = true
		}
		missing = nil
		for _, l := range wanted {
			if !have[strings.ToLower(l.Name)] {
				missing = append(missing, l)
			}
		}
	}
	if len(missing) == 0 {
		_, _ = fmt.Fprintf(out, "labels: all %d present on %s\n", len(wanted), repo)
		return nil
	}

	var lines [][]string
	for _, l := range missing {
		args := createArgs(l)
		if create && remoteKnown {
			if err := f.CreateLabel(context.Background(), l.Name, l.Color); err == nil {
				_, _ = fmt.Fprintf(out, "labels: created %s\n", l.Name)
				continue
			}
		}
		lines = append(lines, args)
	}
	if len(lines) == 0 {
		return nil
	}
	if !remoteKnown {
		target := repo
		if target == "" {
			target = "the repository"
		}
		_, _ = fmt.Fprintf(out, "labels: could not read the labels of %s (%s); create the missing ones with:\n", target, reason)
	} else {
		_, _ = fmt.Fprintf(out, "labels: missing on %s; create them with:\n", repo)
	}
	for _, args := range lines {
		_, _ = fmt.Fprintln(out, shellJoin(args))
	}
	return nil
}
