package repoconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// ownRepoRoot returns the directory that holds go.mod, found by walking up from the package
// directory.
func ownRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the package directory")
		}
		dir = parent
	}
}

func TestOwnLighthouseTomlEnablesReport(t *testing.T) {
	repo, err := Load(ownRepoRoot(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if repo.ReportErr != nil {
		t.Fatalf("ReportErr = %v, want nil", repo.ReportErr)
	}
	want := Report{Enabled: true, Sink: "file", Dir: "docs/developer/verslag"}
	if repo.Report != want {
		t.Errorf("Report = %+v, want %+v", repo.Report, want)
	}
}
