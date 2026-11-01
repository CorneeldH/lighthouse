package triage

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Limits of the free text fields of a judgment, in runes after strings.TrimSpace (spec: the
// free fields table of the triage read limit).
const (
	MaxRouteRunes          = 80
	MaxRouteReasonRunes    = 200
	MaxHeadlessReasonRunes = 300
	MaxQuestionRunes       = 300
	MaxOptionRunes         = 150
	MaxEvidenceRunes       = 220
	MaxEvidenceRefs        = 5
	MaxIssueDigits         = 6
)

// evidenceRef is one reference of redundancy_evidence: #<number> or a commit sha of 7 to 40 hex.
var evidenceRef = fmt.Sprintf(`(#[0-9]{1,%d}|[0-9a-fA-F]{7,40})`, MaxIssueDigits)

// evidenceRe fits the whole field: at most MaxEvidenceRefs references, separated by ", " or " ".
var evidenceRe = regexp.MustCompile(fmt.Sprintf(`^%s((, | )%s){0,%d}$`, evidenceRef, evidenceRef, MaxEvidenceRefs-1))

// checkEvidence checks redundancy_evidence: only references, and every sha a prefix of a sha
// from the git log of the facts.
func checkEvidence(ev string, shas []string) error {
	ev = strings.TrimSpace(ev)
	if ev == "" {
		return nil
	}
	if n := utf8.RuneCountInString(ev); n > MaxEvidenceRunes {
		return invalid("redundancy_evidence is %d characters, the limit is %d", n, MaxEvidenceRunes)
	}
	if !evidenceRe.MatchString(ev) {
		return invalid("redundancy_evidence %q is not only references (at most %d, #<number> or a commit sha, separated by \", \" or \" \")", ev, MaxEvidenceRefs)
	}
	for _, ref := range strings.FieldsFunc(ev, func(r rune) bool { return r == ',' || r == ' ' }) {
		if strings.HasPrefix(ref, "#") {
			continue
		}
		if !slices.ContainsFunc(shas, func(full string) bool {
			return strings.HasPrefix(strings.ToLower(full), strings.ToLower(ref))
		}) {
			return invalid("redundancy_evidence sha %s is not in the git log", ref)
		}
	}
	return nil
}

// checkLen is an invalid error when s is longer than limit runes.
func checkLen(field, s string, limit int) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(s)); n > limit {
		return invalid("%s is %d characters, the limit is %d", field, n, limit)
	}
	return nil
}
