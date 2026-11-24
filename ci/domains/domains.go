package main

import "strings"

// Domains in the order they are printed.
var domains = [...]string{"web", "pack-python", "go"}

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

// Inside docs/, but read by Go: internal/config/config_test.go parses the GitLab example in it;
// the tests of internal/repopins and e2e/gettingstarted_test.go read the getting-started guide,
// the agent install page and the site files. The direction documents are read by the shape test
// of internal/direction (strategie.md, uitgangspunten.md, labels.md and everything under areas/):
// without them a PR that only breaks one of those files would merge green, as no Go check ran.
var goDocs = map[string]bool{
	"docs/install.md":             true,
	"docs/getting-started.md":     true,
	"docs/site/agents/install.md": true,
	"docs/site/install.html":      true,
	"docs/site/index.html":        true,
	"docs/site/download.js":       true,

	"docs/developer/strategie.md":      true,
	"docs/developer/uitgangspunten.md": true,
	"docs/developer/labels.md":         true,
}

// Directories inside docs/ that Go reads whole; a prefix, so areas-extra.md does not match.
var goDocPrefixes = []string{"docs/developer/areas/"}

func isGoDoc(path string) bool {
	if goDocs[path] {
		return true
	}
	for _, d := range goDocPrefixes {
		if strings.HasPrefix(path, d) {
			return true
		}
	}
	return false
}

// Inside tests/, but read by Go: internal/protocol/header_test.go reads the fixtures, and
// internal/canary/bashwrite_test.go reads the ruff canary file that tests/pack/python shares.
var goFixtures = []string{"tests/pack/fixtures/", "tests/pack/python/testdata/ruff-format-check/"}

// Outside web/, but read by no Go build or test, so they leave out go (and pack-python, which
// lists its own inputs below; ci/literal-grep.sh runs in the go job, on pack/ and on the Go sources).
// Every other unknown path still touches go. Measured on main (c72662c, #130): go:embed reads
// only internal/web (dist, placeholder), pack/*/* and internal/store/migrations; no .go file
// names docs/, changelog.d or ci/literal- (TestGoSourcesDoNotReadNotGoPaths fails when one does, except
// the exemptions in withoutExemptions); the Go and e2e tests read outside their own package only
// tests/pack/fixtures/, internal/usage/testdata/ and temporary repos; the literal lists are read
// by ci/literal-grep.sh, tools/gen_code_language_freq.py and, as the input of a black-box run,
// e2e/literalgrep_test.go.
// Measured again for #151: from tests/ Go reads only tests/pack/fixtures/; the other tests/ and
// README.md strings in Go are files in temporary repos. Only the loose root .md files (README.md,
// CHANGELOG.md, CONTRIBUTING.md, SECURITY.md, CLA.md) leave go; AGENTS.md and CLAUDE.md stay, the
// dogfood check reads them. pack/ is embedded. TestGoSourcesDoNotReadTestsOrRootMD fails when Go starts to read
// one of them. Measured again for #163: no .go file names CONTRIBUTING, SECURITY or CLA.md.
var notGoDirs = []string{"docs/", "changelog.d/", "tests/"}

var notGo = map[string]bool{
	"README.md":       true,
	"CHANGELOG.md":    true,
	"CONTRIBUTING.md": true,
	"SECURITY.md":     true,
	"CLA.md":          true,
}

func isNotGo(path string) bool {
	for _, f := range goFixtures {
		if strings.HasPrefix(path, f) {
			return false
		}
	}
	for _, d := range notGoDirs {
		if strings.HasPrefix(path, d) {
			return true
		}
	}
	return notGo[path]
}

// What the pack-python job reads: the pytest suite and the Python of the pack, the packs and Go
// sources whose built binary the suite runs, pack/go/ and pack/node/ (test_imports_gate_hooks.py
// installs and runs their imports gates), e2e/ (the uv e2e tests run in that job) and the
// inputs of the job itself: the Python lock files, ci.yml (the job's own definition) and
// go.mod/go.sum (the inputs of the binary the job builds).
var packPythonPrefixes = []string{"pack/python/", "tests/pack/python/", "pack/core/", "pack/go/", "pack/node/", "cmd/", "internal/", "e2e/"}
var packPythonFiles = map[string]bool{"pyproject.toml": true, "uv.lock": true, ".github/workflows/ci.yml": true, "go.mod": true, "go.sum": true}

func isPackPython(path string) bool {
	if packPythonFiles[path] {
		return true
	}
	for _, d := range packPythonPrefixes {
		if strings.HasPrefix(path, d) {
			return true
		}
	}
	return false
}

// Classify returns the domains (web, pack-python, go) the paths touch; every key is present.
func Classify(paths []string) map[string]bool {
	touched := map[string]bool{"web": false, "pack-python": false, "go": false}
	for _, p := range paths {
		inWeb := strings.HasPrefix(p, "web/")
		if inWeb || webToo[p] {
			touched["web"] = true
		}
		if (!inWeb && !isNotGo(p)) || goToo[p] || isGoDoc(p) {
			touched["go"] = true
		}
		if isPackPython(p) {
			touched["pack-python"] = true
		}
	}
	return touched
}
