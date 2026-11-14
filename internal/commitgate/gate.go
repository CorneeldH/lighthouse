package commitgate

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/deadline"
)

// MaxDeadline is the hook timeout in hooks.json (300) minus the grace of the run (10) minus a
// margin: above it the hook timeout fires first, and that is fail-open.
const MaxDeadline = 280

// slotInterval is the wait between two rounds over the slots.
const slotInterval = time.Second

// selectorLine is a summary line of a selector: `gate-<name>:` or `gate:`.
var selectorLine = regexp.MustCompile(`^gate(-[A-Za-z0-9_.-]+)?:`)

// Row is one root to test. An empty Base is a row of pre, without LIGHTHOUSE_GATE_BASE, _HEAD and
// _FILES.
type Row struct{ Root, Base, Head string }

// Gate is one run of the gate: the configuration, the slot, the memory check, the brake, then
// the test command per root.
type Gate struct {
	// Mode is "pre" or "post".
	Mode string
	// Root is the repository root of the configuration.
	Root string
	// Env is the environment of the children, without LIGHTHOUSE_REPO_ROOT; the gate also reads
	// its test seams (LIGHTHOUSE_GATE_SLOT_DIR and the like) from it.
	Env []string
	// Bash runs the test command as Bash -c <command>.
	Bash string
	// Now is the clock (default time.Now).
	Now func() time.Time
	// LookPath finds systemctl, systemd-run and choom (default exec.LookPath).
	LookPath func(string) (string, error)
	// Run runs the test command until the deadline (default deadline.RunCapture).
	Run func(deadline time.Time, argv []string, o deadline.Options) (int, []byte, error)
	// Trace, when set, gets one line "deadline" when the deadline is set (LIGHTHOUSE_GATE_TRACE).
	Trace string
	// Start, when set, replaces the clock in the deadline (LIGHTHOUSE_GATE_START).
	Start *int64

	msgs []string
	// rows are the rows of this call, and done how many of them passed: post_untested names the
	// rest.
	rows []Row
	done int
	// command is [test_gate] command, once read.
	command string
	// showRun runs `systemctl --user show` (default runCommand).
	showRun func(argv, env []string) (string, string, error)
	// acquire takes a slot (default Acquire).
	acquire func(files []*os.File, wait, interval time.Duration) (int, error)
}

// settings are the [test_gate] and [host] values of one run.
type settings struct {
	hint                              string
	deadlineSeconds, slots, hostSlots int
	slotWait                          int
	unchecked                         map[string]bool
	slice                             string
	memory                            Thresholds
	memorySet                         bool
}

func (g *Gate) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g *Gate) lookPath(name string) (string, error) {
	if g.LookPath != nil {
		return g.LookPath(name)
	}
	return exec.LookPath(name)
}

func (g *Gate) run(dl time.Time, argv []string, o deadline.Options) (int, []byte, error) {
	if g.Run != nil {
		return g.Run(dl, argv, o)
	}
	return deadline.RunCapture(dl, argv, o)
}

// getenv is the last value of key in env, as exec gives it to a child.
func getenv(env []string, key string) string {
	value := ""
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			value = v
		}
	}
	return value
}

