//go:build unix

package checkpointscan

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// realFixture is a fixture with the real gitleaks and the repository's own `.gitleaks.toml`.
// Without gitleaks it skips, unless LIGHTHOUSE_REQUIRE_GITLEAKS is set: then it fails.
func realFixture(t *testing.T) *fixture {
	t.Helper()
	if _, err := exec.LookPath("gitleaks"); err != nil {
		if os.Getenv("LIGHTHOUSE_REQUIRE_GITLEAKS") != "" {
			t.Fatal("gitleaks is not installed but LIGHTHOUSE_REQUIRE_GITLEAKS is set")
		}
		t.Skip("gitleaks is not installed")
	}
	f := newFixture(t, false)
	raw, err := os.ReadFile(filepath.Join("..", "..", ".gitleaks.toml"))
	noErr(t, err)
	noErr(t, os.WriteFile(f.config, raw, 0o644))
	return f
}

var leak = `{"text": "GITHUB_PAT=` + fakePAT + `"}` + "\n"

func TestRealGitleaksIgnoresTheTempPath(t *testing.T) {
	// gitleaks' default path allowlist (node_modules, vendor, ...) applies to the path it
	// reports: a TMPDIR under such a directory must not make every blob clean.
	f := realFixture(t)
	tmp := filepath.Join(t.TempDir(), "node_modules")
	noErr(t, os.MkdirAll(tmp, 0o755))
	t.Setenv("TMPDIR", tmp)
	opts := f.opts()

	res := f.scan(f.single(refA, leak), opts)

	eq(t, 1, len(res.Findings))
	eq(t, "github-pat", res.Findings[0].Rule)
	eq(t, []string{}, f.ledger(opts))
}

func TestRealGitleaksFindsPAT(t *testing.T) {
	f := realFixture(t)
	clean := `{"text": "hello"}` + "\n"
	refs := f.checkpoints(map[string]map[string]string{refA: {"0/full.jsonl": leak}, refB: {"0/full.jsonl": clean}})
	opts := f.opts()

	res := f.scan(refs, opts)

	eq(t, []Finding{{Blob: blobID(leak), Rule: "github-pat", Match: "REDACTED"}}, res.Findings)
	eq(t, []string{blobID(clean)}, f.ledger(opts))
	isFalse(t, strings.Contains(f.stderr.String(), fakePAT))
}

// Text to git, so `gitleaks git` scans it; but the first bytes are the magic number of a
// binary type, and `gitleaks dir` skips such a file unscanned. The newline prefix prevents it.
var magic = map[string]string{
	"zip": "PK\x03\x04", "gzip": "\x1f\x8b\x08", "sqlite": "SQLite format 3", "elf": "\x7fELF",
	"rtf": `{\rtf1`, "pdf": "%PDF-", "ole": "\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1", "bz2": "BZh", "exe": "MZ",
}

func TestRealGitleaksScansTextWithABinaryMagic(t *testing.T) {
	for name, m := range magic {
		t.Run(name, func(t *testing.T) {
			f := realFixture(t)
			opts := f.opts()

			res := f.scan(f.single(refA, m+leak), opts)

			eq(t, "", res.Aborted)
			eq(t, 1, len(res.Findings))
			eq(t, []string{}, f.ledger(opts))
		})
	}
}

func TestRealGitleaksSkipPastThePrefixIsNotClean(t *testing.T) {
	// A binary signature at a fixed offset past the first byte (`ustar` at 257, tar): the
	// blob prefix shifts it by one, so `ustar` at 256 still makes gitleaks skip the file;
	// the byte count catches that.
	f := realFixture(t)
	body := strings.Repeat("x", 256) + "ustar" + leak
	clean := `{"text": "hello"}` + "\n"
	refs := f.checkpoints(map[string]map[string]string{refA: {"0/full.jsonl": body}, refB: {"0/full.jsonl": clean}})
	opts := f.opts()

	res := f.scan(refs, opts)

	contains(t, res.Aborted, "it skipped "+blobID(body))
	eq(t, []string{}, f.ledger(opts))
	isFalse(t, strings.Contains(res.Aborted+f.stderr.String(), fakePAT))
}
