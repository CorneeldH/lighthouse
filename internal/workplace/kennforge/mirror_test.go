package kennforge

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// sourceNode is a node whose transcript route answers from the files in src, read with
// workplace.ReadDelta as serve reads them, and records the query from of every request. delay
// holds each answer back after the read, so two fetches overlap.
type sourceNode struct {
	*fakeNode
	src   string
	delay time.Duration

	mu    sync.Mutex
	froms []string
}

func newSourceNode(t *testing.T) *sourceNode {
	t.Helper()
	s := &sourceNode{fakeNode: newFakeNode(t), src: t.TempDir()}
	s.mux.HandleFunc("GET "+nodeapi.PathTranscript, func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get("from")
		s.mu.Lock()
		s.froms = append(s.froms, raw)
		s.mu.Unlock()
		from := map[string]int64{}
		if raw != "" {
			for _, part := range strings.Split(raw, ",") {
				i := strings.LastIndex(part, ":")
				off, err := strconv.ParseInt(part[i+1:], 10, 64)
				if err != nil {
					writeNode(w, http.StatusBadRequest, nodeapi.Problem{Status: 400, Detail: err.Error()})
					return
				}
				from[part[:i]] = off
			}
		}
		deltas, err := workplace.ReadDelta(s.src, r.PathValue("session"), from)
		if errors.Is(err, fs.ErrNotExist) {
			writeNode(w, http.StatusNotFound, nodeapi.Problem{Status: 404, Code: nodeapi.CodeNoTranscript, Detail: err.Error()})
			return
		}
		if err != nil {
			writeNode(w, http.StatusInternalServerError, nodeapi.Problem{Status: 500, Detail: err.Error()})
			return
		}
		time.Sleep(s.delay)
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		for _, d := range deltas {
			_ = tw.WriteHeader(&tar.Header{
				Name: d.Name, Mode: 0o600, Size: int64(len(d.Data)), Typeflag: tar.TypeReg, Format: tar.FormatPAX,
				PAXRecords: map[string]string{
					nodeapi.PAXSize: strconv.FormatInt(d.Size, 10), nodeapi.PAXFrom: strconv.FormatInt(d.From, 10),
				},
			})
			_, _ = tw.Write(d.Data)
		}
		_ = tw.Close()
		w.Header().Set("Content-Type", "application/x-tar")
		_, _ = w.Write(buf.Bytes())
	})
	return s
}

func (s *sourceNode) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.froms...)
}

// write puts body at name in the source dir of the node.
func (s *sourceNode) write(t *testing.T, name, body string) {
	t.Helper()
	writeFile(t, filepath.Join(s.src, filepath.FromSlash(name)), body)
}

