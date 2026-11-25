package pins

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/stubbin"
)

func fakeRunner(out string, err error) Runner {
	return func(context.Context, string, ...string) (string, error) { return out, err }
}

func toolPin(version string) Pin {
	return Pin{Name: "mytool", Kind: KindTool, Version: version, Command: "mytool", VersionArgs: []string{"--version"}}
}

func TestMeasureTool_current(t *testing.T) {
	got := MeasureTool(context.Background(), fakeRunner("mytool 0.5.1 (abc)\n", nil), toolPin("0.5.1"), time.Second)
	if got.Status != StatusCurrent {
		t.Fatalf("status = %s (%s), want current", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "0.5.1") {
		t.Errorf("detail %q lacks the version", got.Detail)
	}
}

func TestMeasureTool_prereleaseIsBehind(t *testing.T) {
	got := MeasureTool(context.Background(), fakeRunner("codex-cli 0.169.0-alpha.11\n", nil), toolPin("0.169.0"), time.Second)
	if got.Status != StatusBehind {
		t.Fatalf("status = %s (%s), want behind", got.Status, got.Detail)
	}
}

func TestMeasureTool_behind(t *testing.T) {
	got := MeasureTool(context.Background(), fakeRunner("mytool 0.4.9\n", nil), toolPin("0.5.1"), time.Second)
	if got.Status != StatusBehind {
		t.Fatalf("status = %s (%s), want behind", got.Status, got.Detail)
	}
}

func TestMeasureTool_ahead(t *testing.T) {
	got := MeasureTool(context.Background(), fakeRunner("mytool 0.6.0\n", nil), toolPin("0.5.1"), time.Second)
	if got.Status != StatusAhead {
		t.Fatalf("status = %s (%s), want ahead", got.Status, got.Detail)
	}
}

func TestMeasureTool_errors(t *testing.T) {
	blocking := func(ctx context.Context, _ string, _ ...string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	tests := []struct {
		name   string
		run    Runner
		pin    Pin
		detail string
	}{
		{"not on PATH", fakeRunner("", &exec.Error{Name: "mytool", Err: exec.ErrNotFound}), toolPin("0.5.1"), "PATH"},
		{"time-out", blocking, toolPin("0.5.1"), "time"},
		{"no version in output", fakeRunner("hello world\n", nil), toolPin("0.5.1"), "no version"},
		{"command fails", fakeRunner("boom", errors.New("exit status 2")), toolPin("0.5.1"), "exit status 2"},
		{"pin does not load", fakeRunner("mytool 0.5.1", nil), Pin{Name: "mytool", Err: errors.New("pin mytool: version is missing")}, "version is missing"},
		{"pin version unparsable", fakeRunner("mytool 0.5.1", nil), toolPin("latest"), "latest"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MeasureTool(context.Background(), tc.run, tc.pin, 20*time.Millisecond)
			if got.Status != StatusError {
				t.Fatalf("status = %s (%s), want error", got.Status, got.Detail)
			}
			if !strings.Contains(got.Detail, tc.detail) {
				t.Errorf("detail %q lacks %q", got.Detail, tc.detail)
			}
		})
	}
}

func TestMeasureTool_usesVersionArgs(t *testing.T) {
	var gotName string
	var gotArgs []string
	run := func(_ context.Context, name string, args ...string) (string, error) {
		gotName, gotArgs = name, args
		return "kenn-forge 0.5.1\n", nil
	}
	p := Pin{Name: "kenn-forge", Kind: KindTool, Version: "0.5.1", Command: "kenn-forge", VersionArgs: []string{"version"}}
	if got := MeasureTool(context.Background(), run, p, time.Second); got.Status != StatusCurrent {
		t.Fatalf("status = %s (%s)", got.Status, got.Detail)
	}
	if gotName != "kenn-forge" || !reflect.DeepEqual(gotArgs, []string{"version"}) {
		t.Errorf("ran %q %v, want kenn-forge [version]", gotName, gotArgs)
	}
}

// A real script on PATH that knows `version` but fails on `--version`: proves ExecRunner passes
// version_args through, runs without a shell and returns the output.
func TestMeasureTool_realScriptOnPath(t *testing.T) {
	stubbin.RequirePOSIX(t, "test on $1 and $# in the version script")
	dir := t.TempDir()
	script := "if [ \"$1\" = version ] && [ $# -eq 1 ]; then echo 'fakeforge 0.5.1'; exit 0; fi\necho \"unknown flag $*\" >&2\nexit 2\n"
	stubbin.MakeScript(t, dir, "fakeforge", script)
	stubbin.Prepend(t, dir)

	p := Pin{Name: "fakeforge", Kind: KindTool, Version: "0.5.1", Command: "fakeforge", VersionArgs: []string{"version"}}
	if got := MeasureTool(context.Background(), ExecRunner, p, 5*time.Second); got.Status != StatusCurrent {
		t.Fatalf("version_args [version]: status = %s (%s), want current", got.Status, got.Detail)
	}
	p.VersionArgs = []string{"--version"}
	if got := MeasureTool(context.Background(), ExecRunner, p, 5*time.Second); got.Status != StatusError {
		t.Fatalf("version_args [--version]: status = %s (%s), want error", got.Status, got.Detail)
	}
	p.Command = "fakeforge-missing"
	got := MeasureTool(context.Background(), ExecRunner, p, 5*time.Second)
	if got.Status != StatusError || !strings.Contains(got.Detail, "PATH") {
		t.Fatalf("missing command: %s (%s), want error naming PATH", got.Status, got.Detail)
	}
}

// Fails when a tool whose version command leaves a child holding stdout open (a daemon it
// starts) makes MeasureTool, and so `lighthouse init`, wait for that child instead of ending
// as an error shortly after the time-out.
func TestMeasureTool_childHoldingStdoutDoesNotHang(t *testing.T) {
	stubbin.RequirePOSIX(t, "a background child that keeps stdout open")
	dir := t.TempDir()
	stubbin.MakeScript(t, dir, "holder", "sleep 30 &\necho 'holder 1.0.0'\n")
	stubbin.Prepend(t, dir)
	p := Pin{Name: "holder", Kind: KindTool, Version: "1.0.0", Command: "holder"}
	start := time.Now()
	got := MeasureTool(context.Background(), ExecRunner, p, time.Second)
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("MeasureTool took %s: it waited for the child holding stdout", took)
	}
	if got.Status == "" {
		t.Fatalf("no status")
	}
}
