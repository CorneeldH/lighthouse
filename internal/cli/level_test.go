package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/levellog"
	"github.com/CorneeldH/lighthouse/internal/levels"
	"github.com/CorneeldH/lighthouse/internal/manifest"
)

// levelRepo is a git repository with the given components.toml and lighthouse.toml (either
// may be empty: then the file is absent). The state directory is private to the test.
func levelRepo(t *testing.T, installed *levels.Installed, lighthouseTOML string) string {
	t.Helper()
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	t.Setenv("LIGHTHOUSE_REPO_ROOT", root)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if installed != nil {
		p := filepath.Join(root, levels.InstalledPath)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, installed.Marshal(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if lighthouseTOML != "" {
		if err := os.WriteFile(filepath.Join(root, "lighthouse.toml"), []byte(lighthouseTOML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runLevel(t *testing.T) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRoot(bytes.NewReader(nil), &out, &errOut)
	root.SetArgs([]string{"level"})
	code = execute(root, &errOut)
	return out.String(), errOut.String(), code
}

func lightInstalled() *levels.Installed {
	return &levels.Installed{
		Level: "light",
		Since: "2026-03-01T10:00:00Z",
		Components: map[string]levels.Stand{
			"commit-test-gate": levels.StandOff,
			"gofmt-check":      levels.StandOff,
			"md-link-check":    levels.StandLog,
			"skill:fix-pr":     levels.StandOff,
		},
	}
}

// Classification: a cli-output change; the surface is the stdout of `lighthouse level`. It fails
// when the command drops the level line, a row, the count of the on components or the count of a
// log component, or when it counts the entries from before since.
func TestLevelShowsStandsAndCounts(t *testing.T) {
	root := levelRepo(t, lightInstalled(), "")
	since := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{since.Add(-time.Hour), since.Add(time.Hour), since.Add(2 * time.Hour)} {
		if err := levellog.Append(root, "md-link-check", "broken link", at); err != nil {
			t.Fatal(err)
		}
	}
	out, errOut, code := runLevel(t)
	if code != 0 || errOut != "" {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	want := []string{
		"level: light (since 2026-03-01)",
		"commit-test-gate  off",
		"gofmt-check  off",
		"md-link-check  log",
		"skill:fix-pr  off",
	}
	if len(lines) < len(want)+2 {
		t.Fatalf("stdout too short:\n%s", out)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d = %q, want %q", i, lines[i], w)
		}
	}
	if !regexp.MustCompile(`^\d+ components on$`).MatchString(lines[len(want)]) {
		t.Errorf("line %d = %q, want a count of the components that are on", len(want), lines[len(want)])
	}
	if got, w := lines[len(want)+1], "md-link-check  would have reported 2× since 2026-03-01"; got != w {
		t.Errorf("line %d = %q, want %q", len(want)+1, got, w)
	}
}

// Classification: a cli-output change; the surface is the section "not measured at this level".
// It fails when the section lists nothing, lists a component that is not off, lists a skill, or
// reads the wrong next level.
func TestLevelListsUnmeasuredComponentsOfNextLevel(t *testing.T) {
	levelRepo(t, lightInstalled(), "")
	out, _, code := runLevel(t)
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	_, section, found := strings.Cut(out, "not measured at this level\n")
	if !found {
		t.Fatalf("no section in:\n%s", out)
	}
	got := strings.Fields(section)
	want := []string{"commit-test-gate", "gofmt-check"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("section lists %v, want %v (never a skill, never a log component)", got, want)
	}
}

// Classification: a cli-output change; the surface is that no line ever claims readiness. It fails
// when the command prints a verdict about moving to the next level.
func TestLevelNeverClaimsReadiness(t *testing.T) {
	root := levelRepo(t, lightInstalled(), "")
	for range 3 {
		_ = levellog.Append(root, "md-link-check", "x", time.Now())
	}
	for _, name := range []string{"with counts", "without counts"} {
		out, _, _ := runLevel(t)
		if strings.Contains(strings.ToLower(out), "ready") {
			t.Errorf("%s: a line claims readiness:\n%s", name, out)
		}
		_ = os.RemoveAll(os.Getenv("XDG_STATE_HOME"))
	}
}

// Classification: a cli-output change; the surface is the warning per disabled core gate, with the
// words of init, after the rows. It fails when the warning is dropped, worded differently, comes
// before the rows, or when an enabled gate is warned about.
func TestLevelWarnsPerDisabledCoreGate(t *testing.T) {
	i := lightInstalled()
	i.Components["worktree-check"] = levels.StandOff
	i.Components["pr-create-gate"] = levels.StandLog
	levelRepo(t, i, "")
	out, _, code := runLevel(t)
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	w1 := "lighthouse: worktree-check is off — this repo works outside the chain"
	w2 := "lighthouse: pr-create-gate is log — this repo works outside the chain"
	if !strings.HasSuffix(out, w1+"\n"+w2+"\n") && !strings.HasSuffix(out, w2+"\n"+w1+"\n") {
		t.Errorf("the warnings are not the last lines:\n%s", out)
	}
	if strings.Count(out, "works outside the chain") != 2 {
		t.Errorf("want exactly two warnings:\n%s", out)
	}
}

// Classification: a cli-output change; the surface is a repo that init never levelled. It fails
// when the command errors on the missing components.toml or prints another first line.
func TestLevelWithoutComponentsFile(t *testing.T) {
	levelRepo(t, nil, "")
	out, errOut, code := runLevel(t)
	if code != 0 || errOut != "" || out != "level: strict (no level set)\n" {
		t.Errorf("code %d out %q err %q", code, out, errOut)
	}
}

// Classification: a cli-output change; the surface is the first line when lighthouse.toml sets a
// level but components.toml is absent. It fails when the command claims "no level set" while the
// file names one, or does not point to init --upgrade.
func TestLevelFromTomlWithoutComponentsFile(t *testing.T) {
	levelRepo(t, nil, "level = \"standard\"\n")
	out, errOut, code := runLevel(t)
	want := "level: standard (not installed yet; run lighthouse init --upgrade)\n"
	if code != 0 || errOut != "" || out != want {
		t.Errorf("code %d out %q err %q, want %q", code, out, errOut, want)
	}
}

// Classification: a cli-output change; the surface is the exit code and the message for an unknown
// level in lighthouse.toml. It fails when the command does not validate with init's validate (no
// list of valid names) or exits 0.
func TestLevelBogusLevelFails(t *testing.T) {
	levelRepo(t, lightInstalled(), "level = \"bogus\"\n")
	out, errOut, code := runLevel(t)
	if code != 1 || out != "" {
		t.Errorf("code %d out %q", code, out)
	}
	if !strings.Contains(errOut, `unknown level "bogus" (known: light, standard, strict)`) {
		t.Errorf("stderr %q does not name the valid levels", errOut)
	}
}

// coreOnlyManifest gives root a manifest that lists the core package alone.
func coreOnlyManifest(t *testing.T, root string) {
	t.Helper()
	p := filepath.Join(root, manifest.Path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, manifest.Encode(manifest.Manifest{Version: "dev", Packages: []string{"core"}}), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Classification: a cli-output change; the surface is the count line. It fails when the count
// takes the components of packs the repo did not install (every pack of the binary) instead of
// the packages of the manifest: a core-only repo then counts the go, python and node hooks as on.
func TestLevelCountsOnlyInstalledPackages(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   *levels.Installed
		want string
	}{
		{"light", func() *levels.Installed {
			in := lightInstalled()
			in.Components = map[string]levels.Stand{"commit-test-gate": levels.StandOff, "md-link-check": levels.StandLog,
				"code-language-check": levels.StandLog}
			for _, s := range []string{"write-issue", "fix-pr", "audit-worktrees", "audit-agent-config", "test-standard", "review-spec", "review-plan", "map-planned"} {
				in.Components["skill:"+s] = levels.StandOff
			}
			return in
		}(), "12"},
		{"strict", &levels.Installed{Level: "strict", Since: "2026-03-01T10:00:00Z", Components: map[string]levels.Stand{}}, "23"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := levelRepo(t, tc.in, "")
			coreOnlyManifest(t, root)
			out, errOut, code := runLevel(t)
			if code != 0 || errOut != "" {
				t.Fatalf("code %d stderr %q", code, errOut)
			}
			if !strings.Contains(out, "\n"+tc.want+" components on\n") {
				t.Errorf("stdout lacks %q components on:\n%s", tc.want, out)
			}
		})
	}
}

// pusherRepo is levelRepo with a [forge] repo, so the repo has a slug, and a host.toml
// (LIGHTHOUSE_HOST_TOML) with the given body; the repos line is added by the caller.
func pusherRepo(t *testing.T, hostBody string) (root, host string) {
	t.Helper()
	root = levelRepo(t, lightInstalled(), "[forge]\nrepo = \"acme/widgets\"\n")
	host = filepath.Join(t.TempDir(), "host.toml")
	if hostBody != "" {
		if err := os.WriteFile(host, []byte(hostBody), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("LIGHTHOUSE_HOST_TOML", host)
	return root, host
}

const pusherBare = "[dispatch.workflow_push]\ntoken_file = \"/run/t\"\n"

func hostWith(repo, body string) string {
	return "[dispatch]\nrepos = [\"" + repo + "\"]\n" + body
}

// Classification: a cli-output change; the surface is the stdout of `lighthouse level`. It fails
// when the note is dropped, or does not name both the missing reviewers and the missing intent.
func TestLevelNotesPusherWithoutReviewers(t *testing.T) {
	root, host := pusherRepo(t, "")
	if err := os.WriteFile(host, []byte(hostWith(root, pusherBare)), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runLevel(t)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var notes []string
	for _, l := range strings.Split(stdout, "\n") {
		if strings.Contains(l, "reviewers") {
			notes = append(notes, l)
		}
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "intent") {
		t.Errorf("want one line naming reviewers and intent, got %q in:\n%s", notes, stdout)
	}
}

// Classification: same surface; it fails when the note is also written for a worktree path, the
// slug being the key: a worktree of a listed repo is that repo.
func TestLevelNotesPusherFromWorktree(t *testing.T) {
	root, host := pusherRepo(t, "")
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "lighthouse.toml"), []byte("[forge]\nrepo = \"acme/widgets\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// host.toml lists another checkout of the same slug; the current root is a different path.
	if err := os.WriteFile(host, []byte(hostWith(other, pusherBare)), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = root
	stdout, _, _ := runLevel(t)
	if !strings.Contains(stdout, "reviewers") {
		t.Errorf("no note for a path with the listed slug:\n%s", stdout)
	}
}

// Classification: same surface; it fails when the note is written without a host.toml.
func TestLevelNoNoteWithoutHostToml(t *testing.T) {
	pusherRepo(t, "")
	stdout, _, code := runLevel(t)
	if code != 0 || strings.Contains(stdout, "reviewers") {
		t.Errorf("exit %d, stdout:\n%s", code, stdout)
	}
}

// Classification: same surface; it fails when the note ignores whether the repo is listed.
func TestLevelNoNoteWhenRepoNotListed(t *testing.T) {
	_, host := pusherRepo(t, "")
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "lighthouse.toml"), []byte("[forge]\nrepo = \"acme/other\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host, []byte(hostWith(other, pusherBare)), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, _ := runLevel(t)
	if strings.Contains(stdout, "reviewers") {
		t.Errorf("note for an unlisted repo:\n%s", stdout)
	}
}

// Classification: same surface; it fails when the note is written for a complete config.
func TestLevelNoNoteWhenComplete(t *testing.T) {
	root, host := pusherRepo(t, "")
	body := pusherBare + "[[dispatch.workflow_push.reviewers]]\ntopic = \"secrets\"\ncommand = [\"a\"]\n" +
		"[[dispatch.workflow_push.reviewers]]\ntopic = \"injection\"\ncommand = [\"a\"]\n" +
		"[dispatch.workflow_push.intent]\ncommand = [\"b\"]\n"
	if err := os.WriteFile(host, []byte(hostWith(root, body)), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, _ := runLevel(t)
	if strings.Contains(stdout, "reviewers") {
		t.Errorf("note for a complete config:\n%s", stdout)
	}
}

// Classification: same surface; it fails when a host.toml that does not parse makes level exit
// non-zero, or is passed over in silence.
func TestLevelBrokenHostTomlIsOneNote(t *testing.T) {
	_, host := pusherRepo(t, "")
	if err := os.WriteFile(host, []byte("[dispatch\nrepos = "), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runLevel(t)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if n := strings.Count(stdout, "workflow pusher: cannot check"); n != 1 || !strings.Contains(stdout, "host.toml") {
		t.Errorf("want one note about host.toml, got %d:\n%s", n, stdout)
	}
}
