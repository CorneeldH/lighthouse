package bashwrites

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// The pool and the claims of post: which paths of a root go to the gates, and the ledger rows that
// record what this post claims and judged. Port of `_RootView` to `_release_claims` in
// the former pack/core/lib/bash_writes.py.

// diffCommit is DiffCommit; a test swaps it.
var diffCommit = DiffCommit

// claimFunc is claim; a test swaps it.
var claimFunc = claim

// record is one ledger row as post reads it: `seen`, `pending` and `claims` decoded, every other
// field (`head` included) kept raw in raw, so a field this version does not know survives.
type record struct {
	raw     map[string]json.RawMessage
	seen    map[string]json.RawMessage
	pending []json.RawMessage
	claims  map[string]json.RawMessage
}

func decodeObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return nil, false
	}
	return m, true
}

// validRecord is raw if it has the form of a ledger row, otherwise nil (= absent): an object with
// an object `seen`, and `claims` (default {}) an object and `pending` (default []) a list.
func validRecord(raw json.RawMessage) *record {
	fields, ok := decodeObject(raw)
	if !ok {
		return nil
	}
	r := &record{raw: fields, claims: map[string]json.RawMessage{}, pending: []json.RawMessage{}}
	if r.seen, ok = decodeObject(fields["seen"]); !ok {
		return nil
	}
	if c, present := fields["claims"]; present {
		if r.claims, ok = decodeObject(c); !ok {
			return nil
		}
	}
	if p, present := fields["pending"]; present {
		if json.Unmarshal(p, &r.pending) != nil || r.pending == nil {
			return nil
		}
	}
	return r
}

func marshalJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

// encode is the row as JSON, with the decoded fields written back.
func (r *record) encode() json.RawMessage {
	r.raw["seen"] = marshalJSON(r.seen)
	r.raw["pending"] = marshalJSON(r.pending)
	r.raw["claims"] = marshalJSON(r.claims)
	return marshalJSON(r.raw)
}

// pendingStrings are the string entries of `pending`.
func (r *record) pendingStrings() []string {
	out := []string{}
	for _, p := range r.pending {
		if s, ok := pyJSONDecode(p); ok {
			out = append(out, s)
		}
	}
	return out
}

func (r *record) dropPending(rel string) {
	kept := []json.RawMessage{}
	for _, p := range r.pending {
		if s, ok := pyJSONDecode(p); !ok || s != rel {
			kept = append(kept, p)
		}
	}
	r.pending = kept
}

// seenIs says whether `seen` holds sig for rel.
func (r *record) seenIs(rel string, sig Signature) bool {
	var got []int64
	if json.Unmarshal(r.seen[rel], &got) != nil {
		return false
	}
	return len(got) == len(sig) && Signature(got) == sig
}

func (r *record) dropClaim(rel, key string) {
	var holder []json.RawMessage
	if json.Unmarshal(r.claims[rel], &holder) != nil || len(holder) == 0 {
		return
	}
	if owner, ok := pyJSONDecode(holder[0]); ok && owner == key {
		delete(r.claims, rel)
	}
}

// pyJSONString is s as a JSON string the way Python's json.dumps writes a name from os.fsdecode:
// a byte that is not valid UTF-8 becomes the surrogate escape \udcXX.
func pyJSONString(s string) json.RawMessage {
	if utf8.ValidString(s) {
		return marshalJSON(s)
	}
	var b strings.Builder
	b.WriteByte('"')
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&b, `\udc%02x`, s[0])
		} else {
			piece := marshalJSON(s[:size])
			b.Write(piece[1 : len(piece)-1])
		}
		s = s[size:]
	}
	b.WriteByte('"')
	return json.RawMessage(b.String())
}

