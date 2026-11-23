package levels

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// InstalledPath is where a repo records what init installed.
const InstalledPath = ".lighthouse/components.toml"

// Installed is the content of components.toml.
type Installed struct {
	Level      string
	Since      string // RFC3339
	Components map[string]Stand
}

type rawInstalled struct {
	Level      string           `toml:"level"`
	Since      string           `toml:"since"`
	Components map[string]Stand `toml:"components"`
}

// Stand returns the stand of component; an unlisted component is on.
func (i Installed) Stand(component string) Stand {
	if s, ok := i.Components[component]; ok {
		return s
	}
	return StandOn
}

// ReadInstalled reads root/.lighthouse/components.toml. The bool is false when
// the file is absent (everything on); an error means present but unreadable.
func ReadInstalled(root string) (Installed, bool, error) {
	data, err := os.ReadFile(filepath.Join(root, InstalledPath))
	if errors.Is(err, fs.ErrNotExist) {
		return Installed{}, false, nil
	}
	if err != nil {
		return Installed{}, false, fmt.Errorf("read %s: %w", InstalledPath, err)
	}
	var raw rawInstalled
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return Installed{}, false, fmt.Errorf("parse %s: %w", InstalledPath, err)
	}
	return Installed(raw), true, nil
}

// Marshal renders the file deterministically; only non-on components are listed.
func (i Installed) Marshal() []byte {
	var b strings.Builder
	b.WriteString("# Managed by lighthouse init. Do not edit by hand.\n")
	fmt.Fprintf(&b, "level = %s\n", strconv.Quote(i.Level))
	fmt.Fprintf(&b, "since = %s\n", strconv.Quote(i.Since))
	names := make([]string, 0, len(i.Components))
	for c, s := range i.Components {
		if s != StandOn {
			names = append(names, c)
		}
	}
	sort.Strings(names)
	if len(names) > 0 {
		b.WriteString("\n[components]\n")
	}
	for _, c := range names {
		fmt.Fprintf(&b, "%s = %s\n", strconv.Quote(c), strconv.Quote(string(i.Components[c])))
	}
	return []byte(b.String())
}

// BaseHooks are the hooks that no stand switches and that never read components.toml: they
// cannot be off, on log, or blocked by a corrupt file. It mirrors the hook entries of `base` in
// pack/core/levels.toml (a test keeps the two equal).
var BaseHooks = []string{"usage-gate", "permission-check", "workflow-push-gate", "level-check", "no-verify-check"}
