package pusher

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// InIntentClass reports whether every added or changed workflow of c stays inside the class
// intent of spec §1 (#551): it existed on the merge-base and gains no event, action, permission,
// secret, runner, environment or image, and does not lose its post-merge-only trigger. Both
// sides are read in the pusher's own clone. A deleted workflow does not count. Anything it
// cannot read is false; the string names the first requirement that fails, for the log.
func InIntentClass(ctx context.Context, req Request, c Checked) (bool, string) {
	oldEntries, err := workflowEntries(ctx, req.CloneDir, c.MergeBase)
	if err != nil {
		return false, "cannot list the workflows on the merge-base: " + err.Error()
	}
	newEntries, err := workflowEntries(ctx, req.CloneDir, c.SHA)
	if err != nil {
		return false, "cannot list the workflows on the SHA: " + err.Error()
	}
	for _, p := range c.Files {
		if !isWorkflow(p) {
			continue
		}
		ne, ok := newEntries[p]
		if !ok {
			continue // deleted
		}
		oe, ok := oldEntries[p]
		if !ok {
			return false, p + " is a new workflow"
		}
		old, why := readParsed(ctx, req.CloneDir, oe)
		if why != "" {
			return false, p + " on the merge-base " + why
		}
		cur, why := readParsed(ctx, req.CloneDir, ne)
		if why != "" {
			return false, p + " " + why
		}
		if ok, why := intentOK(old, cur, req.Default); !ok {
			return false, p + ": " + why
		}
	}
	return true, ""
}

// readParsed reads and parses one workflow entry; a non-empty string is why it could not.
func readParsed(ctx context.Context, clone string, e treeEntry) (map[string]any, string) {
	text, reason, err := readWorkflow(ctx, clone, e)
	switch {
	case err != nil:
		return nil, "is unreadable: " + err.Error()
	case reason != "":
		return nil, reason
	}
	top, err := parseWorkflow(text)
	if err != nil {
		return nil, "does not parse: " + err.Error()
	}
	return top, ""
}

// intentOK compares one workflow on the merge-base (old) with the same file on the SHA (cur)
// against the requirements of spec §1; def is the default branch.
func intentOK(old, cur map[string]any, def string) (bool, string) {
	o, why := extract(old)
	if why != "" {
		return false, "on the merge-base " + why
	}
	n, why := extract(cur)
	if why != "" {
		return false, why
	}
	if extra := missing(n.events, o.events); extra != "" {
		return false, "new event " + extra
	}
	if postMerge(o.evs, def) && !postMerge(n.evs, def) {
		return false, "ran only post-merge on the merge-base and can run pre-merge now"
	}
	if extra := missing(n.uses, o.uses); extra != "" {
		return false, "new action " + extra
	}
	if why := wider(o, n); why != "" {
		return false, why
	}
	for _, r := range []struct {
		what     string
		cur, old map[string]bool
	}{
		{"secret", n.secrets, o.secrets},
		{"token", n.tokens, o.tokens},
		{"runner", n.runners, o.runners},
		{"environment", n.envs, o.envs},
		{"image", n.images, o.images},
	} {
		if extra := missing(r.cur, r.old); extra != "" {
			return false, "new " + r.what + " " + extra
		}
	}
	return true, ""
}

