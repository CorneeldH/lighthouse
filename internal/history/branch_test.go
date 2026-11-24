package history

import "testing"

func TestResolveBranch(t *testing.T) {
	cases := []struct {
		name, cmd, git string
		known          map[string]string
		want           string
		ok             bool
	}{
		{"refspec target", "git push origin HEAD:main", "x", nil, "main", true},
		{"origin main", "git push origin main", "x", nil, "main", true},
		{"refs/heads", "git push origin HEAD:refs/heads/release/r1", "x", nil, "release/r1", true},
		{"-C unknown", "git -C /wt push", "main", nil, "", false},
		{"-C known", "git -C /wt push", "main", map[string]string{"/wt": "issue-1"}, "issue-1", true},
		{"cd unknown", "cd /wt && git push", "main", map[string]string{}, "", false},
		{"cd known", "cd /wt && git push", "main", map[string]string{"/wt": "issue-1"}, "issue-1", true},
		{"cd known empty branch", "cd /wt && git push", "main", map[string]string{"/wt": ""}, "", false},
		{"plain push", "git push", "feat", nil, "feat", true},
		{"push -u origin HEAD", "git push -u origin HEAD", "feat", nil, "feat", true},
		{"refspec beats cd", "cd /wt && git push origin HEAD:main", "x", nil, "main", true},
		{"no git branch", "git push", "", nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ResolveBranch(c.cmd, c.git, c.known)
			if got != c.want || ok != c.ok {
				t.Fatalf("got %q,%v want %q,%v", got, ok, c.want, c.ok)
			}
		})
	}
}
