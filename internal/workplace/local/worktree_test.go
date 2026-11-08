//go:build linux

package local

import (
	"reflect"
	"testing"
)

func TestParseWorktrees(t *testing.T) {
	porcelain := `worktree /home/u/repo
HEAD 1111111111111111111111111111111111111111
branch refs/heads/main

worktree /home/u/repo/.claude/worktrees/local-issue-1-x-abcdef
HEAD 2222222222222222222222222222222222222222
branch refs/heads/issue-1-x

worktree /tmp/with space
HEAD 3333333333333333333333333333333333333333
detached
locked reason here

`
	want := []worktree{
		{Path: "/home/u/repo", Branch: "main"},
		{Path: "/home/u/repo/.claude/worktrees/local-issue-1-x-abcdef", Branch: "issue-1-x"},
		{Path: "/tmp/with space", Branch: ""},
	}
	if got := parseWorktrees(porcelain); !reflect.DeepEqual(got, want) {
		t.Errorf("parseWorktrees = %+v, want %+v", got, want)
	}
	if got := parseWorktrees(""); len(got) != 0 {
		t.Errorf("parseWorktrees(\"\") = %+v, want none", got)
	}
}