// missing is the first (sorted) member of cur that base lacks, or "".
func missing(cur, base map[string]bool) string {
	keys := make([]string, 0, len(cur))
	for k := range cur {
		if !base[k] {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	return keys[0]
}

// level is a permission level; none < read < write.
type level int

const (
	levelNone level = iota
	levelRead
	levelWrite
)

var levels = map[string]level{"none": levelNone, "read": levelRead, "write": levelWrite}

// perms is an effective permission set: every scope in scopes, and all for any other scope.
// known is false when it cannot be read or is absent: that counts as the widest, and spec §1
// puts every comparison with it outside the class.
type perms struct {
	known  bool
	all    level
	scopes map[string]level
}

func (p perms) of(scope string) level {
	if l, ok := p.scopes[scope]; ok {
		return l
	}
	return p.all
}

// readPerms reads a permissions value: write-all, read-all or a map of scope to level.
func readPerms(v any, present bool) perms {
	if !present {
		return perms{}
	}
	switch x := v.(type) {
	case string:
		switch x {
		case "write-all":
			return perms{known: true, all: levelWrite}
		case "read-all":
			return perms{known: true, all: levelRead}
		}
		return perms{}
	case nil:
		return perms{}
	}
	m, ok := asMap(v)
	if !ok {
		return perms{}
	}
	p := perms{known: true, all: levelNone, scopes: map[string]level{}}
	for scope, val := range m {
		s, ok := val.(string)
		l, known := levels[s]
		if !ok || !known {
			return perms{}
		}
		p.scopes[scope] = l
	}
	return p
}

// notWider reports whether cur grants no scope more than base.
func notWider(cur, base perms) bool {
	if !cur.known || !base.known || cur.all > base.all {
		return false
	}
	for _, p := range []perms{cur, base} {
		for scope := range p.scopes {
			if cur.of(scope) > base.of(scope) {
				return false
			}
		}
	}
	return true
}

// wider names the first top-level or job permission that is wider on the SHA than its base:
// the same job on the merge-base, or for a new job the top level of the merge-base.
func wider(o, n facts) string {
	if !notWider(n.top, o.top) {
		return "wider permission at the top level"
	}
	for _, id := range sortedJobIDs(n.jobPerms) {
		base, ok := o.jobPerms[id]
		if !ok {
			base = o.top
		}
		if !notWider(n.jobPerms[id], base) {
			return "wider permission for job " + id
		}
	}
	return ""
}

func sortedJobIDs(m map[string]perms) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// facts are the parts of one workflow the class compares.
type facts struct {
	evs                                    []event
	events, uses                           map[string]bool
	top                                    perms
	jobPerms                               map[string]perms // effective, after inheriting the top level
	secrets, tokens, runners, envs, images map[string]bool
}

// readKeys are the keys extract reads; a case variant of one of them is unreadable, since the
// check would skip it and a parser that folds case would not.
var readKeys = []string{"on", "jobs", "permissions", "uses", "steps", "runs-on", "environment",
	"container", "services", "image", "secrets", "strategy"}

// extract reads the facts of one parsed workflow; a non-empty string is why it cannot.
func extract(top map[string]any) (facts, string) {
	f := facts{
		events: map[string]bool{}, uses: map[string]bool{}, jobPerms: map[string]perms{},
		secrets: map[string]bool{}, tokens: map[string]bool{}, runners: map[string]bool{},
		envs: map[string]bool{}, images: map[string]bool{},
	}
	if why := caseVariant("the top level", top); why != "" {
		return f, why
	}
	evs, ok := triggers(top)
	if !ok {
		return f, "`on` is unreadable"
	}
	f.evs = evs
	for _, ev := range evs {
		f.events[ev.name] = true
	}
	p, present := top["permissions"]
	f.top = readPerms(p, present)
	if why := scanText(top, &f); why != "" {
		return f, why
	}
	jobs, ok := asMap(top["jobs"])
	if !ok {
		return f, "`jobs` is not a map"
	}
	for _, id := range sortedKeys(jobs) {
		if why := f.job(top, id, jobs[id]); why != "" {
			return f, why
		}
	}
	return f, ""
}

// job reads one job: its effective permissions, uses, runner, environment, images and
// secrets: inherit.
func (f *facts) job(top map[string]any, id string, v any) string {
	job, ok := asMap(v)
	if !ok {
		return "job " + id + " is not a map"
	}
	if why := caseVariant("job "+id, job); why != "" {
		return why
	}
	if p, present := job["permissions"]; present {
		f.jobPerms[id] = readPerms(p, true)
	} else {
		f.jobPerms[id] = f.top
	}
	if u, present := job["uses"]; present {
		f.uses[canon(u)] = true
	}
	if s, present := job["secrets"]; present && !isMap(s) {
		f.secrets["secrets: "+canon(s)] = true
	}
	if r, present := job["runs-on"]; present {
		f.runners[bound(canon(r), top, job)] = true
	}
	if e, present := job["environment"]; present {
		f.envs[bound(canon(e), top, job)] = true
	}
	if c, present := job["container"]; present {
		// The whole value counts: options, volumes, ports and credentials give as much as an image.
		f.images["container "+bound(canon(c), top, job)] = true
	}
	if s, present := job["services"]; present {
		services, ok := asMap(s)
		if !ok {
			return "services of job " + id + " are not a map"
		}
		for _, name := range sortedKeys(services) {
			f.images["service "+name+" "+bound(canon(services[name]), top, job)] = true
		}
	}
	if job["steps"] == nil {
		return ""
	}
	steps, ok := job["steps"].([]any)
	if !ok {
		return "steps of job " + id + " are not a list"
	}
	for i, s := range steps {
		step, ok := asMap(s)
		if !ok {
			return fmt.Sprintf("step %d of job %s is not a map", i, id)
		}
		if why := caseVariant(fmt.Sprintf("step %d of job %s", i, id), step); why != "" {
			return why
		}
		if u, present := step["uses"]; present {
			f.uses[canon(u)] = true
		}
	}
	return ""
}

var needsWord = regexp.MustCompile(`(?i)\bneeds\b`)

// bound is the token of a runner, environment or image. A literal value is itself. An expression
// draws its value from the matrix or the inputs, whose options the class lets change: the job's
// strategy and the workflow's triggers become part of the token, so a change to either is a new
// value. An expression on needs draws it from the output of another job, which a run: step
// writes: then the whole workflow is the token, and every change is a new value.
func bound(val string, top, job map[string]any) string {
	if !strings.Contains(val, "${{") {
		return val
	}
	if needsWord.MatchString(val) {
		return val + " workflow " + canon(top)
	}
	return val + " strategy " + canon(job["strategy"]) + " on " + canon(top["on"]) + " " + canon(top["true"])
}

func isMap(v any) bool { _, ok := asMap(v); return ok }

// canon is a stable text of a decoded YAML value; %#v prints map keys sorted and keeps the type,
// so the string "[a]" and the list [a] never give the same text.
func canon(v any) string { return fmt.Sprintf("%#v", v) }

// caseVariant names a key of m that differs from a key extract reads only in case.
func caseVariant(where string, m map[string]any) string {
	for _, k := range sortedKeys(m) {
		lower := strings.ToLower(k)
		if lower == k {
			continue
		}
		for _, want := range readKeys {
			if lower == want {
				return fmt.Sprintf("key %q in %s is a case variant of %q", k, where, want)
			}
		}
	}
	return ""
}

var (
	// Expressions are case-insensitive, so every pattern is too.
	secretsWord = regexp.MustCompile(`(?i)\bsecrets\b`)
	secretRef   = regexp.MustCompile(`(?i)^secrets\.([A-Za-z_][A-Za-z0-9_]*)`)
	tokenRef    = regexp.MustCompile(`(?i)\bgithub\s*\.\s*token\b|\bgithub_token\b`)
	// github['token'], github.*, toJSON(github) and format('{0}', github) reach the token without
	// naming it.
	githubWhole = regexp.MustCompile(`(?i)\bgithub\s*(\[|\.\s*\*)|[(,]\s*github\s*[,)]`)
)

// scanText walks every key and string of the workflow for secrets.<name> and the GitHub token.
// A reference to secrets in any other form (secrets['X'], secrets[matrix.n], toJSON(secrets))
// is unreadable; a key named secrets is structure, read by job.
func scanText(top map[string]any, f *facts) string {
	var why string
	var walk func(v any)
	scan := func(s string) {
		for _, loc := range secretsWord.FindAllStringIndex(s, -1) {
			m := secretRef.FindStringSubmatch(s[loc[0]:])
			if m == nil {
				if why == "" {
					why = fmt.Sprintf("secret referenced other than as secrets.<name> in %q", s)
				}
				continue
			}
			f.secrets["secrets."+strings.ToLower(m[1])] = true
		}
		for _, m := range tokenRef.FindAllString(s, -1) {
			f.tokens[strings.ToLower(strings.Join(strings.Fields(m), ""))] = true
		}
		if githubWhole.MatchString(s) && why == "" {
			why = fmt.Sprintf("token reached through the whole github context in %q", s)
		}
	}
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			scan(x)
		case []any:
			for _, item := range x {
				walk(item)
			}
		default:
			m, ok := asMap(x)
			if !ok {
				return
			}
			for _, k := range sortedKeys(m) {
				if !strings.EqualFold(k, "secrets") {
					scan(k)
				}
				walk(m[k])
			}
		}
	}
	walk(top)
	return why
}
