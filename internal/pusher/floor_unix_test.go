//go:build unix

package pusher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A record that is not a small regular file is red, and never hangs or fills memory.
func TestFloorRefusesOddRecordFiles(t *testing.T) {
	for name, place := range map[string]func(f *fixture, path string){
		"fifo": func(f *fixture, path string) {
			if err := syscall.Mkfifo(path, 0o644); err != nil {
				f.t.Fatal(err)
			}
		},
		"symlink to /dev/zero": func(f *fixture, path string) {
			if err := os.Symlink("/dev/zero", path); err != nil {
				f.t.Fatal(err)
			}
		},
		"too big": func(f *fixture, path string) {
			text, _ := json.Marshal(done(f.head()))
			write(f.t, path, string(text)+strings.Repeat(" ", 2<<20))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			gitDir := tgit(t, f.wt, "rev-parse", "--absolute-git-dir")
			dir := filepath.Join(gitDir, "lighthouse", "roborev")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			place(f, filepath.Join(dir, branch+".json"))
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			done := make(chan *Red, 1)
			go func() { _, red := floorCtx(t, ctx, f.req); done <- red }()
			select {
			case red := <-done:
				wantStep(t, red, StepRoborev)
			case <-time.After(15 * time.Second):
				t.Fatal("Floor hangs on the record")
			}
		})
	}
}
