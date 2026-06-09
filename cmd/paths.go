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
)
