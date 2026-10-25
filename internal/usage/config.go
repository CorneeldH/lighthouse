package usage

import "time"

// Config holds the alarm thresholds and the cache lifetime.
type Config struct {
	HaltPctSession int           // session percentage at which the alarm goes up
	HaltPctWeek    int           // week percentage at which the alarm goes up
	TTL            time.Duration // age after which the cache is stale
}

// DefaultConfig returns the built-in thresholds: 85, 95 and 300 s.
func DefaultConfig() Config {
	return Config{HaltPctSession: 85, HaltPctWeek: 95, TTL: 300 * time.Second}
}
