package deps

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/packs"
)

var reqs = []packs.Requirement{
	{Tool: "git", NeededBy: []string{"main-commit-guard"}},
	{Tool: "jq", NeededBy: []string{"main-commit-guard"}},
	{Tool: "python3", NeededBy: []string{"main-commit-guard", "lighthouse_lib.config"}},
	{Tool: "tomllib", NeededBy: []string{"lighthouse_lib.config"}},
}

func checker(missing map[string]bool, runErr error, ran *[]string) Checker {
	return Checker{
		LookPath: func(name string) (string, error) {
			if missing[name] {
				return "", errors.New("not found")
			}
			return "/usr/bin/" + name, nil
		},
		Run: func(name string, args ...string) error {
			*ran = append(*ran, name+" "+args[0]+" "+args[1])
			if args[len(args)-1] != "import tomllib" {
				panic("unexpected python code")
			}
			return runErr
		},
	}
}

func TestMissingTool(t *testing.T) {
	var ran []string
	got := checker(map[string]bool{"jq": true}, nil, &ran).Check(reqs)
	if want := []Missing{{Tool: "jq", NeededBy: []string{"main-commit-guard"}}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestTomllibRunsPython(t *testing.T) {
	var ran []string
	got := checker(nil, errors.New("exit 1"), &ran).Check(reqs)
	if len(ran) != 1 || ran[0] != "python3 -I -c" {
		t.Fatalf("ran %v", ran)
	}
	if want := []Missing{{Tool: "tomllib", NeededBy: []string{"lighthouse_lib.config"}}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestTomllibWithoutPython(t *testing.T) {
	var ran []string
	got := checker(map[string]bool{"python3": true}, nil, &ran).Check(reqs)
	if len(ran) != 0 {
		t.Fatalf("ran %v", ran)
	}
	if len(got) != 2 || got[0].Tool != "python3" || got[1].Tool != "tomllib" {
		t.Fatalf("got %+v", got)
	}
}

func TestAllPresent(t *testing.T) {
	var ran []string
	if got := checker(nil, nil, &ran).Check(reqs); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestDefaultIsUsable(t *testing.T) {
	d := Default()
	if d.LookPath == nil || d.Run == nil {
		t.Fatal("Default has nil fields")
	}
}

func TestDefaultTomllibCheckIgnoresTheCwd(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	planted := "open(" + "\"" + marker + "\"" + ", \"w\").close()\n"
	if err := os.WriteFile(filepath.Join(dir, "tomllib.py"), []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	Default().Check([]packs.Requirement{{Tool: "tomllib", NeededBy: []string{"x"}}})
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a tomllib.py in the cwd ran during the dependency check")
	}
}
