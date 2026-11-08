package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runGate(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	t.Setenv("LIGHTHOUSE_GATE_FILES", "")
	var out, errOut bytes.Buffer
	root := NewRoot(bytes.NewReader(nil), &out, &errOut)
	root.SetArgs(append([]string{"gate"}, args...))
	code = execute(root, &errOut)
	return code, out.String(), errOut.String()
}

func TestGateUnknownSubcommandIsUnchecked(t *testing.T) {
	for _, args := range [][]string{{}, {"bogus", "--", "x"}, {"nod", "--", "lint"}} {
		code, _, errOut := runGate(t, args...)
		if code != 3 || !strings.Contains(errOut, "COULD NOT CHECK") {
			t.Errorf("gate %q: code=%d stderr=%q", args, code, errOut)
		}
	}
}

func TestGateCommandPassesFlags(t *testing.T) {
	code, out, errOut := runGate(t, "go", "--", "sh", "-c", `echo "$@"`, "x", "-race")
	if code != 0 || out != "-race ./...\n" {
		t.Errorf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestGateGoPassesExitCode(t *testing.T) {
	code, _, errOut := runGate(t, "go", "--", "sh", "-c", "exit 5")
	if code != 5 || errOut != "" {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}

func TestGateNodePassesFlags(t *testing.T) {
	root := t.TempDir()
	web := filepath.Join(root, "web")
	for path, body := range map[string]string{
		filepath.Join(web, "node_modules", ".keep"): "",
		filepath.Join(web, "package.json"):          `{"scripts": {"lint": "x"}}`,
		filepath.Join(root, "bin", "npm"):           "#!/bin/sh\necho \"npm $*\"\n",
		filepath.Join(root, "bin", "node"):          "#!/bin/sh\nexit 0\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(root)
	code, out, errOut := runGate(t, "node", "--dir", "web", "--", "lint")
	want := "gate-node: web — npm run lint\nnpm run --silent lint\ngate-node: passed — lint\n"
	if code != 0 || out != want {
		t.Errorf("code=%d stdout=%q stderr=%q, want stdout %q", code, out, errOut, want)
	}
}
