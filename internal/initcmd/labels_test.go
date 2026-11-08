package initcmd

import (
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/internal/packs"
	"github.com/CorneeldH/lighthouse/internal/projectconfig"
	"github.com/CorneeldH/lighthouse/pack"
)

func labels(o *Options) { o.Labels = true }

func createLabels(o *Options) { o.CreateLabels = true }

// noSync keeps the tests that are not about the label step from starting python3.
func noSync(o *Options) {
	o.SyncLabels = func(string, bool, io.Writer, io.Writer) error { return nil }
}

const testTemplate = "# Labels\n\n| `blocked-issue` | x |\n"

func TestLabelsDryRunShowsTaxonomy(t *testing.T) {
	root := newRepo(t)
	before := snapshot(t, root)
	r := run(t, root, "", labels)
	if r.code != 1 {
		t.Fatalf("rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	for _, want := range []string{projectconfig.DefaultLabelsPath, "[labels]", `blocked_issue = "blocked-issue"`, `labels = "` + projectconfig.DefaultLabelsPath + `"`} {
		if !strings.Contains(r.out, want) {
			t.Errorf("diff lacks %q:\n%s", want, r.out)
		}
	}
	if !sameSnapshot(before, snapshot(t, root)) {
		t.Fatal("a declined run wrote files")
	}
}

func TestLabelsOneYesApplies(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "y\n", all(labels, noSync, withTailscale("100.64.0.1")))
	if r.code != 0 {
		t.Fatalf("rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	if got := read(t, root, projectconfig.DefaultLabelsPath); got != testTemplate {
		t.Fatalf("taxonomy = %q", got)
	}
	cfg := read(t, root, "lighthouse.toml")
	for _, want := range []string{"[paths]", `labels = "` + projectconfig.DefaultLabelsPath + `"`, "[labels]", `area_prefix = "area:"`} {
		if !strings.Contains(cfg, want) {
			t.Errorf("lighthouse.toml lacks %q:\n%s", want, cfg)
		}
	}
	if _, err := os.Stat(hostTOMLFor(t)); err == nil {
		t.Fatal("host.toml written on a single y")
	}
	if strings.Contains(read(t, root, ".lighthouse/manifest.toml"), "labels.md") {
		t.Fatal("taxonomy file in the manifest")
	}
}

func TestNoLabelsFlagNoTaxonomy(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	if exists(root, projectconfig.DefaultLabelsPath) {
		t.Fatal("taxonomy written without --labels")
	}
	if strings.Contains(read(t, root, "lighthouse.toml"), "[labels]") {
		t.Fatal("[labels] proposed without --labels")
	}
}

func TestUpgradeKeepsEditedTaxonomy(t *testing.T) {
	root := newRepo(t)
	if r := run(t, root, "", all(labels, yes, noSync)); r.code != 0 {
		t.Fatalf("init: rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	write(t, root, projectconfig.DefaultLabelsPath, "# mine\n")
	for name, mod := range map[string]func(*Options){"without": upgrade, "with": all(upgrade, labels, noSync)} {
		r := run(t, root, "", mod)
		if r.code != 0 || r.out != "No changes.\n" {
			t.Fatalf("upgrade %s --labels: rc %d\n%s\n%s", name, r.code, r.out, r.err)
		}
		if got := read(t, root, projectconfig.DefaultLabelsPath); got != "# mine\n" {
			t.Fatalf("upgrade %s --labels: taxonomy = %q", name, got)
		}
	}
}

func TestLabelsUsesConfiguredPath(t *testing.T) {
	root := newRepo(t)
	write(t, root, "lighthouse.toml", "[paths]\nlabels = \"meta/labels.md\"\n")
	if r := run(t, root, "", all(labels, yes, noSync)); r.code != 0 {
		t.Fatalf("rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	if got := read(t, root, "meta/labels.md"); got != testTemplate {
		t.Fatalf("taxonomy = %q", got)
	}
	if exists(root, projectconfig.DefaultLabelsPath) {
		t.Fatal("default path written next to the configured one")
	}
}

func TestLabelsRejectsBadPath(t *testing.T) {
	for _, bad := range []string{"../x.md", "/etc/x.md", "a/../b.md", "a//b.md", "./x.md", ""} {
		root := newRepo(t)
		write(t, root, "lighthouse.toml", "[paths]\nlabels = \""+bad+"\"\n")
		before := snapshot(t, root)
		r := run(t, root, "", all(labels, yes, noSync))
		if r.code != 1 || !strings.Contains(r.err, "paths.labels") {
			t.Errorf("%q: rc %d, err %q", bad, r.code, r.err)
		}
		if !sameSnapshot(before, snapshot(t, root)) {
			t.Errorf("%q: files written", bad)
		}
	}
}

// AC 7: every label lighthouse.toml proposes is a row of the template.
func TestLabelTemplateHasEveryConfiguredLabel(t *testing.T) {
	sub, err := fs.Sub(pack.Files, "core")
	if err != nil {
		t.Fatal(err)
	}
	core, err := packs.Load(sub, "core", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if core.LabelsTemplate == nil {
		t.Fatal("core has no templates/labels.md")
	}
	checked := 0
	for _, tb := range projectconfig.LabelTables(projectconfig.DefaultLabelsPath) {
		if tb.Name != "labels" {
			continue
		}
		for _, kv := range tb.Keys {
			if strings.HasSuffix(kv.Key, "_prefix") {
				continue
			}
			var value string
			if _, err := toml.Decode("v = "+kv.Value, &struct {
				V *string `toml:"v"`
			}{&value}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(core.LabelsTemplate), "| `"+value+"` |") {
				t.Errorf("template has no row for %s = %q", kv.Key, value)
			}
			checked++
		}
	}
	if checked != 6 {
		t.Fatalf("checked %d labels, want 6", checked)
	}
}

type syncCall struct {
	root        string
	create      bool
	hadTaxonomy bool
}

func recordSync(calls *[]syncCall, ret error) func(*Options) {
	return func(o *Options) {
		o.SyncLabels = func(root string, create bool, _, _ io.Writer) error {
			*calls = append(*calls, syncCall{root, create, exists(root, projectconfig.DefaultLabelsPath)})
			return ret
		}
	}
}

func TestCreateLabelsNeedsLabels(t *testing.T) {
	root := newRepo(t)
	before := snapshot(t, root)
	r := run(t, root, "", all(createLabels, yes))
	if r.code != 1 || !strings.Contains(r.err, "--create-labels needs --labels") {
		t.Fatalf("rc %d, err %q", r.code, r.err)
	}
	if !sameSnapshot(before, snapshot(t, root)) {
		t.Fatal("files written")
	}
}

func TestLabelsSyncRunsOnceAfterApply(t *testing.T) {
	for _, create := range []bool{false, true} {
		root := newRepo(t)
		var calls []syncCall
		mods := []func(*Options){labels, yes, recordSync(&calls, nil)}
		if create {
			mods = append(mods, createLabels)
		}
		if r := run(t, root, "", all(mods...)); r.code != 0 {
			t.Fatalf("rc %d\n%s\n%s", r.code, r.out, r.err)
		}
		if len(calls) != 1 || calls[0].create != create || !calls[0].hadTaxonomy {
			t.Fatalf("create=%v: calls %+v", create, calls)
		}
	}
}

func TestDeclinedLabelsDoesNotSync(t *testing.T) {
	root := newRepo(t)
	var calls []syncCall
	r := run(t, root, "", all(labels, createLabels, recordSync(&calls, nil)))
	if r.code != 1 || len(calls) != 0 {
		t.Fatalf("rc %d, calls %+v", r.code, calls)
	}
}

func TestNoLabelsNoSync(t *testing.T) {
	root := newRepo(t)
	var calls []syncCall
	if r := run(t, root, "", all(yes, recordSync(&calls, nil))); r.code != 0 || len(calls) != 0 {
		t.Fatalf("rc %d, calls %+v", r.code, calls)
	}
}

func TestSyncFailureDoesNotFailInit(t *testing.T) {
	root := newRepo(t)
	var calls []syncCall
	r := run(t, root, "", all(labels, yes, recordSync(&calls, io.ErrUnexpectedEOF)))
	if r.code != 0 || !strings.Contains(r.err, "warning: labels:") {
		t.Fatalf("rc %d, err %q", r.code, r.err)
	}
}

func TestLabelsAddsNoQuestion(t *testing.T) {
	root := newRepo(t)
	var calls []syncCall
	r := run(t, root, "y\n", all(labels, createLabels, recordSync(&calls, nil), withTailscale("100.64.0.1")))
	if r.code != 0 || strings.Count(r.out, "[y/N]") != 2 {
		t.Fatalf("rc %d\n%s", r.code, r.out)
	}
	if _, err := os.Stat(hostTOMLFor(t)); err == nil {
		t.Fatal("host.toml written on a single y")
	}
}

func TestLabelsSyncRunsOnUpgradeWithoutChanges(t *testing.T) {
	root := newRepo(t)
	var calls []syncCall
	if r := run(t, root, "", all(labels, yes, recordSync(&calls, nil))); r.code != 0 {
		t.Fatalf("init: rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	r := run(t, root, "", all(upgrade, labels, recordSync(&calls, nil)))
	if r.code != 0 || !strings.HasPrefix(r.out, "No changes.\n") || len(calls) != 2 {
		t.Fatalf("rc %d, calls %d\n%s", r.code, len(calls), r.out)
	}
}

// ghStub puts a `gh` shell script first on PATH; skips without python3.
func ghStub(t *testing.T, script string) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func realCore(o *Options) { o.Packs = nil }

func TestLabelsDefaultSyncRunsInstalledScript(t *testing.T) {
	ghStub(t, "exit 1\n")
	root := newRepo(t)
	r := run(t, root, "", all(realCore, labels, yes))
	if r.code != 0 || !strings.Contains(r.out, "gh label create blocked-issue") {
		t.Fatalf("rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	r = run(t, root, "", all(realCore, upgrade, labels))
	if r.code != 0 || !strings.HasPrefix(r.out, "No changes.\n") || !strings.Contains(r.out, "gh label create blocked-issue") {
		t.Fatalf("upgrade: rc %d\n%s\n%s", r.code, r.out, r.err)
	}
}

func TestCreateLabelsRunsInstalledScriptWithCreate(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "gh.log")
	ghStub(t, `case "$1" in
api) echo needs-hands; exit 0 ;;
label) echo "$@" >> `+logFile+`; exit 0 ;;
esac
exit 1
`)
	// A caller's shell must not point the script at another repository.
	t.Setenv("LIGHTHOUSE_TOML", "/nonexistent/lighthouse.toml")
	t.Setenv("LIGHTHOUSE_REPO_ROOT", "/nonexistent")
	root := newRepo(t)
	write(t, root, "lighthouse.toml", "[forge]\nrepo = \"Owner/repo\"\n")
	r := run(t, root, "", all(realCore, labels, createLabels, yes))
	if r.code != 0 {
		t.Fatalf("rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	b, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("no gh label create calls\n%s\n%s", r.out, r.err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 5 {
		t.Fatalf("%d create calls, want 5:\n%s", len(lines), b)
	}
	for _, l := range lines {
		if !strings.Contains(l, "label create") || !strings.Contains(l, "--repo Owner/repo") || strings.Contains(l, "needs-hands") {
			t.Errorf("bad call: %s", l)
		}
	}
}

func TestUpgradeAdoptsLabels(t *testing.T) {
	root := newRepo(t)
	if r := run(t, root, "", all(yes, noSync)); r.code != 0 {
		t.Fatalf("init: rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	if exists(root, projectconfig.DefaultLabelsPath) {
		t.Fatal("taxonomy written without --labels")
	}
	r := run(t, root, "", all(upgrade, labels, yes, noSync))
	if r.code != 0 {
		t.Fatalf("upgrade --labels: rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	if got := read(t, root, projectconfig.DefaultLabelsPath); got != testTemplate {
		t.Fatalf("taxonomy = %q", got)
	}
	cfg := read(t, root, "lighthouse.toml")
	for _, want := range []string{"[labels]", "[paths]", `labels = "` + projectconfig.DefaultLabelsPath + `"`} {
		if !strings.Contains(cfg, want) {
			t.Errorf("lighthouse.toml lacks %q:\n%s", want, cfg)
		}
	}
}
