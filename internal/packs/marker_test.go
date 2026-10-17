package packs

import "testing"

func TestMark(t *testing.T) {
	cases := []struct {
		name, path, in, want string
	}{
		{"shebang", "x.sh", "#!/bin/bash\necho\n", "#!/bin/bash\n# lighthouse: core@dev\necho\n"},
		{"no shebang", "x.py", "import os\n", "# lighthouse: core@dev\nimport os\n"},
		{"python shebang", "x.py", "#!/usr/bin/env python3\nimport os\n", "#!/usr/bin/env python3\n# lighthouse: core@dev\nimport os\n"},
		{"yaml", "x.yml", "on: push\n", "# lighthouse: core@dev\non: push\n"},
		{"yaml long", "x.yaml", "on: push\n", "# lighthouse: core@dev\non: push\n"},
		{"bash", "x.bash", "echo\n", "# lighthouse: core@dev\necho\n"},
		{"existing marker", "x.sh", "#!/bin/sh\n# lighthouse: core@v0.1.0\n", "#!/bin/sh\n# lighthouse: core@dev\n"},
		{"existing marker no shebang", "x.py", "# lighthouse: core@v0.1.0\nx = 1\n", "# lighthouse: core@dev\nx = 1\n"},
		{"empty file", "__init__.py", "", "# lighthouse: core@dev\n"},
		{"frontmatter without metadata", "a.md", "---\nname: x\n---\nbody\n", "---\nname: x\nmetadata:\n  lighthouse: \"core@dev\"\n---\nbody\n"},
		{"frontmatter with metadata", "a.md", "---\nmetadata:\n  domain: y\n---\n", "---\nmetadata:\n  lighthouse: \"core@dev\"\n  domain: y\n---\n"},
		{"frontmatter with marker", "a.md", "---\nmetadata:\n  lighthouse: \"core@v0.1.0\"\n  domain: y\n---\n", "---\nmetadata:\n  lighthouse: \"core@dev\"\n  domain: y\n---\n"},
		{"json", "s.json", "{}", "{}"},
		{"pr template", ".github/pull_request_template.md", "---\na: b\n---\n", "---\na: b\n---\n"},
		{"md without frontmatter", "README.md", "# t\n", "# t\n"},
		{"toml", "x.toml", "a = 1\n", "a = 1\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Mark(c.path, []byte(c.in), "core", "dev")
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Fatalf("got %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestMarkRejectsInlineMetadata(t *testing.T) {
	if _, err := Mark("a.md", []byte("---\nmetadata: {a: b}\n---\n"), "core", "dev"); err == nil {
		t.Fatal("no error for inline metadata")
	}
}

func TestMarkRejectsUnclosedFrontmatter(t *testing.T) {
	if _, err := Mark("a.md", []byte("---\nname: x\nbody\n"), "core", "dev"); err == nil {
		t.Fatal("no error for unclosed frontmatter")
	}
}
