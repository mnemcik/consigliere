// Package sync implements the deterministic half of workspace-content
// reconciliation (`cg sync`): given the content hashes of the framework-managed
// artifacts as they are on disk, as the manifest last recorded them, and as the
// current binary's embedded framework ships them, it classifies each artifact
// so the caller can decide what to do.
//
// It is pure and hash-based: no file I/O, no diffing. The cmd layer reads the
// workspace and the embed tree, hands this package three maps, and renders the
// resulting Report. The judgment half — detecting that a new framework rule
// semantically contradicts a user's own rule — is the `/cg-sync` skill's job
// (workspace-sync DEC-004); this package only decides what hashing can decide.
package sync

import "sort"

// Status is the reconciliation classification of a single managed artifact.
type Status string

const (
	// StatusUpToDate: the on-disk content already equals the framework's — nothing to do.
	StatusUpToDate Status = "up-to-date"
	// StatusUpdatable: on-disk content is exactly what cg last wrote and the framework
	// has since changed it — safe to auto-update (the user never touched it).
	StatusUpdatable Status = "updatable"
	// StatusDrifted: the user edited a framework artifact (on-disk differs from both
	// what cg recorded and the framework's version) — never clobber; surface it.
	StatusDrifted Status = "drifted"
	// StatusNew: the framework ships an artifact the manifest has never recorded — insert it.
	StatusNew Status = "new"
	// StatusRemoved: the manifest records an artifact the current framework no longer ships — flag it.
	StatusRemoved Status = "removed"
	// StatusMissing: the manifest records an artifact that is gone from disk (user deleted it) — flag it.
	StatusMissing Status = "missing"
)

// Kind distinguishes the two managed-artifact families.
type Kind string

const (
	KindSection Kind = "section"
	KindNote    Kind = "note"
)

// Item is one classified artifact.
type Item struct {
	Kind   Kind
	ID     string // CLAUDE.md section id, or workspace-relative note path
	Status Status
}

// Report is the full classification, ordered deterministically (by kind, then id).
type Report struct {
	Items []Item
}

// classify decides the status of a single artifact from its three optional
// hashes. A nil pointer means the artifact is absent from that source.
func classify(onDisk, recorded, framework *string) Status {
	switch {
	case recorded == nil && framework != nil:
		// The framework brings an artifact the manifest never tracked.
		return StatusNew
	case recorded != nil && framework == nil:
		// The framework dropped an artifact the manifest still tracks.
		return StatusRemoved
	case onDisk == nil:
		// Recorded (and possibly still shipped) but gone from disk.
		return StatusMissing
	case framework != nil && *onDisk == *framework:
		// On-disk content already matches the framework — no action needed.
		return StatusUpToDate
	case recorded != nil && *onDisk == *recorded:
		// Untouched since cg wrote it, and (since the case above did not fire)
		// the framework has changed it — safe to auto-update.
		return StatusUpdatable
	default:
		// On-disk matches neither what cg wrote nor the framework: user-edited.
		return StatusDrifted
	}
}

// ClassifyKind classifies every artifact of one kind. Each map is keyed by
// artifact id and valued by content hash; a key's absence from a map means the
// artifact does not exist in that source.
func ClassifyKind(kind Kind, onDisk, recorded, framework map[string]string) []Item {
	ids := make(map[string]struct{})
	for id := range onDisk {
		ids[id] = struct{}{}
	}
	for id := range recorded {
		ids[id] = struct{}{}
	}
	for id := range framework {
		ids[id] = struct{}{}
	}

	ptr := func(m map[string]string, id string) *string {
		if v, ok := m[id]; ok {
			return &v
		}
		return nil
	}

	items := make([]Item, 0, len(ids))
	for id := range ids {
		items = append(items, Item{
			Kind:   kind,
			ID:     id,
			Status: classify(ptr(onDisk, id), ptr(recorded, id), ptr(framework, id)),
		})
	}
	return items
}

// Classify builds the full Report from the section and note hash maps for all
// three sources, ordered deterministically by kind (sections before notes) then id.
func Classify(
	onDiskSections, recordedSections, frameworkSections map[string]string,
	onDiskNotes, recordedNotes, frameworkNotes map[string]string,
) Report {
	items := ClassifyKind(KindSection, onDiskSections, recordedSections, frameworkSections)
	items = append(items, ClassifyKind(KindNote, onDiskNotes, recordedNotes, frameworkNotes)...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind == KindSection // sections first
		}
		return items[i].ID < items[j].ID
	})
	return Report{Items: items}
}

// ByStatus groups the report's items by status, preserving the report's order.
func (r Report) ByStatus() map[Status][]Item {
	out := make(map[Status][]Item)
	for _, it := range r.Items {
		out[it.Status] = append(out[it.Status], it)
	}
	return out
}

// Actionable reports whether any item needs the user's or `cg sync --apply`'s
// attention (anything other than up-to-date).
func (r Report) Actionable() bool {
	for _, it := range r.Items {
		if it.Status != StatusUpToDate {
			return true
		}
	}
	return false
}
