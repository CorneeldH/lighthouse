package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ErrUnknownKey is wrapped by the error for a key that headings.toml does not have.
var ErrUnknownKey = errors.New("unknown key")

// HeadingsPath is LIGHTHOUSE_HEADINGS, else <repo root>/.lighthouse/headings.toml.
func HeadingsPath() (string, error) {
	if override := os.Getenv("LIGHTHOUSE_HEADINGS"); override != "" {
		return override, nil
	}
	root, err := RepoRoot()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(root, "/") + "/.lighthouse/headings.toml", nil
}

// headingsTable is the validated headings.toml: per key the variants in the order nl, the
// other known languages, then any further language in document order.
type headingsTable struct {
	variants map[string][]headingVariant
}

type headingVariant struct{ language, text string }

func loadHeadings() (*headingsTable, error) {
	rawPath, err := HeadingsPath()
	if err != nil {
		return nil, err
	}
	path := pyPath(rawPath)
	data, md, err := readTOML(rawPath, path+": file is missing")
	if err != nil {
		return nil, err
	}
	// Document order of the extra languages, per key.
	extras := map[string][]string{}
	for _, k := range md.Keys() {
		if len(k) == 2 {
			extras[k[0]] = append(extras[k[0]], k[1])
		}
	}
	table := &headingsTable{variants: map[string][]headingVariant{}}
	for _, key := range sortedNames(data) {
		entry, ok := data[key].(map[string]any)
		if !ok {
			return nil, errf("%s: key %s must be a table", path, key)
		}
		for _, language := range languages {
			if s, ok := entry[language].(string); !ok || s == "" {
				return nil, errf("%s: key %s.%s must be a non-empty string", path, key, language)
			}
		}
		order := append([]string{"nl"}, languages...)
		order = append(order, extras[key]...)
		var out []headingVariant
		seen := map[string]bool{}
		for _, language := range order {
			if seen[language] {
				continue
			}
			seen[language] = true
			if s, ok := entry[language].(string); ok {
				out = append(out, headingVariant{language, s})
			}
		}
		table.variants[key] = out
	}
	return table, nil
}

// Variants is every variant of key, nl first, without duplicates.
func Variants(key string) ([]string, error) {
	table, err := loadHeadings()
	if err != nil {
		return nil, err
	}
	entry, ok := table.variants[key]
	if !ok {
		return nil, fmt.Errorf("%s: %w", key, ErrUnknownKey)
	}
	var out []string
	for _, v := range entry {
		if !contains(out, v.text) {
			out = append(out, v.text)
		}
	}
	return out, nil
}

// Heading is key in language; "" means [project] language of the repo.
func Heading(key, language string) (string, error) {
	table, err := loadHeadings()
	if err != nil {
		return "", err
	}
	entry, ok := table.variants[key]
	if !ok {
		return "", fmt.Errorf("%s: %w", key, ErrUnknownKey)
	}
	if language == "" {
		path, err := Path()
		if err != nil {
			return "", err
		}
		c, err := Load(path)
		if err != nil {
			return "", err
		}
		language = c.Language
	}
	for _, v := range entry {
		if v.language == language {
			return v.text, nil
		}
	}
	return "", errf("key %s has no variant for language %s", key, language)
}

// RunHeadings is `lighthouse headings <key>`: rc 0 with the heading in the project language on
// out, rc 2 with a message on errOut for an unknown key or a configuration error.
func RunHeadings(args []string, out, errOut io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(errOut, "headings: usage: lighthouse headings <key>")
		return 2
	}
	text, err := Heading(args[0], "")
	if errors.Is(err, ErrUnknownKey) {
		_, _ = fmt.Fprintf(errOut, "headings: unknown key %s\n", args[0])
		return 2
	}
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "headings: %v\n", err)
		return 2
	}
	_, _ = fmt.Fprintln(out, text)
	return 0
}
