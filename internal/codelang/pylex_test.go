package codelang

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Every source the tokenizer of Python 3.12 (or ast.parse) refuses is a CannotCheckError.
func TestPyLexErrors(t *testing.T) {
	tests := []struct {
		name, src, reason string
	}{
		{"NUL byte", "x = 1\x00\n", "null bytes"},
		{"a line break ends a string", "x = 'a\n'\n", "unterminated string literal"},
		{"unterminated f-string", "x = f'abc\n", "unterminated f-string literal"},
		{"unterminated triple-quoted string", "x = '''abc\n", "unterminated triple-quoted string literal"},
		{"unterminated triple-quoted f-string", "x = f\"\"\"{a}\n", "unterminated triple-quoted f-string literal"},
		{"EOF with an open bracket", "x = (1,\n", "EOF in multi-line statement"},
		{"stray closer", "x = 1)\n", "unmatched ')'"},
		{"wrong closer", "x = (1]\n", "closing parenthesis ']' does not match opening parenthesis '('"},
		{"dedent to an unknown level", "if x:\n        y = 1\n    z = 2\n", "unindent does not match any outer indentation level"},
		{"a tab indents to the next multiple of 8", "if a:\n\tb = 1\n    c = 2\n", "unindent does not match any outer indentation level"},
		{"backslash at EOF", "x = 1 \\", "unexpected EOF while parsing"},
		{"backslash-newline at EOF", "x = 1\n\\\n", "unexpected EOF while parsing"},
		{"character after a continuation", "x = 1 \\ y\n", "unexpected character after line continuation character"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := scanPython(tt.src)
			var cc *CannotCheckError
			if !errors.As(err, &cc) {
				t.Fatalf("want a CannotCheckError, got %v", err)
			}
			if !strings.HasPrefix(cc.Reason, "cannot parse Python: ") || !strings.Contains(cc.Reason, tt.reason) {
				t.Errorf("reason = %q, want it to name %q", cc.Reason, tt.reason)
			}
		})
	}
}

// Review focus 1, a deliberate deviation: Python's ast.parse refuses "x = = 1", so
// code_language.py reports COULD NOT CHECK. The Go scanner has no parser, only a tokenizer,
// so a grammar error that tokenizes fine is scanned like any other source.
func TestPythonGrammarErrorIsScanned(t *testing.T) {
	units, err := scanPython("x = = 1\n# lees het bestand\n")
	if err != nil {
		t.Fatalf("a grammar error must still be scanned, got %v", err)
	}
	var comments []Unit
	for _, u := range units {
		if u.Kind == "comment" {
			comments = append(comments, u)
		}
	}
	want := []Unit{{Line: 2, Kind: "comment", Text: "lees het bestand", Body: "lees het bestand", LineBreaks: true}}
	if !reflect.DeepEqual(comments, want) {
		t.Errorf("comments = %+v, want %+v", comments, want)
	}
}