// withoutKeys is env without the given keys.
func withoutKeys(env []string, keys ...string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, key := range keys {
			drop = drop || k == key
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// setenv is env with key set to value, replacing any earlier value.
func setenv(env []string, key, value string) []string {
	return append(withoutKeys(env, key), key+"="+value)
}

// captured is text the way `$(…)` leaves it: no NUL bytes, no trailing newlines.
func captured(b []byte) string {
	return strings.TrimRight(string(bytes.ReplaceAll(b, []byte{0}, nil)), "\n")
}

// readConfig is one key from lighthouse.toml or host.toml: set is false when it is not set. A
// configuration that cannot be read gives a block.
func (g *Gate) readConfig(key string) (value string, set bool, block *Result) {
	var out, errOut strings.Builder
	switch config.Run([]string{key}, &out, &errOut) {
	case 0:
		return captured([]byte(out.String())), true, nil
	case 1:
		return "", false, nil
	}
	res := g.Block("the configuration cannot be read (" + key + "): " + captured([]byte(errOut.String())))
	return "", false, &res
}

// readInt is readConfig for a whole number; the config reader checked the type.
func (g *Gate) readInt(key string, into *int) *Result {
	value, set, block := g.readConfig(key)
	if block != nil || !set {
		return block
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		res := g.Block("the configuration cannot be read (" + key + "): not a whole number: '" + value + "'")
		return &res
	}
	*into = n
	return nil
}

// readGiB is readConfig for a memory threshold.
func (g *Gate) readGiB(key string, into **float64) *Result {
	value, set, block := g.readConfig(key)
	if block != nil || !set {
		return block
	}
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		res := g.Block("the configuration cannot be read (" + key + "): not a number: '" + value + "'")
		return &res
	}
	*into = &v
	return nil
}

// readSettings reads every key after test_gate.command, in the order of the bash hook, and
// checks the ranges.
func (g *Gate) readSettings() (s settings, block *Result) {
	// The same path as config.Path(): LIGHTHOUSE_TOML of this process, else the root's.
	configFile := os.Getenv("LIGHTHOUSE_TOML")
	if configFile == "" {
		configFile = g.Root + "/lighthouse.toml"
	}
	s.hint = "The command is [test_gate] command in " + configFile + ": " + g.command + ". Set command = \"\" there to turn the test gate off."
	s.hostSlots = MaxSlots
	for _, read := range []struct {
		key  string
		into *int
	}{
		{"test_gate.deadline_seconds", &s.deadlineSeconds},
		{"test_gate.slots", &s.slots},
		{"host.gate_slots", &s.hostSlots},
		{"test_gate.slot_wait_seconds", &s.slotWait},
	} {
		if block := g.readInt(read.key, read.into); block != nil {
			return s, block
		}
	}
	codes, _, block := g.readConfig("test_gate.unchecked_exit_codes")
	if block != nil {
		return s, block
	}
	s.unchecked = map[string]bool{}
	for _, code := range strings.Fields(codes) {
		s.unchecked[code] = true
	}
	if s.slice, _, block = g.readConfig("host.gate_slice"); block != nil {
		return s, block
	}
	if block := g.readGiB("host.gate_min_mem_available_gb", &s.memory.MinAvailableGiB); block != nil {
		return s, block
	}
	if block := g.readGiB("host.gate_min_swap_free_gb", &s.memory.MinSwapFreeGiB); block != nil {
		return s, block
	}
	s.memorySet = s.memory.MinAvailableGiB != nil || s.memory.MinSwapFreeGiB != nil
	return s, g.checkRanges(&s)
}

// checkRanges: the config reader checked the types; the ranges are this hook's.
func (g *Gate) checkRanges(s *settings) *Result {
	var reason string
	switch {
	case s.deadlineSeconds < 1 || s.deadlineSeconds > MaxDeadline:
		reason = "test_gate.deadline_seconds = " + strconv.Itoa(s.deadlineSeconds) + "; it must be between 1 and " + strconv.Itoa(MaxDeadline) + " (hook timeout 300 s)."
	case s.slots < 1 || s.slots > MaxSlots:
		reason = "test_gate.slots = " + strconv.Itoa(s.slots) + "; it must be between 1 and " + strconv.Itoa(MaxSlots) + "."
	case s.hostSlots < 1 || s.hostSlots > MaxSlots:
		reason = "host.gate_slots = " + strconv.Itoa(s.hostSlots) + " in host.toml; it must be between 1 and " + strconv.Itoa(MaxSlots) + "."
	}
	if reason != "" {
		res := g.Block(reason)
		return &res
	}
	// Not a block: the project cannot know the host it lands on.
	if s.slots > s.hostSlots {
		g.Note(HookName + ": test_gate.slots = " + strconv.Itoa(s.slots) + " above host gate_slots = " + strconv.Itoa(s.hostSlots) + ", the gate uses " + strconv.Itoa(s.hostSlots))
		s.slots = s.hostSlots
	}
	if s.slotWait < 0 {
		res := g.Block("test_gate.slot_wait_seconds = " + strconv.Itoa(s.slotWait) + "; it must not be negative.")
		return &res
	}
	return nil
}

// setDeadline is the one deadline of the whole loop, set before the slot: the wait counts towards
// it. Whole seconds, like `date +%s`.
func (g *Gate) setDeadline(seconds int) time.Time {
	var start int64
	if g.Start != nil {
		start = *g.Start
	} else {
		start = g.now().Unix()
	}
	if g.Trace != "" {
		if f, err := os.OpenFile(g.Trace, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600); err == nil {
			_, _ = f.WriteString("deadline\n")
			_ = f.Close()
		}
	}
	return time.Unix(start+int64(seconds), 0)
}

// runUserDir is /run/user/$UID when it exists.
func runUserDir() (string, bool) {
	dir := "/run/user/" + strconv.Itoa(os.Getuid())
	info, err := os.Stat(dir)
	return dir, err == nil && info.IsDir()
}

// takeSlot takes one of s.slots slots. The slot stays taken as long as the returned file is open;
// the files of the other slots are closed. The test command does not inherit it.
func (g *Gate) takeSlot(s *settings) (*os.File, *Result) {
	_, runUser := runUserDir()
	dir := slotDir(getenv(g.Env, "LIGHTHOUSE_GATE_SLOT_DIR"), os.Getuid(), runUser)
	files, err := OpenSlots(dir, s.slots)
	if err != nil {
		res := g.slotFailed(err.Error())
		return nil, &res
	}
	acquire := g.acquire
	if acquire == nil {
		acquire = Acquire
	}
	slot, err := acquire(files, time.Duration(s.slotWait)*time.Second, slotInterval)
	if err == nil {
		return files[slot-1], nil
	}
	for _, f := range files {
		_ = f.Close()
	}
	if !errors.Is(err, ErrSlotsBusy) {
		res := g.slotFailed(err.Error())
		return nil, &res
	}
	var b strings.Builder
	g.pending(&b)
	b.WriteString(HookName + ": COULD NOT CHECK — all gate slots busy after " + strconv.Itoa(s.slotWait) + " s (slots: " + strconv.Itoa(s.slots) + ")\n\n")
	if g.Mode == "post" {
		b.WriteString("Other gates are running already. Not being able to test is not a green light.\n")
		g.postUntested(&b)
	} else {
		b.WriteString("Other gates are running already. Try the commit again shortly.\n")
		b.WriteString("Not being able to test is not a green light.\n")
	}
	res := blockResult(&b)
	return nil, &res
}

// checkMemory waits, after the slot, until the host has the memory of the thresholds: a gate
// that waits for memory keeps its slot, so seven gates do not slip through the same opening. The
// wait counts towards the deadline.
func (g *Gate) checkMemory(s *settings, dl time.Time) *Result {
	if !s.memorySet {
		g.Note(HookName + ": no gate memory threshold in host.toml, host memory not checked")
		return nil
	}
	wait := min(dl.Unix()-g.now().Unix(), int64(s.slotWait))
	wait = max(wait, 0)
	meminfo := getenv(g.Env, "LIGHTHOUSE_GATE_MEMINFO")
	if meminfo == "" {
		meminfo = "/proc/meminfo"
	}
	notes, below, err := WaitMemory(meminfo, s.memory, time.Duration(wait)*time.Second, time.Sleep, time.Now)
	var b strings.Builder
	switch {
	case err != nil:
		g.pending(&b)
		b.WriteString(HookName + ": COULD NOT CHECK — host memory not measurable: " + err.Error() + "\n\n")
		b.WriteString("Not being able to test is not a green light.\n")
		g.postUntested(&b)
	case below != "":
		g.pending(&b)
		b.WriteString(HookName + ": COULD NOT CHECK — host memory below threshold after " + strconv.FormatInt(wait, 10) + " s (" + below + ")\n\n")
		if g.Mode == "post" {
			b.WriteString("The host has too little free memory for a gate run. Not being able to test is not a green light.\n")
			g.postUntested(&b)
		} else {
			b.WriteString("The host has too little free memory for a gate run. Try the commit again shortly.\n")
			b.WriteString("Not being able to test is not a green light.\n")
		}
	default:
		for _, note := range notes {
			if note != "" {
				g.Note(HookName + ": " + note)
			}
		}
		return nil
	}
	res := blockResult(&b)
	return &res
}

// RunRoots is the gate on rows, answering hook event event. It ends the gate: exit 2 on a red or
// unchecked run, otherwise Emit.
func (g *Gate) RunRoots(rows []Row, event string) Result {
	g.rows = rows
	failLine := HookName + ": tests failed. Fix the failing tests before committing."
	if g.Mode == "post" {
		failLine = HookName + ": tests failed on the commit you just made. Fix them in a new commit."
	}
	// Not inherited from an outer gate run: without a row of its own the command tests everything.
	env := withoutKeys(g.Env, "LIGHTHOUSE_GATE_BASE", "LIGHTHOUSE_GATE_HEAD", "LIGHTHOUSE_GATE_FILES")

	// Not set, or `command = ""` (the opt-out): the same note, nothing was tested.
	command, set, block := g.readConfig("test_gate.command")
	if block != nil {
		return *block
	}
	if !set || command == "" {
		g.Note(HookName + ": no test command configured, nothing was tested")
		return g.Emit(event)
	}
	g.command = command
	s, block := g.readSettings()
	if block != nil {
		return *block
	}

	dl := g.setDeadline(s.deadlineSeconds)
	// So the slot wait never runs past the deadline either.
	s.slotWait = min(s.slotWait, s.deadlineSeconds)

	slot, block := g.takeSlot(&s)
	if block != nil {
		return *block
	}
	defer func() { _ = slot.Close() }()

	if block := g.checkMemory(&s, dl); block != nil {
		return *block
	}

	bt, err := os.MkdirTemp("", "tmp.")
	if err != nil {
		var b strings.Builder
		g.pending(&b)
		b.WriteString(HookName + ": COULD NOT CHECK — no gate tmp dir: " + err.Error() + "\n\n")
		b.WriteString("Not being able to test is not a green light.\n")
		g.postUntested(&b)
		return blockResult(&b)
	}
	defer func() { _ = os.RemoveAll(bt) }()

	// VS Code runs claude without XDG_RUNTIME_DIR and sometimes without a bus address; then
	// `systemctl --user` fails and the brake drops out silently on exactly the path that needs it.
	if dir, ok := runUserDir(); ok && getenv(env, "XDG_RUNTIME_DIR") == "" && getenv(env, "DBUS_SESSION_BUS_ADDRESS") == "" {
		env = setenv(env, "XDG_RUNTIME_DIR", dir)
	}

	show := g.showRun
	if show == nil {
		show = runCommand
	}
	brake := DetectBrake(s.slice, env, g.lookPath, show)
	if s.slice == "" {
		g.Note(HookName + ": no gate_slice in host.toml, tests run without memory brake")
	} else if brake.Reason != "" {
		g.Note(HookName + ": memory brake not available: " + brake.Reason)
	}

	n := 0
	for _, row := range rows {
		if row.Root == "" {
			continue
		}
		n++
		if res, stop := g.runRow(row, n, bt, env, dl, &s, brake, failLine); stop {
			return res
		}
		g.done++
	}
	return g.Emit(event)
}

// runRow runs the command on one root, run number n; stop is true when the gate ends there.
func (g *Gate) runRow(row Row, n int, bt string, env []string, dl time.Time, s *settings, brake Brake, failLine string) (Result, bool) {
	// An empty LIGHTHOUSE_GATE_TMP per run.
	gateTmp := filepath.Join(bt, "run-"+strconv.Itoa(n))
	if err := os.Mkdir(gateTmp, 0o777); err != nil {
		return g.Block("no gate tmp dir for " + row.Root + ": " + err.Error()), true
	}
	env = setenv(env, "LIGHTHOUSE_GATE_TMP", gateTmp)
	// The files of the commit, beside GATE_TMP and not in it: `pytest --basetemp` empties that
	// directory, and a selector later in the command must still read the list.
	if row.Base != "" {
		if row.Head == "" {
			return g.Block("the recorded row for " + row.Root + " has no new HEAD."), true
		}
		gateFiles := gateTmp + ".files"
		if err := writeChangedFiles(row, gateFiles); err != nil {
			return g.Block("could not list the files of the commit in " + row.Root + ": " + err.Error()), true
		}
		env = setenv(env, "LIGHTHOUSE_GATE_BASE", row.Base)
		env = setenv(env, "LIGHTHOUSE_GATE_HEAD", row.Head)
		env = setenv(env, "LIGHTHOUSE_GATE_FILES", gateFiles)
	}

	var oomBefore, highBefore int64
	if brake.Reason == "" {
		oomBefore, highBefore = eventCount(brake.Events, "oom_kill"), eventCount(brake.Events, "high")
	}
	argv := append(append([]string{}, brake.Wrap...), g.Bash, "-c", g.command)
	status, out, err := g.run(dl, argv, deadline.Options{Dir: row.Root, Env: env})
	var interrupted *deadline.Interrupted
	if errors.As(err, &interrupted) {
		// In bash the hook died of the signal here, without output.
		sig, _ := interrupted.Signal.(syscall.Signal)
		return Result{Code: 128 + int(sig)}, true
	}
	if err != nil {
		return g.Block("the test command could not run in " + row.Root + ": " + err.Error()), true
	}
	var oom, high int64
	if brake.Reason == "" {
		oom = delta(oomBefore, eventCount(brake.Events, "oom_kill"))
		high = delta(highBefore, eventCount(brake.Events, "high"))
	}
	output := captured(out)
	if res := g.verdict(status, output, row.Root, s, brake, oom, high, failLine); res != nil {
		return *res, true
	}

	// The summary lines of the selectors, in order, so a `gate-go: skipped — …` stays visible
	// when more runs after it; without one, the last line that is not blank.
	last, summary := "", false
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimLeft(line, " \t\n\v\f\r") == "" {
			continue
		}
		last = line
		if selectorLine.MatchString(line) {
			g.Note(HookName + ": " + row.Root + " — " + line)
			summary = true
		}
	}
	if !summary {
		g.Note(HookName + ": " + row.Root + " — " + last)
	}
	return Result{}, false
}

