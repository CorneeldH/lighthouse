package commitgate

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	kibPerGiB    = 1024 * 1024
	noSwapNote   = "no swap on this host, SwapFree not checked"
	memInterval  = time.Second
	memAvailable = "MemAvailable"
)

// Thresholds are the minimums in GiB; nil means not checked.
type Thresholds struct{ MinAvailableGiB, MinSwapFreeGiB *float64 }

// splitLines splits like Python str.splitlines for the separators a meminfo can hold.
func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(text, "\n")
}

// ReadMeminfo returns the fields of a meminfo file in kB. A file without MemAvailable is an error.
func ReadMeminfo(path string) (map[string]int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	info := map[string]int64{}
	for _, line := range splitLines(string(data)) {
		name, rest, found := strings.Cut(line, ":")
		fields := strings.Fields(rest)
		if !found || len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		info[strings.TrimSpace(name)] = v
	}
	if _, ok := info[memAvailable]; !ok {
		return nil, fmt.Errorf("%s: no MemAvailable: line", path)
	}
	return info, nil
}

// pyG formats like Python's {:g}: six significant digits, no trailing zeros.
func pyG(v float64) string { return strconv.FormatFloat(v, 'g', 6, 64) }

func shortfall(name string, valueKB int64, minimum float64) (string, bool) {
	gib := float64(valueKB) / kibPerGiB
	if gib >= minimum {
		return "", false
	}
	return fmt.Sprintf("%s %.1f GiB < %s GiB", name, gib, pyG(minimum)), true
}

// Shortfalls measures one reading against the thresholds that are set.
func Shortfalls(info map[string]int64, t Thresholds) (found, notes []string) {
	if t.MinAvailableGiB != nil {
		if line, short := shortfall(memAvailable, info[memAvailable], *t.MinAvailableGiB); short {
			found = append(found, line)
		}
	}
	if t.MinSwapFreeGiB != nil {
		if info["SwapTotal"] == 0 {
			notes = append(notes, noSwapNote)
		} else if line, short := shortfall("SwapFree", info["SwapFree"], *t.MinSwapFreeGiB); short {
			found = append(found, line)
		}
	}
	return found, notes
}

// WaitMemory measures at least once, then every second until start+wait, never sleeping past it.
// below is non-empty when the host is still short after wait; err means not measurable.
func WaitMemory(path string, t Thresholds, wait time.Duration, sleep func(time.Duration), now func() time.Time) (notes []string, below string, err error) {
	deadline := now().Add(wait)
	for {
		info, rerr := ReadMeminfo(path)
		if rerr != nil {
			return nil, "", fmt.Errorf("%s: %w", path, rerr)
		}
		found, ns := Shortfalls(info, t)
		if len(found) == 0 {
			return ns, "", nil
		}
		remaining := deadline.Sub(now())
		if remaining <= 0 {
			return nil, strings.Join(found, "; "), nil
		}
		sleep(min(memInterval, remaining))
	}
}
