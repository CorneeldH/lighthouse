package forgecache

import (
	"os"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

func testEntry() forge.CacheEntry {
	return forge.CacheEntry{ETag: `W/"abc"`, Link: `<https://x/?page=2>; rel="next"`, Body: []byte(`{"a":1}`)}
}

func writeFile(p string) error { return os.WriteFile(p, []byte("x"), 0o600) }

func readDirNames(dir string) ([]string, error) {
	es, err := os.ReadDir(dir)
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out, err
}
