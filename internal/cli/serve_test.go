package cli

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuf is a buffer the serve goroutine writes while the test reads (-race).
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestServeDefaults(t *testing.T) {
	var out bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &out)
	root.SetArgs([]string{"serve", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"127.0.0.1:8127", "--db"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("serve --help lacks %q:\n%s", want, out.String())
		}
	}
}

func TestServeStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errOut := &syncBuf{}
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, "127.0.0.1:0", filepath.Join(t.TempDir(), "l.db"), errOut)
	}()

	const marker = "lighthouse: listening on "
	var addr string
	deadline := time.Now().Add(5 * time.Second)
	for addr == "" {
		if _, rest, ok := strings.Cut(errOut.String(), marker); ok && strings.Contains(rest, "\n") {
			addr = strings.TrimSpace(rest)
		} else if time.Now().After(deadline) {
			t.Fatalf("no start line within 5s; stderr: %q", errOut.String())
		} else {
			select {
			case err := <-done:
				t.Fatalf("serve returned early: %v", err)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}

	resp, err := http.Get("http://" + addr + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve = %v, want nil", err)
		}
	case <-time.After(11 * time.Second):
		t.Fatal("serve did not stop within 11s")
	}
}
