package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Classification: these tests fail when the headings command maps an outcome to the wrong exit
// code or loses the message; the parsing itself is covered by the parity fixtures.
func TestHeadingsCommand(t *testing.T) {
	dir := t.TempDir()
	headings := filepath.Join(dir, "headings.toml")
	lighthouse := filepath.Join(dir, "lighthouse.toml")
	if err := os.WriteFile(headings, []byte("[a]\nnl = \"Een\"\nen = \"One\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lighthouse, []byte("[project]\nlanguage = \"nl\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_HEADINGS", headings)
	t.Setenv("LIGHTHOUSE_TOML", lighthouse)
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{"known", []string{"a"}, 0, "Een\n", ""},
		{"unknown", []string{"b"}, 2, "", "headings: unknown key b\n"},
		{"no key", nil, 2, "", "headings: usage: lighthouse headings <key>\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			root := NewRoot(bytes.NewReader(nil), &out, &errOut)
			root.SetArgs(append([]string{"headings"}, tt.args...))
			code := execute(root, &errOut)
			if code != tt.wantCode || out.String() != tt.wantOut || errOut.String() != tt.wantErr {
				t.Errorf("code %d out %q err %q, want %d %q %q", code, out.String(), errOut.String(), tt.wantCode, tt.wantOut, tt.wantErr)
			}
		})
	}
}

// Classification: fails when the pack table loses the `map_placement` key or changes its Dutch
// or English variant; write-issue and map-planned read that heading as a contract.
func TestHeadingsMapPlacementInPackTable(t *testing.T) {
	dir := t.TempDir()
	lighthouse := filepath.Join(dir, "lighthouse.toml")
	t.Setenv("LIGHTHOUSE_HEADINGS", filepath.Join("..", "..", "pack", "core", "headings.toml"))
	t.Setenv("LIGHTHOUSE_TOML", lighthouse)
	for language, want := range map[string]string{"nl": "Op de kaart\n", "en": "On the map\n"} {
		t.Run(language, func(t *testing.T) {
			if err := os.WriteFile(lighthouse, []byte("[project]\nlanguage = \""+language+"\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			root := NewRoot(bytes.NewReader(nil), &out, &errOut)
			root.SetArgs([]string{"headings", "map_placement"})
			if code := execute(root, &errOut); code != 0 || out.String() != want || errOut.String() != "" {
				t.Errorf("code %d out %q err %q, want 0 %q", code, out.String(), errOut.String(), want)
			}
		})
	}
}
