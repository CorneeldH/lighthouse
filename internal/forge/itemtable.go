package forge

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// itemTableKeyPrefix starts the CacheStore key of the item table: "ITEMIDS <repo> <project
// number>". It cannot clash with the "GET <url> <hash>" keys of the ETag cache.
const itemTableKeyPrefix = "ITEMIDS "

// itemTableKeep is how long an item stays in the table after it left the board list.
const itemTableKeep = 30 * 24 * time.Hour

// itemTableEntry is one issue item of the board as a list once showed it. GoneSince is when a
// list first no longer showed it (archived or removed); nil while the list shows it.
type itemTableEntry struct {
	Node      string     `json:"node"`
	ID        int64      `json:"id"`
	Repo      string     `json:"repo"`
	Issue     int        `json:"issue"`
	GoneSince *time.Time `json:"gone_since,omitempty"`
}

// itemTableKey is the CacheStore key of the table of c's repo and board.
func (c *GitHub) itemTableKey() string {
	return fmt.Sprintf("%s%s %d", itemTableKeyPrefix, c.Repo, c.Board.ProjectNumber)
}

// loadItemTable is the saved table: the CacheStore entry when c has a cache, else the copy of
// this process. A missing, unreadable or undecodable entry is an empty table.
func (c *GitHub) loadItemTable(ctx context.Context) []itemTableEntry {
	if c.cache == nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		return slices.Clone(c.itemTable)
	}
	e, ok, err := c.cache.Get(ctx, c.itemTableKey())
	if err != nil || !ok {
		return nil
	}
	var table []itemTableEntry
	if json.Unmarshal(e.Body, &table) != nil {
		return nil
	}
	return table
}

// saveItemTable merges the complete list listed into the saved table and writes it back when
// it changed. Every issue item of listed is in it without GoneSince; an entry of the old table
// whose item, node or issue listed does not hold gets GoneSince now (kept when set) and goes
// once that is longer than itemTableKeep ago. It is best-effort: a store error is never fatal.
func (c *GitHub) saveItemTable(ctx context.Context, listed []restItem) {
	now := time.Now().UTC()
	old := c.loadItemTable(ctx)
	var table []itemTableEntry
	ids, nodes, issues := map[int64]bool{}, map[string]bool{}, map[issueKey]bool{}
	for _, r := range listed {
		if !r.issue() {
			continue
		}
		key := issueKey{repoFromURL(r.Content.RepositoryURL), r.Content.Number}
		table = append(table, itemTableEntry{Node: r.NodeID, ID: r.ID, Repo: key.repo, Issue: key.number})
		ids[r.ID], nodes[r.NodeID], issues[key] = true, true, true
	}
	for _, e := range old {
		key := issueKey{e.Repo, e.Issue}
		if ids[e.ID] || nodes[e.Node] || issues[key] {
			continue
		}
		if e.GoneSince == nil {
			e.GoneSince = &now
		} else if now.Sub(*e.GoneSince) > itemTableKeep {
			continue
		}
		table = append(table, e)
		issues[key] = true
	}
	slices.SortFunc(table, func(a, b itemTableEntry) int {
		return cmp.Or(cmp.Compare(a.Repo, b.Repo), cmp.Compare(a.Issue, b.Issue), cmp.Compare(a.ID, b.ID))
	})
	if c.cache == nil {
		c.mu.Lock()
		c.itemTable = table
		c.mu.Unlock()
		return
	}
	body, err := json.Marshal(table)
	if err != nil {
		return
	}
	if prev, err := json.Marshal(old); err == nil && bytes.Equal(prev, body) {
		return
	}
	_ = c.cache.Put(ctx, c.itemTableKey(), CacheEntry{Body: body})
}

// savedItemID is the numeric item ID of issue key in the saved table.
func (c *GitHub) savedItemID(ctx context.Context, key issueKey) (int64, bool) {
	for _, e := range c.loadItemTable(ctx) {
		if e.Repo == key.repo && e.Issue == key.number {
			return e.ID, true
		}
	}
	return 0, false
}
