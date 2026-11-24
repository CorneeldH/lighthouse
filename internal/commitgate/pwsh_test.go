package commitgate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/deadline"
)

// Tests of `pwsh -File` as the test command, with the real bash and the real executor. They run
// on windows-latest (windows-measure.yml, `go test ./...`), where pwsh is installed.

// pwshGate runs a pre row with the test command `pwsh -NoProfile -File <script>`, where the
// script has body.
func pwshGate(t *testing.T, body string) Result {
	t.Helper()
	_, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("no pwsh")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	script := filepath.Join(t.TempDir(), "gate.ps1")
	write(t, script, body)
	// bash -c reads backslashes as escapes: the path goes in with slashes, in single quotes.
	command := "pwsh -NoProfile -File '" + filepath.ToSlash(script) + "'"
	h := newHarness(t, "pre", "[test_gate]\ncommand = \""+command+"\"\nslot_wait_seconds = 0\n", "")
	h.g.Bash = bash
	h.g.Run = deadline.RunCapture
	h.g.Now = time.Now
	h.g.Env = append(os.Environ(), "LIGHTHOUSE_GATE_SLOT_DIR="+h.slots)
	return h.g.RunRoots(h.rootRow(), "PreToolUse")
}

func TestPowerShellCommandRuns(t *testing.T) {
	res := pwshGate(t, "exit 0\n")
	if res.Code != 0 {
		t.Fatalf("got %d, want 0\n%s", res.Code, res.Stderr)
	}
}

func TestPowerShellCommandRedBlocks(t *testing.T) {
	res := pwshGate(t, "Write-Output \"FAIL x\"\nexit 1\n")
	if res.Code != 2 {
		t.Fatalf("got %d, want 2\n%s", res.Code, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "tests failed") {
		t.Errorf("stderr lacks \"tests failed\":\n%s", res.Stderr)
	}
}