// The expected rows come from code_language.py on Python 3.12 (comments, ast.get_docstring and
// _name_nodes), in the order its scan_python sorts them: by line, stable.
func TestScanPythonUnits(t *testing.T) {
	tests := []struct {
		name, src string
		want      []string
	}{
		{
			"assignment targets",
			"a = b = 1\nx, (y, *z) = 1, 2\n[p, q] = r\nobj.attr = 1\nobj.sub[0] = 1\nf().g = 2\nn += 1\nm: int\nk: int = 0\nself.h: int = 1\n",
			[]string{"1 name a", "1 name b", "2 name x", "2 name y", "2 name z", "3 name p", "3 name q", "4 name attr", "6 name g", "7 name n", "8 name m", "9 name k", "10 name h"},
		},
		{
			"an attribute counts on the line where it ends",
			"(a.\n b) = 1\n",
			[]string{"2 name b"},
		},
		{
			"for targets, comprehensions and walrus",
			"for i, j in x:\n    pass\nv = [k for k in y if (w := k)]\nasync def f():\n    async for u in z:\n        pass\n",
			[]string{"1 name i", "1 name j", "3 name v", "3 name k", "3 name w", "4 name f", "5 name u"},
		},
		{
			"with, except and import aliases",
			"with open(a) as fa, open(b) as (fb, fc):\n    pass\ntry:\n    pass\nexcept (E, F) as err:\n    pass\nimport os.path as osp, sys\nfrom x import (y as yy,\n    z as zz, w)\n",
			[]string{"1 name fa", "1 name fb", "1 name fc", "5 name err", "7 name osp", "8 name yy", "9 name zz"},
		},
		{
			"def, class and lambda parameters",
			"def f(a, b: int = 1, *args, c, d=lambda q, *r: 0, **kw):\n    pass\nclass K(B, metaclass=M):\n    pass\ndef g[T](x, /, y):\n    pass\n",
			[]string{"1 name f", "1 name a", "1 name b", "1 name args", "1 name c", "1 name d", "1 name kw", "1 name q", "1 name r", "3 name K", "5 name g", "5 name x", "5 name y"},
		},
		{
			"keyword arguments, global, del, plain import and match captures are no names",
			"f(key=1)\nglobal gg\ndel dd\nimport plain\nmatch cmd:\n    case [cap, *rest] if (gw := 1):\n        pass\n    case Point(x=px) as whole:\n        pass\n",
			[]string{"6 name gw"},
		},
		{
			"a statement's own names come before those in its expressions",
			"def f(a=lambda x: 0, b=1): pass\nx = [a for a in b]\nfor i in (j := c): pass\na1 = 1; b1 = [c1 for c1 in d]\n",
			[]string{"1 name f", "1 name a", "1 name b", "1 name x", "2 name x", "2 name a", "3 name i", "3 name j", "4 name a1", "4 name b1", "4 name c1"},
		},
		{
			"on one line, a name deeper in the AST comes later",
			"i, j = x = 1\nfor (key, path), attribute in y: pass\nassert all(s for s in p), [t for t in q]\n",
			[]string{"1 name x", "1 name i", "1 name j", "2 name attribute", "2 name key", "2 name path", "3 name t", "3 name s"},
		},
		{
			"a * or / inside a default is an operator, not a parameter marker",
			"def f(a=2 * B, b=x / Y): pass\n",
			[]string{"1 name f", "1 name a", "1 name b"},
		},
		{
			"type alias and soft keywords as names",
			"type Alias = int\ntype = 3\nmatch = 1\nmatch[0] = 2\n",
			[]string{"1 name Alias", "2 name type", "3 name match"},
		},
		{
			"docstrings",
			"r\"\"\"Raw \\n doc.\"\"\"\nclass A:\n    \"Con\" 'cat'\n    def f(self): \"inline\\tdoc\"\n    def g(self):\n        f\"no {x}\"\n    def h(self):\n        b\"no\"\n    def i(self):\n        \"\"\"\n        Indented\n          more\n\n        \"\"\"\n",
			[]string{
				"1 docstring \"Raw \\\\n doc.\" body \"Raw \\\\n doc.\"", "2 name A", "3 docstring \"Concat\" body \"Concat\"",
				"4 docstring \"inline  doc\" body \"inline  doc\"", "4 name f", "4 name self", "5 name g", "5 name self",
				"7 name h", "7 name self", "9 name i", "9 name self", "10 docstring \"Indented\" body \"Indented\\n  more\"",
			},
		},
		{
			"a parenthesized docstring counts on the line of the parenthesis",
			"(\n\"doc\")\n",
			[]string{"1 docstring \"doc\" body \"doc\""},
		},
		{
			"escapes in a docstring",
			"\"\"\"a\\x41\\u00e9\\101\\\nb\\q\"\"\"\n",
			[]string{"1 docstring \"aAéAb\\\\q\" body \"aAéAb\\\\q\""},
		},
		{
			"f-strings: nested quotes, conversions, format specs and expressions",
			"x = f\"{d['a']!r:>{w}} {{lit}} {(y := 2)}\"\nz = rf\"\\{q}\" f'''{\n  [v for v in t]\n}'''\n# na\n",
			[]string{"1 name x", "1 name y", "2 name z", "3 name v", "5 comment \"na\" body \"na\""},
		},
		{
			"a lambda after = ends the targets",
			"x = lambda a, b=c: 0\n",
			[]string{"1 name x", "1 name a", "1 name b"},
		},
		{
			"only the first statement of a body is a docstring",
			"def f(): return 1\n\"\"\"Lees het bestand.\"\"\"\n",
			[]string{"1 name f"},
		},
		{
			"doubled braces in an f-string are text",
			"x = f\"{{y := 1}}\"\n",
			[]string{"1 name x"},
		},
		{
			"a format spec is text, a quote in it included",
			"s = f\"{x:'>10}\"\n# lees het bestand\n",
			[]string{"1 name s", "2 comment \"lees het bestand\" body \"lees het bestand\""},
		},
		{
			"a format spec nested in a format spec",
			"x = f\"{v:{w:>3}}\"\n",
			[]string{"1 name x"},
		},
		{
			"an f-string with nested specs inside the field of another",
			"x = f\"{f'{v:{w:>{(c := 1)}}}':{u:{t}}}\"\n",
			[]string{"1 name x", "1 name c"},
		},
		{
			"match and case as names of an annotated attribute",
			"match(x).y: int = 3\ncase(x).y: int = 3\nmatch x:\n    case (a): b = 1\n",
			[]string{"1 name y", "2 name y", "4 name b"},
		},
		{
			"a number ends before a keyword",
			"v = [1for y in z] + [0x1for w in q] + [1if k else 2]\nn = 1.5e-3j + 1_0.0_1 + 0b1_0 + 0o7 + .5 + 1.e5\n",
			[]string{"1 name v", "1 name y", "2 name n"},
		},
		{
			"continuation lines",
			"x = 1 + \\\n    2\ny = (1,\n     2)\n",
			[]string{"1 name x", "3 name y"},
		},
		{
			"comment blocks; a hash in a string is no comment",
			"s = '# no'  # een\n# twee\n\n# drie\n",
			[]string{"1 comment \"een\" body \"een\\ntwee\"", "1 name s", "4 comment \"drie\" body \"drie\""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			units, err := scanPython(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			sort.SliceStable(units, func(i, j int) bool { return units[i].Line < units[j].Line })
			got := []string{}
			for _, u := range units {
				if u.Name {
					got = append(got, fmt.Sprintf("%d %s %s", u.Line, u.Kind, u.Text))
				} else {
					got = append(got, fmt.Sprintf("%d %s %q body %q", u.Line, u.Kind, u.Text, u.Body))
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}
