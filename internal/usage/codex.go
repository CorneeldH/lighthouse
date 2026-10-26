package usage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const codexTimeout = 20 * time.Second            // per home, for both app-server answers together
const codexWaitDelay = 2 * time.Second           // cmd.WaitDelay: SIGTERM, then kill
const codexClientName = "lighthouse-codex-usage" // clientInfo.name in the initialize call

// CodexWindow is one Codex rate-limit window.
type CodexWindow struct {
	Pct     int    `json:"pct"`
	ResetAt string `json:"reset_at"` // UTC, timeLayout
}

// CodexState is a reading of the Codex windows. It is never written to disk.
type CodexState struct {
	Source      string      `json:"source"` // "app-server" or "rollout-log"
	GeneratedAt string      `json:"generated_at"`
	AgeS        int         `json:"age_s"` // age of the reading at GeneratedAt
	SourceFile  string      `json:"source_file,omitempty"`
	LowerBound  bool        `json:"lower_bound"`
	Session     CodexWindow `json:"session"`
	Week        CodexWindow `json:"week"`
}

// CodexReader reads the Codex windows from `codex app-server`.
type CodexReader struct {
	Homes   []string
	Timeout time.Duration // per home
	Now     func() time.Time
	Command func(ctx context.Context, home string) *exec.Cmd // must use exec.CommandContext
}

// NewCodexReader returns a reader for $HOME/.codex and $HOME/.codex-specreview.
// Without HOME Codex is not measurable.
func NewCodexReader() (CodexReader, error) {
	home := os.Getenv("HOME")
	if home == "" {
		return CodexReader{}, errors.New("HOME is not set")
	}
	return CodexReader{
		Homes:   []string{filepath.Join(home, ".codex"), filepath.Join(home, ".codex-specreview")},
		Timeout: codexTimeout,
		Now:     time.Now,
		Command: codexCommand,
	}, nil
}

func codexCommand(ctx context.Context, home string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "codex", "app-server")
	cmd.Env = append(os.Environ(), "CODEX_HOME="+home)
	return cmd
}

// Read returns the reading of the first home that has one. When every home
// fails, the error lists the reason per home.
func (r CodexReader) Read(ctx context.Context) (CodexState, error) {
	var reasons []string
	for _, home := range r.Homes {
		state, err := r.readHome(ctx, home)
		if err == nil {
			return state, nil
		}
		reasons = append(reasons, home+": "+err.Error())
	}
	state, err := readRollout(r.Homes, r.Now())
	if err == nil {
		return state, nil
	}
	appServer := "no Codex home to read"
	if len(reasons) > 0 {
		appServer = strings.Join(reasons, "; ")
	}
	return CodexState{}, fmt.Errorf("app-server: %s; rollout-log: %w", appServer, err)
}

func (r CodexReader) readHome(parent context.Context, home string) (CodexState, error) {
	ctx, cancel := context.WithTimeout(parent, r.Timeout)
	result, err := readAppServer(ctx, cancel, r.Command(ctx, home))
	if err != nil {
		return CodexState{}, err
	}
	return parseAppServer(result, r.Now())
}

// rpcMessage is the part of a JSON-RPC line the client looks at.
type rpcMessage struct {
	ID     any             `json:"id"`
	Error  json.RawMessage `json:"error"`
	Result json.RawMessage `json:"result"`
}