// pyJSONDecode is a JSON string, with a lone surrogate escape \udc80-\udcff turned back into its
// byte (the inverse of pyJSONString). False if raw is not a JSON string.
func pyJSONDecode(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	if !strings.Contains(strings.ToLower(string(raw)), `\ud`) {
		return s, true
	}
	text := string(raw)
	text = text[1 : len(text)-1]
	var b strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '\\' {
			b.WriteByte(text[i])
			i++
			continue
		}
		if text[i+1] != 'u' {
			var one string
			_ = json.Unmarshal([]byte(`"`+text[i:i+2]+`"`), &one)
			b.WriteString(one)
			i += 2
			continue
		}
		code, _ := strconv.ParseUint(text[i+2:i+6], 16, 32)
		i += 6
		r := rune(code)
		if utf16.IsSurrogate(r) && r < 0xdc00 && i+6 <= len(text) && text[i] == '\\' && text[i+1] == 'u' {
			low, _ := strconv.ParseUint(text[i+2:i+6], 16, 32)
			if pair := utf16.DecodeRune(r, rune(low)); pair != utf8.RuneError {
				b.WriteRune(pair)
				i += 6
				continue
			}
		}
		if r >= 0xdc80 && r <= 0xdcff {
			b.WriteByte(byte(r - 0xdc00))
			continue
		}
		b.WriteRune(r) // another lone surrogate becomes U+FFFD
	}
	return b.String(), true
}

// rootView is what post knows of one root before its pool. fallback is true if the ledger had no
// usable row for the root: then seen is empty, and everything dirty is a candidate (overchecking
// is allowed, being blind is not). head is HEAD as read now: the end point of source 2, the new
// ledger head (compare-and-swap) and the head a fallback root is registered with. startHead is the
// ledger head this post began with (nil without a usable row), pending the pending list from then.
// since is the pre start time from the marker in whole seconds, nil without a marker: the lower
// bound of source 2 in the fallback, and next to startHead the second walk that covers a return to
// the ledger sha or a reflog gap.
type rootView struct {
	root      string
	seen      map[string]json.RawMessage
	fallback  bool
	head      string
	startHead *string
	pending   []string
	since     *int64
}

// rootViewOf decides what post already knows of root. Fails with a *GitError if HEAD cannot be
// read.
func rootViewOf(ledger *Ledger, root string, since *int64, deadline time.Time) (rootView, error) {
	head, err := HeadOf(root, deadline)
	if err != nil {
		return rootView{}, err
	}
	var r *record
	if !ledger.Unreadable {
		r = validRecord(ledger.Roots[root])
	}
	if r == nil {
		return rootView{root: root, seen: map[string]json.RawMessage{}, fallback: true, head: head, pending: []string{}, since: since}, nil
	}
	view := rootView{root: root, seen: r.seen, head: head, pending: r.pendingStrings(), since: since}
	var start string
	if json.Unmarshal(r.raw["head"], &start) == nil && string(r.raw["head"]) != "null" {
		view.startHead = &start
	}
	return view, nil
}

const noStartNote = "source 2 skipped: no ledger HEAD and no marker with a start time to walk back to"

// committed is source 2 of one root. paths is everything a walked commit changed. stranded is,
// per path, the newest walked commit that changed it, unless a still newer one deleted it: if such
// a path is neither on disk nor in HEAD, it is not gone but only committed elsewhere
// (`switch -c x; commit; switch main`). notes are the notices of the walk.
type committed struct {
	paths    map[string]bool
	stranded map[string]string
	notes    []string
}

