package packs

import (
	"bytes"
	"regexp"
)

// Lookup returns the value of a dotted config key: ok false when it is not set, err
// when the config cannot be read.
type Lookup func(key string) (value string, ok bool, err error)

var placeholder = regexp.MustCompile(`@@([a-z_.]+)@@`)

// Fill replaces every @@<key>@@ in content with its value from lookup (spec,
// Portabiliteit: workflows read no TOML). Keys without a value come back in missing,
// each once, and content is then returned unchanged.
func Fill(content []byte, lookup Lookup) (out []byte, missing []string, err error) {
	values := map[string]string{}
	for _, m := range placeholder.FindAllSubmatch(content, -1) {
		key := string(m[1])
		if _, seen := values[key]; seen {
			continue
		}
		v, ok, err := lookup(key)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			missing = append(missing, key)
			values[key] = ""
			continue
		}
		values[key] = v
	}
	if len(missing) > 0 {
		return content, missing, nil
	}
	return placeholder.ReplaceAllFunc(content, func(m []byte) []byte {
		return []byte(values[string(bytes.Trim(m, "@"))])
	}), nil, nil
}
