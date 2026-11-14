package commitgate

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Brake is whether the memory brake (a systemd slice with MemoryHigh/MemoryMax) is there. Reason
// is empty when it is, and then Wrap runs the command inside the slice; Events is the
// memory.events file of the slice.
type Brake struct {
	Reason string
	Wrap   []string
	Events string
}

// DetectBrake checks the brake of slice; the first hit is the reason. env is the environment of
// systemctl (XDG_RUNTIME_DIR included) and of LIGHTHOUSE_GATE_CGROUP_ROOT, the test seam of the
// cgroup root. The check looks at FragmentPath and MemoryMax, not at LoadState: systemd reports
// every slice name loaded, also without a unit file, and such a slice has no ceiling.
func DetectBrake(slice string, env []string, lookPath func(string) (string, error), run func(argv []string, env []string) (stdout, stderr string, err error)) Brake {
	cgroup := getenv(env, "LIGHTHOUSE_GATE_CGROUP_ROOT")
	if cgroup == "" {
		uid := strconv.Itoa(os.Getuid())
		cgroup = "/sys/fs/cgroup/user.slice/user-" + uid + ".slice/user@" + uid + ".service"
	}
	b := Brake{Events: cgroup + "/" + slice + "/memory.events"}
	b.Reason = brakeReason(slice, env, lookPath, run)
	if b.Reason == "" {
		b.Wrap = []string{"systemd-run", "--user", "--scope", "--slice=" + slice, "--quiet", "--", "choom", "-n", "800", "--"}
	}
	return b
}

func brakeReason(slice string, env []string, lookPath func(string) (string, error), run func([]string, []string) (string, string, error)) string {
	if slice == "" {
		return "no gate_slice in host.toml"
	}
	systemctl, err := lookPath("systemctl")
	if err != nil {
		return "no systemctl"
	}
	for _, tool := range []string{"systemd-run", "choom"} {
		if _, err := lookPath(tool); err != nil {
			return "no " + tool
		}
	}
	show, _, err := run([]string{systemctl, "--user", "show", slice, "-p", "FragmentPath", "-p", "MemoryMax"}, env)
	if err != nil {
		return "systemctl --user failed"
	}
	// KEY=VALUE lines in systemd's order, not in that of the -p arguments.
	var fragment, memoryMax string
	for _, line := range strings.Split(show, "\n") {
		key, value, _ := strings.Cut(line, "=")
		switch key {
		case "FragmentPath":
			fragment = value
		case "MemoryMax":
			memoryMax = value
		}
	}
	switch {
	case fragment == "":
		return slice + " not installed"
	case memoryMax == "" || memoryMax == "infinity":
		return slice + " without MemoryMax"
	}
	return ""
}

// runCommand runs argv with env and returns its stdout and stderr apart.
func runCommand(argv, env []string) (string, string, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	return out.String(), errOut.String(), err
}

// eventCount is the value of key in a memory.events file; 0 when the file or the key is missing.
// A value that is not a count is skipped, and a later line with the key still counts.
func eventCount(path, key string) int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		// `read -r key value`: value is the rest of the line, so "high 1 2" is no count.
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 || fields[0] != key || !allDigits(fields[1]) {
			continue
		}
		v, err := strconv.ParseInt(fields[1], 10, 64)
		if err == nil {
			return v
		}
	}
	return 0
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// delta is after minus before; a negative delta (slice recreated) counts as none.
func delta(before, after int64) int64 { return max(after-before, 0) }
