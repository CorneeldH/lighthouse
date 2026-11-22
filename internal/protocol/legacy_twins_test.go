package protocol

import (
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// The twins of the tests of pack/core/lib/protocol.py, which went away with the module. Each
// test names the Python tests it replaces with `twin of <name>`; the ones whose subject has no
// Go counterpart (stack_base, active_stack, fill_started, render_rebased, Comment.from_gh,
// questions_of and the old Dutch question fields) went with the code: nothing used them. These
// 23 tests of Python-only dead code have no twin on purpose: test_from_gh, test_without_reactions,
// test_render_started_with_stack, test_render_started_with_blocker,
// test_fill_started_sets_runner_workspace_and_fallback_and_keeps_the_footer,
// test_fill_started_puts_a_new_field_at_the_end_of_the_header,
// test_render_rebased_parses_as_header, test_render_rebased_follows_default_branch,
// test_render_rebased_without_session_has_no_footer, test_active_stack,
// test_active_stack_without_base_is_no_stack, test_active_stack_closed_by_rebased_or_returned,
// test_active_stack_ignores_ordinary_comments_after, test_active_stack_empty,
// test_stack_base_whatever_comes_after, test_stack_base_without_started_with_base,
// test_stack_base_last_started_with_base_wins, test_reads_legacy_started_stack,
// test_reads_english_started_stack, test_questions_of_reads_old_classification,
// test_questions_of_reads_old_bundle, test_render_question_reads_old_fields and
// test_missing_heading_table_is_loud.

const (
	twinUUID  = "0b7e3c1a-1111-4222-8333-444455556666"
	twinOther = "0b7e3c1a-9999-4222-8333-444455556666"
)

func mustHeader(t *testing.T, body string) Header {
	t.Helper()
	h, ok := ParseHeader(body)
	if !ok {
		t.Fatalf("ParseHeader(%q) found no header", body)
	}
	return h
}

// twin of test_public_names
func TestPublicNames(t *testing.T) {
	if Robot != "🤖" || Letters != "abcdef" || MaxQuestions != 4 {
		t.Errorf("Robot %q, Letters %q, MaxQuestions %d", Robot, Letters, MaxQuestions)
	}
	if !reflect.DeepEqual(artefactMarkers, []string{"<!-- lighthouse:", "<!-- paia:"}) {
		t.Errorf("artefactMarkers = %q", artefactMarkers)
	}
	want := []Kind{"handoff", "started", "question", "resumed", "done", "returned", "rebased", "parked", "repair"}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("kinds = %v, want %v", kinds, want)
	}
}

// twin of test_an_ordinary_comment_is_no_header, test_an_unknown_kind_is_no_header,
// test_a_prefix_of_a_kind_is_no_header
func TestParseHeaderRefuses(t *testing.T) {
	for _, body := range []string{"ik denk a", "🤖 Iets\nx: y", "🤖 Handoffs\nissue: 1"} {
		if h, ok := ParseHeader(body); ok {
			t.Errorf("ParseHeader(%q) = %+v, want no header", body, h)
		}
	}
}

// twin of test_fields_up_to_the_first_blank_line
func TestParseHeaderStopsAtTheFirstBlankLine(t *testing.T) {
	h := mustHeader(t, "🤖 Handoff\nissue: #12\nnext: /lighthouse-fix-pr 3\n\nnote: no longer")
	want := map[string]string{"issue": "#12", "next": "/lighthouse-fix-pr 3"}
	if h.Kind != KindHandoff || !reflect.DeepEqual(h.Fields, want) {
		t.Errorf("got %v %v, want handoff %v", h.Kind, h.Fields, want)
	}
}

