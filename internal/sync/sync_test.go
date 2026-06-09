package sync

import "testing"

func ptrTo(s string) *string { return &s }

func TestClassifySingleArtifact(t *testing.T) {
	cases := []struct {
		name                    string
		onDisk, recorded, frame *string
		want                    Status
	}{
		// Not recorded but shipped → New even if on disk already matches: apply must
		// still record the artifact in the manifest, so it can't be "up-to-date".
		{"new: framework adds, not recorded, on disk identical", ptrTo("a"), nil, ptrTo("a"), StatusNew},
		{"new: framework adds, absent on disk and manifest", nil, nil, ptrTo("a"), StatusNew},
		{"removed: manifest has it, framework dropped", ptrTo("a"), ptrTo("a"), nil, StatusRemoved},
		{"removed even if user drifted it", ptrTo("z"), ptrTo("a"), nil, StatusRemoved},
		{"missing: recorded + shipped but gone from disk", nil, ptrTo("a"), ptrTo("b"), StatusMissing},
		{"up-to-date: on-disk equals framework", ptrTo("b"), ptrTo("a"), ptrTo("b"), StatusUpToDate},
		{"up-to-date: all three equal", ptrTo("a"), ptrTo("a"), ptrTo("a"), StatusUpToDate},
		{"updatable: on-disk equals manifest, framework changed", ptrTo("a"), ptrTo("a"), ptrTo("b"), StatusUpdatable},
		{"drifted: on-disk matches neither manifest nor framework", ptrTo("z"), ptrTo("a"), ptrTo("b"), StatusDrifted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classify(c.onDisk, c.recorded, c.frame); got != c.want {
				t.Errorf("classify() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestClassifyKindCoversUnionOfIDs(t *testing.T) {
	onDisk := map[string]string{"a": "1", "b": "2"}
	recorded := map[string]string{"a": "1", "c": "3"}
	framework := map[string]string{"a": "9", "d": "4"}

	items := ClassifyKind(KindSection, onDisk, recorded, framework)
	if len(items) != 4 {
		t.Fatalf("expected 4 items (union of a,b,c,d), got %d: %v", len(items), items)
	}
	byID := map[string]Status{}
	for _, it := range items {
		if it.Kind != KindSection {
			t.Errorf("item %q has kind %q, want section", it.ID, it.Kind)
		}
		byID[it.ID] = it.Status
	}
	want := map[string]Status{
		"a": StatusUpdatable, // on-disk == recorded, framework changed
		"b": StatusDrifted,   // on disk only — cg neither recorded nor ships it (user's own); surfaced, not touched
		"c": StatusRemoved,   // recorded only — framework dropped it
		"d": StatusNew,       // framework only
	}
	for id, w := range want {
		if byID[id] != w {
			t.Errorf("id %q: got %q, want %q", id, byID[id], w)
		}
	}
}

func TestClassifyOrdersSectionsThenNotesThenID(t *testing.T) {
	r := Classify(
		map[string]string{"zebra": "1", "alpha": "1"}, nil, map[string]string{"zebra": "1", "alpha": "1"},
		map[string]string{"notes/b.md": "1"}, nil, map[string]string{"notes/b.md": "1", "notes/a.md": "1"},
	)
	order := make([]string, 0, len(r.Items))
	for _, it := range r.Items {
		order = append(order, string(it.Kind)+":"+it.ID)
	}
	want := []string{"section:alpha", "section:zebra", "note:notes/a.md", "note:notes/b.md"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q (full: %v)", i, order[i], want[i], order)
		}
	}
}

func TestReportByStatusAndActionable(t *testing.T) {
	// all up-to-date → not actionable
	clean := Classify(
		map[string]string{"a": "1"}, map[string]string{"a": "1"}, map[string]string{"a": "1"},
		nil, nil, nil,
	)
	if clean.Actionable() {
		t.Error("a fully up-to-date report must not be actionable")
	}

	dirty := Classify(
		map[string]string{"a": "1"}, map[string]string{"a": "1"}, map[string]string{"a": "2"},
		nil, nil, map[string]string{"notes/x.md": "1"},
	)
	if !dirty.Actionable() {
		t.Error("a report with updatable/new items must be actionable")
	}
	bs := dirty.ByStatus()
	if len(bs[StatusUpdatable]) != 1 || bs[StatusUpdatable][0].ID != "a" {
		t.Errorf("expected section a updatable, got %v", bs[StatusUpdatable])
	}
	if len(bs[StatusNew]) != 1 || bs[StatusNew][0].ID != "notes/x.md" {
		t.Errorf("expected note notes/x.md new, got %v", bs[StatusNew])
	}
}
