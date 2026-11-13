package codelang

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// Layer 2 of the detector asks a chain of models about doubtful units. Ask tries the models
// in order and the first readable answer counts. Every call runs under a context deadline
// (PerModel each, Total for the chain), which also stops a server that drips bytes. After a
// failed chain a marker file keeps the hook off the endpoint for QuietPeriod.
const (
	PerModel    = 4 * time.Second
	Total       = 8 * time.Second
	QuietPeriod = 60 * time.Second

	// maxTokensFloor leaves a reasoning model room before its answer; the wall clock, not
	// this number, bounds the cost.
	maxTokensFloor = 512
)

// Chain is the [code_language_llm] table of the session's lighthouse.toml. KeyEnv and
// EnvFile are "" when absent; EnvFile is absolute, with ~ expanded.
type Chain struct {
	URL     string
	Models  []string
	KeyEnv  string
	EnvFile string
}

// Reply is the outcome of Ask. NotEnglish holds one verdict per item, nil when the chain
// failed. Failures are "<model>: <reason>", "key: <reason>" or "skipped: <reason>".
type Reply struct {
	NotEnglish []bool
	Model      string
	Failures   []string
}

// ChainFrom reads the [code_language_llm] table of c: nil, nil without one. A table that is
// there but wrong gives a *config.Error.
func ChainFrom(c *config.Config) (*Chain, error) {
	if _, ok := c.Raw["code_language_llm"]; !ok {
		return nil, nil
	}
	get := func(key string) any {
		v, err := config.Lookup(c, "code_language_llm."+key)
		if err != nil {
			return nil
		}
		return v
	}
	text := func(key string) string {
		s, _ := get(key).(string)
		return s
	}
	address, keyEnv, envFile := text("url"), text("key_env"), text("env_file")
	if address == "" {
		return nil, &config.Error{Msg: "key code_language_llm.url is missing"}
	}
	var models []string
	if list, ok := get("models").([]any); ok {
		for _, m := range list {
			if s, ok := m.(string); ok {
				models = append(models, s)
			}
		}
	}
	if len(models) == 0 {
		return nil, &config.Error{Msg: "key code_language_llm.models is missing or empty"}
	}
	path := ""
	if envFile != "" {
		path = expandUser(envFile)
		if !filepath.IsAbs(path) {
			return nil, &config.Error{Msg: "code_language_llm.env_file is relative: " + envFile}
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return nil, &config.Error{Msg: "code_language_llm.env_file does not exist: " + path}
		}
	}
	return &Chain{URL: address, Models: models, KeyEnv: keyEnv, EnvFile: path}, nil
}

// expandUser is Python's os.path.expanduser: a leading ~ is $HOME (else the account's home),
// ~name the home of that account. A path it cannot expand comes back unchanged.
func expandUser(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	name, rest, slash := strings.Cut(p[1:], "/")
	if slash {
		rest = "/" + rest
	}
	var home string
	if name == "" {
		if h, ok := os.LookupEnv("HOME"); ok {
			home = h
		} else if u, err := user.Current(); err == nil {
			home = u.HomeDir
		} else {
			return p
		}
	} else if u, err := user.Lookup(name); err == nil {
		home = u.HomeDir
	} else {
		return p
	}
	if expanded := strings.TrimRight(home, "/") + rest; expanded != "" {
		return expanded
	}
	return "/"
}

// BuildPrompt asks for one verdict per numbered item; whitespace in a text folds to one space.
func BuildPrompt(items [][2]string) string {
	lines := make([]string, len(items))
	for i, item := range items {
		lines[i] = fmt.Sprintf("%d. %s: %s", i+1, item[0], strings.Join(strings.FieldsFunc(item[1], isPySpace), " "))
	}
	return "For each numbered item (a code identifier, a file name or a comment), decide whether " +
		"it is English. Abbreviations and code jargon (getint, asctime, gids, val) count as " +
		"English. Any word of another language, such as Dutch, makes the item NOT-EN. " +
		"Reply with JSON only, for example {\"1\": \"EN\", \"2\": \"NOT-EN\"}.\n" + strings.Join(lines, "\n")
}

// ParseAnswer is true per item that is not English: the JSON object from the first { to the
// last } of content, with "EN" or "NOT-EN" under "1" to n. An error when it cannot be read.
func ParseAnswer(content string, n int) ([]bool, error) {
	start, end := strings.IndexByte(content, '{'), strings.LastIndexByte(content, '}')
	if start < 0 || end < start {
		return nil, errors.New("no JSON object in the answer")
	}
	var data any
	if err := json.Unmarshal([]byte(content[start:end+1]), &data); err != nil {
		return nil, err
	}
	object, ok := data.(map[string]any)
	if !ok {
		return nil, errors.New("the answer is not an object")
	}
	out := make([]bool, n)
	for i := range n {
		verdict := object[strconv.Itoa(i+1)]
		if verdict != "EN" && verdict != "NOT-EN" {
			return nil, fmt.Errorf("item %d has no EN or NOT-EN", i+1)
		}
		out[i] = verdict == "NOT-EN"
	}
	return out, nil
}

// DefaultStateDir is where the marker of a failed chain lives, per user: the uid is in the
// name, because /tmp is shared.
func DefaultStateDir() string { return stateDirFor(os.Getuid()) }

func stateDirFor(uid int) string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = "/tmp"
	}
	return filepath.Join(base, fmt.Sprintf("lighthouse-code-language-%d", uid))
}