func writeChangedFiles(row Row, path string) error {
	files, err := ChangedFiles(row.Root, row.Base, row.Head)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, f := range files {
		b.WriteString(f + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o666); err != nil {
		return errors.New("commit_base: " + err.Error())
	}
	return nil
}

// verdict reads one run; the rows hold from top to bottom and the first hit wins. A green run
// stays green, also with a delta from a neighbouring gate. nil is green.
func (g *Gate) verdict(status int, output, root string, s *settings, brake Brake, oom, high int64, failLine string) *Result {
	if status == 0 {
		return nil
	}
	var b strings.Builder
	head := func(line string) { b.WriteString(HookName + ": COULD NOT CHECK — " + line + "\n") }
	tail := func(lines ...string) *Result {
		b.WriteString("\n")
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
		g.postUntested(&b)
		b.WriteString("\n" + output + "\n")
		res := blockResult(&b)
		return &res
	}
	notTested := "Root: " + root + ". Not being able to test is not a green light."
	switch {
	case oom > 0:
		head("gate throttled (oom-kill in " + s.slice + " during this run).")
		return tail(notTested)
	case brake.Reason == "" && (strings.Contains(output, "Failed to start transient scope unit") ||
		strings.Contains(output, "Failed to connect to bus") ||
		strings.Contains(output, "Failed to create bus connection")):
		head("systemd-run did not start.")
		return tail(notTested)
	case status == deadline.TimeoutExit:
		head("the test command ran past the deadline of " + strconv.Itoa(s.deadlineSeconds) + " s.")
		if high > 0 {
			b.WriteString(s.slice + " was under memory pressure\n")
		}
		return tail("Root: " + root + ". An aborted run is not a green light.")
	case status == deadline.NotFoundExit:
		// The command did not start (usually: a program in it is not on PATH): nothing was
		// tested, which is something else than "tests failed".
		head("the test command could not start (command not found?).")
		return tail(notTested, s.hint)
	case status >= 128:
		head("gate killed by signal " + strconv.Itoa(signalNumber(status)) + " (" + killHint(s.slice, brake.Reason) + ").")
		return tail(notTested)
	case s.unchecked[strconv.Itoa(status)]:
		// A code the project names as "nothing was tested": block, but not as "tests failed",
		// which sends the agent looking for a broken test that is not there.
		head("the test command stopped with exit code " + strconv.Itoa(status) + ", listed in test_gate.unchecked_exit_codes: nothing was tested.")
		return tail(notTested, s.hint)
	}
	b.WriteString(failLine + "\n" + s.hint + "\n\n" + output + "\n")
	res := blockResult(&b)
	return &res
}

// signalNumber: a SIGKILL on a child of bash gives 137, one reported as a negative exit (Python's
// sys.exit(-9)) 247: n = exit - 128, or 256 - exit above 192.
func signalNumber(status int) int {
	if status <= 192 {
		return status - 128
	}
	return 256 - status
}

// killHint: without a brake there is no slice and so no systemd-oomd in it.
func killHint(slice, reason string) string {
	switch {
	case slice == "":
		return reason
	case reason != "":
		return "memory brake not available: " + reason
	}
	return "systemd-oomd in " + slice + "?"
}
