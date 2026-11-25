package history

import (
	"encoding/json"
	"strings"
	"testing"
)

func shellFinding(recs ...Record) Finding {
	d := newShell()
	feed(d, recs...)
	return d.Finish(Env{})
}

func TestShell_Platform(t *testing.T) {
	cases := []struct {
		name       string
		recs       []Record
		platform   string
		confidence string
	}{
		{"windows with powershell", []Record{bashRec("s", "b", `C:\Users\u\r`, "powershell -c ls")}, "windows", "high"},
		{"windows with cmd", []Record{bashRec("s", "b", `d:\r`, "cmd /c dir")}, "windows", "high"},
		{"windows path only", []Record{bashRec("s", "b", `C:\Users\u\r`, "ls")}, "windows", "low"},
		{"msys path only", []Record{bashRec("s", "b", `/c/Users/u/r`, "ls")}, "windows", "low"},
		{"wsl path only", []Record{bashRec("s", "b", "/mnt/c/Users/u/r", "ls")}, "wsl", "low"},
		{"wsl path with wsl command", []Record{bashRec("s", "b", "/mnt/c/r", "wsl -d Ubuntu ls")}, "wsl", "high"},
		{"linux home", []Record{bashRec("s", "b", "/home/u/r", "ls")}, "linux", "low"},
		{"nothing", []Record{{Type: "user"}}, "unknown", "low"},
	}
	for _, c := range cases {
		f := shellFinding(c.recs...)
		if f.Key != "shell" || f.Measured["platform"] != c.platform || f.Confidence != c.confidence {
			t.Errorf("%s: platform %v confidence %s", c.name, f.Measured["platform"], f.Confidence)
		}
	}
}

func TestShell_Measured(t *testing.T) {
	f := shellFinding(
		Record{Type: "user", Entrypoint: "cli", Cwd: `C:\Users\secret\r`},
		Record{Type: "user", Entrypoint: "cli", Cwd: "/c/Users/secret/r"},
		Record{Type: "user", Entrypoint: "sdk-cli", Cwd: "/mnt/c/Users/secret/r"},
		Record{Type: "user", Cwd: "/home/secret/r"},
		bashRec("s", "b", "/home/secret/r", "powershell -c x; cmd /c y; wsl ls"),
	)
	ep := f.Measured["entrypoints"].(map[string]int)
	if ep["cli"] != 2 || ep["sdk-cli"] != 1 {
		t.Errorf("entrypoints %v", ep)
	}
	cf := f.Measured["cwd_forms"].(map[string]int)
	if cf["windows_drive"] != 1 || cf["msys"] != 1 || cf["wsl_mnt"] != 1 || cf["posix_home"] != 2 {
		t.Errorf("cwd_forms %v", cf)
	}
	p := f.Measured["patterns"].(map[string]int)
	if p["powershell"] != 1 || p["cmd_c"] != 1 || p["wsl"] != 1 {
		t.Errorf("patterns %v", p)
	}
	if f.Status != "found" {
		t.Errorf("status %s", f.Status)
	}
}

func TestShell_NoLeak(t *testing.T) {
	f := shellFinding(bashRec("s", "b", `C:\Users\secret\r`, "powershell -c x"))
	if s := strings.ToLower(toJSON(t, f)); strings.Contains(s, "secret") || strings.Contains(s, `users`) {
		t.Errorf("leak: %s", s)
	}
}

func TestShell_CRLF(t *testing.T) {
	for _, cmd := range []string{"dos2unix a.sh", "git config core.autocrlf true", "git config --global core.eol lf", "git config user.eol x", "printf 'a\r\n'"} {
		f := shellFinding(bashRec("s", "b", "/home/u/r", cmd))
		if f.Measured["crlf_signals"] != 1 {
			t.Errorf("%q: %v", cmd, f.Measured["crlf_signals"])
		}
		if f.Proposal.Kind != "advice" || !strings.Contains(f.Proposal.Text, "* text=auto eol=lf") {
			t.Errorf("%q: proposal %+v", cmd, f.Proposal)
		}
	}
	for _, cmd := range []string{"git config user.name x", "ls"} {
		f := shellFinding(bashRec("s", "b", "/home/u/r", cmd))
		if f.Measured["crlf_signals"] != 0 || strings.Contains(f.Proposal.Text, ".gitattributes") {
			t.Errorf("%q: %+v", cmd, f)
		}
	}
}

func TestShell_NoneWithoutSignals(t *testing.T) {
	f := shellFinding(Record{Type: "user"})
	if f.Status != "none" {
		t.Errorf("status %s", f.Status)
	}
}

func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestShell_SourcesOnlyForCommandSignals(t *testing.T) {
	f := shellFinding(
		bashRec("s", "b", `C:\Users\u\r`, "ls"),
		bashRec("s", "b", `C:\Users\u\r`, "ls"),
	)
	if len(f.Sources) != 0 {
		t.Errorf("cwd-only records must carry no sources: %+v", f.Sources)
	}
	f = shellFinding(
		bashRec("s", "b", `C:\Users\u\r`, "ls"),
		bashRec("s", "b", `C:\Users\u\r`, "powershell -c ls"),
		bashRec("s", "b", `C:\Users\u\r`, "dos2unix x"),
	)
	if len(f.Sources) == 0 {
		t.Error("command signals must carry sources")
	}
}