// twin of test_the_question_header_with_dots, test_the_last_footer_counts, test_reads_legacy_footer
func TestParseHeaderSession(t *testing.T) {
	h := mustHeader(t, "🤖 Vraag · #12 Titel · fase plan\n\n**Vraag:** x\n<sub>session "+twinUUID+" · worktree w</sub>")
	if h.Kind != KindQuestion || h.Session != twinUUID {
		t.Errorf("question header = %v %q", h.Kind, h.Session)
	}
	body := "🤖 Vraag · #1\n\n**Wat er speelt:** <sub>session " + twinOther + "</sub>\n<sub>session " + twinUUID + "</sub>"
	if got := mustHeader(t, body).Session; got != twinUUID {
		t.Errorf("a session id in free text overrode the footer: %q", got)
	}
	if got := mustHeader(t, "🤖 Klaar\npr: x\n\n<sub>sessie "+twinUUID+"</sub>").Session; got != twinUUID {
		t.Errorf("legacy footer session = %q", got)
	}
}

// twin of test_leading_whitespace, test_english_header_parses
func TestParseHeaderLeadingWhitespaceAndEnglish(t *testing.T) {
	if got := mustHeader(t, "\n  🤖 Klaar\npr: x").Kind; got != KindDone {
		t.Errorf("leading whitespace: kind %v", got)
	}
	if got := mustHeader(t, "🤖 Started\nsession: s\n").Kind; got != KindStarted {
		t.Errorf("english: kind %v", got)
	}
}

// twin of test_every_variant_gives_the_canonical_kind, test_rebased_is_a_kind
func TestEveryVariantGivesTheCanonicalKind(t *testing.T) {
	for _, c := range []struct {
		nl, en string
		kind   Kind
	}{
		{"Handoff", "Handoff", KindHandoff}, {"Gestart", "Started", KindStarted}, {"Vraag", "Question", KindQuestion},
		{"Hervat", "Resumed", KindResumed}, {"Klaar", "Done", KindDone}, {"Teruggegeven", "Returned", KindReturned},
		{"Herbaseerd", "Rebased", KindRebased},
	} {
		for _, title := range []string{c.nl, c.en} {
			if got := mustHeader(t, "🤖 "+title+"\nx: y").Kind; got != c.kind {
				t.Errorf("%q: kind %v, want %v", title, got, c.kind)
			}
		}
	}
	if got := mustHeader(t, "🤖 Herbaseerd\nbase: abc").Kind; got != KindRebased {
		t.Errorf("rebased: kind %v", got)
	}
}

// twin of test_reads_legacy_keys, test_reads_legacy_rebased_resumed_and_returned
func TestParseHeaderReadsLegacyKeys(t *testing.T) {
	h := mustHeader(t, "🤖 Gestart\nsessie: s\ncommando: /x 1\nstapel: #3 b3\nbasis: abc\n"+
		"blokkade: verrekend\nsprong: y\nterugval: z\n")
	want := map[string]string{"session": "s", "command": "/x 1", "stack": "#3 b3", "base": "abc",
		"blocker": "accounted", "skip": "y", "fallback": "z"}
	if !reflect.DeepEqual(h.Fields, want) {
		t.Errorf("fields = %v, want %v", h.Fields, want)
	}
	for body, want := range map[string]map[string]string{
		"🤖 Herbaseerd\nvoorganger: #11\nop: origin/main": {"predecessor": "#11", "onto": "origin/main"},
		"🤖 Hervat\nantwoord: 5":                          {"answer": "5"},
		"🤖 Teruggegeven\nreden: x":                       {"reason": "x"},
	} {
		if got := mustHeader(t, body).Fields; !reflect.DeepEqual(got, want) {
			t.Errorf("%q: fields = %v, want %v", body, got, want)
		}
	}
}

func handoffHeader(t *testing.T, overrides map[string]string) Header {
	t.Helper()
	fields := map[string]string{"issue": "#12", "worktree": "/w/", "branch": "issue-12-x", "anchor": "/w/spec.md",
		"next": "/superpowers:writing-plans /w/spec.md", "when": "now"}
	for k, v := range overrides {
		if v == "" {
			delete(fields, k)
		} else {
			fields[k] = v
		}
	}
	var lines []string
	for _, k := range []string{"issue", "worktree", "branch", "anchor", "next", "when"} {
		if v, ok := fields[k]; ok {
			lines = append(lines, k+": "+v)
		}
	}
	return mustHeader(t, "🤖 Handoff\n"+strings.Join(lines, "\n"))
}

