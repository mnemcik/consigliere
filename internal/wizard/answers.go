// Package wizard implements the interactive `cg init --wizard` flow.
//
// The wizard collects workspace bootstrap preferences (profile, first area,
// post-init actions) via a TUI form, and exposes pure rendering helpers so
// tests can exercise the file-generation layer without a TTY.
package wizard

// Answers holds everything the wizard collects.
//
// The struct is the boundary between the interactive layer (huh-driven form)
// and the deterministic file-writing layer (`render.go`). Tests construct
// Answers directly and assert on rendered output.
type Answers struct {
	ProfileName  string
	ProfileRole  string
	ProfileFocus string
	AreaSlug     string
	AreaName     string
	AreaTags     string // free-form, comma-separated; e.g. "microservice, compliance"
	AreaOverview string
	RunGitInit   bool
	InstallSlash bool
}

// HasFirstArea reports whether the user provided enough to generate a first
// area file. Empty slug means the user skipped the area step. Nil-safe.
func (a *Answers) HasFirstArea() bool {
	if a == nil {
		return false
	}
	return a.AreaSlug != "" && a.AreaName != ""
}
