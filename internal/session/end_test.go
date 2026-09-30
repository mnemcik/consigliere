package session

import (
	"os"
	"testing"
	"time"
)

func TestEndSession(t *testing.T) {
	root := t.TempDir()
	ws := t.TempDir()
	now := time.Now()
	badge := func(id, content string, mt time.Time) {
		writeCtx(t, root, id, content)
		touch(t, ContextFile(root, id), mt)
	}
	pausedAt := now.Add(-time.Hour)
	writeResume(t, ws, "paused", pausedAt)

	badge("clean", `{"area":"a","project":"p","dirty":false}`, now)
	badge("dirty", `{"area":"a","project":"p","dirty":true}`, now)
	badge("pauser", `{"area":"a","project":"paused","dirty":false,"paused":true}`, pausedAt.Add(-time.Minute))
	badge("resumer", `{"area":"a","project":"paused","dirty":false}`, pausedAt.Add(time.Minute))
	badge("stale-marker", `{"area":"a","project":"done","dirty":false,"paused":true}`, pausedAt)

	cases := []struct {
		id           string
		wantReleased bool
	}{
		{"clean", true},   // finished without a wrap, nothing unlanded
		{"dirty", false},  // unwrapped work may sit in its worktree
		{"pauser", false}, // a pause keeps its claim
		{"resumer", true}, // resumed after the pause, then ended cleanly
		{"ghost", false},  // no badge file
	}
	for _, c := range cases {
		released, err := EndSession(root, ws, c.id)
		if err != nil {
			t.Fatalf("EndSession(%s): %v", c.id, err)
		}
		if released != c.wantReleased {
			t.Errorf("EndSession(%s) released = %v, want %v", c.id, released, c.wantReleased)
		}
		_, serr := os.Stat(ContextFile(root, c.id))
		if exists := serr == nil; c.id != "ghost" && exists == c.wantReleased {
			t.Errorf("badge %s exists = %v after EndSession, want %v", c.id, exists, !c.wantReleased)
		}
	}
}