// twin of test_a_complete_handoff, test_incomplete_or_unreadable, test_no_handoff
func TestParseHandoffCompleteAndRefusals(t *testing.T) {
	h, err := ParseHandoff(handoffHeader(t, nil))
	want := Handoff{Issue: 12, Worktree: "/w", Branch: "issue-12-x", Anchor: "/w/spec.md",
		Next: "/superpowers:writing-plans /w/spec.md", When: "now"}
	if err != nil || h != want {
		t.Errorf("ParseHandoff = %+v, %v; want %+v", h, err, want)
	}
	for _, overrides := range []map[string]string{{"next": ""}, {"when": "tomorrow"}, {"issue": "twelve"}, {"worktree": ""}} {
		if _, err := ParseHandoff(handoffHeader(t, overrides)); !errors.Is(err, ErrHandoff) {
			t.Errorf("overrides %v: error = %v, want ErrHandoff", overrides, err)
		}
	}
	if _, err := ParseHandoff(mustHeader(t, "🤖 Klaar\npr: x")); !errors.Is(err, ErrHandoff) {
		t.Errorf("a done is no handoff: error = %v", err)
	}
}

// twin of test_lane_not_validated, test_handoff_with_auto_lines_parses_unchanged
func TestParseHandoffLaneAndAutoLines(t *testing.T) {
	h, err := ParseHandoff(mustHeader(t, "🤖 Handoff\nissue: 1\nworktree: w\nbranch: b\nnext: /x\nwhen: now\nlane: anything\n"))
	if err != nil || h.Lane != "anything" {
		t.Errorf("lane = %q, %v", h.Lane, err)
	}
	base := "🤖 Handoff\nissue: #12\nworktree: /w/\nbranch: issue-12-x\nanchor: /w/spec.md\n" +
		"next: /superpowers:writing-plans /w/spec.md\nwhen: now"
	auto := "Context: chosen without a question\nAuto: technical-choice — sqlite or json? → sqlite\nAuto: intent-scope — the CLI too? → yes"
	without, err1 := ParseHandoff(mustHeader(t, base+"\nContext: chosen without a question"))
	with, err2 := ParseHandoff(mustHeader(t, base+"\n"+auto))
	if err1 != nil || err2 != nil || with != without {
		t.Errorf("with auto lines %+v (%v), without %+v (%v)", with, err2, without, err1)
	}
}

// twin of test_reads_legacy_handoff, test_reads_legacy_now
func TestParseHandoffReadsLegacyValues(t *testing.T) {
	h, err := ParseHandoff(mustHeader(t, "🤖 Handoff\nissue: #12\nworktree: /w/x/\nbranch: b\nvolgende: /x\nwanneer: nacht\nanker: a\n"))
	if want := (Handoff{12, "/w/x", "b", "a", "/x", "night", "", "", ""}); err != nil || h != want {
		t.Errorf("legacy handoff = %+v, %v; want %+v", h, err, want)
	}
	h, err = ParseHandoff(mustHeader(t, "🤖 Handoff\nissue: 1\nworktree: w\nbranch: b\nvolgende: /x\nwanneer: nu\n"))
	if err != nil || h.When != "now" {
		t.Errorf("legacy now = %q, %v", h.When, err)
	}
}

