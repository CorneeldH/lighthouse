package checkpointscan

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLedgerRoundTripAndMerge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "clean-x.txt")

	noErr(t, AddToLedger(path, []string{"b", "a"}))
	noErr(t, AddToLedger(path, []string{"c", "a"}))
	noErr(t, AddToLedger(path, nil))

	raw, err := os.ReadFile(path)
	noErr(t, err)
	eq(t, "a\nb\nc\n", string(raw))
	got, err := LoadLedger(path)
	noErr(t, err)
	eq(t, map[string]struct{}{"a": {}, "b": {}, "c": {}}, got)
	entries, err := os.ReadDir(filepath.Dir(path))
	noErr(t, err)
	eq(t, 1, len(entries), "no temp file left behind")
}

func TestLoadLedgerMissingIsEmptyAndUnreadableIsCouldNotCheck(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadLedger(filepath.Join(dir, "absent.txt"))
	noErr(t, err)
	eq(t, 0, len(got))

	_, err = LoadLedger(dir) // a directory cannot be read as a ledger
	contains(t, cnc(t, err).Msg, "cannot read the ledger")
}

// fakeGitleaks puts a `gitleaks` on PATH that prints version.
func fakeGitleaks(t *testing.T, version string, code int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\necho '" + version + "'\nexit " + string(rune('0'+code)) + "\n"
	noErr(t, os.WriteFile(filepath.Join(bin, "gitleaks"), []byte(script), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func initRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	_, err := git(repo, nil, "init", "-q")
	noErr(t, err)
	return repo
}

func TestLedgerPathKeyCoversConfigAndGitleaksVersion(t *testing.T) {
	repo := initRepo(t)
	cfg := filepath.Join(t.TempDir(), "gitleaks.toml")
	noErr(t, os.WriteFile(cfg, []byte("title = \"a\"\n"), 0o600))

	fakeGitleaks(t, "8.18.0", 0)
	p1, err := LedgerPath(repo, cfg)
	noErr(t, err)
	again, err := LedgerPath(repo, cfg)
	noErr(t, err)
	eq(t, p1, again)
	eq(t, filepath.Join(repo, ".git", "lighthouse", "secret-scan"), filepath.Dir(p1))
	// Pinned against hashlib: sha256(config + version + NUL + scan method).
	eq(t, "clean-3682839a810204ef724c3746e9441f34d1bb8ac030b9dc151ce9f0ee4324565e.txt", filepath.Base(p1))

	fakeGitleaks(t, "8.19.0", 0)
	p2, err := LedgerPath(repo, cfg)
	noErr(t, err)
	ne(t, p1, p2, "a new gitleaks starts a new ledger")

	noErr(t, os.WriteFile(cfg, []byte("title = \"b\"\n"), 0o600))
	p3, err := LedgerPath(repo, cfg)
	noErr(t, err)
	ne(t, p2, p3, "a changed config starts a new ledger")
}

func TestLedgerPathCouldNotCheck(t *testing.T) {
	repo := initRepo(t)
	cfg := filepath.Join(t.TempDir(), "gitleaks.toml")

	_, err := LedgerPath(repo, cfg)
	contains(t, cnc(t, err).Msg, "cannot read")

	noErr(t, os.WriteFile(cfg, nil, 0o600))
	fakeGitleaks(t, "x", 3)
	_, err = LedgerPath(repo, cfg)
	contains(t, cnc(t, err).Msg, "gitleaks version exited 3")

	_, err = LedgerPath(t.TempDir(), cfg) // not a repository
	_ = cnc(t, err)
}
