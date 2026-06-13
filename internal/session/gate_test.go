package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGateSkipAll(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"/wrap", "idea: a new thing", "just a quick question", "no project needed", "skip the project"} {
		if _, emit := Gate(context.Background(), root, GateInput{Prompt: p, SessionID: "s", CWD: root}, "", 0); emit {
			t.Errorf("prompt %q should suppress the gate", p)
		}
	}
}

func TestGateEmitsDefault(t *testing.T) {
	root := t.TempDir()
	text, emit := Gate(context.Background(), root,
		GateInput{Prompt: "implement the thing", SessionID: "sess-123", CWD: root}, "", 0)
	if !emit {
		t.Fatal("expected the gate to emit for a normal prompt")
	}
	for _, want := range []string{"SESSION-START", "sess-123", ContextFile(root, "sess-123"), "<user-prompt-submit-hook>"} {
		if !strings.Contains(text, want) {
			t.Errorf("gate output missing %q:\n%s", want, text)
		}
	}
}

func TestGateUsesTemplate(t *testing.T) {
	root := t.TempDir()
	tmpl := filepath.Join(root, "gate.md")
	if err := os.WriteFile(tmpl, []byte("CUSTOM gate for {{session_id}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	text, emit := Gate(context.Background(), root,
		GateInput{Prompt: "do work", SessionID: "abc", CWD: root}, "gate.md", 0)
	if !emit || !strings.Contains(text, "CUSTOM gate for abc") {
		t.Errorf("expected custom template body, got:\n%s", text)
	}
}

func TestGatePrunesStaleContexts(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(ContextDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	old := ContextFile(root, "old")
	fresh := ContextFile(root, "fresh")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Backdate the old file 10 days.
	past := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	Gate(context.Background(), root, GateInput{Prompt: "work", SessionID: "x", CWD: root}, "", 7)

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("stale context file should have been pruned")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("fresh context file should survive pruning")
	}
}
