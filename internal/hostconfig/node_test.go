package hostconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/testpath"
)

func TestLoadNodeAbsentIsFalse(t *testing.T) {
	for name, body := range map[string]string{
		"no node table": "[dispatch]\nslots = 2\n",
		"no file":       "",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "missing.toml")
			if body != "" {
				path = writeHost(t, body)
			}
			n, ok, err := LoadNode(path)
			if err != nil || ok || n != (NodeServe{}) {
				t.Errorf("LoadNode = %+v, %v, %v; want zero, false, nil", n, ok, err)
			}
		})
	}
	if _, ok, err := LoadNode(""); ok || err != nil {
		t.Errorf("LoadNode(\"\") = %v, %v", ok, err)
	}
}

func TestLoadNodePresent(t *testing.T) {
	tok := testpath.Abs("tok", "n")
	n, ok, err := LoadNode(writeHost(t, "[node]\ntoken_file = "+lit(tok)+"\n"))
	if err != nil || !ok || n.TokenFile != tok {
		t.Errorf("LoadNode = %+v, %v, %v", n, ok, err)
	}
}

func TestLoadNodeTokenFileMustBeAbsolute(t *testing.T) {
	for name, body := range map[string]string{
		"relative": "[node]\ntoken_file = \"rel/tok\"\n",
		"missing":  "[node]\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := LoadNode(writeHost(t, body))
			if err == nil || !strings.Contains(err.Error(), "node.token_file") {
				t.Errorf("err = %v, want one naming node.token_file", err)
			}
		})
	}
}

func TestValidateNodeForgeNeedsToken(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(good, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, file string
		ok         bool
	}{
		{"present", good, true},
		{"missing", filepath.Join(dir, "none"), false},
		{"empty", empty, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := DefaultDispatch()
			d.Forge.TokenFile = c.file
			err := ValidateNodeForge(d)
			if c.ok && err != nil {
				t.Fatalf("err = %v", err)
			}
			if !c.ok && (err == nil || !strings.Contains(err.Error(), "dispatch.forge.token_file")) {
				t.Fatalf("err = %v, want one naming dispatch.forge.token_file", err)
			}
		})
	}
}