// appendTo appends body to name in the source dir of the node.
func (s *sourceNode) appendTo(t *testing.T, name, body string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(s.src, filepath.FromSlash(name)), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(body); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// nodeAdapter is an adapter on a node with a random id, talking to c, mirroring under stateDir.
func nodeAdapter(t *testing.T, c *NodeClient, stateDir string) *Adapter {
	t.Helper()
	return &Adapter{Node: "node-" + randomID(t), Serve: c, StateDir: stateDir, Profile: "default"}
}

// mirrored is the content of name in the mirror of session on the adapter's node.
func mirrored(t *testing.T, a *Adapter, session, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(a.StateDir, "mirror", a.Node, session, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const (
	line1 = `{"type":"user","timestamp":"2026-11-15T10:00:00Z","message":{"content":"go"}}` + "\n"
	line2 = `{"type":"assistant","timestamp":"2026-11-15T10:01:00Z","message":{}}` + "\n"
	line3 = `{"type":"user","timestamp":"2026-11-15T10:02:00Z","message":{"content":"more"}}` + "\n"
	sub1  = `{"type":"assistant","timestamp":"2026-11-15T10:00:30Z","message":{}}` + "\n"
)

func TestSecondTickFetchesOnlyNewBytes(t *testing.T) {
	s := newSourceNode(t)
	sess := randomID(t)
	main, sub := sess+".jsonl", sess+"/subagents/agent-a.jsonl"
	s.write(t, main, line1+line2)
	s.write(t, sub, sub1)
	a := nodeAdapter(t, s.client(t), t.TempDir())
	ctx := context.Background()

	if _, err := a.Transcript(ctx, randomID(t), sess); err != nil {
		t.Fatal(err)
	}
	s.appendTo(t, main, line3)
	got, err := a.Transcript(ctx, randomID(t), sess)
	if err != nil {
		t.Fatal(err)
	}

	// line1+line2 is 78+69 bytes, sub1 is 69 bytes: counted by hand, not measured.
	want := []string{"", main + ":147," + sub + ":69"}
	if f := s.recorded(); !reflect.DeepEqual(f, want) {
		t.Errorf("from per fetch = %q, want %q", f, want)
	}
	if m := mirrored(t, a, sess, main); m != line1+line2+line3 {
		t.Errorf("mirror of main = %q, want the three lines once", m)
	}
	if !got.LastEvent.Equal(time.Date(2026, 11, 15, 10, 2, 0, 0, time.UTC)) {
		t.Errorf("LastEvent = %v, want the appended line's 10:02", got.LastEvent)
	}
}

func TestShorterNodeFileReplacesMirror(t *testing.T) {
	s := newSourceNode(t)
	sess := randomID(t)
	main := sess + ".jsonl"
	s.write(t, main, line1+line2+line3)
	a := nodeAdapter(t, s.client(t), t.TempDir())
	ctx := context.Background()
	if _, err := a.Transcript(ctx, randomID(t), sess); err != nil {
		t.Fatal(err)
	}
	// A new session with the same id: a shorter file with other bytes.
	fresh := `{"type":"user","timestamp":"2026-11-16T08:00:00Z"}` + "\n"
	s.write(t, main, fresh)
	got, err := a.Transcript(ctx, randomID(t), sess)
	if err != nil {
		t.Fatal(err)
	}
	if m := mirrored(t, a, sess, main); m != fresh {
		t.Errorf("mirror of main = %q, want only %q", m, fresh)
	}
	if got.AssistantTurn {
		t.Error("AssistantTurn = true, but the replaced transcript has no assistant line")
	}
	if !got.LastEvent.Equal(time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("LastEvent = %v, want 2026-11-16 08:00", got.LastEvent)
	}
}

func TestMirrorDropsSubagentFileGoneOnNode(t *testing.T) {
	s := newSourceNode(t)
	sess := randomID(t)
	main, sub := sess+".jsonl", sess+"/subagents/agent-b.jsonl"
	s.write(t, main, line1)
	s.write(t, sub, `{"type":"assistant","timestamp":"2026-11-15T11:00:00Z"}`+"\n")
	a := nodeAdapter(t, s.client(t), t.TempDir())
	ctx := context.Background()
	if _, err := a.Transcript(ctx, randomID(t), sess); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(s.src, filepath.FromSlash(sub))); err != nil {
		t.Fatal(err)
	}
	got, err := a.Transcript(ctx, randomID(t), sess)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Threads["agent-b"]; ok || len(got.Threads) != 1 {
		t.Errorf("threads = %v, want only the main thread: the subagent file is gone on the node", got.Threads)
	}
	if _, err := os.Stat(filepath.Join(a.StateDir, "mirror", a.Node, sess, filepath.FromSlash(sub))); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("mirror of the gone subagent file: stat err = %v, want not exist", err)
	}
}

func TestReadTranscriptOnMirrorEqualsSource(t *testing.T) {
	for name, tc := range map[string]struct {
		first, rest string
		check       func(workplace.Transcript) bool
	}{
		"plain": {line1, line2,
			func(tr workplace.Transcript) bool { return tr.AssistantTurn && tr.Background == 0 }},
		// scanStart: a start failure before any assistant line.
		"unknown command": {line1, `{"type":"system","timestamp":"2026-11-15T10:03:00Z","content":"Unknown command: /nope"}` + "\n",
			func(tr workplace.Transcript) bool { return tr.UnknownCommand == "/nope" && !tr.AssistantTurn }},
		// scanWork: a background task started in the first fetch, still running after the second.
		"background task": {line1 + `{"type":"user","timestamp":"2026-11-15T10:04:00Z","toolUseResult":{"backgroundTaskId":"bg1"}}` + "\n", line2,
			func(tr workplace.Transcript) bool { return tr.Background == 1 }},
		// scanWork: a turn opens after the last stop_hook_summary, across the two fetches.
		"turn open": {line2 + `{"type":"system","subtype":"stop_hook_summary","timestamp":"2026-11-15T10:05:00Z"}` + "\n", line3,
			func(tr workplace.Transcript) bool { return tr.TurnOpen }},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSourceNode(t)
			sess := randomID(t)
			s.write(t, sess+".jsonl", tc.first)
			s.write(t, sess+"/subagents/agent-c.jsonl", sub1)
			a := nodeAdapter(t, s.client(t), t.TempDir())
			ctx := context.Background()
			if _, err := a.Transcript(ctx, randomID(t), sess); err != nil {
				t.Fatal(err)
			}
			s.appendTo(t, sess+".jsonl", tc.rest)
			got, err := a.Transcript(ctx, randomID(t), sess)
			if err != nil {
				t.Fatal(err)
			}
			want, err := workplace.ReadTranscript(s.src, sess)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Transcript on the mirror = %+v, want %+v as on the source", got, want)
			}
			if !tc.check(got) {
				t.Errorf("Transcript %+v does not show what the fixture holds", got)
			}
		})
	}
}

