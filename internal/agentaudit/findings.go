package agentaudit

import "sort"

// Kinds of a finding (spec §4 and §3).
const (
	KindBlockingPair = "blocking-pair"
	KindPermission   = "permission"
	KindMissing      = "missing-script"
	KindUnregistered = "unregistered-script"
	KindOverlap      = "overlap"
)

// Finding is one conflict or overlap candidate. Key is stable, so `ignore` can name it;
// Text holds the lines shown under the key; Sources are the sources the key names, for
// the rule that an ignore key is stale only when each of them was measured.
type Finding struct {
	Key, Kind string
	Text      []string
	Sources   []string
}

// findings gathers findings by key: a second finding under a key adds the text lines and
// sources it does not have yet.
type findings map[string]*Finding

func (fs findings) add(kind, key, text string, sources ...string) {
	f, ok := fs[key]
	if !ok {
		f = &Finding{Key: key, Kind: kind}
		fs[key] = f
	}
	f.Text = appendNew(f.Text, text)
	for _, s := range sources {
		f.Sources = appendNew(f.Sources, s)
	}
}

// list is every finding, sorted by key, with sorted sources.
func (fs findings) list() []Finding {
	out := make([]Finding, 0, len(fs))
	for _, f := range fs {
		sort.Strings(f.Sources)
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func appendNew(list []string, s string) []string {
	for _, have := range list {
		if have == s {
			return list
		}
	}
	return append(list, s)
}

// hookPath is how a hook is named in a key: its script as shown, or else the command.
func hookPath(h Hook) string {
	if h.Script != "" {
		return h.Shown
	}
	return h.Command
}

// hookID is a hook as a key names it: <source>/<path>.
func hookID(h Hook) string { return h.Source + "/" + hookPath(h) }

// hookLabel is a hook as a text line names it: <source> <path>.
func hookLabel(h Hook) string { return h.Source + " " + hookPath(h) }

// eventLabel is "<event> <matcher>", or the event alone for an empty matcher.
func eventLabel(event, matcher string) string {
	if matcher == "" {
		return event
	}
	return event + " " + matcher
}
