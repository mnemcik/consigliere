package worktree

import (
	"bytes"
	"testing"
)

func TestList(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer

	// One clean session worktree, one with an unlanded commit.
	if _, err := Create(ctx, "ls-clean", defaultOpts(root), &log); err != nil {
		t.Fatalf("Create ls-clean: %v", err)
	}
	wt, err := Create(ctx, "ls-ahead", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create ls-ahead: %v", err)
	}
	commitFile(t, ctx, wt, "a.txt", "a\n", "ahead by one")

	entries, err := List(ctx, defaultOpts(root))
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	bySlug := map[string]ListEntry{}
	nonSession := 0
	for _, e := range entries {
		if e.IsSession() {
			bySlug[e.Slug] = e
		} else {
			nonSession++
		}
	}

	if nonSession < 1 {
		t.Errorf("expected the main worktree to appear as a non-session entry, got %+v", entries)
	}
	clean, ok := bySlug["ls-clean"]
	if !ok {
		t.Fatalf("ls-clean not listed: %+v", entries)
	}
	if clean.Branch != "session/ls-clean" || clean.Ahead != 0 {
		t.Errorf("ls-clean: branch=%q ahead=%d, want session/ls-clean ahead=0", clean.Branch, clean.Ahead)
	}
	ahead, ok := bySlug["ls-ahead"]
	if !ok {
		t.Fatalf("ls-ahead not listed: %+v", entries)
	}
	if ahead.Ahead != 1 {
		t.Errorf("ls-ahead: ahead=%d, want 1", ahead.Ahead)
	}
}