func TestConcurrentFetchesLeaveMirrorEqualToSource(t *testing.T) {
	s := newSourceNode(t)
	sess := randomID(t)
	main := sess + ".jsonl"
	s.write(t, main, line1)
	stateDir := t.TempDir()
	node := "node-" + randomID(t)
	// Two adapters, as the tick and lighthouse run, each with its own client.
	tick := &Adapter{Node: node, Serve: s.client(t), StateDir: stateDir, Profile: "default"}
	run := &Adapter{Node: node, Serve: s.client(t), StateDir: stateDir, Profile: "default"}
	ctx := context.Background()
	if _, err := tick.Transcript(ctx, randomID(t), sess); err != nil {
		t.Fatal(err)
	}
	s.appendTo(t, main, line2+line3)
	s.delay = 100 * time.Millisecond
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, a := range []*Adapter{tick, run} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = a.Transcript(ctx, randomID(t), sess)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if m := mirrored(t, tick, sess, main); m != line1+line2+line3 {
		t.Errorf("mirror after two concurrent fetches = %q, want each line once", m)
	}
}

func TestNoTranscriptIsNotExist(t *testing.T) {
	s := newSourceNode(t) // an empty source dir: no main transcript
	a := nodeAdapter(t, s.client(t), t.TempDir())
	_, err := a.Transcript(context.Background(), randomID(t), randomID(t))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
	if errors.Is(err, workplace.ErrGone) {
		t.Errorf("err = %v, must not be ErrGone", err)
	}
}

// A serve that does not answer the transcript (unreachable, 5xx, 401, 403, an answer that does not
// fit the mirror) is not measurable and unanswered: the tick must not read the run as silent.
// No transcript yet, and a transcript without a timestamp, are no failure of the node.
func TestServeUnreachableIsNotMeasurableNotError(t *testing.T) {
	down := newFakeNode(t)
	c := down.client(t)
	down.srv.Close()
	answering := func(status int) *NodeClient {
		n := newFakeNode(t)
		n.mux.HandleFunc("GET "+nodeapi.PathTranscript, func(w http.ResponseWriter, _ *http.Request) {
			writeNode(w, status, nodeapi.Problem{Status: status, Detail: "no"})
		})
		return n.client(t)
	}
	sess := randomID(t)
	gap := transcriptNode(t, []tarMember{{sess + ".jsonl", "def\n", "7", "3"}})
	for name, client := range map[string]*NodeClient{
		"unreachable": c, "5xx": answering(http.StatusBadGateway), "401": answering(http.StatusUnauthorized),
		"403": answering(http.StatusForbidden), "gap": gap.client(t),
	} {
		t.Run(name, func(t *testing.T) {
			a := nodeAdapter(t, client, t.TempDir())
			_, err := a.Transcript(context.Background(), randomID(t), sess)
			if !errors.Is(err, workplace.ErrNotMeasurable) || !errors.Is(err, workplace.ErrUnanswered) {
				t.Fatalf("err = %v, want ErrNotMeasurable and ErrUnanswered", err)
			}
			if errors.Is(err, workplace.ErrGone) || errors.Is(err, fs.ErrNotExist) {
				t.Errorf("err = %v, must be neither gone nor no transcript", err)
			}
		})
	}
	t.Run("no transcript", func(t *testing.T) {
		a := nodeAdapter(t, newSourceNode(t).client(t), t.TempDir())
		if _, err := a.Transcript(context.Background(), randomID(t), randomID(t)); errors.Is(err, workplace.ErrUnanswered) {
			t.Errorf("err = %v, want no ErrUnanswered: the node answered", err)
		}
	})
	t.Run("no timestamp", func(t *testing.T) {
		s := randomID(t)
		n := transcriptNode(t, []tarMember{{s + ".jsonl", "{}\n", "3", "0"}})
		_, err := nodeAdapter(t, n.client(t), t.TempDir()).Transcript(context.Background(), randomID(t), s)
		if !errors.Is(err, workplace.ErrNotMeasurable) || errors.Is(err, workplace.ErrUnanswered) {
			t.Errorf("err = %v, want ErrNotMeasurable without ErrUnanswered: the node answered", err)
		}
	})
}