// twin of test_started_reads_back (without head_sha: the Go renderer has no head field),
// test_render_started_without_stack_has_no_stack_fields,
// test_render_started_without_runner_has_no_forge_fields, test_render_started_with_runner_workspace_and_fallback
// (without fallback, which Go does not write), test_started_with_skip
func TestRenderStartedReadsBack(t *testing.T) {
	h := mustHeader(t, RenderStarted(Started{Session: twinUUID, Worktree: "/w", Command: "/lighthouse-fix-pr 7", Start: "t"}, "nl"))
	if h.Kind != KindStarted || h.Session != twinUUID || h.Fields["command"] != "/lighthouse-fix-pr 7" {
		t.Errorf("read back = %+v", h)
	}
	for _, key := range []string{"stack", "base", "branch", "blocker", "runner", "workspace", "fallback"} {
		if _, ok := h.Fields[key]; ok {
			t.Errorf("a plain started has the field %q", key)
		}
	}
	forge := mustHeader(t, RenderStarted(Started{Session: twinUUID, Worktree: "/w", Command: "c", Start: "t",
		Runner: "forge", Workspace: "ws1"}, "nl"))
	if forge.Fields["runner"] != "forge" || forge.Fields["workspace"] != "ws1" {
		t.Errorf("forge fields = %v", forge.Fields)
	}
	body := RenderStarted(Started{Session: twinUUID, Worktree: "/w", Command: "c", Start: "t", Skip: "spec-review skipped (config)"}, "nl")
	if !strings.Contains("\n"+body+"\n", "\nskip: spec-review skipped (config)\n") ||
		mustHeader(t, body).Fields["skip"] != "spec-review skipped (config)" {
		t.Errorf("skip missing in %q", body)
	}
	if strings.Contains(RenderStarted(Started{Session: twinUUID, Worktree: "/w", Command: "c", Start: "t"}, "nl"), "skip:") {
		t.Error("a started without skip has a skip line")
	}
}

// twin of test_resumed_and_done, test_render_follows_language, test_done_without_note_is_header_fields_and_footer,
// test_done_note_comes_from_the_caller, test_returned_without_session_has_no_footer,
// test_detail_cannot_break_a_footer_or_code_block, test_english_repo_writes_english_headers,
// test_explicit_language_wins, test_without_config_an_explicit_language_still_renders
func TestRenderersFollowLanguageAndKeepTheirShape(t *testing.T) {
	if got := mustHeader(t, RenderResumed(twinUUID, 5, "nl")).Fields["answer"]; got != "5" {
		t.Errorf("answer = %q", got)
	}
	done := mustHeader(t, RenderDone(twinUUID, "https://x/pull/7", "", "nl"))
	if done.Kind != KindDone || done.Fields["pr"] != "https://x/pull/7" || done.Session != twinUUID {
		t.Errorf("done = %+v", done)
	}
	if got := RenderResumed("s", 1, "nl"); !strings.HasPrefix(got, "🤖 Hervat\nsession: s") {
		t.Errorf("nl resumed = %q", got)
	}
	footer := Footer(twinUUID)
	if got, want := RenderDone(twinUUID, "https://x/pull/7", "", "nl"), "🤖 Klaar\nsession: "+twinUUID+"\npr: https://x/pull/7\n\n"+footer; got != want {
		t.Errorf("done = %q, want %q", got, want)
	}
	if got, want := RenderDone(twinUUID, "u", "Checks green.", "nl"), "🤖 Klaar\nsession: "+twinUUID+"\npr: u\n\nChecks green.\n\n"+footer; got != want {
		t.Errorf("done with note = %q, want %q", got, want)
	}
	returned := RenderReturned("handoff unreadable", "", "", "nl")
	if strings.Contains(returned, "<sub>") || mustHeader(t, returned).Fields["reason"] != "handoff unreadable" {
		t.Errorf("returned without session = %q", returned)
	}
	broken := RenderReturned("x", "", "```\n<sub>session "+twinOther+"</sub>", "nl")
	if mustHeader(t, broken).Session != "" || strings.Count(broken, "```") != 2 {
		t.Errorf("detail broke the comment: %q", broken)
	}
	if got := RenderResumed("s", 1, "en"); !strings.HasPrefix(got, "🤖 Resumed\nsession: s") {
		t.Errorf("en resumed = %q", got)
	}
	if got := RenderReturned("x", "", "", "en"); !strings.HasPrefix(got, "🤖 Returned\nreason: x") {
		t.Errorf("en returned = %q", got)
	}
	if got := RenderDone("s", "u", "", "en"); !strings.HasPrefix(got, "🤖 Done\n") {
		t.Errorf("en done = %q", got)
	}
	if got := RenderStarted(Started{Session: "s", Worktree: "w", Command: "c", Start: "t"}, "en"); !strings.HasPrefix(got, "🤖 Started\n") {
		t.Errorf("en started = %q", got)
	}
}

