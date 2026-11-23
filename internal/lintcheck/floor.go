package lintcheck

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	blockingLine  = regexp.MustCompile(`(?m)^BLOCKING_RULES="([^"]*)"[ \t]*$`)
	nonLinterLine = regexp.MustCompile(`(?m)^NON_LINTER_RULES="([^"]*)"[ \t]*$`)
)

// Floor reads BLOCKING_RULES and NON_LINTER_RULES from the installed hook
// <root>/.claude/hooks/lighthouse/<hook> and returns the rules that are linters, in order.
// A missing hook, or one without BLOCKING_RULES, is an UncheckedError.
func Floor(root, hook string) ([]string, error) {
	p := filepath.Join(root, ".claude", "hooks", "lighthouse", hook)
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, UncheckedError(fmt.Sprintf("hook %s is not installed", hook))
		}
		return nil, UncheckedError(fmt.Sprintf("cannot read hook %s: %v", hook, err))
	}
	m := blockingLine.FindSubmatch(data)
	if m == nil {
		return nil, UncheckedError(fmt.Sprintf("hook %s has no BLOCKING_RULES", hook))
	}
	skip := map[string]bool{}
	if n := nonLinterLine.FindSubmatch(data); n != nil {
		for _, id := range strings.Split(string(n[1]), ",") {
			skip[id] = true
		}
	}
	var rules []string
	for _, id := range strings.Split(string(m[1]), ",") {
		if id != "" && !skip[id] {
			rules = append(rules, id)
		}
	}
	return rules, nil
}
