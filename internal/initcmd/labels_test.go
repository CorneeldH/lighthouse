package initcmd

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/internal/packs"
	"github.com/CorneeldH/lighthouse/internal/projectconfig"
	"github.com/CorneeldH/lighthouse/pack"
)

func labels(o *Options) { o.Labels = true }

const testTemplate = "# Labels\n\n| `blocked-issue` | x |\n"

func TestLabelsDryRunShowsTaxonomy(t *testing.T) {
	root := newRepo(t)
	before := snapshot(t, root)
	r := run(t, root, "", labels)
	if r.code != 1 {
		t.Fatalf("rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	for _, want := range []string{"docs/developer/labels.md", "[labels]", `blocked_issue = "blocked-issue"`, `labels = "docs/developer/labels.md"`} {
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
	r := run(t, root, "y\n", all(labels, withTailscale("100.64.0.1")))
	if r.code != 0 {
		t.Fatalf("rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	if got := read(t, root, "docs/developer/labels.md"); got != testTemplate {
		t.Fatalf("taxonomy = %q", got)
	}
	cfg := read(t, root, "lighthouse.toml")
	for _, want := range []string{"[paths]", `labels = "docs/developer/labels.md"`, "[labels]", `area_prefix = "area:"`} {
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
	if exists(root, "docs/developer/labels.md") {
		t.Fatal("taxonomy written without --labels")
	}
	if strings.Contains(read(t, root, "lighthouse.toml"), "[labels]") {
		t.Fatal("[labels] proposed without --labels")
	}
}

func TestUpgradeKeepsEditedTaxonomy(t *testing.T) {
	root := newRepo(t)
	if r := run(t, root, "", all(labels, yes)); r.code != 0 {
		t.Fatalf("init: rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	write(t, root, "docs/developer/labels.md", "# mine\n")
	for name, mod := range map[string]func(*Options){"without": upgrade, "with": all(upgrade, labels)} {
		r := run(t, root, "", mod)
		if r.code != 0 || r.out != "No changes.\n" {
			t.Fatalf("upgrade %s --labels: rc %d\n%s\n%s", name, r.code, r.out, r.err)
		}
		if got := read(t, root, "docs/developer/labels.md"); got != "# mine\n" {
			t.Fatalf("upgrade %s --labels: taxonomy = %q", name, got)
		}
	}
}

func TestLabelsUsesConfiguredPath(t *testing.T) {
	root := newRepo(t)
	write(t, root, "lighthouse.toml", "[paths]\nlabels = \"meta/labels.md\"\n")
	if r := run(t, root, "", all(labels, yes)); r.code != 0 {
		t.Fatalf("rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	if got := read(t, root, "meta/labels.md"); got != testTemplate {
		t.Fatalf("taxonomy = %q", got)
	}
	if exists(root, "docs/developer/labels.md") {
		t.Fatal("default path written next to the configured one")
	}
}

func TestLabelsRejectsBadPath(t *testing.T) {
	for _, bad := range []string{"../x.md", "/etc/x.md", "a/../b.md", "a//b.md", "./x.md"} {
		root := newRepo(t)
		write(t, root, "lighthouse.toml", "[paths]\nlabels = \""+bad+"\"\n")
		before := snapshot(t, root)
		r := run(t, root, "", all(labels, yes))
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
