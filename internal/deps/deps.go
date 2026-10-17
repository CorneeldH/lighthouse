// Package deps checks the tools a package needs (spec, init punt 1). A missing tool is
// reported with the units that need it; it does not stop the installation.
package deps

import (
	"os/exec"

	"github.com/CorneeldH/lighthouse/internal/packs"
)

// Missing is a tool that is not there, with the units that need it.
type Missing struct {
	Tool     string
	NeededBy []string
}

// Checker looks tools up. The fields are replaceable for tests.
type Checker struct {
	LookPath func(string) (string, error)
	Run      func(name string, args ...string) error
}

// Default checks the real PATH.
func Default() Checker {
	return Checker{
		LookPath: exec.LookPath,
		Run:      func(name string, args ...string) error { return exec.Command(name, args...).Run() },
	}
}

// Check returns the requirements that are not met, in the order of reqs. `tomllib`
// means a python3 that can import it (Python 3.11 or newer); any other name is a
// program on PATH.
func (c Checker) Check(reqs []packs.Requirement) []Missing {
	var missing []Missing
	for _, r := range reqs {
		if !c.present(r.Tool) {
			missing = append(missing, Missing{Tool: r.Tool, NeededBy: r.NeededBy})
		}
	}
	return missing
}

func (c Checker) present(tool string) bool {
	if tool == "tomllib" {
		if _, err := c.LookPath("python3"); err != nil {
			return false
		}
		// -I: isolated mode, so a tomllib.py in the user's cwd is never imported.
		return c.Run("python3", "-I", "-c", "import tomllib") == nil
	}
	_, err := c.LookPath(tool)
	return err == nil
}