func TestMirrorRejectsDeltaWithGap(t *testing.T) {
	sess := randomID(t)
	// The mirror is empty, so only a delta from 0 fits; one from 3 would leave a hole.
	n := transcriptNode(t, []tarMember{{sess + ".jsonl", "def\n", "7", "3"}})
	a := nodeAdapter(t, n.client(t), t.TempDir())
	_, err := a.Transcript(context.Background(), randomID(t), sess)
	if !errors.Is(err, workplace.ErrNotMeasurable) {
		t.Fatalf("err = %v, want ErrNotMeasurable", err)
	}
	if _, err := os.Stat(filepath.Join(a.StateDir, "mirror", a.Node, sess, sess+".jsonl")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("mirror file after a rejected delta: stat err = %v, want not exist", err)
	}
}

func TestMirrorModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix modes on windows")
	}
	s := newSourceNode(t)
	sess := randomID(t)
	s.write(t, sess+".jsonl", line1)
	s.write(t, sess+"/subagents/agent-d.jsonl", sub1)
	a := nodeAdapter(t, s.client(t), t.TempDir())
	if _, err := a.Transcript(context.Background(), randomID(t), sess); err != nil {
		t.Fatal(err)
	}
	s.appendTo(t, sess+".jsonl", line2)
	if _, err := a.Transcript(context.Background(), randomID(t), sess); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(a.StateDir, "mirror", a.Node, sess)
	for _, p := range []string{filepath.Join(a.StateDir, "mirror"), filepath.Join(a.StateDir, "mirror", a.Node), dir,
		filepath.Join(dir, sess, "subagents")} {
		if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v %v, want a 0700 directory", p, st, err)
		}
	}
	for _, p := range []string{filepath.Join(dir, sess+".jsonl"), filepath.Join(dir, sess, "subagents", "agent-d.jsonl")} {
		if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v, want a 0600 file", p, st, err)
		}
	}
}

func TestMirrorRejectsUnsafeNodeID(t *testing.T) {
	for _, node := range []string{"..", ".", "a/b", `a\b`, ""} {
		t.Run(node, func(t *testing.T) {
			s := newSourceNode(t)
			sess := randomID(t)
			s.write(t, sess+".jsonl", line1)
			stateDir := t.TempDir()
			a := &Adapter{Node: node, Serve: s.client(t), StateDir: stateDir, Profile: "default"}
			if node == "" {
				// An empty node is self; RemoveMirror has no self.
				if err := RemoveMirror(stateDir, node, sess); err == nil {
					t.Error("RemoveMirror with an empty node id: no error")
				}
				return
			}
			if _, err := a.Transcript(context.Background(), randomID(t), sess); err == nil {
				t.Errorf("Transcript on node %q: no error", node)
			}
			if len(s.recorded()) != 0 {
				t.Errorf("node %q was asked for the transcript", node)
			}
			if err := RemoveMirror(stateDir, node, sess); err == nil {
				t.Errorf("RemoveMirror on node %q: no error", node)
			}
		})
	}
}

