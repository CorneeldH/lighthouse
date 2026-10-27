package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"net/http"
	"os"
	"testing"
)

var update = flag.Bool("update", false, "rewrite web/openapi.json from the served description")

const snapshotPath = "../../web/openapi.json"

const staleMsg = "web/openapi.json is stale: run go test ./internal/server -run TestOpenAPISnapshot -update, then cd web && npm run gen"

// TestOpenAPISnapshot keeps web/openapi.json, the input of the generated
// TypeScript client, equal to the description the server serves.
func TestOpenAPISnapshot(t *testing.T) {
	rec := get(New(testSite(), fakeReport, stubSource{}), "/api/openapi.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode description: %v", err)
	}
	info, ok := doc["info"].(map[string]any)
	if !ok {
		t.Fatal("description has no info object")
	}
	info["version"] = "dev" // independent of VCS or ldflags version
	want, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')

	if *update {
		if err := os.WriteFile(snapshotPath, want, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(snapshotPath)
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatal(staleMsg)
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal(staleMsg)
	}
}