// committedPaths is source 2: what was committed in the call. It walks the reflog of HEAD back to
// the ledger head and, with a marker, also to the pre start time; in the fallback only to that
// start time. Without both there is no lower bound and source 2 is skipped with a notice.
// HEAD == ledger head is a short-circuit only without a marker. An unborn HEAD yields nothing.
func committedPaths(view rootView, deadline time.Time) (committed, error) {
	none := committed{paths: map[string]bool{}, stranded: map[string]string{}}
	stop := view.startHead
	if view.head == "unborn" {
		return none, nil
	}
	if stop != nil && *stop == view.head && view.since == nil {
		return none, nil
	}
	if stop == nil && view.since == nil {
		none.notes = []string{noStartNote}
		return none, nil
	}
	entries, err := ReadReflog(view.root, deadline)
	if err != nil {
		return none, err
	}
	commits, notes := CommitsToCheck(entries, stop, view.since)
	result := committed{paths: map[string]bool{}, stranded: map[string]string{}, notes: notes}
	// Newest first: the first commit that names a path determines its fate.
	fate := map[string]string{}
	for _, c := range commits {
		changed, deleted, err := diffCommit(view.root, c.SHA, c.Merge, deadline)
		if err != nil {
			return none, err
		}
		for rel := range changed {
			result.paths[rel] = true
			if _, known := fate[rel]; !known {
				fate[rel] = c.SHA
			}
		}
		for rel := range deleted {
			if _, known := fate[rel]; !known {
				fate[rel] = ""
			}
		}
	}
	for rel, sha := range fate {
		if sha != "" {
			result.stranded[rel] = sha
		}
	}
	return result, nil
}

// inHead says which of paths are in the tree of head (`git cat-file -e <head>:<path>`). Every
// exit counts as "not in HEAD", as HEAD was just read; only a timeout or a git that does not
// start fails.
func inHead(root, head string, paths []string, deadline time.Time) (map[string]bool, error) {
	present := map[string]bool{}
	if head == "unborn" {
		return present, nil
	}
	for _, rel := range paths {
		if _, err := Git(root, deadline, "cat-file", "-e", head+":"+rel); err != nil {
			var ge *GitError
			if errors.As(err, &ge) && ge.Code != -1 {
				continue
			}
			return nil, err
		}
		present[rel] = true
	}
	return present, nil
}

// inARef says which of shas a ref still contains. A commit only the reflog still knows was thrown
// away on purpose (`reset --hard`, `--amend`): its paths are not elsewhere, they are gone.
func inARef(root string, shas map[string]bool, deadline time.Time) (map[string]bool, error) {
	kept := map[string]bool{}
	for sha := range shas {
		out, err := Git(root, deadline, "for-each-ref", "--count=1", "--format=%(refname)", "--contains", sha)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(out)) != "" {
			kept[sha] = true
		}
	}
	return kept, nil
}

// unchecked is a path that cannot be looked at, with the reason.
type unchecked struct{ rel, why string }

// pool is the pool of one root, after the net filter and seen. candidates go to the dispatch;
// unchecked are paths that cannot be looked at; keep are source 2 and pending paths without a
// verdict (they stay in pending); settled are paths that may leave pending; notes are the notices
// of source 2.
type pool struct {
	view       rootView
	candidates []string
	unchecked  []unchecked
	keep       []string
	settled    []string
	notes      []string
}

