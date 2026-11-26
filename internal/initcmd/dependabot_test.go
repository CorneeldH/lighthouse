package initcmd

import (
	"reflect"
	"testing"
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
