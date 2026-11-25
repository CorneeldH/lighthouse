package checkpointscan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/gitpath"
	"github.com/CorneeldH/lighthouse/internal/procrun"
)

// LedgerPath is the ledger for this config, this gitleaks and this ScanMethod, shared by
// every worktree: a change to any of them starts a new ledger.
func LedgerPath(repo, config string) (string, error) {
	common, err := git(repo, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(config)
	if err != nil {
		return "", CouldNotCheck{Msg: fmt.Sprintf("cannot read %s: %v", config, err)}
	}
	res, err := procrun.Exec(30*time.Second)(context.Background(), "", nil, "gitleaks", "version")
	if err != nil {
		return "", CouldNotCheck{Msg: fmt.Sprintf("gitleaks version failed: %v", err)}
	}
	if res.Code != 0 {
		return "", CouldNotCheck{Msg: fmt.Sprintf("gitleaks version exited %d", res.Code)}
	}
	h := sha256.New()
	h.Write(body)
	h.Write(bytes.Trim([]byte(res.Stdout), " \t\n\r\v\f"))
	h.Write([]byte{0})
	h.Write([]byte(ScanMethod))
	key := hex.EncodeToString(h.Sum(nil))
	return filepath.Join(gitpath.Native(common), "lighthouse", "secret-scan", "clean-"+key+".txt"), nil
}

// LoadLedger reads the ledger: the set of blob ids that came out clean. A missing file is an
// empty ledger.
func LoadLedger(path string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, CouldNotCheck{Msg: fmt.Sprintf("cannot read the ledger %s: %v", path, err)}
	}
	for _, oid := range strings.Fields(string(raw)) {
		out[oid] = struct{}{}
	}
	return out, nil
}

// AddToLedger merges blobs into the ledger: sorted, through a temp file and a rename, so a
// kill leaves the old ledger or the new one, never half of one.
func AddToLedger(path string, blobs []string) error {
	if len(blobs) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	merged, err := LoadLedger(path)
	if err != nil {
		return err
	}
	for _, oid := range blobs {
		merged[oid] = struct{}{}
	}
	ids := make([]string, 0, len(merged))
	for oid := range merged {
		ids = append(ids, oid)
	}
	sort.Strings(ids)
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // gone after a successful rename
	if _, err := tmp.WriteString(strings.Join(ids, "\n") + "\n"); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
