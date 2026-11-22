package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

// requiredAgreement is the sentence every signature must carry, byte for byte.
const requiredAgreement = "I have read the CLA document and I hereby sign the CLA."

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

var signatureFields = []string{"name", "github_login", "date", "cla_revision", "agreement"}

// ignoredCommitterEmails: GitHub signs a merge commit made through the web interface with its
// own `web-flow` identity. That is no contributor and there is nothing to ask of it.
var ignoredCommitterEmails = map[string]bool{"noreply@github.com": true}

// logFormat uses NUL as the field separator, with `git log -z` as the record separator. The
// whole stream is then one flat run of NUL-separated chunks, exactly `fieldsPerCommit` of them
// per commit, and that count is an integrity check in itself.
//
// Not `%x1f`: git's identity cleanup strips only `<`, `>`, newlines and leading or trailing
// control characters, so an embedded `\x1f` survives in an author name and silently shifts every
// field after it, so that the real address lands in a name field and is never held against the
// register. Fail-open, which is what this gate must prevent. A NUL cannot occur in an identity;
// in a commit message a crafted object can carry one, and then only the `% fieldsPerCommit`
// check catches the shift (not when the number of extra NULs is a multiple of six).
const (
	logFormat       = "%h%x00%an%x00%ae%x00%cn%x00%ce%x00%B"
	fieldsPerCommit = 6
)

// coAuthorPattern matches a `Co-authored-by:` trailer in any letter case, anchored per line.
var coAuthorPattern = regexp.MustCompile(`(?im)^[ \t]*co-authored-by:[ \t]*(.*?)[ \t]*<([^>]+)>[ \t]*$`)

// Identity is one person or bot as it appears in one commit.
type Identity struct {
	Email  string
	Name   string
	Role   string // "author" | "committer" | "co-author"
	Commit string
}

// Register is the decoded signature register. It stays a generic map, with numbers kept as
// `json.Number`: a typed struct would reject entries the original ignored, and a float64 would
// weaken the signature key.
type Register map[string]any

// runGit runs git in repo and returns its stdout. LC_ALL=C keeps the messages that
// checkAppendOnly matches on in English.
func runGit(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		code := -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
		detail := strings.TrimSpace(stderr.String())
		if code == -1 {
			detail = err.Error()
		}
		return "", fmt.Errorf("git %s failed (exit %d): %s", strings.Join(args, " "), code, detail)
	}
	return stdout.String(), nil
}

// newlines folds CRLF and lone CR into LF, as Python's text mode did: a `Co-authored-by` line in
// a message with CRLF endings must still match `$`.
var newlines = strings.NewReplacer("\r\n", "\n", "\r", "\n")

// collectIdentities returns every identity in `base..head`: author, committer and co-authors.
func collectIdentities(repo, base, head string) ([]Identity, error) {
	raw, err := runGit(repo, "log", "-z", "--format="+logFormat, base+".."+head)
	if err != nil {
		return nil, err
	}
	return parseLog(raw)
}

// parseLog turns the output of `git log -z --format=logFormat` into identities.
func parseLog(raw string) ([]Identity, error) {
	chunks := strings.Split(newlines.Replace(raw), "\x00")
	// `git log -z` ends every record with a NUL, so the last split always yields one empty
	// tail. An empty range yields nothing at all.
	if chunks[len(chunks)-1] == "" {
		chunks = chunks[:len(chunks)-1]
	}
	if len(chunks)%fieldsPerCommit != 0 {
		return nil, fmt.Errorf("git log gave %d NUL-separated fields, not a multiple of %d; "+
			"the output cannot be trusted", len(chunks), fieldsPerCommit)
	}

	var identities []Identity
	for start := 0; start < len(chunks); start += fieldsPerCommit {
		c := chunks[start : start+fieldsPerCommit]
		sha, authorName, authorEmail, committerName, committerEmail, body := c[0], c[1], c[2], c[3], c[4], c[5]
		identities = append(identities, Identity{normEmail(authorEmail), strings.TrimSpace(authorName), "author", sha})
		if committer := normEmail(committerEmail); !ignoredCommitterEmails[committer] {
			identities = append(identities, Identity{committer, strings.TrimSpace(committerName), "committer", sha})
		}
		for _, m := range coAuthorPattern.FindAllStringSubmatch(body, -1) {
			identities = append(identities, Identity{normEmail(m[2]), strings.TrimSpace(m[1]), "co-author", sha})
		}
	}
	return identities, nil
}

func normEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// decodeRegister decodes JSON text into a Register. Invalid UTF-8 is refused: the decoder
// would replace it silently, and two different values could then compare equal.
func decodeRegister(data []byte) (Register, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("invalid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var reg Register
	if err := dec.Decode(&reg); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("invalid character after top-level value")
	}
	if reg == nil {
		return nil, errors.New("the top-level value is not an object")
	}
	return reg, nil
}

func loadRegister(path string) (Register, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s does not exist", path)
	}
	if err != nil {
		return nil, fmt.Errorf("%s cannot be read: %v", path, err)
	}
	reg, err := decodeRegister(data)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %v", path, err)
	}
	return reg, nil
}

// emailsOf returns the normalised, non-empty addresses of `entry["emails"]`.
func emailsOf(entry map[string]any) []string {
	raw, ok := entry["emails"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, e := range raw {
		if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, normEmail(s))
		}
	}
	return out
}

// pyRepr renders a JSON value the way the Python original did in its messages (`{x!r}`).
func pyRepr(v any) string {
	switch t := v.(type) {
	case nil:
		return "None"
	case bool:
		if t {
			return "True"
		}
		return "False"
	case string:
		quote := byte('\'')
		if strings.Contains(t, "'") && !strings.Contains(t, `"`) {
			quote = '"'
		}
		var b strings.Builder
		b.WriteByte(quote)
		for _, r := range t {
			switch {
			case r == '\\':
				b.WriteString(`\\`)
			case r == '\n':
				b.WriteString(`\n`)
			case r == '\r':
				b.WriteString(`\r`)
			case r == '\t':
				b.WriteString(`\t`)
			case r == rune(quote):
				b.WriteString(`\` + string(r))
			case strconv.IsPrint(r):
				b.WriteRune(r)
			case r < 0x100:
				fmt.Fprintf(&b, `\x%02x`, r)
			case r < 0x10000:
				fmt.Fprintf(&b, `\u%04x`, r)
			default:
				fmt.Fprintf(&b, `\U%08x`, r)
			}
		}
		b.WriteByte(quote)
		return b.String()
	case json.Number:
		return t.String()
	}
	return fmt.Sprintf("%v", v)
}

func listContains(list []any, v any) bool {
	for _, item := range list {
		if reflect.DeepEqual(item, v) {
			return true
		}
	}
	return false
}

func validateSignatureFields(where string, sig map[string]any, revisions []any) []string {
	var errs []string
	for _, field := range signatureFields {
		if s, ok := sig[field].(string); !ok || strings.TrimSpace(s) == "" {
			errs = append(errs, fmt.Sprintf("%s: field `%s` is missing or empty", where, field))
		}
	}
	if sig["agreement"] != requiredAgreement {
		errs = append(errs, fmt.Sprintf("%s: `agreement` must be exactly %s", where, pyRepr(requiredAgreement)))
	}
	if date, ok := sig["date"].(string); ok && !datePattern.MatchString(date) {
		errs = append(errs, fmt.Sprintf("%s: `date` %s is not YYYY-MM-DD", where, pyRepr(date)))
	}
	if rev, ok := sig["cla_revision"].(string); ok && !listContains(revisions, rev) {
		errs = append(errs, fmt.Sprintf("%s: unknown `cla_revision` %s", where, pyRepr(rev)))
	}
	return errs
}

// validateSignatureEmails checks the emails of one signature: present, not the owner's, not
// claimed elsewhere already.
func validateSignatureEmails(where string, sig map[string]any, owner map[string]bool, claimed map[string]string) []string {
	var errs []string
	emails := emailsOf(sig)
	if len(emails) == 0 {
		errs = append(errs, where+": field `emails` is missing or empty")
	}
	for _, email := range emails {
		if owner[email] {
			errs = append(errs, fmt.Sprintf("%s: %s is already an owner address", where, email))
		}
		if by, ok := claimed[email]; ok {
			errs = append(errs, fmt.Sprintf("%s: %s is already claimed by %s", where, email, by))
		} else {
			claimed[email] = where
		}
	}
	return errs
}

// validateRegister returns every structural error in the register; nil means valid.
func validateRegister(reg Register) []string {
	var errs []string

	revisions, _ := reg["cla_revisions"].([]any)
	if len(revisions) == 0 {
		return []string{"register: `cla_revisions` is missing or empty"}
	}
	current := reg["current_cla_revision"]
	if !listContains(revisions, current) {
		errs = append(errs, fmt.Sprintf("register: `current_cla_revision` %s is not in `cla_revisions`", pyRepr(current)))
	}

	owner, isObj := reg["owner"].(map[string]any)
	if !isObj || len(emailsOf(owner)) == 0 {
		errs = append(errs, "register: `owner` is missing or has no `emails`")
	}
	ownerEmails := map[string]bool{}
	if isObj {
		for _, e := range emailsOf(owner) {
			ownerEmails[e] = true
		}
	}

	if _, ok := reg["bots"].([]any); !ok {
		errs = append(errs, "register: `bots` is missing or not a list")
	}

	signatures, ok := reg["signatures"].([]any)
	if !ok {
		return append(errs, "register: `signatures` is missing or not a list")
	}

	claimed := map[string]string{}
	for i, s := range signatures {
		where := fmt.Sprintf("signatures[%d]", i)
		sig, ok := s.(map[string]any)
		if !ok {
			errs = append(errs, where+": not an object")
			continue
		}
		errs = append(errs, validateSignatureFields(where, sig, revisions)...)
		errs = append(errs, validateSignatureEmails(where, sig, ownerEmails, claimed)...)
	}
	return errs
}

// allowlistedEmails are the addresses covered outside `signatures`: owner and bots. They are
// separate from the covered set because exactly these two fields may never grow in the same
// pull request, see checkAppendOnly.
func allowlistedEmails(reg Register) map[string]bool {
	covered := map[string]bool{}
	if owner, ok := reg["owner"].(map[string]any); ok {
		for _, e := range emailsOf(owner) {
			covered[e] = true
		}
	}
	bots, _ := reg["bots"].([]any)
	for _, b := range bots {
		bot, ok := b.(map[string]any)
		if !ok {
			continue
		}
		if email, ok := bot["email"].(string); ok {
			if email = normEmail(email); email != "" {
				covered[email] = true
			}
		}
	}
	return covered
}

// coveredEmails is every address that needs no signature any more: owner, bots, signers.
func coveredEmails(reg Register) map[string]bool {
	covered := allowlistedEmails(reg)
	sigs, _ := reg["signatures"].([]any)
	for _, s := range sigs {
		if sig, ok := s.(map[string]any); ok {
			for _, e := range emailsOf(sig) {
				covered[e] = true
			}
		}
	}
	return covered
}

// unsigned returns the identities without coverage, one per address, in order of appearance.
func unsigned(identities []Identity, reg Register) []Identity {
	covered := coveredEmails(reg)
	seen := map[string]bool{}
	var missing []Identity
	for _, id := range identities {
		if covered[id.Email] || seen[id.Email] {
			continue
		}
		seen[id.Email] = true
		missing = append(missing, id)
	}
	return missing
}

// cell makes a name or address safe for a markdown table cell. Name and email come from the
// commit and are thus chosen by the contributor: a `|` breaks the column count, a backtick breaks
// out of the code span, and a newline breaks the row, all in exactly the report the maintainer
// relies on to see who did not sign. Escape, do not trust.
func cell(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, "|", `\|`, "`", "'").Replace(value)
	fields := strings.FieldsFunc(escaped, func(r rune) bool {
		return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
	})
	return strings.Join(fields, " ")
}

// renderReport is markdown: who is missing, and what that person has to do.
func renderReport(missing []Identity, repoURL string) string {
	lines := []string{
		"## CLA: not every contributor has signed",
		"",
		"| Address | Name | Role | Commit |",
		"| --- | --- | --- | --- |",
	}
	for _, id := range missing {
		email, name := cell(id.Email), cell(id.Name)
		if email == "" {
			email = "*(empty)*"
		}
		if name == "" {
			name = "*(empty)*"
		}
		lines = append(lines, fmt.Sprintf("| `%s` | `%s` | %s | `%s` |", email, name, id.Role, cell(id.Commit)))
	}
	lines = append(lines,
		"",
		"Every address above must be covered before this pull request can be merged.",
		"Signing takes one commit: add your own entry to `.github/cla-signatures.json`.",
		"The format and the two routes are in",
		// A full URL, not a relative path: in a job summary that does not resolve.
		fmt.Sprintf("[`CLA.md`](%s/blob/main/CLA.md),", repoURL),
		"section 11 — you may sign in this pull request.",
		"",
		"If an address above is yours but you do not recognise it, it is probably the",
		"address in your git configuration (`git config user.email`).",
		"Add the address as it stands here, or change your commits.",
		"",
		"An empty address comes from a commit without an author email. It cannot be",
		"covered and must be rewritten (`git commit --amend --reset-author`).",
	)
	return strings.Join(lines, "\n") + "\n"
}

// repoURL is the web URL of the repository, from `[forge] repo` in `<repo>/lighthouse.toml`.
func repoURL(repo string) (string, error) {
	var cfg struct {
		Forge struct {
			Repo string `toml:"repo"`
		} `toml:"forge"`
	}
	path := filepath.Join(repo, "lighthouse.toml")
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return "", fmt.Errorf("%s: %v", path, err)
	}
	if cfg.Forge.Repo == "" {
		return "", fmt.Errorf("%s: `[forge] repo` is missing", path)
	}
	return "https://github.com/" + cfg.Forge.Repo, nil
}

// emit writes the report to stdout, and appends it to the job summary when running in Actions.
func emit(stdout, stderr io.Writer, report string) {
	_, _ = fmt.Fprintln(stdout, report)
	summary := os.Getenv("GITHUB_STEP_SUMMARY")
	if summary == "" {
		return
	}
	f, err := os.OpenFile(summary, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		_, err = f.WriteString(report)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "cannot write GITHUB_STEP_SUMMARY: %v\n", err)
	}
}

// signatureKey is a stable key over a whole signature, independent of the key order.
// `json.Marshal` sorts map keys but not a nested list: a reordered `emails` list therefore reads
// as a change. That is the safe direction, it fails closed.
func signatureKey(sig map[string]any) string {
	b, err := json.Marshal(sig)
	if err != nil {
		// Unreachable for a decoded value; a key that cannot match fails closed.
		return "\x00unmarshalable"
	}
	return string(b)
}

func truthyString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok && s != ""
}

// lostSignatures lists every signature on `base` that does not come back unchanged in HEAD.
func lostSignatures(previous, reg Register, base string) []string {
	current := map[string]bool{}
	curSigs, _ := reg["signatures"].([]any)
	for _, s := range curSigs {
		if sig, ok := s.(map[string]any); ok {
			current[signatureKey(sig)] = true
		}
	}
	var errs []string
	prevSigs, _ := previous["signatures"].([]any)
	for _, s := range prevSigs {
		sig, ok := s.(map[string]any)
		if !ok || current[signatureKey(sig)] {
			continue
		}
		who, ok := truthyString(sig["github_login"])
		if !ok {
			if who, ok = truthyString(sig["name"]); !ok {
				who = "?"
			}
		}
		errs = append(errs, fmt.Sprintf("the signature of `%s` was removed or changed compared to `%s`", who, base))
	}
	return errs
}

// resolve makes a path absolute and follows symlinks where it can.
func resolve(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// checkAppendOnly requires every signature and revision on the base branch to stand unchanged in
// HEAD. If the register does not exist on the base branch yet, this is the pull request that
// introduces it and there is nothing to compare.
func checkAppendOnly(repo, base, registerPath string, reg Register) ([]string, error) {
	rel, err := filepath.Rel(resolve(repo), resolve(registerPath))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		// The register lies outside the repository. Nothing to compare.
		return nil, nil
	}
	raw, err := runGit(repo, "show", base+":"+filepath.ToSlash(rel))
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "exists on disk, but not in") {
			return nil, nil
		}
		return nil, err
	}
	previous, err := decodeRegister([]byte(raw))
	if err != nil {
		return []string{fmt.Sprintf("the register on `%s` is not valid JSON: %v", base, err)}, nil
	}

	errs := lostSignatures(previous, reg, base)

	curRevs, _ := reg["cla_revisions"].([]any)
	prevRevs, _ := previous["cla_revisions"].([]any)
	for _, rev := range prevRevs {
		if !listContains(curRevs, rev) {
			errs = append(errs, fmt.Sprintf("revision `%v` was removed from `cla_revisions`; that "+
				"invalidates every signature on that revision", rev))
		}
	}

	// `owner` and `bots` give coverage without a signature, and the gate reads the register
	// from the working tree. Without this check a contributor can add themselves to
	// `owner.emails` or to `bots` in the same pull request and be covered at once: no agreement
	// sentence, no validation, nothing. Only `signatures` may grow in a pull request; that
	// route is the intended one. Shrinking is fine: removing a bot or correcting a name covers
	// nobody extra.
	before := allowlistedEmails(previous)
	var added []string
	for email := range allowlistedEmails(reg) {
		if !before[email] {
			added = append(added, email)
		}
	}
	sort.Strings(added)
	for _, email := range added {
		errs = append(errs, fmt.Sprintf("`%s` was added to `owner` or `bots` compared to `%s`; coverage "+
			"outside `signatures` belongs in a pull request of its own by the "+
			"maintainer, not in this one", email, base))
	}
	return errs, nil
}
