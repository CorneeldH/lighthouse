package scheduled

import (
	"os"
	"path/filepath"
	"reflect"
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
		"clean.txt":  "sk-tooshort ghp_" + strings.Repeat("a", 35) + " AKIAlower000000000000 xoxq-1",
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
		"sk.txt:1: secret key (sk-)":       true,
		"live.txt:1: Stripe live key":      true,
		"test.txt:1: Stripe test key":      true,
		"ghp.txt:1: GitHub personal token": true,
		"gho.txt:1: GitHub OAuth token":    true,
		"glpat.txt:1: GitLab token":        true,
		"aws.txt:1: AWS access key":        true,
		"slack.txt:1: Slack token":         true,
		"pem.txt:1: private key":           true,
		"jwt.txt:1: JSON web token":        true,
		"second.txt:3: Stripe live key":    true,
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
	want := []string{"link:1: Stripe live key", "adir: cannot read"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ScanSecrets = %q, want %q", got, want)
	}
}
