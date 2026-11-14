package commitgate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

// lookOnly finds exactly the named tools, at /fake/<name>.
func lookOnly(names ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, n := range names {
			if n == name {
				return "/fake/" + name, nil
			}
		}
		return "", exec.ErrNotFound
	}
}

// showReply is a fake `systemctl --user show`: it checks the arguments and the env, and answers out.
func showReply(t *testing.T, wantEnv string, out string, err error) func([]string, []string) (string, string, error) {
	return func(argv, env []string) (string, string, error) {
		t.Helper()
		want := []string{"/fake/systemctl", "--user", "show", "x.slice", "-p", "FragmentPath", "-p", "MemoryMax"}
		if !reflect.DeepEqual(argv, want) {
			t.Errorf("argv = %q, want %q", argv, want)
		}
		if wantEnv != "" && !contains(env, wantEnv) {
			t.Errorf("env %q lacks %q", env, wantEnv)
		}
		return out, "", err
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func neverRun(t *testing.T) func([]string, []string) (string, string, error) {
	return func([]string, []string) (string, string, error) {
		t.Helper()
		t.Error("systemctl ran")
		return "", "", nil
	}
}

func TestDetectBrakeReasons(t *testing.T) {
	all := lookOnly("systemctl", "systemd-run", "choom")
	cases := []struct {
		name  string
		slice string
		look  func(string) (string, error)
		run   func([]string, []string) (string, string, error)
		want  string
	}{
		{"no slice", "", all, neverRun(t), "no gate_slice in host.toml"},
		{"no systemctl", "x.slice", lookOnly("systemd-run", "choom"), neverRun(t), "no systemctl"},
		{"no systemd-run", "x.slice", lookOnly("systemctl", "choom"), neverRun(t), "no systemd-run"},
		{"no choom", "x.slice", lookOnly("systemctl", "systemd-run"), neverRun(t), "no choom"},
		{"show fails", "x.slice", all, showReply(t, "", "", errors.New("exit 1")), "systemctl --user failed"},
		{"no unit file", "x.slice", all, showReply(t, "", "MemoryMax=1073741824\nFragmentPath=\n", nil), "x.slice not installed"},
		{"no MemoryMax", "x.slice", all, showReply(t, "", "FragmentPath=/etc/x.slice\n", nil), "x.slice without MemoryMax"},
		{"infinite", "x.slice", all, showReply(t, "", "MemoryMax=infinity\nFragmentPath=/etc/x.slice\n", nil), "x.slice without MemoryMax"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := DetectBrake(c.slice, []string{"A=1"}, c.look, c.run)
			if b.Reason != c.want || b.Wrap != nil {
				t.Fatalf("DetectBrake = %+v, want reason %q and no wrap", b, c.want)
			}
		})
	}
}

func TestDetectBrakeWrapsTheCommandInTheSlice(t *testing.T) {
	env := []string{"XDG_RUNTIME_DIR=/run/user/7", "LIGHTHOUSE_GATE_CGROUP_ROOT=/cg"}
	b := DetectBrake("x.slice", env, lookOnly("systemctl", "systemd-run", "choom"),
		showReply(t, "XDG_RUNTIME_DIR=/run/user/7", "FragmentPath=/etc/x.slice\nMemoryMax=1073741824\n", nil))
	want := Brake{
		Wrap:   []string{"systemd-run", "--user", "--scope", "--slice=x.slice", "--quiet", "--", "choom", "-n", "800", "--"},
		Events: "/cg/x.slice/memory.events",
	}
	if !reflect.DeepEqual(b, want) {
		t.Fatalf("DetectBrake = %+v, want %+v", b, want)
	}
}

func TestDetectBrakeEventsDefaultToTheUserCgroup(t *testing.T) {
	b := DetectBrake("x.slice", nil, lookOnly(), neverRun(t))
	uid := strconv.Itoa(os.Getuid())
	want := "/sys/fs/cgroup/user.slice/user-" + uid + ".slice/user@" + uid + ".service/x.slice/memory.events"
	if b.Events != want {
		t.Fatalf("Events = %q, want %q", b.Events, want)
	}
}

func TestEventCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.events")
	if got := eventCount(path, "oom_kill"); got != 0 {
		t.Fatalf("missing file = %d, want 0", got)
	}
	text := "low 0\nhigh 12\nmax x\noom 3\noom_kill 4\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]int64{"high": 12, "oom_kill": 4, "max": 0, "absent": 0, "oom": 3} {
		if got := eventCount(path, key); got != want {
			t.Errorf("eventCount(%s) = %d, want %d", key, got, want)
		}
	}
}

func TestDelta(t *testing.T) {
	for _, c := range []struct{ before, after, want int64 }{{1, 3, 2}, {3, 3, 0}, {5, 2, 0}} {
		if got := delta(c.before, c.after); got != c.want {
			t.Errorf("delta(%d, %d) = %d, want %d", c.before, c.after, got, c.want)
		}
	}
}
