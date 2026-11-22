package worktreeroot

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolve(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cases := []struct {
		name, value, want string
	}{
		{"leeg is de standaard", "", filepath.Join(repo, ".claude", "worktrees")},
		{"relatief onder de repo", ".wt", filepath.Join(repo, ".wt")},
		{"relatief naar boven wordt schoon", "../wt", filepath.Join(filepath.Dir(repo), "wt")},
	}
	// Red when Resolve goes back to filepath.IsAbs alone: on Windows `/data/wt` has no drive and
	// would hang under the repo, while the skills' case treats `/*` as absolute.
	cases = append(cases,
		struct{ name, value, want string }{"slash zonder schijf is absoluut", "/data/wt", filepath.Clean("/data/wt")},
		struct{ name, value, want string }{"backslash zonder schijf is absoluut", `\data\wt`, filepath.Clean(`\data\wt`)},
	)
	abs := filepath.Join(t.TempDir(), "mijn wt")
	cases = append(cases, struct{ name, value, want string }{"absoluut met spaties blijft gelijk", abs, abs})
	if runtime.GOOS == "windows" {
		cases = append(cases, struct{ name, value, want string }{"Windows-pad met spaties", `C:\Users\A B\wt`, `C:\Users\A B\wt`})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Resolve(repo, c.value); got != c.want {
				t.Errorf("Resolve(%q, %q) = %q, want %q", repo, c.value, got, c.want)
			}
		})
	}
}

func TestContains(t *testing.T) {
	root := filepath.FromSlash("/tmp/wt")
	cases := []struct {
		name, path string
		want       bool
	}{
		{"pad eronder", "/tmp/wt/a", true},
		{"zusterpad met zelfde voorvoegsel", "/tmp/wt-old/a", false},
		{"de root zelf", "/tmp/wt", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Contains(root, filepath.FromSlash(c.path)); got != c.want {
				t.Errorf("Contains(%q, %q) = %v, want %v", root, c.path, got, c.want)
			}
		})
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestMainCheckout(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(base, "main")
	linked := filepath.Join(base, "linked")
	if err := os.Mkdir(main, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, main, "init", "-q")
	git(t, main, "commit", "-q", "--allow-empty", "-m", "init")
	git(t, main, "worktree", "add", "-q", "-b", "extra", linked)
	for _, dir := range []string{main, linked} {
		got, err := MainCheckout(dir)
		if err != nil {
			t.Fatalf("MainCheckout(%q): %v", dir, err)
		}
		if got != main {
			t.Errorf("MainCheckout(%q) = %q, want %q", dir, got, main)
		}
	}
}

// Red when InSyncFolder stops matching whole path segments (or stops reading `\`): the table
// holds a Windows path, a prefixed OneDrive name, and a lookalike that must not match.
func TestInSyncFolder(t *testing.T) {
	cases := []struct {
		path, provider string
	}{
		{`C:\Users\A\OneDrive - Bedrijf\repo`, "OneDrive"},
		{`C:\Users\A\OneDrive\repo`, "OneDrive"},
		{"/home/a/onedrive/x", "OneDrive"},
		{"/home/a/Dropbox/x", "Dropbox"},
		{"/Users/a/iCloud Drive/x", "iCloud Drive"},
		{"/Users/a/iCloudDrive/x", "iCloud Drive"},
		{"/Users/a/Library/Mobile Documents/com~apple~CloudDocs/x", "iCloud Drive"},
		{"/Users/a/Library/CloudStorage/OneDrive-Bedrijf/x", "OneDrive"},
		{"/Users/a/Library/CloudStorage/Dropbox (Team)/x", "Dropbox"},
		{"/Users/a/Library/CloudStorage/GoogleDrive-a@b.nl/x", "Google Drive"},
		{"/Users/a/Google Drive/x", "Google Drive"},
		{"/Users/a/GoogleDrive/x", "Google Drive"},
		{"/home/a/MyOneDriveNotes/x", ""},
		{"/home/a/MyGoogleDriveNotes/x", ""},
		{"/home/a/Dropboxes/x", ""},
		{"/home/a/Dropbox-old/x", ""},
		{"/Users/a/Library/Mobile Documents/other/x", ""},
		{"/home/a/src/repo", ""},
		{"", ""},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			got, ok := InSyncFolder(c.path)
			if got != c.provider || ok != (c.provider != "") {
				t.Errorf("InSyncFolder(%q) = %q, %v; want %q", c.path, got, ok, c.provider)
			}
		})
	}
}
