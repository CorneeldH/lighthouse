package forge

import (
	"fmt"
	"slices"
	"strings"
)

// TokenError reports that none of the variables that may hold a forge token is set. It names
// the variables, never a value.
type TokenError struct {
	Kind string   // "github" or "gitlab"
	Vars []string // the variables that were looked at, in order
}

func (e *TokenError) Error() string {
	label := "GitHub"
	if e.Kind == "gitlab" {
		label = "GitLab"
	}
	return "no " + label + " token: set " + strings.Join(e.Vars, " or ")
}

// defaultTokenVars are the variables Token reads per forge kind without token_env, in order.
var defaultTokenVars = map[string][]string{
	"github": {"GH_TOKEN", "GITHUB_TOKEN"},
	"gitlab": {"GITLAB_TOKEN"},
}

// otherTokenVars hold a forge token for tools other than lighthouse (gh on GitHub Enterprise);
// Token never reads them, but a process that must not see a token must not inherit them either.
var otherTokenVars = []string{"GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}

// TokenVarNames are the names of every variable that may hold a forge token in this process:
// the default variables of every kind, the ones of other tools (otherTokenVars), plus tokenEnv
// when set, sorted and each once. A process that must not see a token (the agent of a scheduled
// run) gets all of them removed.
func TokenVarNames(tokenEnv string) []string {
	out := slices.Clone(otherTokenVars)
	for _, vars := range defaultTokenVars {
		out = append(out, vars...)
	}
	if tokenEnv != "" {
		out = append(out, tokenEnv)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Token returns the token for a forge of the given kind ("" is github). With tokenEnv set only
// that variable is read; otherwise the default variables of the kind, first non-empty wins.
// None set gives a *TokenError; an unknown kind gives a plain error.
func Token(kind, tokenEnv string, getenv func(string) string) (string, error) {
	name, err := TokenVar(kind, tokenEnv, getenv)
	if err != nil {
		return "", err
	}
	return getenv(name), nil
}

// TokenVar returns the name of the variable Token reads the token from: the first one that is
// set, with the same order, errors and *TokenError as Token. A process that hands the token on
// by its variable (a credential helper) names it without reading it into an argument.
func TokenVar(kind, tokenEnv string, getenv func(string) string) (string, error) {
	if kind == "" {
		kind = "github"
	}
	vars, ok := defaultTokenVars[kind]
	if !ok {
		return "", fmt.Errorf("unknown forge kind %q", kind)
	}
	if tokenEnv != "" {
		vars = []string{tokenEnv}
	}
	for _, v := range vars {
		if getenv(v) != "" {
			return v, nil
		}
	}
	return "", &TokenError{Kind: kind, Vars: vars}
}
