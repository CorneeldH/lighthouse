package archscan

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseDefaults(t *testing.T) {
	c, err := parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.command != "/lighthouse-architecture-scan" || c.window != "3 months" || c.maxRuntime != 60*time.Minute {
		t.Errorf("defaults = %+v", c)
	}
	if want := []string{"Read", "Glob", "Grep", "Write"}; !reflect.DeepEqual(c.tools, want) {
		t.Errorf("tools = %v, want %v", c.tools, want)
	}
}

// Classification: security boundary. Fails if parse lets Bash (or any tool beyond Read, Glob,
// Grep, Write) through to the unattended agent.
func TestParseRejectsBash(t *testing.T) {
	_, err := parse(map[string]any{"tools": []any{"Read", "Bash"}})
	if err == nil || !strings.Contains(err.Error(), "Bash") {
		t.Fatalf("err = %v, want one naming Bash", err)
	}
}

func TestParseRejectsUnknownKey(t *testing.T) {
	_, err := parse(map[string]any{"comand": "/x"})
	if err == nil || !strings.Contains(err.Error(), "comand") {
		t.Fatalf("err = %v, want one naming the key", err)
	}
}

func TestParseLabels(t *testing.T) {
	c, err := parse(map[string]any{"labels": []any{"2-area:core"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"2-area:core"}; !reflect.DeepEqual(c.labels, want) {
		t.Errorf("labels = %v, want %v", c.labels, want)
	}
}
