package automemory

import (
	"reflect"
	"testing"
)

func TestWriteTargets(t *testing.T) {
	const m = "/h/.claude/projects/p/memory/a.md"
	tests := []struct {
		name string
		cmd  string
		want []string
	}{
		{"redirect", "echo x > " + m, []string{m}},
		{"append", "echo x >> " + m, []string{m}},
		{"glued redirect", "echo x >" + m, []string{m}},
		{"quoted redirect", `echo x > "` + m + `"`, []string{m}},
		{"tee", "printf x | tee -a " + m, []string{m}},
		{"cp", "cp a.md " + m, []string{m}},
		{"mv", "mv a " + m, []string{m}},
		{"rm", "rm -f " + m, []string{m}},
		{"touch", "touch " + m, []string{m}},
		{"mkdir", "mkdir -p /h/.claude/projects/p/memory", []string{"/h/.claude/projects/p/memory"}},
		{"sed -i", "sed -i 's/a/b/' " + m, []string{m}},
		{"heredoc", "cat > " + m + " <<'EOF'\nx\nEOF", []string{m}},
		{"dev null", "cat " + m + " 2>/dev/null", nil},
		{"fd dup", "ls /h/.claude/projects/p/memory 2>&1", nil},
		{"other redirect", "grep foo " + m + " > /tmp/out", []string{"/tmp/out"}},
		{"cp from memory", "cp " + m + " out/a.md", []string{"out/a.md"}},
		{"and chain", "cd x && echo y > " + m, []string{m}},
		{"git status", "git status", nil},
		{"home unexpanded", "echo x > $HOME/.claude/projects/p/memory/a.md", []string{"$HOME/.claude/projects/p/memory/a.md"}},
		{"tilde unexpanded", "touch ~/.claude/projects/p/memory/a.md", []string{"~/.claude/projects/p/memory/a.md"}},
		{"semicolon glued", "echo a;rm " + m, []string{m}},
		{"or glued", "false||touch " + m, []string{m}},
		{"pipe glued", "echo a|tee " + m, []string{m}},
		{"sudo prefix", "sudo tee " + m, []string{m}},
		{"command prefix", "command rm " + m, []string{m}},
		{"assignment prefix", "X=1 touch " + m, []string{m}},
		{"tee quoted path", `tee "/h/my dir/a.md"`, []string{"/h/my dir/a.md"}},
		{"sed without -i", "sed 's/a/b/' " + m, nil},
		{"stderr append", "cmd 2>> " + m, []string{m}},
		{"both redirect", "cmd &> " + m, []string{m}},
		{"background not redirect", "sleep 1 & touch " + m, []string{m}},
		{"quoted separator", `echo "a;b" > ` + m, []string{m}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WriteTargets(tt.cmd)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WriteTargets(%q) = %q, want %q", tt.cmd, got, tt.want)
			}
		})
	}
}
