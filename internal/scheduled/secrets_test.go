package scheduled

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// Classification: io/filesystem. Fails if a pattern of the paia set stops matching (or starts
// matching a near miss), if the line number or kind in a finding changes, if a finding echoes the
// secret, if a deleted file is reported, or if an unreadable file passes silently.
func TestScanSecrets(t *testing.T) {
	// Fake values with the shape of each credential; none is a real secret.
	secrets := map[string]string{
		"sk.txt":     "key = sk-abcdefghijklmnopqrstuvwxyz0123",
		"live.txt":   "sk_live_abc123",
		"test.txt":   "sk_test_abc123",
		"ghp.txt":    "token: ghp_" + strings.Repeat("a", 36),
		"gho.txt":    "token: gho_" + strings.Repeat("B", 36),
		"glpat.txt":  "glpat-abc_DEF-123",
		"aws.txt":    "AKIAABCDEFGHIJKLMNOP",
		"slack.txt":  "xoxb-1234-abcd",
		"pem.txt":    "-----BEGIN OPENSSH PRIVATE KEY-----",
		"jwt.txt":    "eyJhbGciOi.eyJzdWIiOi.sig",
		"second.txt": "harmless\nalso harmless\nsk_live_zz",
		"ant.txt":    "ANTHROPIC_API_KEY=sk-ant-api03-abcdefghij_KLMNOPQRST",
		"pat.txt":    "github_pat_11ABCDEFG0123456789_abcdef",
		"ghs.txt":    "ghs_" + strings.Repeat("c", 36),
		"clean.txt":  "sk-ant-short github_pat_short ghs_" + strings.Repeat("c", 35) + " sk-tooshort ghp_" + strings.Repeat("a", 35) + " AKIAlower000000000000 xoxq-1",
	}
	dir := t.TempDir()
	var files []string
	for name, body := range secrets {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, name)
	}
	files = append(files, "deleted.txt")

	got := map[string]bool{}
	for _, f := range ScanSecrets(dir, files) {
		got[f] = true
	}
	want := map[string]bool{
		`"sk.txt":1: secret key (sk-)`:           true,
		`"live.txt":1: Stripe live key`:          true,
		`"test.txt":1: Stripe test key`:          true,
		`"ghp.txt":1: GitHub personal token`:     true,
		`"gho.txt":1: GitHub OAuth token`:        true,
		`"glpat.txt":1: GitLab token`:            true,
		`"aws.txt":1: AWS access key`:            true,
		`"slack.txt":1: Slack token`:             true,
		`"pem.txt":1: private key`:               true,
		`"jwt.txt":1: JSON web token`:            true,
		`"second.txt":3: Stripe live key`:        true,
		`"ant.txt":1: Anthropic key`:             true,
		`"pat.txt":1: GitHub fine-grained token`: true,
		`"ghs.txt":1: GitHub token`:              true,
	}
	// Exact equality also proves no finding echoes a secret value.
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ScanSecrets =\n%v\nwant\n%v", got, want)
	}
}

// Classification: io/filesystem. Fails if a symlink's committed content (its target) is not
// scanned, or if a file that cannot be read passes the scan silently.
func TestScanSecretsSymlinkAndUnreadable(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("sk_live_inthetarget", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "adir"), 0o700); err != nil {
		t.Fatal(err)
	}
	got := ScanSecrets(dir, []string{"link", "adir"})
	want := []string{`"link":1: Stripe live key`, `"adir": cannot read`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ScanSecrets = %q, want %q", got, want)
	}
}

// Classification: pure-logic. Fails if a token in a file name is not reported, or if a finding
// echoes such a name (in the name finding or in a content finding of that file).
func TestScanSecretsFileNames(t *testing.T) {
	dir := t.TempDir()
	tokenName := "out/ghp_" + strings.Repeat("a", 36) + ".md"
	if err := os.MkdirAll(filepath.Join(dir, "out"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, tokenName), []byte("x\nsk_live_abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ScanSecrets(dir, []string{tokenName, "out/gho_" + strings.Repeat("b", 36) + ".md"})
	want := []string{
		"file name contains a GitHub personal token",
		"(file name withheld: GitHub personal token):2: Stripe live key",
		"file name contains a GitHub OAuth token",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ScanSecrets =\n%q\nwant\n%q", got, want)
	}
}

// Classification: pure-logic. Fails if a path with a newline reaches a finding unquoted.
func TestScanSecretsQuotesANewlineFileName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("NTFS refuses a newline in a file name; see docs/host-assumptions.md")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "out"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "out/a\nb.md"), []byte("sk_live_abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ScanSecrets(dir, []string{"out/a\nb.md"})
	want := []string{`"out/a\nb.md":1: Stripe live key`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ScanSecrets =\n%q\nwant\n%q", got, want)
	}
}
