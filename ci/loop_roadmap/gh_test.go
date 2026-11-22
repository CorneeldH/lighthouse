package main

import (
	"reflect"
	"testing"
)

func TestParseIssueLines(t *testing.T) {
	got, err := parseIssueLines("7\tM2 later\n9\t\n\n")
	if err != nil || !reflect.DeepEqual(got, map[int]string{7: "M2 later", 9: ""}) {
		t.Errorf("got %v, %v", got, err)
	}
	if got, err := parseIssueLines(""); err != nil || len(got) != 0 {
		t.Errorf("empty: got %v, %v", got, err)
	}
	if _, err := parseIssueLines("x\tM\n"); err == nil {
		t.Error("a line without a number must be an error")
	}
}

func TestParseClosingKeepsOnlyTheLabelInThisRepo(t *testing.T) {
	reply := `{"data":{"repository":{"pullRequest":{"closingIssuesReferences":{"nodes":[
	 {"number":7,"repository":{"nameWithOwner":"O/R"},"labels":{"nodes":[{"name":"loop"},{"name":"bug"}]}},
	 {"number":8,"repository":{"nameWithOwner":"o/r"},"labels":{"nodes":[{"name":"bug"}]}},
	 {"number":9,"repository":{"nameWithOwner":"other/repo"},"labels":{"nodes":[{"name":"loop"}]}}]}}}}}`
	got, err := parseClosing(reply, "o/r", 5)
	if err != nil || !reflect.DeepEqual(got, map[int]bool{7: true}) {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestParseClosingEmptyNodesIsEmptySet(t *testing.T) {
	got, err := parseClosing(`{"data":{"repository":{"pullRequest":{"closingIssuesReferences":{"nodes":[]}}}}}`, "o/r", 5)
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestParseClosingMalformedReplyIsAnError(t *testing.T) {
	for name, reply := range map[string]string{
		"null pull request": `{"data":{"repository":{"pullRequest":null}}}`,
		"no closing key":    `{"data":{"repository":{"pullRequest":{}}}}`,
		"no nodes":          `{"data":{"repository":{"pullRequest":{"closingIssuesReferences":{}}}}}`,
		"null repository":   `{"data":{"repository":null}}`,
		"errors only":       `{"errors":[{"message":"x"}]}`,
		"not json":          `nope`,
	} {
		if got, err := parseClosing(reply, "o/r", 5); err == nil {
			t.Errorf("%s: got %v, want an error", name, got)
		}
	}
}
