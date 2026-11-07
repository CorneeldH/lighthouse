//go:build linux

package local

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"
)

// fakeProc writes <root>/<name>/<file> with content.
func fakeProc(t *testing.T, root, name, file, content string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if file == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func markedTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	fakeProc(t, root, "10", "environ", "A=1\x00"+markVar+"=sid1\x00")
	fakeProc(t, root, "11", "environ", markVar+"=sid10\x00")
	fakeProc(t, root, "12", "environ", "A=1\x00")
	fakeProc(t, root, "self", "environ", markVar+"=sid1\x00")
	fakeProc(t, root, "13", "", "")
	return root
}

func TestMarkedFindsExactMark(t *testing.T) {
	p := procFS{root: markedTree(t), uid: os.Getuid(), self: 999}
	got, err := p.Marked("sid1")
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{10}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Marked = %v, want %v", got, want)
	}
}

func TestMarkedSkipsSelf(t *testing.T) {
	p := procFS{root: markedTree(t), uid: os.Getuid(), self: 10}
	got, err := p.Marked("sid1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Marked = %v, want none", got)
	}
}

func TestMarkedSkipsOtherUser(t *testing.T) {
	p := procFS{root: markedTree(t), uid: os.Getuid() + 1, self: 999}
	got, err := p.Marked("sid1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Marked = %v, want none", got)
	}
}

func TestMarkedUnreadableProcIsError(t *testing.T) {
	p := procFS{root: filepath.Join(t.TempDir(), "nope"), uid: os.Getuid(), self: 999}
	if _, err := p.Marked("sid1"); err == nil {
		t.Fatal("missing root: want error")
	}
	q := procFS{root: markedTree(t), uid: os.Getuid(), self: 999}
	if _, err := q.Marked(""); err == nil {
		t.Fatal("empty sid: want error")
	}
}

func TestStartTimeParsesField22(t *testing.T) {
	root := t.TempDir()
	fakeProc(t, root, "42", "stat", "42 (we ird) x) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 777 19 20")
	fakeProc(t, root, "43", "stat", "43 (we ird) x) Z 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 777 19 20")
	fakeProc(t, root, "44", "stat", "42 (x) S 1")
	p := procFS{root: root, uid: os.Getuid(), self: 999}

	start, ok, err := p.StartTime(42)
	if err != nil || !ok || start != 777 {
		t.Fatalf("StartTime(42) = %d, %v, %v; want 777, true, nil", start, ok, err)
	}
	start, ok, err = p.StartTime(41)
	if err != nil || ok || start != 0 {
		t.Fatalf("StartTime(gone) = %d, %v, %v; want 0, false, nil", start, ok, err)
	}
	start, ok, err = p.StartTime(43)
	if err != nil || ok || start != 0 {
		t.Fatalf("StartTime(zombie) = %d, %v, %v; want 0, false, nil", start, ok, err)
	}
	if _, _, err = p.StartTime(44); err == nil {
		t.Fatal("short stat: want error")
	}
}

func startMarkedSleep(t *testing.T, sid string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	cmd.Env = append(os.Environ(), markVar+"="+sid)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

func TestMarkedRealProcess(t *testing.T) {
	sid := uniqueSID()
	cmd := startMarkedSleep(t, sid)
	got, err := newProcFS().Marked(sid)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int{cmd.Process.Pid}) {
		t.Fatalf("Marked = %v, want [%d]", got, cmd.Process.Pid)
	}
}

func TestSignalRealAndGone(t *testing.T) {
	cmd := startMarkedSleep(t, uniqueSID())
	p := newProcFS()
	if err := p.Signal(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	ee, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("Wait = %v, want ExitError", err)
	}
	ws := ee.Sys().(syscall.WaitStatus)
	if !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Fatalf("wait status = %v, want killed by SIGTERM", ws)
	}
	if err := p.Signal(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatalf("Signal on gone pid = %v, want nil", err)
	}
}

func uniqueSID() string {
	return fmt.Sprintf("test-%d-%d", os.Getpid(), time.Now().UnixNano())
}
