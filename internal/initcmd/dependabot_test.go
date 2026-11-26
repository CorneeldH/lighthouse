package initcmd

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/CorneeldH/lighthouse/internal/manifest"
)

func writeAll(t *testing.T, root string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		write(t, root, rel, "x\n")
	}
}

func TestDetectEcosystemsMonorepo(t *testing.T) {
	root := newRepo(t)
	writeAll(t, root, "go.mod", "web/go.mod", "web/package.json", ".github/workflows/ci.yml")
	got, err := detectEcosystems(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []ecosystem{
		{Name: "github-actions", Dirs: []string{"/"}},
		{Name: "gomod", Dirs: []string{"/", "/web"}},
		{Name: "npm", Dirs: []string{"/web"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDetectEcosystemsUvVersusPip(t *testing.T) {
	root := newRepo(t)
	writeAll(t, root, "a/pyproject.toml", "a/uv.lock", "b/pyproject.toml")
	got, err := detectEcosystems(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []ecosystem{
		{Name: "pip", Dirs: []string{"/b"}},
		{Name: "uv", Dirs: []string{"/a"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDetectEcosystemsIgnoresFixtures(t *testing.T) {
	root := newRepo(t)
	writeAll(t, root, "internal/x/testdata/go.mod", "node_modules/p/package.json", "vendor/m/go.mod")
	got, err := detectEcosystems(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want none", got)
	}
}

func TestDetectEcosystemsDocker(t *testing.T) {
	root := newRepo(t)
	writeAll(t, root, "Dockerfile", "svc/Dockerfile.dev")
	got, err := detectEcosystems(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []ecosystem{{Name: "docker", Dirs: []string{"/", "/svc"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRenderDependabot(t *testing.T) {
	got := string(renderDependabot([]ecosystem{
		{Name: "gomod", Dirs: []string{"/", "/web"}},
		{Name: "github-actions", Dirs: []string{"/"}},
	}))
	var doc struct {
		Version int `yaml:"version"`
		Updates []struct {
			Ecosystem    string   `yaml:"package-ecosystem"`
			Directory    string   `yaml:"directory"`
			Directories  []string `yaml:"directories"`
			ExcludePaths []string `yaml:"exclude-paths"`
			Schedule     struct {
				Interval string `yaml:"interval"`
			} `yaml:"schedule"`
			Groups map[string]struct {
				Patterns []string `yaml:"patterns"`
			} `yaml:"groups"`
		} `yaml:"updates"`
	}
	if err := yaml.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("not YAML: %v\n%s", err, got)
	}
	if doc.Version != 2 || len(doc.Updates) != 2 {
		t.Fatalf("version %d, %d updates\n%s", doc.Version, len(doc.Updates), got)
	}
	ga, gm := doc.Updates[0], doc.Updates[1]
	if ga.Ecosystem != "github-actions" || gm.Ecosystem != "gomod" {
		t.Fatalf("order: %s, %s", ga.Ecosystem, gm.Ecosystem)
	}
	if ga.Directory != "/" || len(ga.Directories) != 0 {
		t.Errorf("github-actions directory %q %v", ga.Directory, ga.Directories)
	}
	if !reflect.DeepEqual(ga.ExcludePaths, []string{".github/workflows/lighthouse-*.yml"}) {
		t.Errorf("github-actions exclude-paths %v", ga.ExcludePaths)
	}
	if gm.Directory != "" || !reflect.DeepEqual(gm.Directories, []string{"/", "/web"}) || len(gm.ExcludePaths) != 0 {
		t.Errorf("gomod %q %v %v", gm.Directory, gm.Directories, gm.ExcludePaths)
	}
	for _, u := range doc.Updates {
		if u.Schedule.Interval != "weekly" || !reflect.DeepEqual(u.Groups["all"].Patterns, []string{"*"}) {
			t.Errorf("%s: schedule %q groups %v", u.Ecosystem, u.Schedule.Interval, u.Groups)
		}
	}
	if !strings.HasPrefix(got, "# ") || !strings.Contains(got, "lighthouse init") {
		t.Errorf("no leading comment:\n%s", got)
	}
}

func githubRepo(t *testing.T) string {
	t.Helper()
	root := newRepo(t)
	gitIn(t, root, "remote", "add", "origin", "https://github.com/x/y.git")
	return root
}

func TestInitSeedsDependabot(t *testing.T) {
	root := githubRepo(t)
	write(t, root, "go.mod", "module x\n")
	mustRun(t, root, yes)
	if !strings.Contains(read(t, root, dependabotPath), `package-ecosystem: "gomod"`) {
		t.Fatalf("dependabot.yml = %q", read(t, root, dependabotPath))
	}
	if strings.Contains(read(t, root, manifest.Path), "dependabot") {
		t.Error("dependabot.yml is in the manifest")
	}
}

func TestInitSeedsNothingWithoutEcosystems(t *testing.T) {
	root := githubRepo(t)
	r := mustRun(t, root, yes)
	if exists(root, dependabotPath) {
		t.Error("dependabot.yml seeded in an empty repo")
	}
	if !strings.Contains(r.err, "dependabot: no ecosystems found; nothing seeded") {
		t.Errorf("stderr = %q", r.err)
	}
}

func TestUpgradeLeavesExistingDependabot(t *testing.T) {
	root := githubRepo(t)
	write(t, root, "go.mod", "module x\n")
	mustRun(t, root, yes)
	for _, mod := range []func(*Options){upgrade, all(upgrade, force)} {
		write(t, root, dependabotPath, "# mijn\n")
		r := mustRun(t, root, all(yes, mod))
		if got := read(t, root, dependabotPath); got != "# mijn\n" {
			t.Errorf("dependabot.yml = %q", got)
		}
		if n := strings.Count(r.err, "dependabot: .github/dependabot.yml exists; left as it is\n"); n != 1 {
			t.Errorf("stderr has the line %d times: %q", n, r.err)
		}
	}
}

func TestDependabotSkippedOnGitLab(t *testing.T) {
	root := newRepo(t)
	gitIn(t, root, "remote", "add", "origin", "https://gitlab.com/x/y.git")
	write(t, root, "go.mod", "module x\n")
	r := mustRun(t, root, yes)
	if exists(root, dependabotPath) || strings.Contains(r.err, "dependabot:") {
		t.Errorf("gitlab: exists %v, stderr %q", exists(root, dependabotPath), r.err)
	}
}

func TestSecondUpgradeNoChanges(t *testing.T) {
	root := githubRepo(t)
	write(t, root, "go.mod", "module x\n")
	mustRun(t, root, yes)
	mustRun(t, root, all(yes, upgrade))
	before := snapshot(t, root)
	r := mustRun(t, root, all(yes, upgrade))
	if r.out != "No changes.\n" {
		t.Errorf("second upgrade: %q", r.out)
	}
	if !sameSnapshot(before, snapshot(t, root)) {
		t.Error("second upgrade changed files")
	}
}

const selfHostedWarning = "runs on a self-hosted runner"

func initWithWorkflow(t *testing.T, content string) string {
	t.Helper()
	root := githubRepo(t)
	write(t, root, "go.mod", "module x\n")
	write(t, root, ".github/workflows/ci.yml", content)
	return mustRun(t, root, yes).err
}

func TestWarnsSelfHostedWorkflow(t *testing.T) {
	for name, content := range map[string]string{
		"scalar": "jobs:\n  a:\n    runs-on: self-hosted\n",
		"flow":   "jobs:\n  a:\n    runs-on: [self-hosted, linux]\n",
	} {
		t.Run(name, func(t *testing.T) {
			errOut := initWithWorkflow(t, content)
			want := "dependabot: .github/workflows/ci.yml runs on a self-hosted runner; guard it so Dependabot PRs use a hosted runner (runs-on: ${{ github.actor == 'dependabot[bot]' && 'ubuntu-latest' || 'self-hosted' }})\n"
			if !strings.Contains(errOut, want) {
				t.Errorf("stderr = %q", errOut)
			}
		})
	}
}

func TestNoWarningWhenGuarded(t *testing.T) {
	errOut := initWithWorkflow(t, "jobs:\n  a:\n    runs-on: ${{ github.actor == 'dependabot[bot]' && 'ubuntu-latest' || 'self-hosted' }}\n")
	if strings.Contains(errOut, selfHostedWarning) {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestNoWarningHostedOnly(t *testing.T) {
	errOut := initWithWorkflow(t, "jobs:\n  a:\n    runs-on: ubuntu-latest\n")
	if strings.Contains(errOut, selfHostedWarning) {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestNoWarningSelfHostedInComment(t *testing.T) {
	errOut := initWithWorkflow(t, "# this may NEVER become self-hosted\njobs:\n  a:\n    runs-on: ubuntu-latest # not self-hosted\n")
	if strings.Contains(errOut, selfHostedWarning) {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestWarnsSelfHostedWhenDependabotExists(t *testing.T) {
	root := githubRepo(t)
	write(t, root, dependabotPath, "# mijn\n")
	write(t, root, ".github/workflows/ci.yml", "jobs:\n  a:\n    runs-on: self-hosted\n")
	if r := mustRun(t, root, yes); !strings.Contains(r.err, selfHostedWarning) {
		t.Errorf("stderr = %q", r.err)
	}
}

func TestDetectEcosystemsIgnoresLighthouseWorkflows(t *testing.T) {
	root := newRepo(t)
	writeAll(t, root, ".github/workflows/lighthouse-x.yml")
	got, err := detectEcosystems(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want no ecosystems", got)
	}
}

func TestUpgradeLeavesExistingDependabotYaml(t *testing.T) {
	root := githubRepo(t)
	write(t, root, "go.mod", "module x\n")
	write(t, root, ".github/dependabot.yaml", "# mine\n")
	r := mustRun(t, root, yes)
	if exists(root, dependabotPath) {
		t.Error("dependabot.yml seeded next to dependabot.yaml")
	}
	if !strings.Contains(r.err, "dependabot: .github/dependabot.yaml exists; left as it is\n") {
		t.Errorf("stderr = %q", r.err)
	}
}