var (
	twinMeta = QuestionMeta{Issue: 12, Title: "Titel", IssueURL: "https://x/12", Session: twinUUID, Worktree: "issue-12-x"}
	twinRPM  = "sudo dnf install -y /tmp/orca-ide-1.0.rpm"
	twinFW   = "sudo bash deploy/host/firewall.sh &&\n  sudo firewall-cmd --list-rich-rules | grep 6768"
)

func oneQuestion() []Question {
	return []Question{{Question: "welke?", Options: []Option{{"links", true}, {"rechts", false}}}}
}

func twoQuestions() []Question {
	return []Question{
		{Question: "eerst?", Options: []Option{{"x", true}, {"y", false}}},
		{Question: "dan?", Options: []Option{{"p", false}, {"q", false}}},
	}
}

// questionFields is the base fields with one question; the callers adjust what they test.
func questionFields(mod func(*QuestionFields)) QuestionFields {
	f := QuestionFields{Where: "in de plan-fase", What: "twee wegen",
		Questions: []Question{{Question: "welke?", Options: []Option{{"links", false}}}}}
	if mod != nil {
		mod(&f)
	}
	return f
}

func questionBody(f QuestionFields, language string) string {
	return RenderQuestion(twinMeta, f, language)
}

func roundtrip(t *testing.T, f QuestionFields, language string) {
	t.Helper()
	got, ok := ParseQuestion(questionBody(f, language))
	if want := CleanFields(f); !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("roundtrip:\n got  %+v (%v)\n want %+v", got, ok, want)
	}
}

const twinQuestion = "🤖 Vraag · #12 Titel ([link](https://x/12)) · fase plan\n\n" +
	"**Waar het staat:** in de plan-fase\n**Wat er speelt:** twee wegen\n" +
	"**Vraag:** welke?\n- **a)** links *(aanbevolen)*\n- **b)** rechts\n\n" +
	"**Max 1 comment als reactie.**\n<sub>session " + twinUUID + " · worktree issue-12-x</sub>"

// twin of test_question
func TestRenderQuestionLayout(t *testing.T) {
	meta := twinMeta
	meta.HandoffURL = "https://x/12#c1"
	f := QuestionFields{Phase: "uitvoering", Task: "3/7", Where: "Taak 3 is half af.", What: "De test faalt.",
		Questions: []Question{{Question: "Welke kant op?", Options: []Option{{"links", true}, {"rechts", false}}}}}
	body := RenderQuestion(meta, f, "nl")
	for _, want := range []string{"🤖 Vraag · #12 Titel ([link](https://x/12)) · fase uitvoering · taak 3/7",
		"- **a)** links *(aanbevolen)*", "- **b)** rechts"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if lines[len(lines)-2] != "**Max 1 comment als reactie.**" {
		t.Errorf("the close line is %q", lines[len(lines)-2])
	}
	if want := "<sub>session " + twinUUID + " · worktree issue-12-x · handoff https://x/12#c1</sub>"; lines[len(lines)-1] != want {
		t.Errorf("footer = %q, want %q", lines[len(lines)-1], want)
	}
	if h := mustHeader(t, body); h.Kind != KindQuestion || h.Session != twinUUID {
		t.Errorf("header = %+v", h)
	}
}

// twin of test_legacy_question_parses_as_bundle_of_one, test_one_question_renders_as_before,
// test_english_question_parses_in_a_dutch_repo, test_roundtrip_in_english
func TestOneQuestionAndItsLegacyForm(t *testing.T) {
	legacy := strings.Replace(twinQuestion, "<sub>session", "<sub>sessie", 1)
	got, ok := ParseQuestion(legacy)
	if !ok || !reflect.DeepEqual(got.Questions, oneQuestion()) {
		t.Errorf("legacy question = %+v (%v)", got.Questions, ok)
	}
	f := questionFields(func(f *QuestionFields) { f.Phase, f.Questions = "plan", oneQuestion() })
	if got := questionBody(f, "nl"); got != twinQuestion {
		t.Errorf("one question =\n%s\nwant\n%s", got, twinQuestion)
	}
	en := questionBody(f, "en")
	for _, want := range []string{"🤖 Question · #12 Titel ([link](https://x/12)) · phase plan", "**Where it stands:** in de plan-fase",
		"- **a)** links *(recommended)*", "**At most 1 comment in reply.**"} {
		if !strings.Contains(en, want) {
			t.Errorf("english body lacks %q", want)
		}
	}
	if got, ok := ParseQuestion(en); !ok || !reflect.DeepEqual(got, CleanFields(f)) {
		t.Errorf("english question parsed as %+v (%v)", got, ok)
	}
	f.Commands = []string{twinRPM}
	roundtrip(t, f, "en")
}

// twin of test_bundle_numbers_questions_and_options
func TestBundleNumbersQuestionsAndOptions(t *testing.T) {
	body := questionBody(questionFields(func(f *QuestionFields) { f.Questions = twoQuestions() }), "nl")
	for _, want := range []string{"**Vraag 1:** eerst?", "**Vraag 2:** dan?", "\n\n**Vraag 2:**", "- **1a)** x *(aanbevolen)*",
		"- **2b)** q", "Antwoord per vraag, bijvoorbeeld `1a, 2c`."} {
		if !strings.Contains(body, want) {
			t.Errorf("bundle lacks %q:\n%s", want, body)
		}
	}
	// Without a blank line, question 2 would hang as a lazy continuation in the last option.
	for _, unwanted := range []string{"\n\n**Vraag 1:**", "**Vraag:**"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("bundle holds %q", unwanted)
		}
	}
}

