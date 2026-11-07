package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Relocate moves the path argument of command into worktree, the run's own. A handoff written
// in another checkout of the repo names its spec or plan there (#237): a path the run cannot
// read, because its devcontainer mounts only the forge worktrees. The longest tail of at least
// two components that names a file in worktree replaces the path. A command without a path
// argument, or with one already in worktree, stays as it is; a path of which no such tail is a
// file in worktree is an error.
func Relocate(command, worktree string) (string, error) {
	fields := strings.Fields(command)
	if len(fields) != 2 || !pathArg.MatchString(fields[1]) {
		return command, nil
	}
	p, root := filepath.Clean(fields[1]), filepath.Clean(worktree)
	if strings.HasPrefix(p, root+string(filepath.Separator)) {
		return command, nil
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i := 0; i <= len(parts)-2; i++ {
		candidate := filepath.Join(append([]string{root}, parts[i:]...)...)
		if st, err := os.Stat(candidate); err == nil && st.Mode().IsRegular() {
			return fields[0] + " " + candidate, nil
		}
	}
	return "", fmt.Errorf("%s is not in the worktree %s, and no file there has its tail", fields[1], worktree)
}
