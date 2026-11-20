package main

import "strings"

// Domains in the order they are printed.
var domains = [...]string{"web", "python", "go"}

// Outside web/, but a change here can break the web checks: the CI job, this command, and the
// gate command in lighthouse.toml.
var webToo = map[string]bool{
	".github/workflows/ci.yml": true,
	"ci/domains/domains.go":    true,
	"ci/domains/main.go":       true,
	"lighthouse.toml":          true,
}

// Inside web/, but read by Go: the API snapshot test and the module that keeps `./...` out of
// web/node_modules.
var goToo = map[string]bool{"web/openapi.json": true, "web/go.mod": true}

// Inside docs/, but read by Go: internal/config/config_test.go parses the GitLab example in it.
var goDocs = map[string]bool{"docs/install.md": true}

// Inside tests/, but read by Go: internal/protocol/header_test.go reads the fixtures.
const goFixtures = "tests/pack/fixtures/"

// Outside web/, but read by no Go build or test, so they leave out go (python stays: pytest reads
// docs/ and tests/, and ci/literal-grep.sh runs in that job, on pack/ and on the Go sources).
// Every other unknown path still touches go. Measured on main (c72662c, #130): go:embed reads
// only internal/web (dist, placeholder), pack/*/* and internal/store/migrations; no .go file
// names docs/, changelog.d or ci/literal- (TestGoSourcesDoNotReadNotGoPaths fails when one does, except
// the exemptions in withoutExemptions); the Go and e2e tests read outside their own package only
// tests/pack/fixtures/, internal/usage/testdata/ and temporary repos; the literal lists are read
// only by ci/literal-grep.sh and tools/gen_code_language_freq.py.
// Measured again for #151: from tests/ Go reads only tests/pack/fixtures/; the other tests/ and
// README.md strings in Go are files in temporary repos. Only the loose root .md files (README.md,
// CHANGELOG.md, CONTRIBUTING.md, SECURITY.md, CLA.md) leave go; AGENTS.md and CLAUDE.md stay, the
// dogfood check reads them. pack/ is embedded. TestGoSourcesDoNotReadTestsOrRootMD fails when Go starts to read
// one of them. Measured again for #163: no .go file names CONTRIBUTING, SECURITY or CLA.md.
var notGoDirs = []string{"docs/", "changelog.d/", "tests/"}

var notGo = map[string]bool{
	"ci/literal-denylist.txt":    true,
	"ci/literal-denylist-go.txt": true,
	"ci/literal-allowlist.txt":   true,
	"README.md":                  true,
	"CHANGELOG.md":               true,
	"CONTRIBUTING.md":            true,
	"SECURITY.md":                true,
	"CLA.md":                     true,
}

func isNotGo(path string) bool {
	if strings.HasPrefix(path, goFixtures) {
		return false
	}
	for _, d := range notGoDirs {
		if strings.HasPrefix(path, d) {
			return true
		}
	}
	return notGo[path]
}

// Classify returns the domains (web, python, go) the paths touch; every key is present.
func Classify(paths []string) map[string]bool {
	touched := map[string]bool{"web": false, "python": false, "go": false}
	for _, p := range paths {
		inWeb := strings.HasPrefix(p, "web/")
		if inWeb || webToo[p] {
			touched["web"] = true
		}
		if (!inWeb && !isNotGo(p)) || goToo[p] || goDocs[p] {
			touched["go"] = true
		}
		if !inWeb {
			touched["python"] = true
		}
	}
	return touched
}
