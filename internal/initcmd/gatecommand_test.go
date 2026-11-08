package initcmd

import (
	"strings"
	"testing"
	"testing/fstest"
)

// gateRepo installs the test packs, then replaces lighthouse.toml by body.
func gateRepo(t *testing.T, body string) string {
	t.Helper()
	root := newRepo(t)
	initYes(t, root)
	write(t, root, "lighthouse.toml", body)
	return root
}

func gateToml(command string) string {
	return "[test_gate]\ncommand = \"" + command + "\"\n"
}

// withGatePython installs scripts/gate_python.py, the way pack/python does.
func withGatePython(o *Options) {
	o.Packs.(fstest.MapFS)["core/scripts/gate_python.py"] = file("#!/usr/bin/env python3\nprint(1)\n")
}

func TestUpgradeRewritesGateGo(t *testing.T) {
	root := gateRepo(t, gateToml("python3 .lighthouse/scripts/gate_go.py -- go test -race && make lint"))
	r := run(t, root, "", all(upgrade, yes))
	if r.code != 0 {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
	if got := read(t, root, "lighthouse.toml"); !strings.Contains(got, `command = "lighthouse gate go -- go test -race && make lint"`) {
		t.Fatalf("lighthouse.toml:\n%s", got)
	}
	for _, want := range []string{
		"lighthouse.toml: [test_gate] command rewritten",
		"old: ", "python3 .lighthouse/scripts/gate_go.py -- go test -race && make lint",
		"new: ", "lighthouse gate go -- go test -race && make lint",
	} {
		if !strings.Contains(r.err, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, r.err)
		}
	}
}

