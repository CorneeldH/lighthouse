package checkpointscan

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Prefix starts every line the scan says on stderr.
const Prefix = "secret-scan"

// ceilingRCs is the return code of a command the memory ceiling killed. Measured on
// systemd 259 (Fedora 44): `systemd-run --scope` execs the command, so the OOM kill hits the
// process we wait for and its status carries SIGKILL: -9 from a wait status, 137 from a shell.
// `memory.events` is not read at all: the scope cgroup is gone as soon as the scope is empty.
var ceilingRCs = []int{137, -9}

// Run is one bounded command.
type Run struct {
	// ReturnCode is the exit code, or -<signal> when a signal killed the command.
	ReturnCode int
	// CeilingHit is true when the command died the way the memory ceiling kills it, and there
	// was a ceiling.
	CeilingHit bool
	// Output is stdout and stderr together.
	Output string
	// PeakRSSKB is the child's peak resident set in KB; nil where it is not measured.
	PeakRSSKB *int
}

// gomaxprocs: 200% means two CPUs; rounded down, at least one.
func gomaxprocs(quota string) (int, error) {
	n, err := strconv.Atoi(strings.TrimRight(quota, "%"))
	if err != nil {
		return 0, CouldNotCheck{Msg: fmt.Sprintf("scan_cpu_quota %q is not a percentage", quota)}
	}
	return max(1, n/100), nil
}

// The probe runs once per process: every batch of one scan is bounded the same way. A reason
// is said once per process, not once per batch.
var bounding struct {
	mu     sync.Mutex
	probed bool
	reason string
	said   map[string]bool
}

// resetBounding forgets the probe and the reasons said; for tests, which change PATH.
func resetBounding() {
	bounding.mu.Lock()
	defer bounding.mu.Unlock()
	bounding.probed, bounding.reason, bounding.said = false, "", nil
}

// noCeilingReason says why there is no scope here; "" means there is one.
func noCeilingReason() string {
	bounding.mu.Lock()
	defer bounding.mu.Unlock()
	if !bounding.probed {
		bounding.reason, bounding.probed = probeCeiling(), true
	}
	return bounding.reason
}

// sayOnce reports whether reason has not been said yet, and marks it said.
func sayOnce(reason string) bool {
	bounding.mu.Lock()
	defer bounding.mu.Unlock()
	if bounding.said[reason] {
		return false
	}
	if bounding.said == nil {
		bounding.said = map[string]bool{}
	}
	bounding.said[reason] = true
	return true
}

// wrap is the bounded command line, and the reason when it has no memory ceiling.
func wrap(argv []string, limits Limits) ([]string, string, error) {
	procs, err := gomaxprocs(limits.CPUQuota)
	if err != nil {
		return nil, "", err
	}
	env := "GOMAXPROCS=" + strconv.Itoa(procs)
	if reason := noCeilingReason(); reason != "" {
		return unboundedCommand(env, argv), reason, nil
	}
	scope := []string{
		"systemd-run", "--user", "--scope", "--quiet",
		"-p", "MemoryMax=" + limits.MemoryMax,
		"-p", "MemorySwapMax=0",
		"-p", "CPUQuota=" + limits.CPUQuota,
		// Unload the scope when it ends, failed or not: else every ceiling hit leaves a failed
		// unit behind in `systemctl --user --failed`.
		"--collect",
	}
	if limits.Slice != nil && *limits.Slice != "" {
		scope = append(scope, "--slice="+*limits.Slice)
	}
	scope = append(scope, "--", "env", env, "choom", "-n", "800", "--", "nice", "-n", "10")
	return append(scope, argv...), "", nil
}

// RunBounded runs argv bounded, in cwd when it is not empty, and waits for it. Every error is a
// CouldNotCheck: a bad CPU quota, or a command that cannot start.
func RunBounded(argv []string, limits Limits, cwd string) (Run, error) {
	return runBounded(argv, limits, cwd, os.Stderr)
}

func runBounded(argv []string, limits Limits, cwd string, stderr io.Writer) (Run, error) {
	wrapped, reason, err := wrap(argv, limits)
	if err != nil {
		return Run{}, err
	}
	if reason != "" && sayOnce(reason) {
		_, _ = fmt.Fprintf(stderr, "%s: no memory ceiling: %s\n", Prefix, reason)
	}
	// A file, not a pipe: wait4 does not drain a pipe, and a full one would hang the child.
	out, err := os.CreateTemp("", "bounded-*")
	if err != nil {
		return Run{}, CouldNotCheck{Msg: fmt.Sprintf("cannot capture the output of %s: %v", filepath.Base(argv[0]), err)}
	}
	defer os.Remove(out.Name()) //nolint:errcheck // a temp file
	defer out.Close()           //nolint:errcheck // read-only from here on
	rc, rss, err := execWait(wrapped, cwd, out)
	if err != nil {
		return Run{}, CouldNotCheck{Msg: fmt.Sprintf("%s did not start: %v", filepath.Base(argv[0]), err)}
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return Run{}, CouldNotCheck{Msg: fmt.Sprintf("cannot read the output of %s: %v", filepath.Base(argv[0]), err)}
	}
	output, err := io.ReadAll(out)
	if err != nil {
		return Run{}, CouldNotCheck{Msg: fmt.Sprintf("cannot read the output of %s: %v", filepath.Base(argv[0]), err)}
	}
	return Run{ReturnCode: rc, CeilingHit: reason == "" && slices.Contains(ceilingRCs, rc), Output: string(output), PeakRSSKB: rss}, nil
}
