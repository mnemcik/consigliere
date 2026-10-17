package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mnemcik/consigliere/templates"
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
	for _, want := range []string{"SESSION-START", "sess-123", ContextFile(root, "sess-123"), "cg session set-context --session-id sess-123", "<user-prompt-submit-hook>"} {
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

// TestGateAddsSetContext covers #195: a workspace's gate template is user-owned
// and never updated, so cg adds the set-context instruction to templates of
// every vintage, exactly once.
func TestGateAddsSetContext(t *testing.T) {
	const cmd = "cg session set-context --session-id abc"
	cases := map[string]string{
		// Default template as cg init seeded it before 2026-09-25.
		"original": "⚠️ SESSION-START — confirm:\n1. Area.\n\nSession ID: {{session_id}}\n" +
			"Badge state file for this session: {{badge_file}}\n{{worktree_warning}}\n",
		// Default template from 2026-09-25, which carries the command itself.
		"with command": "⚠️ SESSION-START — confirm:\n1. Area.\n\nSession ID: {{session_id}}\n" +
			"Badge state file for this session: {{badge_file}}\n" +
			"Once the area and project are confirmed, record them for the status line:\n" +
			"  cg session set-context --session-id {{session_id}} --area <area-slug> --project <project-slug>\n" +
			"{{worktree_warning}}\n",
		// Current default template.
		"token":    "⚠️ SESSION-START — confirm:\n1. Area.\n\n{{set_context}}\n{{worktree_warning}}\n",
		"shipped":  shippedGate(t),
		"empty":    " \n",
		"no token": "CUSTOM gate\r\nwith CRLF\r\n",
		"crlf original": "Gate\r\nSession ID: {{session_id}}\r\n" +
			"Badge state file for this session: {{badge_file}}\r\n{{worktree_warning}}\r\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "gate.md"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			text, emit := Gate(context.Background(), root,
				GateInput{Prompt: "do work", SessionID: "abc", CWD: root}, "gate.md", 0)
			if !emit {
				t.Fatal("expected the gate to emit")
			}
			for _, want := range []string{cmd, "Session ID: abc", "Badge state file for this session: " + ContextFile(root, "abc")} {
				if n := strings.Count(text, want); n != 1 {
					t.Errorf("want %q once, got %d times:\n%s", want, n, text)
				}
			}
			if strings.Contains(text, "{{") {
				t.Errorf("unsubstituted token left:\n%s", text)
			}
		})
	}
}

// TestGateSetContextBeforeWorktreeWarning keeps the worktree warning last, as
// it was before cg added the block.
func TestGateSetContextBeforeWorktreeWarning(t *testing.T) {
	got := withSetContext("Gate\n{{worktree_warning}}")
	if !strings.HasSuffix(got, setContextBlock+"\n{{worktree_warning}}") {
		t.Errorf("block should sit right before the worktree warning:\n%s", got)
	}
}

// shippedGate returns the gate template cg init seeds today.
func shippedGate(t *testing.T) string {
	t.Helper()
	b, err := templates.FS.ReadFile("workspace/.claude/cg/session-gate.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestGateEmptyTemplateStartsWithBlock keeps an empty template from rendering
// leading blank lines.
func TestGateEmptyTemplateStartsWithBlock(t *testing.T) {
	if got := withSetContext(" \n"); got != setContextBlock {
		t.Errorf("empty template should render just the block, got:\n%q", got)
	}
}
