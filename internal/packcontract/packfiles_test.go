package packcontract

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// Files of a package outside its skills: the lighthouse.toml fragment init merges into the
// project config.

// fragmentBase is the project table init writes before a fragment's tables.
const fragmentBase = "[project]\nlanguage = \"en\"\n\n"

// leafKeys returns the dotted key of every non-table value in table.
func leafKeys(prefix string, table map[string]any) []string {
	var keys []string
	for k, v := range table {
		if sub, ok := v.(map[string]any); ok {
			keys = append(keys, leafKeys(prefix+k+".", sub)...)
			continue
		}
		keys = append(keys, prefix+k)
	}
	sort.Strings(keys)
	return keys
}

// Classification: fails when a pack/*/lighthouse.toml.fragment holds a value of the wrong type
// or a key the config reader does not know: init copies the keys into lighthouse.toml as they
// are, and every hook that reads the config would break.
func TestFragmentIsValidConfig(t *testing.T) {
	fragments, err := filepath.Glob(filepath.Join(packRoot, "*", "lighthouse.toml.fragment"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) == 0 {
		t.Fatal("no pack/*/lighthouse.toml.fragment found")
	}
	for _, path := range fragments {
		rel, err := filepath.Rel(packRoot, path)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(filepath.Dir(rel), func(t *testing.T) {
			text := readPack(t, rel)
			if _, err := config.Parse([]byte(fragmentBase+text), "lighthouse.toml"); err != nil {
				t.Fatalf("config.Parse: %v", err)
			}
			var table map[string]any
			if _, err := toml.Decode(text, &table); err != nil {
				t.Fatal(err)
			}
			keys := leafKeys("", table)
			if len(keys) == 0 {
				t.Fatal("the fragment has no keys")
			}
			for _, key := range keys {
				if config.KindOf(key) == "unknown" {
					t.Errorf("%s: unknown key", key)
				}
			}
		})
	}
}
