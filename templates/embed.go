// Package templates holds the framework content that cg ships: workspace
// templates, framework notes, the workspace CLAUDE.md, slash commands, skills
// and hooks. It is the only copy; edit the files here.
package templates

import "embed"

// FS is the framework content embedded into the cg binary. The pattern list
// is explicit so this Go file stays out of it; all: keeps dotfiles such as
// workspace/.claude/ and workspace/.gitignore inside the listed directories.
// TestEmbedCoversTree fails if anything under this directory is left out,
// for example a new top-level entry missing from the list.
//
//go:embed all:commands all:notes all:project all:skills all:workspace *.md
var FS embed.FS
