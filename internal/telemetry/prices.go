package telemetry

import (
	_ "embed"
	"fmt"
	"sort"
	"time"

	"github.com/BurntSushi/toml"
)

// Tokens are the token counts of one message.
type Tokens struct{ Input, Output, CacheRead, CacheWrite5m, CacheWrite1h int64 }

// CacheWrite is the cache-write total, 5m plus 1h.
func (t Tokens) CacheWrite() int64 { return t.CacheWrite5m + t.CacheWrite1h }

//go:embed prices.toml
var pricesTOML string

// priceRow is one [[price]] table; prices are USD per million tokens.
type priceRow struct {
	Model        string    `toml:"model"`
	AsOf         time.Time `toml:"as_of"`
	Source       string    `toml:"source"`
	Input        float64   `toml:"input"`
	Output       float64   `toml:"output"`
	CacheRead    float64   `toml:"cache_read"`
	CacheWrite5m float64   `toml:"cache_write_5m"`
	CacheWrite1h float64   `toml:"cache_write_1h"`
}

// priceTable maps a model id to its rows, sorted by AsOf ascending.
type priceTable map[string][]priceRow

func parsePrices(src string) (priceTable, error) {
	var doc struct {
		Price []priceRow `toml:"price"`
	}
	if _, err := toml.Decode(src, &doc); err != nil {
		return nil, fmt.Errorf("parse prices: %w", err)
	}
	table := priceTable{}
	for _, r := range doc.Price {
		table[r.Model] = append(table[r.Model], r)
	}
	for _, rows := range table {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].AsOf.Before(rows[j].AsOf) })
	}
	return table, nil
}

var embeddedPrices = func() priceTable {
	t, err := parsePrices(pricesTOML)
	if err != nil {
		panic(err) // the embedded file is checked by a test
	}
	return t
}()

// Estimate prices one message at the rate valid at `at`. ok is false for a model
// that is not in the table; only the literal model id matches.
func Estimate(model string, at time.Time, t Tokens) (cost float64, ok bool) {
	return estimateWith(embeddedPrices, model, at, t)
}

func estimateWith(table priceTable, model string, at time.Time, t Tokens) (float64, bool) {
	rows := table[model]
	if len(rows) == 0 {
		return 0, false
	}
	r := rows[0] // before the first row, the first row applies
	for _, c := range rows {
		if c.AsOf.After(at) {
			break
		}
		r = c
	}
	const m = 1_000_000.0
	cost := (float64(t.Input)*r.Input +
		float64(t.Output)*r.Output +
		float64(t.CacheRead)*r.CacheRead +
		float64(t.CacheWrite5m)*r.CacheWrite5m +
		float64(t.CacheWrite1h)*r.CacheWrite1h) / m
	return cost, true
}
