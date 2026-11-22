package mapplanned

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Issues returns every integer value of a key "issue" anywhere in the JSON or YAML document,
// chosen by the extension of path (.json; .yaml or .yml). Another extension or a parse error
// is an error that names path.
func Issues(path string, doc []byte) (map[int]bool, error) {
	var tree any
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		dec := json.NewDecoder(bytes.NewReader(doc))
		dec.UseNumber()
		if err := dec.Decode(&tree); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(doc, &tree); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	default:
		return nil, fmt.Errorf("%s: unsupported document format, want .json, .yaml or .yml", path)
	}
	found := map[int]bool{}
	collectIssues(tree, found)
	return found, nil
}

func collectIssues(node any, found map[int]bool) {
	switch v := node.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "issue" {
				if n, ok := wholeNumber(child); ok {
					found[n] = true
				}
			}
			collectIssues(child, found)
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
