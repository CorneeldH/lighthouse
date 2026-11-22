package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sigText = `{"name":"Jane Doe","github_login":"janedoe","emails":["jane@example.com"],` +
	`"date":"2026-09-01","cla_revision":"2026-08-24",` +
	`"agreement":"I have read the CLA document and I hereby sign the CLA."}`

const johnText = `{"name":"John Roe","github_login":"johnroe","emails":["john@example.com"],` +
	`"date":"2026-09-02","cla_revision":"2026-08-24",` +
	`"agreement":"I have read the CLA document and I hereby sign the CLA."}`

// registerText is a valid register with one signature in it.
const registerText = `{
  "cla_revisions": ["2026-08-24"],
  "current_cla_revision": "2026-08-24",
  "owner": {"name": "Corneel den Hartogh", "github_login": "CorneeldH", "emails": ["owner@example.com"]},
  "bots": [{"login": "somebot[bot]", "email": "bot@example.com"}],
  "signatures": [` + sigText + `]
}
`

// edited returns registerText with one replacement; a replacement that matches nothing fails the
// test instead of silently producing the valid register.
func edited(t *testing.T, old, repl string) string {
	t.Helper()
	if !strings.Contains(registerText, old) {
		t.Fatalf("fixture does not contain %q", old)
	}
	return strings.Replace(registerText, old, repl, 1)
}

func mustDecode(t *testing.T, text string) Register {
	t.Helper()
	reg, err := decodeRegister([]byte(text))
	if err != nil {
		t.Fatalf("decodeRegister: %v", err)
	}
	return reg
}

func TestShippedRegisterIsValid(t *testing.T) {
	reg, err := loadRegister(filepath.Join("..", "..", ".github", "cla-signatures.json"))
	if err != nil {
		t.Fatal(err)
	}
	if errs := validateRegister(reg); len(errs) != 0 {
		t.Fatalf("shipped register is invalid: %v", errs)
	}
}

