//go:build unix

package checkpointscan

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

func lim(quota string, slice *string) Limits {
	return Limits{MemoryMax: "2G", CPUQuota: quota, Slice: slice}
}

func TestWrapBuildsScope(t *testing.T) {
	stubPath(t, boundedStubs())

	argv, reason, err := wrap([]string{"gitleaks", "dir", "x"}, lim("200%", nil))

	noErr(t, err)
	eq(t, "", reason)
	eq(t, []string{
		"systemd-run", "--user", "--scope", "--quiet",
		"-p", "MemoryMax=2G", "-p", "MemorySwapMax=0", "-p", "CPUQuota=200%",
		"--collect", "--", "env", "GOMAXPROCS=2", "choom", "-n", "800", "--",
		"nice", "-n", "10", "gitleaks", "dir", "x",
	}, argv)
}

func TestWrapAddsSliceBeforeTheSeparator(t *testing.T) {
	stubPath(t, boundedStubs())
	slice := "s.slice"

	argv, reason, err := wrap([]string{"gitleaks"}, lim("200%", &slice))

	noErr(t, err)
	eq(t, "", reason)
	eq(t, []string{"--collect", "--slice=s.slice", "--", "env"}, argv[10:14])
}

func TestGomaxprocsFollowsQuota(t *testing.T) {
	stubPath(t, boundedStubs())
	for quota, want := range map[string]string{"200%": "GOMAXPROCS=2", "250%": "GOMAXPROCS=2", "50%": "GOMAXPROCS=1", "400%": "GOMAXPROCS=4"} {
		argv, _, err := wrap([]string{"gitleaks"}, lim(quota, nil))
		noErr(t, err)
		isTrue(t, strings.Contains(joined(argv), " "+want+" "), quota)
	}
}

func TestWrapQuotaThatIsNoPercentageIsCouldNotCheck(t *testing.T) {
	stubPath(t, boundedStubs())

	_, _, err := wrap([]string{"gitleaks"}, lim("lots", nil))

	contains(t, cnc(t, err).Msg, "lots")
}

func TestWrapReasonLadder(t *testing.T) {
	chain := []string{"env", "nice", "sh"}
	cases := []struct {
		name   string
		stubs  map[string]string
		reason string
	}{
		{"no-systemctl", map[string]string{"systemd-run": passThrough, "choom": passThrough}, "no systemctl"},
		{"no-systemd-run", map[string]string{"systemctl": systemctlOK, "choom": passThrough}, "no systemd-run"},
		{"no-choom", map[string]string{"systemctl": systemctlOK, "systemd-run": passThrough}, "no choom"},
		{"no-user-bus", map[string]string{"systemctl": "#!/bin/sh\nexit 1\n", "systemd-run": passThrough, "choom": passThrough}, "systemctl --user failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			onlyPath(t, tc.stubs, chain...)

			argv, reason, err := wrap([]string{"gitleaks"}, lim("200%", nil))

			noErr(t, err)
			eq(t, tc.reason, reason)
			// Without a scope the command still gets GOMAXPROCS and nice.
			eq(t, []string{"env", "GOMAXPROCS=2", "nice", "-n", "10", "gitleaks"}, argv)
		})
	}
}

func TestProbeIsCachedPerProcess(t *testing.T) {
	count := t.TempDir() + "/probes"
	// Succeeds on the first probe only.
	systemctl := fmt.Sprintf("#!/bin/sh\necho x >> %q\n[ -e %q ] && exit 1\n: > %q\n", count, count+".seen", count+".seen")
	stubPath(t, map[string]string{"systemctl": systemctl, "systemd-run": passThrough, "choom": passThrough})

	first, _, _ := wrap([]string{"gitleaks"}, lim("200%", nil))
	second, reason, _ := wrap([]string{"gitleaks"}, lim("200%", nil))

	eq(t, "", reason)
	eq(t, first, second)
	raw, err := os.ReadFile(count)
	noErr(t, err)
	eq(t, 1, strings.Count(string(raw), "x"))
}

func TestRuntimeDirIsRepairedForSystemctl(t *testing.T) {
	runtime := fmt.Sprintf("/run/user/%d", os.Getuid())
	if st, err := os.Stat(runtime); err != nil || !st.IsDir() {
		t.Skipf("%s does not exist on this host", runtime)
	}
	// VS Code runs without XDG_RUNTIME_DIR; the probe restores it for systemctl.
	systemctl := fmt.Sprintf("#!/bin/sh\n[ \"$XDG_RUNTIME_DIR\" = %q ] || exit 1\nexit 0\n", runtime)
	stubs := boundedStubs()
	stubs["systemctl"] = systemctl
	stubPath(t, stubs)
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")

	_, reason, err := wrap([]string{"gitleaks"}, lim("200%", nil))

	noErr(t, err)
	eq(t, "", reason)
}