// twin of test_roundtrip_maximal_bundle, test_roundtrip_one_option, test_roundtrip_six_options_with_recommended,
// test_roundtrip_without_phase, test_free_text_with_asterisks, test_multiline_option, test_blank_line_in_last_option
func TestQuestionRoundtrips(t *testing.T) {
	var maximal []Question
	for n := 1; n <= 4; n++ {
		q := Question{Question: "vraag " + string(rune('0'+n)) + "\n\nmet lege regel"}
		for _, letter := range "abcdef" {
			q.Options = append(q.Options, Option{Text: "optie " + string(rune('0'+n)) + string(letter) + "\ntweede regel", Recommended: letter == 'c'})
		}
		maximal = append(maximal, q)
	}
	var six []Option
	for _, letter := range "abcdef" {
		six = append(six, Option{Text: "optie " + string(letter), Recommended: letter == 'b'})
	}
	for name, f := range map[string]QuestionFields{
		"maximal bundle": questionFields(func(f *QuestionFields) { f.Phase, f.Task, f.Questions = "uitvoering", "2/5", maximal }),
		"one option":     questionFields(func(f *QuestionFields) { f.Phase = "plan" }),
		"six options":    questionFields(func(f *QuestionFields) { f.Phase, f.Task, f.Questions[0].Options = "uitvoering", "3/7", six }),
		"without phase":  questionFields(nil),
		"asterisks": questionFields(func(f *QuestionFields) {
			f.Questions[0] = Question{Question: "kies **a** of **b**?", Options: []Option{{"**vet** bedoeld", false}}}
		}),
		"multiline": questionFields(func(f *QuestionFields) {
			f.Questions[0].Options = []Option{{"eerste regel\ntweede regel", true}, {"andere", false}}
		}),
		"blank in option": questionFields(func(f *QuestionFields) { f.Questions[0].Options = []Option{{"a\n\nb", true}} }),
	} {
		t.Run(name, func(t *testing.T) { roundtrip(t, f, "nl") })
	}
	if got, _ := ParseQuestion(questionBody(questionFields(nil), "nl")); got.Phase != "?" {
		t.Errorf("a question without phase parsed as %q", got.Phase)
	}
}

