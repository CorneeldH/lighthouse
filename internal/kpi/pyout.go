package kpi

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// object is a JSON object that keeps its key order, like a Python dict.
type object []field

type field struct {
	Key   string
	Value any
}

// pyFloat is a float that prints the way Python's repr does ("1.0", "0.667").
type pyFloat float64

// encode renders v like Python's json.dumps(v, indent=2, ensure_ascii=False), without the
// closing newline. It is not json.MarshalIndent: that escapes U+2028 and <>&.
func encode(v any) string {
	var sb strings.Builder
	writeValue(&sb, v, 0)
	return sb.String()
}

func writeValue(sb *strings.Builder, v any, depth int) {
	switch x := v.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		sb.WriteString(strconv.FormatBool(x))
	case int:
		sb.WriteString(strconv.Itoa(x))
	case pyFloat:
		sb.WriteString(config.FormatFloat(float64(x)))
	case string:
		writeString(sb, x)
	case []string:
		items := make([]any, len(x))
		for i, s := range x {
			items[i] = s
		}
		writeList(sb, items, depth)
	case []any:
		writeList(sb, x, depth)
	case object:
		if len(x) == 0 {
			sb.WriteString("{}")
			return
		}
		sb.WriteString("{\n")
		for i, f := range x {
			sb.WriteString(strings.Repeat("  ", depth+1))
			writeString(sb, f.Key)
			sb.WriteString(": ")
			writeValue(sb, f.Value, depth+1)
			if i < len(x)-1 {
				sb.WriteString(",")
			}
			sb.WriteString("\n")
		}
		sb.WriteString(strings.Repeat("  ", depth) + "}")
	default:
		panic(fmt.Sprintf("kpi: cannot encode %T", v))
	}
}

func writeList(sb *strings.Builder, items []any, depth int) {
	if len(items) == 0 {
		sb.WriteString("[]")
		return
	}
	sb.WriteString("[\n")
	for i, it := range items {
		sb.WriteString(strings.Repeat("  ", depth+1))
		writeValue(sb, it, depth+1)
		if i < len(items)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString(strings.Repeat("  ", depth) + "]")
}

func writeString(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			sb.WriteString(`\"`)
		case r == '\\':
			sb.WriteString(`\\`)
		case r == '\n':
			sb.WriteString(`\n`)
		case r == '\r':
			sb.WriteString(`\r`)
		case r == '\t':
			sb.WriteString(`\t`)
		case r == '\b':
			sb.WriteString(`\b`)
		case r == '\f':
			sb.WriteString(`\f`)
		case r < 0x20:
			fmt.Fprintf(sb, `\u%04x`, r)
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
}

// pyRound is Python's round(x, n): half-even on the exact binary value of x.
func pyRound(x float64, n int) float64 {
	f, err := strconv.ParseFloat(strconv.FormatFloat(x, 'f', n, 64), 64)
	if err != nil {
		return x
	}
	return f
}

// medianOf is the median of values: the middle one, or the mean of the two middle ones.
func medianOf(values []float64) float64 {
	s := slices.Clone(values)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func toFloats(values []int) []float64 {
	out := make([]float64, len(values))
	for i, v := range values {
		out[i] = float64(v)
	}
	return out
}

// summary is Python's _summary: an odd count gives an int median, an even count a float.
func summary(values []int) object {
	if len(values) == 0 {
		return object{{"n", 0}}
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	var median any = sorted[len(sorted)/2]
	if len(sorted)%2 == 0 {
		median = pyFloat(medianOf(toFloats(values)))
	}
	sum := 0
	for _, v := range values {
		sum += v
	}
	return object{
		{"n", len(values)},
		{"min", sorted[0]},
		{"median", median},
		{"max", sorted[len(sorted)-1]},
		{"mean", pyFloat(pyRound(float64(sum)/float64(len(values)), 1))},
	}
}

// summaryInt is Python's _summary_int: the median is rounded half-even to an int.
func summaryInt(values []int) object {
	if len(values) == 0 {
		return object{{"n", 0}, {"median", nil}, {"max", nil}}
	}
	return object{
		{"n", len(values)},
		{"median", int(math.RoundToEven(medianOf(toFloats(values))))},
		{"max", slices.Max(values)},
	}
}

// summaryFloat is Python's _summary_float: median and max rounded to one decimal.
func summaryFloat(values []float64) object {
	if len(values) == 0 {
		return object{{"n", 0}, {"median", nil}, {"max", nil}}
	}
	return object{
		{"n", len(values)},
		{"median", pyFloat(pyRound(medianOf(values), 1))},
		{"max", pyFloat(pyRound(slices.Max(values), 1))},
	}
}

// pyPath prints a path like str(Path(p)): double slashes merged, "." parts and a trailing
// slash dropped, ".." kept.
func pyPath(p string) string {
	if p == "" {
		return "."
	}
	prefix := ""
	if strings.HasPrefix(p, "/") {
		prefix = "/"
		if strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "///") {
			prefix = "//"
		}
	}
	var parts []string
	for _, part := range strings.Split(p, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 && prefix == "" {
		return "."
	}
	return prefix + strings.Join(parts, "/")
}

// comparePaths orders paths like pathlib: component by component, not as plain strings.
func comparePaths(a, b string) int {
	return slices.Compare(strings.Split(a, "/"), strings.Split(b, "/"))
}

func sortPaths(paths []string) {
	sort.SliceStable(paths, func(i, j int) bool { return comparePaths(paths[i], paths[j]) < 0 })
}
