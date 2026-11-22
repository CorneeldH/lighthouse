package forge

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// countingCache is a memory CacheStore that counts the Puts per key.
type countingCache struct {
	CacheStore
	mu   sync.Mutex
	puts map[string]int
}

func (c *countingCache) Put(ctx context.Context, key string, e CacheEntry) error {
	c.mu.Lock()
	c.puts[key]++
	c.mu.Unlock()
	return c.CacheStore.Put(ctx, key, e)
}

// tableOf is the saved table of c by issue number; it fails the test on two entries of one issue.
func tableOf(t *testing.T, c *GitHub) map[int]itemTableEntry {
	t.Helper()
	out := map[int]itemTableEntry{}
	for _, e := range c.loadItemTable(context.Background()) {
		if _, dup := out[e.Issue]; dup {
			t.Fatalf("table holds issue %d twice: %+v", e.Issue, c.loadItemTable(context.Background()))
		}
		out[e.Issue] = e
	}
	return out
}

// Classification: io. Fails when an item that left the list drops out of the table before 30
// days, stays after them, comes back as a second entry, keeps its gone_since once listed again,
// or when an unchanged table is written again.
func TestItemTableKeepsVanishedItems(t *testing.T) {
	srv, c := projectServer(t)
	srv.BoardNodes = []map[string]any{statusIssue("PVTI_7", 7, "opt-ctrl"), statusIssue("PVTI_9", 9, "opt-ctrl")}
	c.Board = restBoard()
	store := &countingCache{CacheStore: NewMemoryCache(), puts: map[string]int{}}
	c.cache = store
	ctx := context.Background()
	list := func() {
		t.Helper()
		if _, err := c.BoardItems(ctx); err != nil {
			t.Fatal(err)
		}
	}
	key := itemTableKeyPrefix + "o/r 1"

	list()
	list()
	if tb := tableOf(t, c); len(tb) != 2 || tb[7].ID != 5000 || tb[7].Node != "PVTI_7" || tb[7].Repo != "o/r" || tb[7].GoneSince != nil {
		t.Fatalf("table after two lists = %+v; want 7 and 9, both listed", tb)
	}
	if n := store.puts[key]; n != 1 {
		t.Errorf("Puts of %s after two equal lists = %d, want 1", key, n)
	}

	srv.BoardNodes[0]["isArchived"] = true
	list()
	tb := tableOf(t, c)
	if tb[7].GoneSince == nil || time.Since(*tb[7].GoneSince) > time.Minute || tb[9].GoneSince != nil {
		t.Fatalf("table after archiving 7 = %+v; want 7 gone since now, 9 listed", tb)
	}
	gone := *tb[7].GoneSince
	list()
	if tb := tableOf(t, c); tb[7].GoneSince == nil || !tb[7].GoneSince.Equal(gone) {
		t.Errorf("table after a second list without 7 = %+v; want gone_since kept at %v", tb, gone)
	}

	delete(srv.BoardNodes[0], "isArchived")
	list()
	if tb := tableOf(t, c); len(tb) != 2 || tb[7].GoneSince != nil || tb[7].ID != 5000 {
		t.Fatalf("table after unarchiving 7 = %+v; want 7 once, listed", tb)
	}

	// 7 gone for 31 days and 9 for 29: only 9 stays.
	srv.BoardNodes = []map[string]any{statusIssue("PVTI_8", 8, "opt-ctrl")}
	old, recent := time.Now().Add(-31*24*time.Hour).UTC(), time.Now().Add(-29*24*time.Hour).UTC()
	body, err := json.Marshal([]itemTableEntry{
		{Node: "PVTI_7", ID: 4999, Repo: "o/r", Issue: 7, GoneSince: &old},
		{Node: "PVTI_9", ID: 5001, Repo: "o/r", Issue: 9, GoneSince: &recent},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, key, CacheEntry{Body: body}); err != nil {
		t.Fatal(err)
	}
	list()
	tb = tableOf(t, c)
	if _, ok := tb[7]; ok {
		t.Errorf("table = %+v; want 7 dropped after 30 days gone", tb)
	}
	if e, ok := tb[9]; !ok || e.GoneSince == nil || !e.GoneSince.Equal(recent) {
		t.Errorf("table = %+v; want 9 kept with gone_since %v", tb, recent)
	}
	if e, ok := tb[8]; !ok || e.ID != 5000 || e.GoneSince != nil {
		t.Errorf("table = %+v; want 8 listed as 5000", tb)
	}
}
