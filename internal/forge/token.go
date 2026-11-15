package forge

import (
	"fmt"
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

// Token returns the token for a forge of the given kind ("" is github). With tokenEnv set only
// that variable is read; otherwise the default variables of the kind, first non-empty wins.
// None set gives a *TokenError; an unknown kind gives a plain error.
func Token(kind, tokenEnv string, getenv func(string) string) (string, error) {
	if kind == "" {
		kind = "github"
	}
	var vars []string
	switch kind {
	case "github":
		vars = []string{"GH_TOKEN", "GITHUB_TOKEN"}
	case "gitlab":
		vars = []string{"GITLAB_TOKEN"}
	default:
		return "", fmt.Errorf("unknown forge kind %q", kind)
	}
	if tokenEnv != "" {
		vars = []string{tokenEnv}
	}
	for _, v := range vars {
		if t := getenv(v); t != "" {
			return t, nil
		}
	}
	return "", &TokenError{Kind: kind, Vars: vars}
}
