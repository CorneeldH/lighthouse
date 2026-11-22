package mapplanned

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Issues returns every integer value of a key "issue" anywhere in the JSON or YAML document,
// chosen by the extension of path (.json; .yaml or .yml). Another extension, a parse error, or
// a second JSON value or YAML document after the first is an error that names path.
func Issues(path string, doc []byte) (map[int]bool, error) {
	var tree any
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		dec := json.NewDecoder(bytes.NewReader(doc))
		dec.UseNumber()
		if err := dec.Decode(&tree); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if _, err := dec.Token(); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: data after the first JSON value", path)
		}
	case ".yaml", ".yml":
		dec := yaml.NewDecoder(bytes.NewReader(doc))
		// An empty document is no document at all, as yaml.Unmarshal reads it.
		if err := dec.Decode(&tree); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		var second any
		if err := dec.Decode(&second); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: more than one YAML document", path)
		}
	default:
		return nil, fmt.Errorf("%s: unsupported document format, want .json, .yaml or .yml", path)
	}
	found := map[int]bool{}
	collectIssues(tree, found)
	return found, nil
}

func collectIssues(node any, found map[int]bool) {
	visit := func(key, child any) {
		if key == "issue" {
			if n, ok := wholeNumber(child); ok {
				found[n] = true
			}
		}
		collectIssues(child, found)
	}
	switch v := node.(type) {
	case map[string]any:
		for key, child := range v {
			visit(key, child)
		}
	case []any:
		for _, child := range v {
			collectIssues(child, found)
		}
	}
}

// wholeNumber reads a JSON number (json.Number) or a YAML int as an int; anything else,
// a fraction included, is not an issue number.
func wholeNumber(v any) (int, bool) {
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		if err != nil || i > math.MaxInt32 {
			return 0, false
		}
		return int(i), true
	case int:
		return n, true
	}
	return 0, false
}