func TestWrapWithoutSystemdRunReportsNoCeiling(t *testing.T) {
	onlyPath(t, map[string]string{"systemctl": systemctlOK, "choom": passThrough}, "env", "nice", "sh")
	var stderr bytes.Buffer

	run, err := runBounded([]string{"sh", "-c", "echo ran"}, lim("200%", nil), "", &stderr)

	noErr(t, err)
	eq(t, 0, run.ReturnCode)
	eq(t, "ran\n", run.Output)
	eq(t, "secret-scan: no memory ceiling: no systemd-run\n", stderr.String())
}

func TestNoCeilingReasonIsSaidOncePerProcess(t *testing.T) {
	onlyPath(t, map[string]string{"systemctl": systemctlOK}, "env", "nice", "sh")
	var stderr bytes.Buffer

	_, err := runBounded([]string{"sh", "-c", "true"}, lim("200%", nil), "", &stderr)
	noErr(t, err)
	_, err = runBounded([]string{"sh", "-c", "true"}, lim("200%", nil), "", &stderr)
	noErr(t, err)

	eq(t, 1, strings.Count(stderr.String(), "no memory ceiling"))
}

func TestRunBoundedPassesRCAndOutputThrough(t *testing.T) {
	stubPath(t, boundedStubs())
	for _, rc := range []int{0, 1, 5} {
		run, err := runBounded([]string{"sh", "-c", fmt.Sprintf("echo out; echo err >&2; exit %d", rc)}, lim("200%", nil), "", &bytes.Buffer{})

		noErr(t, err)
		eq(t, rc, run.ReturnCode)
		eq(t, "out\nerr\n", run.Output) // stdout and stderr together
		isFalse(t, run.CeilingHit)
	}
}

func TestRunBoundedRunsInCwd(t *testing.T) {
	stubPath(t, boundedStubs())
	dir := realTempDir(t)

	run, err := runBounded([]string{"sh", "-c", "pwd -P"}, lim("200%", nil), dir, &bytes.Buffer{})

	noErr(t, err)
	eq(t, dir+"\n", run.Output)
}

func TestRunBoundedReportsCeilingKill(t *testing.T) {
	stubPath(t, boundedStubs())

	run, err := runBounded([]string{"sh", "-c", "exit 137"}, lim("200%", nil), "", &bytes.Buffer{})

	noErr(t, err)
	eq(t, 137, run.ReturnCode)
	isTrue(t, run.CeilingHit)
}

func TestRunBoundedReportsSignalKillAsCeiling(t *testing.T) {
	stubPath(t, boundedStubs())

	run, err := runBounded([]string{"sh", "-c", "kill -9 $$"}, lim("200%", nil), "", &bytes.Buffer{})

	noErr(t, err)
	eq(t, -9, run.ReturnCode) // not exited -1
	isTrue(t, run.CeilingHit)
}

func TestRunBoundedWithoutScopeIsNoCeilingKill(t *testing.T) {
	onlyPath(t, map[string]string{"systemctl": systemctlOK}, "env", "nice", "sh")

	run, err := runBounded([]string{"sh", "-c", "exit 137"}, lim("200%", nil), "", &bytes.Buffer{})

	noErr(t, err)
	eq(t, 137, run.ReturnCode)
	isFalse(t, run.CeilingHit) // no scope, so no ceiling to blame
}

func TestRunBoundedCommandThatCannotStartIsCouldNotCheck(t *testing.T) {
	// No `env` on PATH: the unbounded chain cannot start at all.
	onlyPath(t, map[string]string{"systemctl": systemctlOK}, "sh")

	_, err := runBounded([]string{"sh", "-c", "true"}, lim("200%", nil), "", &bytes.Buffer{})

	c := cnc(t, err)
	contains(t, c.Msg, "sh did not start")
}

func TestRunBoundedMeasuresPeakRSSOfTheChild(t *testing.T) {
	stubPath(t, boundedStubs())
	t.Setenv("CHECKPOINTSCAN_HELPER", "alloc")

	run, err := runBounded([]string{os.Args[0], "-test.run=^TestHelperProcess$"}, lim("200%", nil), "", &bytes.Buffer{})

	noErr(t, err)
	eq(t, 0, run.ReturnCode)
	if run.PeakRSSKB == nil {
		t.Skip("no peak RSS on this platform")
	}
	isTrue(t, *run.PeakRSSKB >= 96*1024, fmt.Sprint("peak ", *run.PeakRSSKB, " KB"))
}

// TestHelperProcess is the child of the tests above, not a test.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("CHECKPOINTSCAN_HELPER") != "alloc" {
		t.Skip("helper process")
	}
	b := make([]byte, 96<<20)
	for i := 0; i < len(b); i += 4096 {
		b[i] = 1
	}
	if b[len(b)-4096] != 1 {
		os.Exit(3)
	}
}