func TestSweepMirrorsRemovesClosedRuns(t *testing.T) {
	stateDir := t.TempDir()
	nodeA, nodeB := "node-"+randomID(t), "node-"+randomID(t)
	open, closedA, closedB := randomID(t), randomID(t), randomID(t)
	for _, m := range [][2]string{{nodeA, open}, {nodeA, closedA}, {nodeB, closedB}} {
		writeFile(t, filepath.Join(stateDir, "mirror", m[0], m[1], m[1]+".jsonl"), line1)
	}
	var asked [][2]string
	err := SweepMirrors(stateDir, func(node, session string) bool {
		asked = append(asked, [2]string{node, session})
		return node == nodeA && session == open
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 3 {
		t.Errorf("keep asked for %v, want the three mirrors", asked)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "mirror", nodeA, open, open+".jsonl")); err != nil {
		t.Errorf("mirror of the open run: %v, want kept", err)
	}
	for _, m := range [][2]string{{nodeA, closedA}, {nodeB, closedB}} {
		if _, err := os.Stat(filepath.Join(stateDir, "mirror", m[0], m[1])); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("mirror %s/%s of a closed run: stat err = %v, want removed", m[0], m[1], err)
		}
	}
	// No mirror dir at all is nothing to sweep.
	if err := SweepMirrors(t.TempDir(), func(string, string) bool { return false }); err != nil {
		t.Errorf("SweepMirrors without a mirror dir: %v", err)
	}
}

func TestRemoveMirror(t *testing.T) {
	stateDir := t.TempDir()
	node, gone, other := "node-"+randomID(t), randomID(t), randomID(t)
	writeFile(t, filepath.Join(stateDir, "mirror", node, gone, gone+".jsonl"), line1)
	writeFile(t, filepath.Join(stateDir, "mirror", node, other, other+".jsonl"), line1)
	if err := RemoveMirror(stateDir, node, gone); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "mirror", node, gone)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("removed mirror: stat err = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "mirror", node, other, other+".jsonl")); err != nil {
		t.Errorf("other mirror on the node: %v, want kept", err)
	}
	if err := RemoveMirror(stateDir, node, gone); err != nil {
		t.Errorf("RemoveMirror of a missing mirror: %v, want nil", err)
	}
}

// RawTranscript on a node syncs the mirror and gives the whole files, also on the second call,
// when serve sends only the new bytes.
func TestRawTranscriptOnNodeGivesWholeMirrorFiles(t *testing.T) {
	s := newSourceNode(t)
	sess := randomID(t)
	s.write(t, sess+".jsonl", line1)
	s.write(t, sess+"/subagents/agent-c.jsonl", sub1)
	a := nodeAdapter(t, s.client(t), t.TempDir())
	ctx := context.Background()
	got, err := a.RawTranscript(ctx, randomID(t), sess)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != sess+".jsonl" || string(got[0].Data) != line1 || got[0].From != 0 ||
		got[1].Name != sess+"/subagents/agent-c.jsonl" || string(got[1].Data) != sub1 {
		t.Fatalf("first RawTranscript = %+v", got)
	}
	if mirrored(t, a, sess, sess+".jsonl") != line1 {
		t.Error("the mirror does not hold the main transcript")
	}
	s.appendTo(t, sess+".jsonl", line2)
	got, err = a.RawTranscript(ctx, randomID(t), sess)
	if err != nil {
		t.Fatal(err)
	}
	if string(got[0].Data) != line1+line2 || got[0].From != 0 || got[0].Size != int64(len(line1+line2)) {
		t.Errorf("second RawTranscript main = %+v, want the whole file", got[0])
	}
	if froms := s.recorded(); len(froms) != 2 || froms[1] == "" {
		t.Errorf("froms = %q, want the second fetch to ask only for the new bytes", froms)
	}
}

func TestRawTranscriptOnNodeErrors(t *testing.T) {
	s := newSourceNode(t)
	a := nodeAdapter(t, s.client(t), t.TempDir())
	if _, err := a.RawTranscript(context.Background(), randomID(t), randomID(t)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("no transcript on the node: err = %v, want fs.ErrNotExist", err)
	}
	a.Serve = nil
	_, err := a.RawTranscript(context.Background(), randomID(t), randomID(t))
	if !errors.Is(err, workplace.ErrNotMeasurable) || !errors.Is(err, workplace.ErrUnanswered) {
		t.Errorf("no serve: err = %v, want ErrNotMeasurable and ErrUnanswered", err)
	}
}