// failMarker is the marker of a failed chain to url.
func failMarker(stateDir, url string) string {
	sum := sha1.Sum([]byte(url))
	return filepath.Join(stateDir, "layer2-failed-"+hex.EncodeToString(sum[:])[:12])
}

// unixNow is the time as Unix seconds with a fraction.
func unixNow() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// quietFor is the age of the marker in seconds, and whether that is within QuietPeriod. A
// marker counts only in a private state dir, and never through a symlink.
func quietFor(stateDir, url string) (float64, bool) {
	if !private(stateDir) {
		return 0, false
	}
	f, err := os.OpenFile(failMarker(stateDir, url), os.O_RDONLY|noFollow, 0)
	if err != nil {
		return 0, false
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(f)
	if err != nil {
		return 0, false
	}
	stamp, err := strconv.ParseFloat(trimSpace(string(raw)), 64)
	if err != nil {
		return 0, false
	}
	age := unixNow() - stamp
	return age, age >= 0 && age < QuietPeriod.Seconds()
}

// markFailed writes the marker. It never follows a symlink and never writes into a state dir
// others can write.
func markFailed(stateDir, url string) {
	if err := os.MkdirAll(filepath.Dir(stateDir), 0o777); err != nil {
		return
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return
	}
	if !private(stateDir) {
		return
	}
	f, err := os.OpenFile(failMarker(stateDir, url), os.O_WRONLY|os.O_CREATE|os.O_TRUNC|noFollow, 0o600)
	if err != nil {
		return
	}
	stamp := strconv.FormatFloat(unixNow(), 'f', -1, 64)
	if !strings.Contains(stamp, ".") {
		stamp += ".0" // as Python's str(time.time())
	}
	_, _ = f.WriteString(stamp)
	_ = f.Close()
}

// key is the API key from the environment, else from EnvFile; "" when there is none.
func key(ch *Chain) string {
	if ch.KeyEnv == "" {
		return ""
	}
	if v := os.Getenv(ch.KeyEnv); v != "" {
		return v
	}
	if ch.EnvFile == "" {
		return ""
	}
	raw, err := os.ReadFile(ch.EnvFile)
	if err != nil || !utf8.Valid(raw) {
		return ""
	}
	for _, line := range splitLines(string(raw)) {
		line = trimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		left, value, _ := strings.Cut(line, "=")
		if trimSpace(left) == ch.KeyEnv {
			return strings.Trim(trimSpace(value), "\"'")
		}
	}
	return ""
}

// Ask puts items ([kind, text] each) to the models of ch, in order, and returns the first
// readable answer. perModel bounds each call, total the chain. A failed chain writes the
// marker in stateDir; while it is younger than QuietPeriod, Ask calls nothing.
func Ask(ctx context.Context, ch *Chain, items [][2]string, stateDir string, perModel, total time.Duration) Reply {
	if age, quiet := quietFor(stateDir, ch.URL); quiet {
		return Reply{Failures: []string{fmt.Sprintf("skipped: the chain failed %.0f s ago", age)}}
	}
	apiKey := key(ch)
	if ch.KeyEnv != "" && apiKey == "" {
		return Reply{Failures: []string{"key: " + ch.KeyEnv + " is set in neither the environment nor env_file"}}
	}
	prompt := BuildPrompt(items)
	deadline := time.Now().Add(total)
	failures := []string{}
	for _, model := range ch.Models {
		left := time.Until(deadline)
		if left <= 0 {
			failures = append(failures, fmt.Sprintf("%s: total time of %.6g s is up", model, total.Seconds()))
			continue
		}
		answer, reason := call(ctx, ch, model, prompt, len(items), apiKey, min(perModel, left))
		if answer != nil {
			_ = os.Remove(failMarker(stateDir, ch.URL))
			return Reply{NotEnglish: answer, Model: model, Failures: failures}
		}
		failures = append(failures, model+": "+reason)
	}
	markFailed(stateDir, ch.URL)
	return Reply{Failures: failures}
}

// completion is the request body of /chat/completions.
type completion struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature int       `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// call asks one model within wait: the verdicts, or nil and the reason it failed.
func call(ctx context.Context, ch *Chain, model, prompt string, n int, apiKey string, wait time.Duration) ([]bool, string) {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	body, err := json.Marshal(completion{
		Model:       model,
		Messages:    []message{{Role: "user", Content: prompt}},
		Temperature: 0,
		MaxTokens:   max(maxTokensFloor, 20+12*n),
	})
	if err != nil {
		return nil, err.Error()
	}
	endpoint := strings.TrimRight(ch.URL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, transportReason(ctx, err, wait)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, transportReason(ctx, err, wait)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, transportReason(ctx, err, wait)
	}
	answer, err := readCompletion(raw, n)
	if err != nil {
		return nil, "unreadable answer"
	}
	return answer, ""
}

// transportReason says why a call got no answer: the deadline, or the transport's error
// without the URL around it.
func transportReason(ctx context.Context, err error, wait time.Duration) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Sprintf("no answer within %.1f s", wait.Seconds())
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

// readCompletion is the verdicts in choices[0].message.content of a reply.
func readCompletion(raw []byte, n int) ([]bool, error) {
	var reply struct {
		Choices []struct {
			Message struct {
				Content *string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, err
	}
	if len(reply.Choices) == 0 || reply.Choices[0].Message.Content == nil {
		return nil, errors.New("no content in the reply")
	}
	return ParseAnswer(*reply.Choices[0].Message.Content, n)
}
