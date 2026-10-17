// Package orderedjson reads and writes JSON while keeping the key order of objects.
//
// `lighthouse init` rewrites `.claude/settings.json`; a map would sort the keys of a
// file that belongs to the project. Objects decode to *Object, arrays to []any,
// numbers to json.Number (so `15` and `1.50` stay as written), and the other
// scalars to string, bool and nil.
package orderedjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

// Object is a JSON object with its keys in order.
type Object struct {
	keys []string
	vals map[string]any
}

// NewObject returns an empty object.
func NewObject() *Object {
	return &Object{vals: map[string]any{}}
}

// Get returns the value of key.
func (o *Object) Get(key string) (any, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// Set sets key to v; a new key comes last.
func (o *Object) Set(key string, v any) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

// Delete removes key.
func (o *Object) Delete(key string) {
	if _, ok := o.vals[key]; !ok {
		return
	}
	delete(o.vals, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

// Keys returns the keys in order.
func (o *Object) Keys() []string {
	return append([]string(nil), o.keys...)
}

// Parse decodes one JSON value. Duplicate keys, comments and trailing data are errors.
func Parse(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("unexpected data after the JSON value at offset %d", dec.InputOffset())
	}
	return v, nil
}

func parseValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("unexpected end of JSON input")
		}
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := NewObject()
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("object key is not a string at offset %d", dec.InputOffset())
				}
				if _, dup := obj.vals[key]; dup {
					return nil, fmt.Errorf("duplicate key %q at offset %d", key, dec.InputOffset())
				}
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				obj.Set(key, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("unexpected %q at offset %d", t, dec.InputOffset())
	default:
		return tok, nil
	}
}

// Marshal encodes v with two-space indentation, without HTML escaping, and a final newline.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := write(&buf, v, ""); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// Canonical encodes v compactly with the keys of every object sorted, for hashing.
func Canonical(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeCanonical(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func write(buf *bytes.Buffer, v any, indent string) error {
	inner := indent + "  "
	switch t := v.(type) {
	case *Object:
		if len(t.keys) == 0 {
			buf.WriteString("{}")
			return nil
		}
		buf.WriteString("{\n")
		for i, k := range t.keys {
			buf.WriteString(inner)
			if err := writeScalar(buf, k); err != nil {
				return err
			}
			buf.WriteString(": ")
			if err := write(buf, t.vals[k], inner); err != nil {
				return err
			}
			if i < len(t.keys)-1 {
				buf.WriteByte(',')
			}
			buf.WriteByte('\n')
		}
		buf.WriteString(indent + "}")
		return nil
	case []any:
		if len(t) == 0 {
			buf.WriteString("[]")
			return nil
		}
		buf.WriteString("[\n")
		for i, e := range t {
			buf.WriteString(inner)
			if err := write(buf, e, inner); err != nil {
				return err
			}
			if i < len(t)-1 {
				buf.WriteByte(',')
			}
			buf.WriteByte('\n')
		}
		buf.WriteString(indent + "]")
		return nil
	default:
		return writeScalar(buf, v)
	}
}

func writeCanonical(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case *Object:
		keys := t.Keys()
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeScalar(buf, k); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := writeCanonical(buf, t.vals[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	case []any:
		buf.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	default:
		return writeScalar(buf, v)
	}
}

func writeScalar(buf *bytes.Buffer, v any) error {
	if n, ok := v.(json.Number); ok {
		buf.WriteString(n.String())
		return nil
	}
	var tmp bytes.Buffer
	enc := json.NewEncoder(&tmp)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	buf.Write(bytes.TrimSuffix(tmp.Bytes(), []byte("\n")))
	return nil
}
