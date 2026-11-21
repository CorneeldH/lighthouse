package packcontract

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// Files of a package outside its skills: the lighthouse.toml fragment init merges into the
// project config, and the shell every package installs.

// fragmentBase is the project table init writes before a fragment's tables.
const fragmentBase = "[project]\nlanguage = \"en\"\n\n"

// leafKeys returns the dotted key of every non-table value in table.
func leafKeys(prefix string, table map[string]any) []string {
	var keys []string
	for k, v := range table {
		if sub, ok := v.(map[string]any); ok {
			keys = append(keys, leafKeys(prefix+k+".", sub)...)
			continue
		}
		keys = append(keys, prefix+k)
	}
	sort.Strings(keys)
	return keys
}

// Classification: fails when a pack/*/lighthouse.toml.fragment holds a value of the wrong type
// or a key the config reader does not know: init copies the keys into lighthouse.toml as they
// are, and every hook that reads the config would break.
func TestFragmentIsValidConfig(t *testing.T) {
	fragments, err := filepath.Glob(filepath.Join(packRoot, "*", "lighthouse.toml.fragment"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) == 0 {
		t.Fatal("no pack/*/lighthouse.toml.fragment found")
	}
	for _, path := range fragments {
		rel, err := filepath.Rel(packRoot, path)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(filepath.Dir(rel), func(t *testing.T) {
			text := readPack(t, rel)
			if _, err := config.Parse([]byte(fragmentBase+text), "lighthouse.toml"); err != nil {
				t.Fatalf("config.Parse: %v", err)
			}
			var table map[string]any
			if _, err := toml.Decode(text, &table); err != nil {
				t.Fatal(err)
			}
			keys := leafKeys("", table)
			if len(keys) == 0 {
				t.Fatal("the fragment has no keys")
			}
			for _, key := range keys {
				if config.KindOf(key) == "unknown" {
					t.Errorf("%s: unknown key", key)
				}
			}
		})
	}
}

// Shell under pack/ must run on macOS (BSD userland, bash 3.2) as well as on Linux (#90). A line
// that deliberately uses a banned construct next to a fallback carries `# portable: <reason>`.
// A `command -v <tool>` probe is a fallback test, not a use.

// portableRule is a construct that does not run on macOS, and why.
type portableRule struct {
	re     *regexp.Regexp
	reason string
}

// gnuOnly are GNU userland constructs BSD lacks. RE2 has no lookbehind: the Python
// (?<![\w-])sha256sum\b is (^|[^\w-])sha256sum\b.
var gnuOnly = []portableRule{
	{regexp.MustCompile(`\bsed -i(\s|$)`), "sed -i without a suffix (BSD needs -i '')"},
	{regexp.MustCompile(`\bdate -d\b`), "GNU date -d"},
	{regexp.MustCompile(`\bstat -c\b`), "GNU stat -c"},
	{regexp.MustCompile(`\breadlink -f\b`), "readlink -f"},
	{regexp.MustCompile(`\bgrep -[a-zA-Z]*P`), "grep -P"},
	{regexp.MustCompile(`\bxargs -r\b`), "xargs -r"},
	{regexp.MustCompile(`(^|[^\w-])sha256sum\b`), "sha256sum (macOS has shasum -a 256)"},
	{regexp.MustCompile(`-printf\b`), "find -printf"},
}

// bash4 are constructs bash 3.2 lacks.
var bash4 = []portableRule{
	{regexp.MustCompile(`\bdeclare -A\b`), "associative array"},
	{regexp.MustCompile(`\b(mapfile|readarray)\b`), "mapfile/readarray"},
	{regexp.MustCompile(`\$\{\w+(,,|\^\^)\}`), "case conversion"},
	{regexp.MustCompile(`&>>`), "&>>"},
	{regexp.MustCompile(`\|&`), "|&"},
	{regexp.MustCompile(`\blocal -n\b`), "nameref"},
}

var (
	portableMarker  = regexp.MustCompile(`#\s*portable:(.*)$`)
	commandProbe    = regexp.MustCompile(`\bcommand -v \S+`)
	trailingComment = regexp.MustCompile(`(^|\s)#.*$`)
	shellShebang    = regexp.MustCompile(`^#!.*\b(ba)?sh\b`)
)

// shellLines returns the lines of every shell file under pack/ (a .sh file, or a file without
// suffix whose shebang names sh or bash), keyed by its path from the repo root. It fails when it
// finds no shell file, so the checks never pass on nothing.
func shellLines(t *testing.T) map[string][]string {
	t.Helper()
	files := map[string][]string{}
	err := filepath.WalkDir(packRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := filepath.Ext(d.Name())
		if ext == d.Name() {
			ext = ""
		}
		if ext != ".sh" && ext != "" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		if ext == "" {
			first, _, _ := strings.Cut(text, "\n")
			if !shellShebang.MatchString(first) {
				return nil
			}
		}
		rel, err := filepath.Rel(filepath.Dir(packRoot), path)
		if err != nil {
			return err
		}
		var lines []string
		sc := bufio.NewScanner(strings.NewReader(text))
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		if err := sc.Err(); err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = lines
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no shell file under pack/")
	}
	return files
}

// portableHits reports every line of pack/ shell whose code matches a rule. A line with a
// portable marker is skipped; a trailing comment and a `command -v` probe are not code.
func portableHits(t *testing.T, rules []portableRule) []string {
	t.Helper()
	var hits []string
	for path, lines := range shellLines(t) {
		for i, line := range lines {
			if portableMarker.MatchString(line) {
				continue
			}
			code := commandProbe.ReplaceAllString(trailingComment.ReplaceAllString(line, ""), "")
			for _, r := range rules {
				if r.re.MatchString(code) {
					hits = append(hits, fmt.Sprintf("%s:%d: %s", path, i+1, r.reason))
				}
			}
		}
	}
	sort.Strings(hits)
	return hits
}

// Classification: fails when shell under pack/ uses a GNU-only construct (sed -i without a
// suffix, date -d, sha256sum, ...) on a line without a `# portable:` marker.
func TestPackShellHasNoGNUOnlyConstructs(t *testing.T) {
	if hits := portableHits(t, gnuOnly); len(hits) > 0 {
		t.Errorf("GNU-only constructs:\n%s", strings.Join(hits, "\n"))
	}
}

// Classification: fails when shell under pack/ uses a bash 4 construct (declare -A, mapfile,
// ${x,,}, &>>, |&, local -n) on a line without a `# portable:` marker.
func TestPackShellHasNoBash4Constructs(t *testing.T) {
	if hits := portableHits(t, bash4); len(hits) > 0 {
		t.Errorf("bash 4 constructs:\n%s", strings.Join(hits, "\n"))
	}
}

// Classification: fails when a `# portable:` marker in pack/ shell carries no reason.
func TestPortableMarkerNeedsAReason(t *testing.T) {
	var bare []string
	for path, lines := range shellLines(t) {
		for i, line := range lines {
			if m := portableMarker.FindStringSubmatch(line); m != nil && strings.TrimSpace(m[1]) == "" {
				bare = append(bare, fmt.Sprintf("%s:%d", path, i+1))
			}
		}
	}
	sort.Strings(bare)
	if len(bare) > 0 {
		t.Errorf("`# portable:` without a reason: %s", strings.Join(bare, ", "))
	}
}