func TestValidateRegister(t *testing.T) {
	const agreement = "I have read the CLA document and I hereby sign the CLA."
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"valid", registerText, nil},
		{"unknown fields are ignored", edited(t, `"bots"`, `"extra": 1, "bots"`), nil},
		{"non-string emails are ignored", edited(t, `["jane@example.com"]`, `[5, null, "jane@example.com"]`), nil},
		{"duplicate key, the last wins", edited(t, `"agreement":"`+agreement+`"`, `"agreement":"nope","agreement":"`+agreement+`"`), nil},
		{"missing field", edited(t, `"github_login":"janedoe",`, ``),
			[]string{"signatures[0]: field `github_login` is missing or empty"}},
		{"empty field", edited(t, `"name":"Jane Doe"`, `"name":"  "`),
			[]string{"signatures[0]: field `name` is missing or empty"}},
		{"wrong agreement", edited(t, agreement, "I agree."),
			[]string{"signatures[0]: `agreement` must be exactly '" + agreement + "'"}},
		{"near-miss agreement, no full stop", edited(t, `sign the CLA."`, `sign the CLA"`),
			[]string{"signatures[0]: `agreement` must be exactly '" + agreement + "'"}},
		{"unknown revision", edited(t, `"cla_revision":"2026-08-24"`, `"cla_revision":"1999-01-01"`),
			[]string{"signatures[0]: unknown `cla_revision` '1999-01-01'"}},
		{"malformed date", edited(t, `"2026-09-01"`, `"1 september 2026"`),
			[]string{"signatures[0]: `date` '1 september 2026' is not YYYY-MM-DD"}},
		{"date without zero padding", edited(t, `"2026-09-01"`, `"2026-9-01"`),
			[]string{"signatures[0]: `date` '2026-9-01' is not YYYY-MM-DD"}},
		{"date with trailing newline", edited(t, `"2026-09-01"`, `"2026-09-01\n"`),
			[]string{`signatures[0]: ` + "`date`" + ` '2026-09-01\n' is not YYYY-MM-DD`}},
		{"date with non-ASCII digits", edited(t, `"2026-09-01"`, `"٢٠٢٦-٠٩-٠١"`),
			[]string{"signatures[0]: `date` '٢٠٢٦-٠٩-٠١' is not YYYY-MM-DD"}},
		{"email of the owner", edited(t, `["jane@example.com"]`, `["Owner@Example.com"]`),
			[]string{"signatures[0]: owner@example.com is already an owner address"}},
		{"empty emails", edited(t, `["jane@example.com"]`, `[]`),
			[]string{"signatures[0]: field `emails` is missing or empty"}},
		{"email claimed twice, case-insensitive",
			edited(t, `"signatures": [`, `"signatures": [`+strings.Replace(johnText, "john@", "JANE@", 1)+`,`),
			[]string{"signatures[1]: jane@example.com is already claimed by signatures[0]"}},
		{"signature that is not an object", edited(t, `"signatures": [`, `"signatures": ["x", `),
			[]string{"signatures[0]: not an object"}},
		{"current revision not listed", edited(t, `"current_cla_revision": "2026-08-24"`, `"current_cla_revision": "2027-01-01"`),
			[]string{"register: `current_cla_revision` '2027-01-01' is not in `cla_revisions`"}},
		{"current revision absent", edited(t, `"current_cla_revision": "2026-08-24",`, ``),
			[]string{"register: `current_cla_revision` None is not in `cla_revisions`"}},
		{"no revisions", edited(t, `"cla_revisions": ["2026-08-24"]`, `"cla_revisions": []`),
			[]string{"register: `cla_revisions` is missing or empty"}},
		{"owner without emails", edited(t, `"emails": ["owner@example.com"]`, `"emails": []`),
			[]string{"register: `owner` is missing or has no `emails`"}},
		{"bots not a list", edited(t, `[{"login": "somebot[bot]", "email": "bot@example.com"}]`, `{}`),
			[]string{"register: `bots` is missing or not a list"}},
		{"signatures not a list", edited(t, `[`+sigText+`]`, `{}`),
			[]string{"register: `signatures` is missing or not a list"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validateRegister(mustDecode(t, tc.text))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadRegister(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	tests := []struct {
		name string
		path string
		want string // substring of the error; empty means no error
	}{
		{"valid", write("ok.json", registerText), ""},
		{"broken json", write("broken.json", "{not json"), "broken.json is not valid JSON: "},
		{"top level is a list", write("list.json", "[]"), "list.json is not valid JSON: "},
		{"top level is null", write("null.json", "null"), "null.json is not valid JSON: "},
		{"trailing garbage", write("trail.json", "{} {}"), "trail.json is not valid JSON: "},
		{"invalid UTF-8", write("utf8.json", "{\"a\":\"\xff\"}"), "utf8.json is not valid JSON: invalid UTF-8"},
		{"missing", filepath.Join(dir, "absent.json"), "absent.json does not exist"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadRegister(tc.path)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("error %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestUnsigned(t *testing.T) {
	reg := mustDecode(t, registerText)
	tests := []struct {
		name string
		in   []Identity
		want []string // emails of the missing identities
	}{
		{"owner", []Identity{{"owner@example.com", "O", "author", "a"}}, nil},
		{"bot", []Identity{{"bot@example.com", "B", "author", "a"}}, nil},
		{"signer", []Identity{{"jane@example.com", "J", "author", "a"}}, nil},
		{"stranger", []Identity{{"mallory@example.com", "M", "author", "a"}}, []string{"mallory@example.com"}},
		{"empty address is never covered", []Identity{{"", "Nobody", "author", "a"}}, []string{""}},
		{"one address is reported once", []Identity{
			{"mallory@example.com", "M", "author", "a"},
			{"mallory@example.com", "M", "committer", "a"},
			{"mallory@example.com", "M", "author", "b"},
			{"eve@example.com", "E", "author", "b"},
		}, []string{"mallory@example.com", "eve@example.com"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, id := range unsigned(tc.in, reg) {
				got = append(got, id.Email)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUnsignedKeepsTheFirstIdentityPerAddress(t *testing.T) {
	got := unsigned([]Identity{
		{"m@example.com", "First", "author", "aaa"},
		{"m@example.com", "Second", "committer", "bbb"},
	}, mustDecode(t, registerText))
	want := []Identity{{"m@example.com", "First", "author", "aaa"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// --- collecting identities: on throwaway repositories ---------------------------------------

func TestCollectIdentitiesAuthorAndCommitter(t *testing.T) {
	repo := rangeRepo(t)
	commit(t, repo, "work", bob, alice)
	got := collect(t, repo)
	want := []Identity{
		{"bob@example.com", "Bob Example", "author", anySHA},
		{"alice@example.com", "Alice Example", "committer", anySHA},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCollectIdentitiesLowercasesEmail(t *testing.T) {
	repo := rangeRepo(t)
	commit(t, repo, "work", person{"Bob Example", "BOB@Example.com"}, person{"Bob Example", "BOB@Example.com"})
	for _, id := range collect(t, repo) {
		if id.Email != "bob@example.com" {
			t.Errorf("%s email %q is not lowercased", id.Role, id.Email)
		}
	}
}

func TestIgnoresNoreplyGithubCommitter(t *testing.T) {
	repo := rangeRepo(t)
	commit(t, repo, "web merge", alice, person{"GitHub", "noreply@github.com"})
	got := collect(t, repo)
	want := []Identity{{"alice@example.com", "Alice Example", "author", anySHA}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCollectIdentitiesLeavesTheBaseOut(t *testing.T) {
	repo := rangeRepo(t)
	if got := collect(t, repo); len(got) != 0 {
		t.Fatalf("got %v, want nothing", got)
	}
}

func TestCoAuthorTrailerCounts(t *testing.T) {
	repo := rangeRepo(t)
	commit(t, repo, "work\n\nCo-authored-by: Bob Example <Bob@Example.com>\n", alice, alice)
	var got []Identity
	for _, id := range collect(t, repo) {
		if id.Role == "co-author" {
			got = append(got, id)
		}
	}
	want := []Identity{{"bob@example.com", "Bob Example", "co-author", anySHA}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCoAuthorTrailerIsMatchedInAnyLetterCase(t *testing.T) {
	repo := rangeRepo(t)
	commit(t, repo, "work\n\nCO-AUTHORED-BY: Bob <bob@example.com>\nco-Authored-By:Carol<carol@example.com>\n", alice, alice)
	var got []string
	for _, id := range collect(t, repo) {
		if id.Role == "co-author" {
			got = append(got, id.Name+"<"+id.Email+">")
		}
	}
	want := []string{"Bob<bob@example.com>", "Carol<carol@example.com>"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCoAuthorTrailerWithCRLFLineEndings(t *testing.T) {
	repo := rangeRepo(t)
	commitMessageFile(t, repo, "work\r\n\r\nCo-authored-by: Bob Example <bob@example.com>\r\n", alice)
	var got []string
	for _, id := range collect(t, repo) {
		if id.Role == "co-author" {
			got = append(got, id.Email)
		}
	}
	if want := []string{"bob@example.com"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCoAuthorTrailerMustStandOnItsOwnLine(t *testing.T) {
	repo := rangeRepo(t)
	commit(t, repo, "work, see Co-authored-by: Bob <bob@example.com> in the text\n", alice, alice)
	for _, id := range collect(t, repo) {
		if id.Role == "co-author" {
			t.Fatalf("a mention inside a sentence is no trailer: %v", id)
		}
	}
}

func TestMessageWithX1eByteDoesNotBreakParsing(t *testing.T) {
	repo := rangeRepo(t)
	commitMessageFile(t, repo, "work \x1e work\n\nCo-authored-by: Bob Example <bob@example.com>\n", alice)
	var got []string
	for _, id := range collect(t, repo) {
		if id.Role == "co-author" {
			got = append(got, id.Email)
		}
	}
	if want := []string{"bob@example.com"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestX1fByteInAuthorNameCannotHideTheRealAddress(t *testing.T) {
	repo := rangeRepo(t)
	writeFile(t, filepath.Join(repo, "file.txt"), "sneaky")
	git(t, repo, nil, "add", "file.txt")
	git(t, repo, []string{
		"GIT_AUTHOR_NAME=X\x1fowner@example.com", "GIT_AUTHOR_EMAIL=evil@example.com",
		"GIT_COMMITTER_NAME=owner@example.com", "GIT_COMMITTER_EMAIL=alice@example.com",
	}, "commit", "-q", "-m", "sneaky")
	var got []string
	for _, id := range collect(t, repo) {
		got = append(got, id.Role+":"+id.Email)
	}
	want := []string{"author:evil@example.com", "committer:alice@example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEmptyCommitMessageStillYieldsTheAuthor(t *testing.T) {
	repo := rangeRepo(t)
	writeFile(t, filepath.Join(repo, "file.txt"), "empty")
	git(t, repo, nil, "add", "file.txt")
	msg := filepath.Join(t.TempDir(), "empty.txt")
	writeFile(t, msg, "")
	git(t, repo, bob.env(), "commit", "-q", "--allow-empty-message", "-F", msg)
	got := collect(t, repo)
	want := []Identity{
		{"bob@example.com", "Bob Example", "author", anySHA},
		{"bob@example.com", "Bob Example", "committer", anySHA},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// git itself cuts a message at a NUL, so a desynchronised stream cannot be made with a commit;
// the six-field check is exercised on the stream directly.
func TestParseLogRefusesADesynchronisedStream(t *testing.T) {
	_, err := parseLog("aaa\x00Name\x00mail\x00")
	want := "git log gave 3 NUL-separated fields, not a multiple of 6; the output cannot be trusted"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

func TestParseLogEmptyStream(t *testing.T) {
	ids, err := parseLog("")
	if err != nil || len(ids) != 0 {
		t.Fatalf("got %v, %v", ids, err)
	}
}

func TestCollectIdentitiesUnknownBase(t *testing.T) {
	repo := rangeRepo(t)
	_, err := collectIdentities(repo, "origin/does-not-exist", "HEAD")
	if err == nil || !strings.HasPrefix(err.Error(), "git log -z --format=") ||
		!strings.Contains(err.Error(), "failed (exit 128): ") {
		t.Fatalf("got %v", err)
	}
}

// --- the report ----------------------------------------------------------------------------

func TestRenderReportExactText(t *testing.T) {
	got := renderReport([]Identity{{"mallory@example.com", "Mallory", "author", "abc1234"}}, "https://github.com/Other/repo")
	want := "## CLA: not every contributor has signed\n" +
		"\n" +
		"| Address | Name | Role | Commit |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `mallory@example.com` | `Mallory` | author | `abc1234` |\n" +
		"\n" +
		"Every address above must be covered before this pull request can be merged.\n" +
		"Signing takes one commit: add your own entry to `.github/cla-signatures.json`.\n" +
		"The format and the two routes are in\n" +
		"[`CLA.md`](https://github.com/Other/repo/blob/main/CLA.md),\n" +
		"section 11 — you may sign in this pull request.\n" +
		"\n" +
		"If an address above is yours but you do not recognise it, it is probably the\n" +
		"address in your git configuration (`git config user.email`).\n" +
		"Add the address as it stands here, or change your commits.\n" +
		"\n" +
		"An empty address comes from a commit without an author email. It cannot be\n" +
		"covered and must be rewritten (`git commit --amend --reset-author`).\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderReportEmptyAddressAndName(t *testing.T) {
	got := renderReport([]Identity{{"", "", "author", "abc1234"}}, "https://github.com/X/y")
	if !strings.Contains(got, "| `*(empty)*` | `*(empty)*` | author | `abc1234` |\n") {
		t.Fatalf("no empty markers in:\n%s", got)
	}
}

func TestCraftedNameCannotBreakTheReportTable(t *testing.T) {
	got := renderReport([]Identity{{"evil`@example.com", "Mallory | **nothing to see here**\nextra\x1fline", "author", "abc1234"}}, "https://github.com/X/y")
	var row string
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "Mallory") {
			row = line
		}
	}
	want := "| `evil'@example.com` | `Mallory \\| **nothing to see here** extra line` | author | `abc1234` |"
	if row != want {
		t.Fatalf("got  %q\nwant %q", row, want)
	}
}

func TestRepoURL(t *testing.T) {
	dir := t.TempDir()
	if _, err := repoURL(dir); err == nil || !strings.Contains(err.Error(), "lighthouse.toml") {
		t.Fatalf("missing file: got %v", err)
	}
	writeFile(t, filepath.Join(dir, "lighthouse.toml"), "[forge]\nplatform = \"github\"\n")
	if _, err := repoURL(dir); err == nil || !strings.Contains(err.Error(), "`[forge] repo` is missing") {
		t.Fatalf("missing key: got %v", err)
	}
	writeFile(t, filepath.Join(dir, "lighthouse.toml"), "[forge]\nrepo = \"Other/repo\"\n")
	got, err := repoURL(dir)
	if err != nil || got != "https://github.com/Other/repo" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// --- the register guard --------------------------------------------------------------------

func TestCheckAppendOnly(t *testing.T) {
	const base = "base"
	tests := []struct {
		name string
		head string
		want []string
	}{
		{"unchanged", registerText, nil},
		{"adding a signature", edited(t, `"signatures": [`, `"signatures": [`+johnText+`,`), nil},
		{"removing a bot", edited(t, `[{"login": "somebot[bot]", "email": "bot@example.com"}]`, `[]`), nil},
		{"removing a signature", edited(t, `[`+sigText+`]`, `[]`),
			[]string{"the signature of `janedoe` was removed or changed compared to `base`"}},
		{"editing a signature", edited(t, `["jane@example.com"]`, `["jane@example.com","sneaky@example.com"]`),
			[]string{"the signature of `janedoe` was removed or changed compared to `base`"}},
		{"dropping a revision", edited(t, `"cla_revisions": ["2026-08-24"]`, `"cla_revisions": ["2027-01-01"]`),
			[]string{"revision `2026-08-24` was removed from `cla_revisions`; that invalidates every signature on that revision"}},
		{"adding yourself to the owner", edited(t, `["owner@example.com"]`, `["owner@example.com", "Mallory@example.com"]`),
			[]string{"`mallory@example.com` was added to `owner` or `bots` compared to `base`; coverage outside `signatures` belongs in a pull request of its own by the maintainer, not in this one"}},
		{"adding yourself as a bot", edited(t, `"email": "bot@example.com"}]`, `"email": "bot@example.com"},{"login":"x[bot]","email":"mallory@example.com"}]`),
			[]string{"`mallory@example.com` was added to `owner` or `bots` compared to `base`; coverage outside `signatures` belongs in a pull request of its own by the maintainer, not in this one"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo, path := repoWithRegister(t, registerText)
			writeFile(t, path, tc.head)
			got, err := checkAppendOnly(repo, base, path, mustDecode(t, tc.head))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCheckAppendOnlyReorderedSignaturesPass(t *testing.T) {
	two := edited(t, `"signatures": [`, `"signatures": [`+johnText+`,`)
	repo, path := repoWithRegister(t, two)
	reversed := strings.Replace(two, `"signatures": [`+johnText+`,`+sigText, `"signatures": [`+sigText+`,`+johnText, 1)
	if reversed == two {
		t.Fatal("fixture was not reordered")
	}
	writeFile(t, path, reversed)
	got, err := checkAppendOnly(repo, "base", path, mustDecode(t, reversed))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestCheckAppendOnlyKeepsLargeNumbersExact(t *testing.T) {
	baseText := edited(t, `"signatures": [`, `"signatures": [`+strings.Replace(johnText, `{"name"`, `{"n":12345678901234567890,"name"`, 1)+`,`)
	repo, path := repoWithRegister(t, baseText)
	head := strings.Replace(baseText, "12345678901234567890", "12345678901234567891", 1)
	writeFile(t, path, head)
	got, err := checkAppendOnly(repo, "base", path, mustDecode(t, head))
	want := []string{"the signature of `johnroe` was removed or changed compared to `base`"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}

func TestCheckAppendOnlyRegisterAbsentOnBase(t *testing.T) {
	repo := rangeRepo(t)
	path := filepath.Join(repo, ".github", "cla-signatures.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, registerText)
	got, err := checkAppendOnly(repo, "base", path, mustDecode(t, registerText))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestCheckAppendOnlyRegisterOutsideTheRepo(t *testing.T) {
	repo := rangeRepo(t)
	path := filepath.Join(t.TempDir(), "cla-signatures.json")
	writeFile(t, path, registerText)
	got, err := checkAppendOnly(repo, "base", path, mustDecode(t, registerText))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestCheckAppendOnlyUnknownBaseIsAnError(t *testing.T) {
	repo, path := repoWithRegister(t, registerText)
	_, err := checkAppendOnly(repo, "origin/does-not-exist", path, mustDecode(t, registerText))
	if err == nil || !strings.Contains(err.Error(), "failed (exit 128)") {
		t.Fatalf("got %v", err)
	}
}

func TestCheckAppendOnlyBrokenRegisterOnBase(t *testing.T) {
	repo, path := repoWithRegister(t, "{not json")
	got, err := checkAppendOnly(repo, "base", path, mustDecode(t, registerText))
	if err != nil || len(got) != 1 || !strings.HasPrefix(got[0], "the register on `base` is not valid JSON: ") {
		t.Fatalf("got %q, %v", got, err)
	}
}