// rootPool is source 1, source 2 and pending, filtered on extension and net, sorted. A path that
// is neither on disk nor in HEAD is deleted and drops out, like a path source 1 names deleted; if
// it IS in HEAD it is unchecked. A path this walk saw committed and no later walked commit deleted
// exists only elsewhere: unchecked too, but only if a ref still contains that commit. A file unit
// of the manifest still as installed is never a candidate and leaves pending. Fails with a
// *GitError or a *ManifestError.
func rootPool(view rootView, deadline time.Time) (pool, error) {
	managed, err := ManagedFiles(view.root)
	if err != nil {
		return pool{}, err
	}
	data, err := Git(view.root, deadline, "status", "--porcelain", "-z", "-uall")
	if err != nil {
		return pool{}, err
	}
	dirty, removed := ParsePorcelain(data)
	walk, err := committedPaths(view, deadline)
	if err != nil {
		return pool{}, err
	}
	fromSource2 := map[string]bool{}
	for rel := range walk.paths {
		fromSource2[rel] = true
	}
	for _, rel := range view.pending {
		fromSource2[rel] = true
	}
	all := map[string]bool{}
	for rel := range dirty {
		all[rel] = true
	}
	for rel := range fromSource2 {
		all[rel] = true
	}
	installed := map[string]bool{}
	var paths []string
	for rel := range all {
		if asInstalled(view.root, rel, managed) {
			installed[rel] = true
		} else if _, ok := Gates[splitExt(rel)]; ok {
			paths = append(paths, rel)
		}
	}
	sort.Strings(paths)
	sigs := map[string]*Signature{}
	var missing []string
	for _, rel := range paths {
		if sig, ok := signatureOf(filepath.Join(view.root, rel)); ok {
			sigs[rel] = &sig
		} else if !removed[rel] {
			missing = append(missing, rel)
		}
	}
	present, err := inHead(view.root, view.head, missing, deadline)
	if err != nil {
		return pool{}, err
	}
	elsewhereShas := map[string]bool{}
	for _, rel := range missing {
		if sha, ok := walk.stranded[rel]; ok && !present[rel] {
			elsewhereShas[sha] = true
		}
	}
	kept, err := inARef(view.root, elsewhereShas, deadline)
	if err != nil {
		return pool{}, err
	}

	result := pool{view: view, notes: walk.notes}
	for rel := range fromSource2 {
		if installed[rel] {
			result.settled = append(result.settled, rel)
		}
	}
	sort.Strings(result.settled)
	for _, rel := range paths {
		sig := sigs[rel]
		sha, stranded := walk.stranded[rel]
		stranded = stranded && kept[sha]
		elsewhere := sig == nil && !present[rel]
		if elsewhere && (removed[rel] || !stranded) {
			result.settled = append(result.settled, rel)
			continue
		}
		if sig != nil && view.seenIs(rel, *sig) {
			result.settled = append(result.settled, rel)
			continue
		}
		switch {
		case elsewhere:
			why := "committed in " + sha[:min(12, len(sha))] + ", not on disk and not in HEAD"
			result.unchecked = append(result.unchecked, unchecked{rel, why})
		case sig == nil:
			result.unchecked = append(result.unchecked, unchecked{rel, "in HEAD, not on disk"})
		case !utf8.ValidString(rel):
			result.unchecked = append(result.unchecked, unchecked{rel, "name is not valid UTF-8"})
		default:
			result.candidates = append(result.candidates, rel)
		}
		if fromSource2[rel] {
			result.keep = append(result.keep, rel)
		}
	}
	return result, nil
}

// seenIs says whether the seen of the view holds sig for rel.
func (v rootView) seenIs(rel string, sig Signature) bool {
	return (&record{seen: v.seen}).seenIs(rel, sig)
}

// recordFor is the ledger row of view.root; a missing one (a fallback root, or a row that vanished
// meanwhile) is registered fresh with the HEAD as read now. The caller writes it back with encode.
func recordFor(ledger *Ledger, view rootView) *record {
	if r := validRecord(ledger.Roots[view.root]); r != nil {
		return r
	}
	return &record{
		raw:     map[string]json.RawMessage{"head": marshalJSON(view.head)},
		seen:    map[string]json.RawMessage{},
		pending: []json.RawMessage{},
		claims:  map[string]json.RawMessage{},
	}
}

// headIs is Python's `record.get("head") == view.start_head`.
func (r *record) headIs(startHead *string) bool {
	raw, present := r.raw["head"]
	if startHead == nil {
		return !present || string(raw) == "null"
	}
	var head string
	return json.Unmarshal(raw, &head) == nil && string(raw) != "null" && head == *startHead
}

