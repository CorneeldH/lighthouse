package pins

import (
	"reflect"
	"testing"
)

func TestSkillMetadata(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want map[string]string
	}{
		{"no frontmatter", "# Title\nmetadata:\n  origin: own\n", map[string]string{}},
		{"no metadata block", "---\nname: x\ndescription: y\n---\nbody\n", map[string]string{}},
		{
			"plain and quoted values",
			"---\nname: x\nmetadata:\n  binding: default\n  source: \"#22\"\n  lighthouse: 'core@dev'\n  origin: own\n---\nbody\n",
			map[string]string{"binding": "default", "source": "#22", "lighthouse": "core@dev", "origin": "own"},
		},
		{
			"origin outside metadata is ignored",
			"---\nname: x\norigin: own\nmetadata:\n  binding: default\n---\n",
			map[string]string{"binding": "default"},
		},
		{
			"block ends at next top-level key",
			"---\nmetadata:\n  origin: extended\nother: z\n  origin: own\n---\n",
			map[string]string{"origin": "extended"},
		},
		{
			"body after frontmatter is not read",
			"---\nname: x\n---\nmetadata:\n  origin: own\n",
			map[string]string{},
		},
		{
			"value with a colon keeps the rest",
			"---\nmetadata:\n  source: \"https://example.com/a\"\n---\n",
			map[string]string{"source": "https://example.com/a"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SkillMetadata(c.in); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("SkillMetadata() = %v, want %v", got, c.want)
			}
		})
	}
}
