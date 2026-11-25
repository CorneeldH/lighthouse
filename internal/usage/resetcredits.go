package usage

import "encoding/json"

// ResetCredits is the typed form of the app-server's reset credits: how many
// there are and when each one expires (UTC, timeLayout, oldest first).
type ResetCredits struct {
	Available int      `json:"available"`
	Expires   []string `json:"expires" nullable:"false"`
	// The struct-wide override makes the type as a whole nullable in the schema;
	// huma panics on nullable:"true" on an object field.
	_ struct{} `nullable:"true"`
}

// Credits parses ResetCredits. It returns nil when the credits are unknown:
// absent, null or unreadable. Otherwise Expires is never nil.
func (s CodexState) Credits() *ResetCredits {
	if len(s.ResetCredits) == 0 {
		return nil
	}
	var c *ResetCredits
	if err := json.Unmarshal(s.ResetCredits, &c); err != nil || c == nil {
		return nil
	}
	if c.Expires == nil {
		c.Expires = []string{}
	}
	return c
}
