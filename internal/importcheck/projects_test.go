package importcheck

import (
	"reflect"
	"testing"
)

func dirs(ps []Project) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Dir)
	}
	return out
}

func TestDiscoverGoModules(t *testing.T) {
	root := gitRepo(t, map[string]string{"go.mod": "", "web/go.mod": "", "main.go": ""})
	ps, err := Discover(root, "go")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".", "web"}; !reflect.DeepEqual(dirs(ps), want) {
		t.Errorf("got %v, want %v", dirs(ps), want)
	}
	if ps[0].Lang != "go" {
		t.Errorf("lang %q", ps[0].Lang)
	}
}

func TestDiscoverIgnoresUntracked(t *testing.T) {
	root := gitRepo(t, map[string]string{"go.mod": ""})
	writeUntracked(t, root, "other/go.mod")
	ps, err := Discover(root, "go")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"."}; !reflect.DeepEqual(dirs(ps), want) {
		t.Errorf("got %v, want %v", dirs(ps), want)
	}
}

func TestDiscoverNodeWorkspaceRoots(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"package.json workspaces", map[string]string{
			"package.json":            `{"workspaces": ["packages/*"]}`,
			"packages/a/package.json": "{}",
		}, []string{"."}},
		{"workspaces object form", map[string]string{
			"package.json":            `{"workspaces": {"packages": ["packages/*"]}}`,
			"packages/a/package.json": "{}",
		}, []string{"."}},
		{"pnpm workspace", map[string]string{
			"package.json":            `{}`,
			"pnpm-workspace.yaml":     "packages:\n  - 'packages/*'\n",
			"packages/a/package.json": "{}",
		}, []string{"."}},
		{"not matching the pattern", map[string]string{
			"package.json":         `{"workspaces": ["packages/*"]}`,
			"tools/a/package.json": "{}",
		}, []string{".", "tools/a"}},
		{"two separate", map[string]string{
			"a/package.json": "{}", "b/package.json": "{}",
		}, []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := gitRepo(t, tt.files)
			ps, err := Discover(root, "node")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(dirs(ps), tt.want) {
				t.Errorf("got %v, want %v", dirs(ps), tt.want)
			}
		})
	}
}

func TestDiscoverWeirdFilenames(t *testing.T) {
	root := gitRepo(t, map[string]string{"my app/go.mod": "", "café/go.mod": ""})
	ps, err := Discover(root, "go")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"café", "my app"}; !reflect.DeepEqual(dirs(ps), want) {
		t.Errorf("got %v, want %v", dirs(ps), want)
	}
}

func TestDiscoverSkipsTestdataAndNodeModules(t *testing.T) {
	root := gitRepo(t, map[string]string{
		"internal/x/testdata/go.mod": "", "node_modules/p/package.json": "{}",
		"web/node_modules/q/package.json": "{}",
	})
	for _, l := range []Lang{"go", "node"} {
		ps, err := Discover(root, l)
		if err != nil || len(ps) != 0 {
			t.Errorf("%s: got %v, %v; want none", l, ps, err)
		}
	}
}

func TestInScope(t *testing.T) {
	web := Project{Lang: "node", Dir: "web"}
	root := Project{Lang: "node", Dir: "."}
	tests := []struct {
		name         string
		p            Project
		files        []string
		all, touched bool
	}{
		{"file under dir", web, []string{"web/x.ts"}, false, true},
		{"manifest of project", web, []string{"web/package.json"}, true, true},
		{"manifest of other dir", web, []string{"package.json"}, false, false},
		{"sibling prefix", web, []string{"webapp/x.ts"}, false, false},
		{"root takes any file", root, []string{"x.ts"}, false, true},
		{"root manifest", root, []string{"yarn.lock"}, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			all, touched := InScope(tt.p, tt.files)
			if all != tt.all || touched != tt.touched {
				t.Errorf("got %v %v, want %v %v", all, touched, tt.all, tt.touched)
			}
		})
	}
}
