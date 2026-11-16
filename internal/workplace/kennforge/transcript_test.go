package kennforge

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// The start-failure transcript of run 19 (#237): no user line, only the system warning.
const unknownTranscript = `{"type":"mode","mode":"normal","sessionId":"` + testSessionID + `"}
{"type":"system","subtype":"informational","content":"Unknown command: /superpowers:subagent-driven-development","timestamp":"2026-11-06T17:20:36.129Z"}
`

const subagentLine = `{"type":"assistant","timestamp":"2026-11-06T17:25:00Z"}
`

// containerExec is the podman exec that tars the transcript of testSessionID out of container c.
func containerExec(c string) string {
	return "podman exec " + c + " sh -c " + containerScript + " sh -srv-forge-ws-issue-7-x " + testSessionID + " "
}

// tarOf is a tar stream holding files, in the order given.
func tarOf(t *testing.T, files ...[2]string) string {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: f[0], Mode: 0o600, Size: int64(len(f[1])), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func softwareTranscriptAdapter(t *testing.T, steps []step) (*Adapter, *[]string) {
	t.Helper()
	f, a := newFakeForge(t)
	f.path = "/srv/forge/ws/issue-7.x"
	a.Profile = "software"
	a.Home = t.TempDir() // a transcript on the host must not count for software
	calls := &[]string{}
	a.Run = scriptRunner(t, steps, calls)
	return a, calls
}

// The tick sees an unknown command of a software run, whose transcript lives in the devcontainer.
func TestTranscriptSoftwareFromTheContainer(t *testing.T) {
	stream := tarOf(t,
		[2]string{deltaManifest, "236 0 " + testSessionID + ".jsonl\n56 0 " + testSessionID + "/subagents/agent-a1.jsonl\n"},
		[2]string{testSessionID + ".jsonl", unknownTranscript},
		[2]string{testSessionID + "/subagents/agent-a1.jsonl", subagentLine},
	)
	steps := []step{
		{cmd: podmanPS, out: "c1\nc2\n"},
		{cmd: containerExec("c1"), code: notInContainer},
		{cmd: containerExec("c2"), out: stream},
	}
	a, calls := softwareTranscriptAdapter(t, steps)
	got, err := a.Transcript(context.Background(), "ws1", testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UnknownCommand != "/superpowers:subagent-driven-development" {
		t.Errorf("UnknownCommand = %q, want /superpowers:subagent-driven-development", got.UnknownCommand)
	}
	if want := time.Date(2026, 11, 6, 17, 25, 0, 0, time.UTC); !got.LastEvent.Equal(want) {
		t.Errorf("LastEvent = %v, want %v (the subagent line)", got.LastEvent, want)
	}
	if len(*calls) != len(steps) {
		t.Errorf("calls = %q, want %d", *calls, len(steps))
	}
}

// A transcript in no container is a missing transcript, as on the host: the tick then measures
// from the forge state.
func TestTranscriptSoftwareInNoContainer(t *testing.T) {
	a, _ := softwareTranscriptAdapter(t, []step{
		{cmd: podmanPS, out: "c1\n"},
		{cmd: containerExec("c1"), code: notInContainer},
	})
	if _, err := a.Transcript(context.Background(), "ws1", testSessionID); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

// A failing podman is an error the tick reports, never a quiet "nothing to measure".
func TestTranscriptSoftwareUnmeasurableIsAnError(t *testing.T) {
	cases := map[string][]step{
		"podman ps fails":   {{cmd: podmanPS, code: 125}},
		"podman exec fails": {{cmd: podmanPS, out: "c1\n"}, {cmd: containerExec("c1"), code: 125}},
	}
	for name, steps := range cases {
		t.Run(name, func(t *testing.T) {
			a, _ := softwareTranscriptAdapter(t, steps)
			_, err := a.Transcript(context.Background(), "ws1", testSessionID)
			if err == nil || errors.Is(err, fs.ErrNotExist) || errors.Is(err, workplace.ErrNotMeasurable) {
				t.Fatalf("err = %v, want an error that is neither ErrNotExist nor ErrNotMeasurable", err)
			}
		})
	}
}

// A member outside the session's files never lands on the host.
func TestTranscriptSoftwareSkipsForeignMembers(t *testing.T) {
	escape := filepath.Join(t.TempDir(), "escaped")
	stream := tarOf(t,
		[2]string{deltaManifest, "236 0 " + testSessionID + ".jsonl\n"},
		[2]string{testSessionID + ".jsonl", unknownTranscript},
		[2]string{"../../../../../../../.." + escape, "x"},
	)
	a, _ := softwareTranscriptAdapter(t, []step{
		{cmd: podmanPS, out: "c1\n"},
		{cmd: containerExec("c1"), out: stream},
	})
	if _, err := a.Transcript(context.Background(), "ws1", testSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(escape); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stat %s = %v, want it never written", escape, err)
	}
}

func TestTranscriptSoftwareRejectsAnUnsafeSessionID(t *testing.T) {
	a, calls := softwareTranscriptAdapter(t, nil)
	for _, sid := range []string{"", "../x", "a b"} {
		if _, err := a.Transcript(context.Background(), "ws1", sid); err == nil {
			t.Errorf("Transcript(%q) = nil error, want a rejection", sid)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("calls = %q, want none", *calls)
	}
}

func TestTranscriptHost(t *testing.T) {
	t.Run("the project dir of the worktree", func(t *testing.T) {
		f, a := newFakeForge(t)
		f.path = "/srv/forge/ws/issue-7.x"
		a.Home = t.TempDir()
		dir := filepath.Join(a.Home, ".claude", "projects", "-srv-forge-ws-issue-7-x")
		writeFile(t, filepath.Join(dir, testSessionID+".jsonl"), unknownTranscript)
		got, err := a.Transcript(context.Background(), "ws1", testSessionID)
		if err != nil || got.UnknownCommand != "/superpowers:subagent-driven-development" {
			t.Errorf("Transcript = %+v, %v; want the unknown command", got, err)
		}
	})
	t.Run("no project dir on the host", func(t *testing.T) {
		f, a := newFakeForge(t)
		f.path = "/workspaces/issue-7"
		a.Home = t.TempDir()
		if _, err := a.Transcript(context.Background(), "ws1", testSessionID); !errors.Is(err, workplace.ErrNotMeasurable) {
			t.Errorf("err = %v, want ErrNotMeasurable", err)
		}
	})
	t.Run("a file where the dir should be", func(t *testing.T) {
		f, a := newFakeForge(t)
		f.path = "/w"
		a.Home = t.TempDir()
		writeFile(t, filepath.Join(a.Home, ".claude", "projects", "-w"), "")
		if _, err := a.Transcript(context.Background(), "ws1", testSessionID); !errors.Is(err, workplace.ErrNotMeasurable) {
			t.Errorf("err = %v, want ErrNotMeasurable", err)
		}
	})
	t.Run("no worktree path", func(t *testing.T) {
		_, a := newFakeForge(t)
		a.Home = t.TempDir()
		_, err := a.Transcript(context.Background(), "ws1", testSessionID)
		if err == nil || errors.Is(err, workplace.ErrNotMeasurable) {
			t.Errorf("err = %v, want an error that is not ErrNotMeasurable", err)
		}
	})
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// shellPodman is a fake podman with one devcontainer, c1, whose home is containerHome: podman ps
// lists it, and podman exec c1 sh … runs that sh on this host with HOME set to containerHome,
// so the real containerScript runs. Any other call fails the test.
func shellPodman(t *testing.T, containerHome string, calls *[]string) workplace.Runner {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("containerScript runs in a Linux devcontainer; no POSIX sh to stand in for it here")
	}
	for _, tool := range []string{"sh", "tar", "tail", "head", "wc", "mktemp"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s on this host: %v", tool, err)
		}
	}
	return func(ctx context.Context, _ time.Duration, name string, args ...string) (string, int, error) {
		*calls = append(*calls, strings.Join(append([]string{name}, args...), " "))
		switch {
		case name == "podman" && len(args) > 0 && args[0] == "ps":
			return "c1\n", 0, nil
		case name == "podman" && len(args) > 3 && args[0] == "exec" && args[1] == "c1" && args[2] == "sh":
			cmd := exec.CommandContext(ctx, "sh", args[3:]...)
			cmd.Env = append(os.Environ(), "HOME="+containerHome)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return string(out), ee.ExitCode(), nil
			}
			if err != nil {
				return "", 0, err
			}
			if stderr.Len() > 0 {
				t.Errorf("containerScript wrote to stderr: %s", stderr.String())
			}
			return string(out), 0, nil
		}
		t.Errorf("unexpected call %s %q", name, args)
		return "", 0, fmt.Errorf("unexpected call")
	}
}

// The software branch reads the transcript inside the container from the asked offset, never
// on the host, and reports the size of each file with the offset its bytes start at.
func TestContainerScriptReadsFromOffset(t *testing.T) {
	f, a := newFakeForge(t)
	f.path = "/srv/forge/ws/issue-7.x"
	a.Profile = "software"
	a.Home = t.TempDir()
	// A transcript on the host under the same project: the software branch must never read it.
	writeFile(t, filepath.Join(a.Home, ".claude", "projects", "-srv-forge-ws-issue-7-x", testSessionID+".jsonl"), "host bytes, never read\n")
	container := t.TempDir()
	project := filepath.Join(container, ".claude", "projects", "-srv-forge-ws-issue-7-x")
	writeFile(t, filepath.Join(project, testSessionID+".jsonl"), "0123456789\n")
	writeFile(t, filepath.Join(project, testSessionID, "subagents", "agent-a1.jsonl"), "abc\n")
	writeFile(t, filepath.Join(project, testSessionID, "subagents", "agent-b2.jsonl"), "short\n")
	calls := &[]string{}
	a.Run = shellPodman(t, container, calls)

	got, err := a.TranscriptDelta(context.Background(), "ws1", testSessionID, map[string]int64{
		testSessionID + ".jsonl":                    4,
		testSessionID + "/subagents/agent-b2.jsonl": 99, // longer than the file: rotated
		"../escape.jsonl":                           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []workplace.FileDelta{
		{Name: testSessionID + ".jsonl", Size: 11, From: 4, Data: []byte("456789\n")},
		{Name: testSessionID + "/subagents/agent-a1.jsonl", Size: 4, From: 0, Data: []byte("abc\n")},
		{Name: testSessionID + "/subagents/agent-b2.jsonl", Size: 6, From: 0, Data: []byte("short\n")},
	}
	if len(got) != len(want) {
		t.Fatalf("deltas = %+v, want %d files", got, len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Name != w.Name || g.Size != w.Size || g.From != w.From || string(g.Data) != string(w.Data) {
			t.Errorf("delta %d = %s size %d from %d %q, want %s size %d from %d %q",
				i, g.Name, g.Size, g.From, g.Data, w.Name, w.Size, w.From, w.Data)
		}
	}
	// The offsets go to the script as $3; a name outside the session never does.
	last := (*calls)[len(*calls)-1]
	if !strings.HasSuffix(last, " "+testSessionID+".jsonl:4,"+testSessionID+"/subagents/agent-b2.jsonl:99") {
		t.Errorf("podman exec = %q, want the session's offsets as the last argument", last)
	}
}

// A session in no container is fs.ErrNotExist, as Transcript.
func TestTranscriptDeltaInNoContainer(t *testing.T) {
	f, a := newFakeForge(t)
	f.path = "/srv/forge/ws/issue-7.x"
	a.Profile = "software"
	a.Home = t.TempDir()
	calls := &[]string{}
	a.Run = shellPodman(t, t.TempDir(), calls)
	if _, err := a.TranscriptDelta(context.Background(), "ws1", testSessionID, nil); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

// A member the manifest does not account for, or one whose bytes do not match it, is an
// error: a mirror must never take a delta it cannot place.
func TestTranscriptDeltaRejectsUnaccountedMembers(t *testing.T) {
	cases := map[string]string{
		"no manifest": tarOf(t, [2]string{testSessionID + ".jsonl", "abc\n"}),
		"short data": tarOf(t,
			[2]string{deltaManifest, "10 2 " + testSessionID + ".jsonl\n"},
			[2]string{testSessionID + ".jsonl", "abc\n"}),
	}
	for name, stream := range cases {
		t.Run(name, func(t *testing.T) {
			a, _ := softwareTranscriptAdapter(t, []step{
				{cmd: podmanPS, out: "c1\n"},
				{cmd: containerExec("c1"), out: stream},
			})
			_, err := a.TranscriptDelta(context.Background(), "ws1", testSessionID, nil)
			if err == nil || errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("err = %v, want an error that is not ErrNotExist", err)
			}
		})
	}
}

func TestTranscriptDeltaHost(t *testing.T) {
	f, a := newFakeForge(t)
	f.path = "/srv/forge/ws/issue-7.x"
	a.Home = t.TempDir()
	writeFile(t, filepath.Join(a.Home, ".claude", "projects", "-srv-forge-ws-issue-7-x", testSessionID+".jsonl"), "0123456789\n")
	got, err := a.TranscriptDelta(context.Background(), "ws1", testSessionID, map[string]int64{testSessionID + ".jsonl": 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Size != 11 || got[0].From != 8 || string(got[0].Data) != "89\n" {
		t.Fatalf("deltas = %+v, want 89\\n from 8 of 11", got)
	}
}

func TestTranscriptDeltaOnNodeIsNotMeasurable(t *testing.T) {
	_, a := newFakeForge(t)
	a.Node = "node-q4"
	if _, err := a.TranscriptDelta(context.Background(), "ws1", testSessionID, nil); !errors.Is(err, workplace.ErrNotMeasurable) {
		t.Fatalf("err = %v, want ErrNotMeasurable", err)
	}
}
