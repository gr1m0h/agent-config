package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLowerAgentToSkill(t *testing.T) {
	raw := "---\nname: reviewer\ndescription: Reviews code carefully\nmodel: sonnet\n---\nDo a strict review.\n"
	got := lowerAgentToSkill("Reviewer", raw)
	for _, want := range []string{"name: agent-reviewer", `description: "Reviews code carefully"`, "Do a strict review."} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "model: sonnet") {
		t.Fatalf("Claude metadata leaked into Codex skill: %s", got)
	}
}

func TestBuildCodexAgentsMD(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "instructions.md"), []byte("Base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(d, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "rules", "go.md"), []byte("Use gofmt.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := buildCodexAgentsMD(d)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "Base") || !strings.Contains(got, "## go") || !strings.Contains(got, "Use gofmt.") {
		t.Fatal(got)
	}
}
