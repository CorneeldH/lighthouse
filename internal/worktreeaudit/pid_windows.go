//go:build windows

package worktreeaudit

// PidAlive is never measurable on Windows: signal 0 is CTRL_C_EVENT there, so the Python never
// sent it. An unmeasurable pid blocks, so a locked worktree stays rank 2.
func PidAlive(int) *bool { return nil }
