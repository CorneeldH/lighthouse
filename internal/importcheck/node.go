package importcheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
)

// NodeChecker checks a Node project, an Expo one included, with knip.
type NodeChecker struct{}

// knipReason is the reason of the one blocking category; the other knip categories are left
// out on purpose, they are another axis than undeclared imports.
const knipReason = "package is imported but missing from package.json" // lighthouse: allow-language — name of the tool

// Check runs knip in the project root; it never installs anything.
func (NodeChecker) Check(p Project, env Env) ([]Finding, error) {
	unchecked := func(format string, a ...any) error {
		return UncheckedError(fmt.Sprintf(format, a...))
	}
	dir := filepath.Join(env.Root, filepath.FromSlash(p.Dir))
	if info, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil || !info.IsDir() {
		return nil, unchecked("node_modules missing in %s", p.Dir)
	}
	if _, err := env.LookPath("npx"); err != nil {
		return nil, unchecked("npx not found on PATH, needed in %s: %v", p.Dir, err)
	}
	argv := []string{"npx", "--yes", "knip@" + KnipVersion, "--include", "unlisted", "--reporter", "json"}
	stdout, stderr, code, err := env.Exec(dir, argv, nil)
	if err != nil {
		return nil, unchecked("knip could not start in %s: %v", p.Dir, err)
	}
	// knip exits 0 (clean) or 1 (findings, but also an error): the JSON decides, and an exit
	// other than 0 or 1 is a failure to run.
	findings, perr := parseKnip(stdout)
	if code != 0 && code != 1 {
		return nil, unchecked("knip failed in %s: exit %d: %s", p.Dir, code, string(stderr))
	}
	if perr != nil {
		return nil, unchecked("cannot read the knip output of %s: %v: %s", p.Dir, perr, string(stderr))
	}
	for i := range findings {
		findings[i].File = path.Join(p.Dir, findings[i].File)
	}
	return findings, nil
}

type knipReport struct { // lighthouse: allow-language — name of the tool
	Issues *[]struct {
		File     string `json:"file"`
		Unlisted []struct {
			Name string `json:"name"`
			Line int    `json:"line"`
		} `json:"unlisted"`
	} `json:"issues"`
}

// parseKnip reads `knip --reporter json` into the blocking findings (ID "unlisted"). File stays
// as knip reports it, relative to its working directory; a missing line becomes 1.
func parseKnip(out []byte) ([]Finding, error) { // lighthouse: allow-language — name of the tool
	var r knipReport
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	if r.Issues == nil {
		return nil, fmt.Errorf("no issues list in the output")
	}
	var findings []Finding
	for _, issue := range *r.Issues {
		for _, u := range issue.Unlisted {
			line := u.Line
			if line < 1 {
				line = 1
			}
			findings = append(findings, Finding{
				File: filepath.ToSlash(issue.File), Line: line,
				ID: "unlisted", Import: u.Name, Reason: knipReason,
			})
		}
	}
	return findings, nil
}