func TestUpgradeRewritesGateNode(t *testing.T) {
	root := gateRepo(t, gateToml("python3 .lighthouse/scripts/gate_node.py --dir web -- lint"))
	r := run(t, root, "", all(upgrade, yes))
	if r.code != 0 {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
	if got := read(t, root, "lighthouse.toml"); !strings.Contains(got, `command = "lighthouse gate node --dir web -- lint"`) {
		t.Fatalf("lighthouse.toml:\n%s", got)
	}
	if !strings.Contains(r.err, "new: ") || !strings.Contains(r.err, "lighthouse gate node --dir web -- lint") || !strings.Contains(r.err, "python3 .lighthouse/scripts/gate_node.py --dir web -- lint") {
		t.Fatalf("stderr:\n%s", r.err)
	}
}

func TestUpgradeLeavesCommentAlone(t *testing.T) {
	body := "# was: python3 .lighthouse/scripts/gate_go.py -- go test\n[test_gate]\n# python3 .lighthouse/scripts/gate_go.py\ncommand = \"python3 .lighthouse/scripts/gate_go.py -- go test\"\n\n[other]\ncommand = \"python3 .lighthouse/scripts/gate_go.py\"\n"
	root := gateRepo(t, body)
	r := run(t, root, "", all(upgrade, yes))
	if r.code != 0 {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
	got := read(t, root, "lighthouse.toml")
	for _, want := range []string{
		"# was: python3 .lighthouse/scripts/gate_go.py -- go test\n",
		"[test_gate]\n# python3 .lighthouse/scripts/gate_go.py\ncommand = \"lighthouse gate go -- go test\"\n",
		"[other]\ncommand = \"python3 .lighthouse/scripts/gate_go.py\"\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("lighthouse.toml lacks %q:\n%s", want, got)
		}
	}
}

func TestUpgradeWarnsOnOtherScriptRef(t *testing.T) {
	for _, command := range []string{
		"python .lighthouse/scripts/gate_go.py -- go test",
		"python3 -m lighthouse_lib.x",
	} {
		root := gateRepo(t, gateToml(command))
		r := run(t, root, "", all(upgrade, yes))
		if r.code != 0 {
			t.Fatalf("%s: rc %d err %q", command, r.code, r.err)
		}
		if got := read(t, root, "lighthouse.toml"); !strings.Contains(got, `command = "`+command+`"`) {
			t.Fatalf("%s: rewritten:\n%s", command, got)
		}
		if strings.Contains(r.err, "rewritten") {
			t.Fatalf("%s: announced a rewrite:\n%s", command, r.err)
		}
	}
	root := gateRepo(t, gateToml("python .lighthouse/scripts/gate_go.py -- go test"))
	r := run(t, root, "", all(upgrade, yes))
	want := "warning: lighthouse.toml: [test_gate] command calls .lighthouse/scripts/gate_go.py, which lighthouse no longer installs; the test gate breaks until you change it\n"
	if !strings.Contains(r.err, want) {
		t.Fatalf("stderr lacks %q:\n%s", want, r.err)
	}
	root = gateRepo(t, gateToml("python3 -m lighthouse_lib.x"))
	r = run(t, root, "", all(upgrade, yes))
	want = "warning: lighthouse.toml: [test_gate] command uses lighthouse_lib, which lighthouse is phasing out; move it to a lighthouse command\n"
	if !strings.Contains(r.err, want) {
		t.Fatalf("stderr lacks %q:\n%s", want, r.err)
	}
	if strings.Contains(r.err, "no longer installs") {
		t.Fatalf("lighthouse_lib is still installed, so no path warning:\n%s", r.err)
	}
}

func TestUpgradeKeepsInstalledRefQuiet(t *testing.T) {
	root := gateRepo(t, gateToml("python3 .lighthouse/scripts/gate_python.py -- pytest"))
	r := run(t, root, "", all(upgrade, yes, withGatePython))
	if r.code != 0 {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
	if strings.Contains(r.err, "[test_gate] command") {
		t.Fatalf("warned about an installed script:\n%s", r.err)
	}
	if got := read(t, root, "lighthouse.toml"); !strings.Contains(got, "gate_python.py -- pytest") {
		t.Fatalf("lighthouse.toml:\n%s", got)
	}
}

func TestUpgradeGateCommandIsIdempotent(t *testing.T) {
	root := gateRepo(t, gateToml("python3 .lighthouse/scripts/gate_go.py -- go test"))
	if r := run(t, root, "", all(upgrade, yes)); r.code != 0 {
		t.Fatalf("first upgrade: rc %d err %q", r.code, r.err)
	}
	r := run(t, root, "", all(upgrade, yes))
	if r.code != 0 || r.out != "No changes.\n" || strings.Contains(r.err, "[test_gate]") {
		t.Fatalf("second upgrade: rc %d out %q err %q", r.code, r.out, r.err)
	}
}

func TestInitDoesNotRewrite(t *testing.T) {
	root := newRepo(t)
	write(t, root, "lighthouse.toml", gateToml("python3 .lighthouse/scripts/gate_go.py -- go test"))
	r := run(t, root, "", yes)
	if r.code != 0 {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
	if got := read(t, root, "lighthouse.toml"); !strings.Contains(got, `command = "python3 .lighthouse/scripts/gate_go.py -- go test"`) {
		t.Fatalf("lighthouse.toml:\n%s", got)
	}
	if strings.Contains(r.err, "[test_gate] command") {
		t.Fatalf("stderr:\n%s", r.err)
	}
}

func TestUpgradeRewritesOnlyTheStringBeforeAnInlineComment(t *testing.T) {
	body := "[test_gate]\ncommand = \"python3 .lighthouse/scripts/gate_go.py -- go test\"  # was python3 .lighthouse/scripts/gate_go.py, see .lighthouse/scripts/foo.py\n"
	root := gateRepo(t, body)
	r := run(t, root, "", all(upgrade, yes))
	if r.code != 0 {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
	want := "command = \"lighthouse gate go -- go test\"  # was python3 .lighthouse/scripts/gate_go.py, see .lighthouse/scripts/foo.py\n"
	if got := read(t, root, "lighthouse.toml"); !strings.Contains(got, want) {
		t.Fatalf("lighthouse.toml lacks %q:\n%s", want, got)
	}
	if strings.Contains(r.err, "warning: lighthouse.toml") || strings.Contains(r.err, "# was") {
		t.Fatalf("the comment reached the report:\n%s", r.err)
	}
}

func TestUpgradeRewritesMultiLineGateCommand(t *testing.T) {
	body := "[test_gate]\ncommand = \"\"\"\npython3 .lighthouse/scripts/gate_go.py -- x\n\"\"\"\ndeadline_seconds = 5\n"
	root := gateRepo(t, body)
	r := run(t, root, "", all(upgrade, yes))
	if r.code != 0 {
		t.Fatalf("rc %d err %q", r.code, r.err)
	}
	want := "command = \"\"\"\nlighthouse gate go -- x\n\"\"\"\ndeadline_seconds = 5\n"
	if got := read(t, root, "lighthouse.toml"); !strings.Contains(got, want) {
		t.Fatalf("lighthouse.toml lacks %q:\n%s", want, read(t, root, "lighthouse.toml"))
	}
}