// twin of test_code_block_is_cleaned, test_blank_line_in_free_text
func TestQuestionFreeTextIsCleanedAndKeepsBlankLines(t *testing.T) {
	f := questionFields(func(f *QuestionFields) { f.What = "zie\n```\nx\n```" })
	got, _ := ParseQuestion(questionBody(f, "nl"))
	if got.What != "zie\n'''\nx\n'''" || !reflect.DeepEqual(got, CleanFields(f)) {
		t.Errorf("what = %q", got.What)
	}
	f = questionFields(func(f *QuestionFields) { f.What = "a\n\nb" })
	got, _ = ParseQuestion(questionBody(f, "nl"))
	if got.What != "a\n\nb" || !reflect.DeepEqual(got, CleanFields(f)) {
		t.Errorf("what = %q", got.What)
	}
}

// twin of test_no_question_gives_none
func TestParseQuestionRefusesOtherComments(t *testing.T) {
	for _, body := range []string{"ik denk a", RenderDone(twinUUID, "u", "", "nl")} {
		if _, ok := ParseQuestion(body); ok {
			t.Errorf("ParseQuestion(%q) found a question", body)
		}
	}
}

var shapeFence = regexp.MustCompile(`^( *)(` + "`{3,}|~{3,}" + `)(.*)$`)

// shapeErrors is markdown_shape_errors of the Python test: no enclosing code block, no indented
// code block outside a fence, every fence closed, every command literally as a fenced block.
func shapeErrors(text string, commands ...string) []string {
	var errs []string
	lines := strings.Split(text, "\n")
	first := ""
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			first = l
			break
		}
	}
	if shapeFence.MatchString(first) {
		errs = append(errs, "the text starts with a code block")
	}
	var blocks, content []string
	open, indent, char := false, 0, ""
	for _, line := range lines {
		if open {
			bare := strings.TrimSpace(line)
			if bare != "" && strings.Trim(bare, char[:1]) == "" && len(bare) >= len(char) {
				blocks = append(blocks, strings.Join(content, "\n"))
				open, content = false, nil
			} else {
				if len(line) >= indent && strings.TrimSpace(line[:indent]) == "" {
					line = line[indent:]
				}
				content = append(content, line)
			}
			continue
		}
		if m := shapeFence.FindStringSubmatch(line); m != nil {
			open, indent, char = true, len(m[1]), m[2]
		} else if strings.HasPrefix(line, "    ") && strings.TrimSpace(line) != "" {
			errs = append(errs, "indented code block: "+line)
		}
	}
	if open {
		errs = append(errs, "a code block does not close")
	}
	for _, c := range commands {
		found := false
		for _, b := range blocks {
			found = found || b == c
		}
		if !found {
			errs = append(errs, "command not as code block: "+c)
		}
	}
	return errs
}

// twin of test_without_commands_no_code_block_and_valid_shape,
// test_every_command_stands_literally_as_its_own_code_block, test_commands_stand_between_question_and_options,
// test_a_command_with_backticks_gets_a_longer_fence, test_free_text_cannot_open_a_tilde_code_block
func TestQuestionShape(t *testing.T) {
	plain := questionBody(questionFields(nil), "nl")
	if strings.Contains(plain, "```") || strings.Contains(plain, "**Zelf draaien:**") || len(shapeErrors(plain)) != 0 {
		t.Errorf("plain question: %q %v", plain, shapeErrors(plain))
	}
	body := questionBody(questionFields(func(f *QuestionFields) { f.Commands = []string{twinRPM, twinFW} }), "nl")
	if !strings.Contains(body, "**Zelf draaien:**\n\n```bash\n"+twinRPM+"\n```") || len(shapeErrors(body, twinRPM, twinFW)) != 0 {
		t.Errorf("commands as blocks: %v\n%s", shapeErrors(body, twinRPM, twinFW), body)
	}
	one := questionBody(questionFields(func(f *QuestionFields) { f.Commands = []string{twinRPM} }), "nl")
	if q, c, o := strings.Index(one, "**Vraag:**"), strings.Index(one, twinRPM), strings.Index(one, "- **a)**"); q >= c || c >= o {
		t.Errorf("the command is not between question and options:\n%s", one)
	}
	cmd := "echo '```' && printf '%s' \"$(date)\""
	ticks := questionBody(questionFields(func(f *QuestionFields) { f.Commands = []string{cmd} }), "nl")
	if !strings.Contains(ticks, "````bash\n"+cmd+"\n````") || len(shapeErrors(ticks, cmd)) != 0 {
		t.Errorf("fence of a command with backticks: %v\n%s", shapeErrors(ticks, cmd), ticks)
	}
	tilde := questionBody(questionFields(func(f *QuestionFields) { f.What = "zie\n~~~\nx" }), "nl")
	if strings.Contains(tilde, "~~~") || len(shapeErrors(tilde)) != 0 {
		t.Errorf("a tilde fence in free text survived:\n%s", tilde)
	}
}