// advance is the compare-and-swap on head, then pending, under the lock. head becomes the HEAD as
// read only if the ledger still has the value this post began with: a parallel post that was
// already further wins. pending gets the source 2 paths without a verdict; what another post added
// meanwhile stays.
func advance(r *record, p pool) {
	if r.headIs(p.view.startHead) {
		r.raw["head"] = marshalJSON(p.view.head)
	}
	names := map[string]bool{}
	for _, rel := range r.pendingStrings() {
		names[rel] = true
	}
	for _, rel := range p.keep {
		names[rel] = true
	}
	for _, rel := range p.settled {
		delete(names, rel)
	}
	r.pending = []json.RawMessage{}
	for _, rel := range sortedSet(names) {
		r.pending = append(r.pending, pyJSONString(rel))
	}
}

// claimedByOther says whether holder is a live claim of another post. A claim older than the
// deadline plus grace counts as absent (that post was aborted without cleaning up), and so does a
// malformed one.
func claimedByOther(holder json.RawMessage, key string, nowS float64) bool {
	var pair []json.RawMessage
	if json.Unmarshal(holder, &pair) != nil || len(pair) != 2 {
		return false
	}
	if owner, ok := pyJSONDecode(pair[0]); ok && owner == key {
		return false
	}
	var epoch float64
	if json.Unmarshal(pair[1], &epoch) != nil {
		return false
	}
	return nowS-epoch <= (postDeadline() + gateGrace).Seconds()
}

// sortedSet are the keys of m, sorted.
func sortedSet[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func epochSeconds(t time.Time) float64 { return float64(t.UnixNano()) / 1e9 }

// claimed is one candidate this post claimed.
type claimed struct {
	view rootView
	rel  string
}

// claim runs advance per root under the lock, then claims what nobody is checking yet. A
// candidate meanwhile in seen with its current signature drops out (and out of pending). An
// unreadable ledger is replaced here on purpose by a fresh one with only the roots this post
// walked, otherwise every following post falls back again; under the same lock comes the sentinel
// ledger.reset, so pre does not seed the roots missing here. Returns (claimed, replaced).
func claim(state, key string, pools []pool) ([]claimed, bool, error) {
	nowS := epochSeconds(now())
	var got []claimed
	replaced := false
	err := withLedger(state, func(l *Ledger) error {
		replaced = l.Unreadable
		if replaced {
			f, err := os.OpenFile(filepath.Join(state, resetName), os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			_ = f.Close()
			l.Roots = map[string]json.RawMessage{}
			l.Unreadable = false
		}
		for _, p := range pools {
			r := recordFor(l, p.view)
			advance(r, p)
			for _, rel := range p.candidates {
				if sig, ok := signatureOf(filepath.Join(p.view.root, rel)); ok && r.seenIs(rel, sig) {
					r.dropPending(rel)
					continue
				}
				if claimedByOther(r.claims[rel], key, nowS) {
					continue
				}
				r.claims[rel] = marshalJSON([]any{key, nowS})
				got = append(got, claimed{p.view, rel})
			}
			l.Roots[p.view.root] = r.encode()
		}
		return nil
	})
	return got, replaced, err
}

// recordVerdict drops the claim after all gates of a file, under the lock; on a real verdict the
// path also leaves pending and goes into seen (with sig, if the file is still there).
func recordVerdict(state, key string, view rootView, rel, verdict string, sig *Signature) error {
	return withLedger(state, func(l *Ledger) error {
		if l.Unreadable {
			return nil
		}
		r := recordFor(l, view)
		r.dropClaim(rel, key)
		if verdict != "unchecked" {
			r.dropPending(rel)
			if sig != nil {
				r.seen[rel] = marshalJSON(sig[:])
			}
		}
		l.Roots[view.root] = r.encode()
		return nil
	})
}

// releaseClaims removes every claim that still carries key: a claim exists only during the gate.
func releaseClaims(state, key string) error {
	return withLedger(state, func(l *Ledger) error {
		if l.Unreadable {
			return nil
		}
		for root, raw := range l.Roots {
			r := validRecord(raw)
			if r == nil {
				continue
			}
			for rel := range r.claims {
				r.dropClaim(rel, key)
			}
			l.Roots[root] = r.encode()
		}
		return nil
	})
}
