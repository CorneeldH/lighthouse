// Package checkpointscan is the secret scan of the checkpoint branches: it finds the blobs a
// push would send, scans them in batches under a resource ceiling and keeps a ledger of the
// blobs that came out clean.
package checkpointscan

import (
	"errors"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// Constants of the scan, literal from the Python original.
const (
	DefaultMemoryMax = "2G"
	DefaultCPUQuota  = "200%"

	// BatchBytes is the most blob bytes one gitleaks run gets.
	BatchBytes = 64 << 20
	// HookVolumeLimit is the most a pre-push hook scans before it refuses.
	HookVolumeLimit = 256 << 20

	// Test-only overrides of BatchBytes and HookVolumeLimit.
	BatchBytesEnv  = "LIGHTHOUSE_SCAN_BATCH_BYTES"
	VolumeLimitEnv = "LIGHTHOUSE_SCAN_VOLUME_LIMIT"

	// ScanMethod is part of the ledger key: a change of method starts a new ledger.
	ScanMethod = "dir in blobdir, newline prefix, byte count"
)

// CouldNotCheck means the check cannot be done: the command says `COULD NOT CHECK — <Msg>`
// and exits 2. Library code returns it and never exits, so a push can still report what it did.
type CouldNotCheck struct{ Msg string }

func (e CouldNotCheck) Error() string { return e.Msg }

// Limits are the resource limits of one scan; Slice is nil when host.toml sets no gate slice.
type Limits struct {
	MemoryMax, CPUQuota string
	Slice               *string
}

// hostValue is one `[host]` key; ok is false when it is not set (or empty). An invalid
// host.toml is a CouldNotCheck: a scan with limits nobody chose is not a bounded scan.
func hostValue(key string) (value string, ok bool, err error) {
	v, err := config.HostValue(key)
	if errors.Is(err, config.ErrNotSet) {
		return "", false, nil
	}
	if err != nil {
		return "", false, CouldNotCheck{Msg: err.Error()}
	}
	return v, v != "", nil
}

// LoadLimits reads the scan limits from host.toml in-process; a missing file or key gives
// the defaults. It needs no `lighthouse` on PATH.
func LoadLimits() (Limits, error) {
	l := Limits{MemoryMax: DefaultMemoryMax, CPUQuota: DefaultCPUQuota}
	if v, ok, err := hostValue("host.scan_memory_max"); err != nil {
		return Limits{}, err
	} else if ok {
		l.MemoryMax = v
	}
	if v, ok, err := hostValue("host.scan_cpu_quota"); err != nil {
		return Limits{}, err
	} else if ok {
		l.CPUQuota = v
	}
	if v, ok, err := hostValue("host.gate_slice"); err != nil {
		return Limits{}, err
	} else if ok {
		l.Slice = &v
	}
	return l, nil
}
