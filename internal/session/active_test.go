package session

import (
	"os"
	"testing"
	"time"
)

func TestActiveProjects(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	active := 4 * time.Hour
	dirty := 48 * time.Hour

	mk := func(id, content string, age time.Duration) {
		writeCtx(t, root, id, content)
		mt := now.Add(-age)
		if err := os.Chtimes(ContextFile(root, id), mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	// fresh clean → active
	mk("s1", `{"area":"a1","project":"p1","dirty":false}`, 1*time.Hour)
	// clean but old → stale (beyond 4h)
	mk("s2", `{"area":"a2","project":"p2","dirty":false}`, 10*time.Hour)
	// dirty and within 48h → active
	mk("s3", `{"area":"a3","project":"p3","dirty":true}`, 10*time.Hour)
	// dirty but beyond 48h → stale
	mk("s4", `{"area":"a4","project":"p4","dirty":true}`, 60*time.Hour)
	// no project → skipped
	mk("s5", `{"area":"a5","dirty":false}`, 1*time.Hour)

	got, err := ActiveProjects(root, now, active, dirty)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"p1": true, "p3": true}
	if len(got) != len(want) {
		t.Fatalf("expected %d active, got %d: %+v", len(want), len(got), got)
	}
	for _, s := range got {
		if !want[s.Project] {
			t.Errorf("unexpected active project %q", s.Project)
		}
	}
}
