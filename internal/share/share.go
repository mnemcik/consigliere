// Package share implements the export core for read-only project sharing: it
// renders one workspace project into a self-contained copy that can leave the
// workspace. The owner writes; recipients only ever read the rendered copy.
//
// The core is split in two. Render is a pure transform over file contents: it
// touches neither disk nor git, so every rule is unit-testable. Export is the
// thin I/O layer that selects files, checks the project is committed, and
// resolves the stamp (source commit, date, owner) before calling Render.
//
// Rules, in pipeline order:
//   - only allowlisted files leave: README.md, decisions.md and todo.md by
//     default, others when explicitly included; resume.md never does
//   - YAML frontmatter is dropped (it is a derived copy, and carries priority)
//   - content between share:exclude markers is removed; a malformed marker is
//     an error, never a best-effort guess
//   - README's ## Meta block is rewritten from the project index to a fixed
//     set of shareable fields
//   - the [You] placeholder is replaced with the owner's name
//   - relative links are rewritten when their target is part of the shared
//     set, de-linked (text kept, target dropped) when it is private, and kept
//     when external; a link escaping the workspace is an error
//   - every file gets a read-only mirror header naming the owner and the
//     source commit it was rendered from
package share

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

const (
	// ResumeFile is the pause cursor a /wrap pause leaves behind. It is
	// session-private and is never exported, even when explicitly included.
	ResumeFile = "resume.md"

	// ExcludeStart and ExcludeEnd delimit content removed from the export.
	ExcludeStart = "<!-- share:exclude:start -->"
	ExcludeEnd   = "<!-- share:exclude:end -->"

	// OwnerPlaceholder is the template placeholder replaced with the owner.
	OwnerPlaceholder = "[You]"

	projectsDir = "projects"
	readmeFile  = "README.md"
)

// DefaultFiles are exported without being included explicitly.
var DefaultFiles = []string{readmeFile, "decisions.md", "todo.md"}

// Project identifies the project being exported, with the metadata that comes
// from the project index rather than from the project folder.
type Project struct {
	Slug   string
	Status string
	Areas  []string
}

// Stamp is what the mirror header records about the source.
type Stamp struct {
	Owner string
	SHA   string // source commit the export was rendered from
	Date  string // that commit's date, YYYY-MM-DD
}

// Input is everything Render needs.
type Input struct {
	Project Project
	// Files maps a file name within the project folder (slash-separated) to
	// its content. It must already be restricted to the allowlisted set.
	Files map[string]string
	// Shared maps the workspace-relative path of a file in another project
	// shared with the same audience to its published path, so links between
	// shared projects survive. Nil means no other project is shared.
	Shared map[string]string
	Stamp  Stamp
}

// Render transforms the input files into their published form, keyed by
// published path ("<slug>/<file>").
func Render(in *Input) (map[string]string, error) {
	if in.Project.Slug == "" {
		return nil, fmt.Errorf("share: project slug is empty")
	}
	if in.Stamp.Owner == "" || in.Stamp.SHA == "" || in.Stamp.Date == "" {
		return nil, fmt.Errorf("share: stamp needs owner, commit and date")
	}

	targets := publishedTargets(in)
	names := make([]string, 0, len(in.Files))
	for name := range in.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make(map[string]string, len(names))
	for _, name := range names {
		if path.Base(name) == ResumeFile {
			return nil, fmt.Errorf("share: %s is never exported", name)
		}
		body, err := renderFile(in, name, targets)
		if err != nil {
			return nil, fmt.Errorf("share: %s: %w", name, err)
		}
		out[in.Project.Slug+"/"+name] = body
	}
	return out, nil
}

func renderFile(in *Input, name string, targets map[string]string) (string, error) {
	body := stripFrontmatter(in.Files[name])
	body, err := applyExclusions(body)
	if err != nil {
		return "", err
	}
	if name == readmeFile {
		body = rewriteMeta(body, in.Project)
	}
	body = strings.ReplaceAll(body, OwnerPlaceholder, in.Stamp.Owner)

	src := projectsDir + "/" + in.Project.Slug + "/" + name
	body, err = resolveLinks(body, src, in.Project.Slug+"/"+name, targets)
	if err != nil {
		return "", err
	}
	return header(in.Stamp) + body, nil
}

// publishedTargets maps every workspace-relative path that has a published
// counterpart to that published path: this project's exported files, plus the
// other projects shared with the same audience.
func publishedTargets(in *Input) map[string]string {
	targets := make(map[string]string, len(in.Files)+len(in.Shared))
	for ws, pub := range in.Shared {
		targets[ws] = pub
	}
	for name := range in.Files {
		targets[projectsDir+"/"+in.Project.Slug+"/"+name] = in.Project.Slug + "/" + name
	}
	return targets
}

func header(s Stamp) string {
	return fmt.Sprintf("> **Read-only mirror.** Owned by %s; published from their Consigliere workspace. "+
		"Edits here are overwritten on the next publish; contact the owner instead.\n"+
		">\n"+
		"> **Authority:** mirror — %s's workspace @ `%s` · **Verified:** %s\n\n",
		s.Owner, s.Owner, shortSHA(s.SHA), s.Date)
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
