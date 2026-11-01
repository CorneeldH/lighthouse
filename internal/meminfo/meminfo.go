// Package meminfo reads available memory from a /proc/meminfo-shaped file.
package meminfo

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const kbPerGiB = 1024 * 1024

// AvailableGiB reads the MemAvailable line of the file at path, in GiB. A
// missing file, a missing line or an unparsable value is an error.
func AvailableGiB(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] != "MemAvailable:" {
			continue
		}
		kb, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0, fmt.Errorf("parse MemAvailable in %s: %w", path, err)
		}
		return kb / kbPerGiB, nil
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("no MemAvailable line in %s", path)
}