// twin of test_roundtrip_with_commands, test_a_command_that_looks_like_an_option_stays_a_command
func TestQuestionCommandsRoundtrip(t *testing.T) {
	roundtrip(t, questionFields(func(f *QuestionFields) { f.Commands = []string{twinRPM, twinFW} }), "nl")
	cmd := "- **a)** geen optie"
	got, _ := ParseQuestion(questionBody(questionFields(func(f *QuestionFields) { f.Commands = []string{cmd} }), "nl"))
	if !reflect.DeepEqual(got.Commands, []string{cmd}) || len(got.Questions[0].Options) != 1 || got.Questions[0].Options[0].Text != "links" {
		t.Errorf("commands %q, options %+v", got.Commands, got.Questions[0].Options)
	}
}

const twinQuestionWithCommand = "🤖 Vraag · #12 Titel ([link](https://x/12)) · fase plan\n\n" +
	"**Waar het staat:** in de plan-fase\n**Wat er speelt:** twee wegen\n" +
	"**Vraag:** welke?\n\n**Zelf draaien:**\n\n```bash\nsudo dnf install -y /tmp/orca-ide-1.0.rpm\n```\n\n" +
	"- **a)** links *(aanbevolen)*\n- **b)** rechts\n\n" +
	"**Max 1 comment als reactie.**\n<sub>session " + twinUUID + " · worktree issue-12-x</sub>"

// twin of test_one_question_with_command, test_question_with_command_parses_as_bundle_of_one
func TestOneQuestionWithCommand(t *testing.T) {
	f := questionFields(func(f *QuestionFields) { f.Phase, f.Questions, f.Commands = "plan", oneQuestion(), []string{twinRPM} })
	if got := questionBody(f, "nl"); got != twinQuestionWithCommand {
		t.Errorf("body =\n%s\nwant\n%s", got, twinQuestionWithCommand)
	}
	got, ok := ParseQuestion(twinQuestionWithCommand)
	if !ok || !reflect.DeepEqual(got.Commands, []string{twinRPM}) || !reflect.DeepEqual(got.Questions, oneQuestion()) {
		t.Errorf("parsed = %+v (%v)", got, ok)
	}
}

// twin of test_bundle_puts_the_block_before_question_1, test_bundle_with_commands_roundtrips,
// test_bundle_with_commands_has_valid_markdown_shape
func TestBundleWithCommands(t *testing.T) {
	f := questionFields(func(f *QuestionFields) {
		f.Phase, f.Questions, f.Commands = "plan", twoQuestions(), []string{twinRPM, twinFW}
	})
	body := questionBody(f, "nl")
	order := []int{strings.Index(body, "**Wat er speelt:**"), strings.Index(body, "**Zelf draaien:**"),
		strings.Index(body, twinRPM), strings.Index(body, "**Vraag 1:**"), strings.Index(body, "**Vraag 2:**")}
	for i := 1; i < len(order); i++ {
		if order[i-1] < 0 || order[i-1] >= order[i] {
			t.Errorf("order of the bundle = %v:\n%s", order, body)
		}
	}
	if strings.Count(body, "**Zelf draaien:**") != 1 {
		t.Error("the run-yourself block is not unique")
	}
	if strings.Index(body, "Antwoord per vraag, bijvoorbeeld `1a, 2c`.") < strings.Index(body, "**Max 1 comment als reactie.**") {
		t.Error("the bundle hint stands before the close line")
	}
	roundtrip(t, f, "nl")
	if errs := shapeErrors(body, twinRPM, twinFW); len(errs) != 0 {
		t.Errorf("shape errors: %v", errs)
	}
}
