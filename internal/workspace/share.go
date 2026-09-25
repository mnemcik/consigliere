package workspace

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ShareConfig is the `share` block of .cg.json: which projects are shared
// read-only, with whom, and how. It holds no state — whether a project has
// been published is read from the share repo's manifest, never stored here.
type ShareConfig struct {
	// Owner is the display name in the mirror header and the stakeholders
	// table. Empty falls back to git user.name.
	Owner string `json:"owner,omitempty"`
	// Denylist holds terms that must never leave (customer names,
	// codenames), matched case-insensitively in every export.
	Denylist []string `json:"denylist,omitempty"`
	// Audiences maps an audience name to its share repo and projects.
	Audiences map[string]ShareAudience `json:"audiences,omitempty"`
}

// ShareAudience is one set of recipients: a share repo and the projects
// published to it.
type ShareAudience struct {
	// Repo is the git URL of the share repo, which cg owns entirely.
	Repo string `json:"repo"`
	// Branch is the branch published to; empty means "main".
	Branch string `json:"branch,omitempty"`
	// AuthorName and AuthorEmail override the publish commit's author, for
	// a work repo published from a machine whose global identity is personal.
	AuthorName  string `json:"authorName,omitempty"`
	AuthorEmail string `json:"authorEmail,omitempty"`
	// Projects maps a project slug to its per-project settings.
	Projects map[string]ShareProject `json:"projects"`
}

// ShareProject is the per-project part of an audience.
type ShareProject struct {
	// Include names files beyond the default allowlist.
	Include []string `json:"include,omitempty"`
	// Acknowledged holds scan findings the owner judged safe to publish.
	Acknowledged []ShareAck `json:"acknowledged,omitempty"`
}

// ShareAck acknowledges one scan finding: a rule and a line hash, exactly as
// the findings report prints them.
type ShareAck struct {
	Rule string `json:"rule"`
	Hash string `json:"hash"`
}

// DefaultShareBranch is published to when an audience names no branch.
const DefaultShareBranch = "main"

// BranchOrDefault returns the audience's branch, defaulting to main.
func (a ShareAudience) BranchOrDefault() string {
	if a.Branch == "" {
		return DefaultShareBranch
	}
	return a.Branch
}

var shareNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// shareBranchRe is a conservative subset of git's ref-name rules: no leading
// '-', no spaces, no "..", no control or special characters.
var shareBranchRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/-]*$`)

// NormalizeRepo reduces a git URL to a comparable form: trimmed, lowercased,
// without a trailing "/" or ".git". It catches the same repo written twice in
// one style; an SSH alias and an https URL for one repo still differ.
func NormalizeRepo(repo string) string {
	r := strings.ToLower(strings.TrimSpace(repo))
	r = strings.TrimSuffix(r, "/")
	return strings.TrimSuffix(r, ".git")
}

// Validate reports the first structural problem in the share block, so a
// typo fails loudly instead of silently sharing less (or more) than meant.
func (s *ShareConfig) Validate() error {
	if s == nil {
		return nil
	}
	// One repo and branch belongs to one audience: the share repo is a pure
	// function of its audience's config, so a second audience on the same
	// target would read the first one's projects as removed, and publishing
	// it would delete them.
	targets := map[string]string{}
	for _, name := range s.AudienceNames() {
		a := s.Audiences[name]
		if !shareNameRe.MatchString(name) {
			return fmt.Errorf("share: audience %q: names are lowercase letters, digits, '.', '_' and '-'", name)
		}
		if strings.TrimSpace(a.Repo) == "" {
			return fmt.Errorf("share: audience %q has no repo", name)
		}
		if a.Branch != "" && (!shareBranchRe.MatchString(a.Branch) || strings.Contains(a.Branch, "..") || strings.HasSuffix(a.Branch, "/")) {
			return fmt.Errorf("share: audience %q: %q is not a usable branch name", name, a.Branch)
		}
		target := NormalizeRepo(a.Repo) + "#" + a.BranchOrDefault()
		if other, dup := targets[target]; dup {
			return fmt.Errorf("share: audiences %q and %q publish to the same repo and branch; each audience needs its own", other, name)
		}
		targets[target] = name
		if len(a.Projects) == 0 {
			return fmt.Errorf("share: audience %q shares no projects", name)
		}
		for slug, p := range a.Projects {
			if !shareNameRe.MatchString(slug) {
				return fmt.Errorf("share: audience %q: %q is not a project slug", name, slug)
			}
			for _, inc := range p.Include {
				if strings.TrimSpace(inc) == "" {
					return fmt.Errorf("share: audience %q, project %q: include has an empty entry", name, slug)
				}
			}
			for _, ack := range p.Acknowledged {
				if ack.Rule == "" || ack.Hash == "" {
					return fmt.Errorf("share: audience %q, project %q: an acknowledgement needs both rule and hash", name, slug)
				}
			}
		}
	}
	return nil
}

// AudienceNames returns the audience names, sorted.
func (s *ShareConfig) AudienceNames() []string {
	if s == nil {
		return nil
	}
	names := make([]string, 0, len(s.Audiences))
	for n := range s.Audiences {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ProjectSlugs returns the audience's project slugs, sorted.
func (a ShareAudience) ProjectSlugs() []string {
	slugs := make([]string, 0, len(a.Projects))
	for s := range a.Projects {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	return slugs
}
