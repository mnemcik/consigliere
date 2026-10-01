package cmd

// Workspace top-level directory names and the projects index path. Centralized
// so the same string literals are not repeated across commands (goconst).
const (
	dirProjects = "projects"
	dirAreas    = "areas"
	dirIdeas    = "ideas"
	dirNotes    = "notes"
	dirInsights = "insights"

	indexProjectsPath = "projects/TODO.md"

	// notesEmbedRoot is the embed-tree directory holding framework-shipped notes.
	notesEmbedRoot = "notes"

	// claudeEmbedPath is the embedded workspace CLAUDE.md template — the canonical
	// framework content for the binary's version, used by `cg init` and `cg sync`.
	claudeEmbedPath = "workspace/CLAUDE.md"
)
