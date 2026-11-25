package history

import "regexp"

var (
	powershellRe = regexp.MustCompile(`(?i)\b(?:powershell|pwsh)(?:\.exe)?\b`)
	cmdCRe       = regexp.MustCompile(`(?i)\bcmd(?:\.exe)?\s+/c\b`)
	wslRe        = regexp.MustCompile(`(?i)(?:^|[;&|]\s*)wsl(?:\.exe)?(?:\s|$)`)
	crlfRe       = regexp.MustCompile(`(?i)\bdos2unix\b|\bautocrlf\b|\bgit\s+config\b[^\n]*\beol\b|\r|\\r\\n`)
)

// shellForm classifies a cwd into a fixed form name; "" for anything else, so no part of
// the path itself reaches the report.
func shellForm(cwd string) string {
	switch {
	case len(cwd) >= 3 && cwd[1] == ':' && (cwd[2] == '\\' || cwd[2] == '/') && isLetter(cwd[0]):
		return "windows_drive"
	case hasDrivePrefix(cwd, "/mnt/"):
		return "wsl_mnt"
	case hasDrivePrefix(cwd, "/"):
		return "msys"
	case len(cwd) >= 6 && cwd[:6] == "/home/":
		return "posix_home"
	}
	return ""
}

func isLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// hasDrivePrefix reports whether p is prefix, one drive letter, then / or the end.
func hasDrivePrefix(p, prefix string) bool {
	n := len(prefix)
	if len(p) < n+1 || p[:n] != prefix || !isLetter(p[n]) {
		return false
	}
	return len(p) == n+1 || p[n+1] == '/'
}

// shell derives the platform (native Windows, WSL or Linux) from the entrypoint, the
// form of the cwd and shell command patterns. It keeps only counts and fixed names.
type shell struct {
	entrypoints map[string]int
	forms       map[string]int
	patterns    map[string]int
	crlf        int
	hits        Hits
}

func newShell() Detector {
	return &shell{entrypoints: map[string]int{}, forms: map[string]int{}, patterns: map[string]int{}}
}

func (d *shell) Add(r Record) {
	hit := false
	if r.Entrypoint != "" {
		d.entrypoints[r.Entrypoint]++
	}
	if f := shellForm(r.Cwd); f != "" {
		d.forms[f]++
	}
	for _, c := range r.Commands {
		if powershellRe.MatchString(c) {
			d.patterns["powershell"]++
			hit = true
		}
		if cmdCRe.MatchString(c) {
			d.patterns["cmd_c"]++
			hit = true
		}
		if wslRe.MatchString(c) {
			d.patterns["wsl"]++
			hit = true
		}
		if crlfRe.MatchString(c) {
			d.crlf++
			hit = true
		}
	}
	if hit {
		d.hits.Add(r.File, r.Line, r.Timestamp)
	}
}

// platform picks the platform with the most cwd records (windows, wsl, linux on a tie)
// and says whether a command pattern backs it.
func (d *shell) platform() (platform string, backed bool) {
	win := d.forms["windows_drive"] + d.forms["msys"]
	wsl := d.forms["wsl_mnt"]
	lin := d.forms["posix_home"]
	switch {
	case win == 0 && wsl == 0 && lin == 0:
		switch {
		case d.patterns["powershell"]+d.patterns["cmd_c"] > 0:
			return "windows", true
		case d.patterns["wsl"] > 0:
			return "wsl", true
		}
		return "unknown", false
	case win >= wsl && win >= lin:
		platform = "windows"
		backed = d.patterns["powershell"]+d.patterns["cmd_c"] > 0
	case wsl >= lin:
		platform = "wsl"
		backed = d.patterns["wsl"] > 0
	default:
		platform = "linux"
	}
	return platform, backed
}

func (d *shell) Finish(Env) Finding {
	platform, backed := d.platform()
	f := Finding{
		Key:        "shell",
		Status:     "none",
		Confidence: "low",
		Measured: map[string]any{
			"entrypoints":  d.entrypoints,
			"cwd_forms":    d.forms,
			"patterns":     d.patterns,
			"crlf_signals": d.crlf,
			"platform":     platform,
		},
		Meaning: "Shows whether Claude Code runs on native Windows, WSL or Linux, which decides the install block of the guide.",
		Proposal: Proposal{Kind: "advice",
			Text: "Follow the install block of the guide for this platform."},
	}
	if backed {
		f.Confidence = "high"
	}
	if platform != "unknown" || d.crlf > 0 {
		f.Status = "found"
		f.Sources = d.hits.Top()
	}
	if d.crlf > 0 {
		f.Proposal.Text = "Line endings caused trouble; add a .gitattributes with `* text=auto eol=lf`."
	}
	return f
}