// readAppServer runs the three-message exchange and returns the result of
// account/rateLimits/read. It owns cmd: on return the process is stopped
// (SIGTERM, then a kill after WaitDelay) and waited for. cancel releases ctx.
func readAppServer(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd) (json.RawMessage, error) {
	defer cancel()
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = codexWaitDelay
	cmd.Stderr = nil
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("app-server stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New("codex not found")
		}
		return nil, fmt.Errorf("codex could not start: %w", err)
	}
	lines := make(chan []byte)
	done := make(chan struct{})
	go serveLines(stdout, lines, done)
	defer func() {
		close(done)
		_ = stdin.Close()
		cancel() // before Wait: a child that ignores EOF gets SIGTERM, then the kill
		_ = cmd.Wait()
	}()

	send := func(msg string) error {
		if _, err := io.WriteString(stdin, msg+"\n"); err != nil {
			return fmt.Errorf("app-server not writable: %w", err)
		}
		return nil
	}
	if err := send(`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"` + codexClientName + `","version":"1"}}}`); err != nil {
		return nil, err
	}
	if _, err := awaitAnswer(ctx, lines, 1); err != nil {
		return nil, err
	}
	if err := send(`{"method":"initialized"}`); err != nil {
		return nil, err
	}
	if err := send(`{"id":2,"method":"account/rateLimits/read","params":{"excludeResetCreditDetails":false}}`); err != nil {
		return nil, err
	}
	return awaitAnswer(ctx, lines, 2)
}

// serveLines sends every stdout line to lines until EOF or until done closes,
// then closes lines. ReadBytes has no line limit.
func serveLines(r io.Reader, lines chan<- []byte, done <-chan struct{}) {
	defer close(lines)
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			select {
			case lines <- line:
			case <-done:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// awaitAnswer returns the result of the answer with the given id. Lines that
// are not a JSON object, or carry another id, are skipped.
func awaitAnswer(ctx context.Context, lines <-chan []byte, id int) (json.RawMessage, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, errors.New("app-server gave no answer within the timeout")
		case line, ok := <-lines:
			if !ok {
				return nil, errors.New("app-server stopped without an answer")
			}
			var msg rpcMessage
			if json.Unmarshal(bytes.TrimSpace(line), &msg) != nil || msg.ID != float64(id) {
				continue
			}
			if msg.Error != nil {
				return nil, fmt.Errorf("app-server returned an error: %s", msg.Error)
			}
			if !bytes.HasPrefix(bytes.TrimSpace(msg.Result), []byte("{")) {
				return nil, errors.New("app-server returned an answer without result")
			}
			return msg.Result, nil
		}
	}
}

type appServerWindow struct {
	UsedPercent *float64 `json:"usedPercent"`
	ResetsAt    *float64 `json:"resetsAt"`
}

type appServerLimits struct {
	Primary   *appServerWindow `json:"primary"`
	Secondary *appServerWindow `json:"secondary"`
}

// parseAppServer turns the result of account/rateLimits/read into a state:
// rateLimitsByLimitId.codex, else rateLimits. Nothing else is taken over.
func parseAppServer(result []byte, now time.Time) (CodexState, error) {
	var res struct {
		RateLimits          *appServerLimits           `json:"rateLimits"`
		RateLimitsByLimitID map[string]appServerLimits `json:"rateLimitsByLimitId"`
	}
	if err := json.Unmarshal(result, &res); err != nil {
		return CodexState{}, fmt.Errorf("app-server answer unreadable: %w", err)
	}
	limits := res.RateLimits
	if l, ok := res.RateLimitsByLimitID["codex"]; ok {
		limits = &l
	}
	if limits == nil {
		return CodexState{}, errors.New("no rateLimits in the app-server answer")
	}
	session, err := appServerWindowState(limits.Primary, "primary")
	if err != nil {
		return CodexState{}, err
	}
	week, err := appServerWindowState(limits.Secondary, "secondary")
	if err != nil {
		return CodexState{}, err
	}
	return CodexState{
		Source:      "app-server",
		GeneratedAt: now.UTC().Format(timeLayout),
		Session:     session,
		Week:        week,
	}, nil
}

func appServerWindowState(w *appServerWindow, name string) (CodexWindow, error) {
	if w == nil {
		return CodexWindow{}, fmt.Errorf("window %s missing from the app-server answer", name)
	}
	if w.UsedPercent == nil || w.ResetsAt == nil {
		return CodexWindow{}, fmt.Errorf("window %s unreadable: usedPercent or resetsAt missing", name)
	}
	return CodexWindow{
		Pct:     int(*w.UsedPercent),
		ResetAt: time.Unix(int64(*w.ResetsAt), 0).UTC().Format(timeLayout),
	}, nil
}
