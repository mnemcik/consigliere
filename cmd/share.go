package cmd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/share"
	"github.com/mnemcik/consigliere/internal/workspace"
)

func init() {
	shareExportCmd.Flags().String("out", "", "directory to write the export to (must not exist or be empty)")
	shareExportCmd.Flags().StringSlice("include", nil, "extra file in the project folder to export (repeatable)")
	shareExportCmd.Flags().String("owner", "", "owner display name for the mirror header (default: git user.name)")
	shareExportCmd.Flags().Bool("check", false, "render and scan only: report findings, write nothing")
	shareExportCmd.Flags().StringSlice("ack", nil, "acknowledge a finding judged safe, as rule:hash from the report (repeatable)")
	shareExportCmd.Flags().String("audience", "", "render as shared with this audience from the .cg.json share block")
	shareCmd.AddCommand(shareExportCmd)
	shareCmd.AddCommand(shareStatusCmd)
	rootCmd.AddCommand(shareCmd)
}

var shareCmd = &cobra.Command{
	Use:   "share",
	Short: "Share selected projects read-only",
	Long:  "Render workspace projects into read-only copies that can leave the workspace. The owner writes; recipients read.",
}

var shareExportCmd = &cobra.Command{
	Use:   "export <slug>",
	Short: "Render one project into a shareable folder",
	Long: `Render projects/<slug>/ into <out>/<slug>/ as a read-only copy.

Only README.md, decisions.md and todo.md leave by default; name others with
--include. resume.md is never exported. Content between
<!-- share:exclude:start --> and <!-- share:exclude:end --> is removed.
Links into the private workspace are de-linked, README's Meta block is rebuilt
from the project index, and every file gets a mirror header naming the owner
and the source commit. The project must have no uncommitted changes.

The rendered output is scanned for credentials (token formats, private keys,
JWTs, high-entropy assignments), local paths, op:// references and leftover
template placeholders. Any finding stops the export before anything is
written. Resolve a finding by fixing the source, wrapping the passage in
exclusion markers, or, for a false positive, passing --ack rule:hash.
--check runs the scan without writing.

The share block in .cg.json supplies the owner and the denylist. With
--audience, that audience's per-project include and acknowledged entries are
merged with the flags, and links to the other projects shared with the same
audience are kept instead of de-linked.`,
	Args: cobra.ExactArgs(1),
	RunE: runShareExport,
}

func runShareExport(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	slug := args[0]
	out, _ := cmd.Flags().GetString("out")
	include, _ := cmd.Flags().GetStringSlice("include")
	owner, _ := cmd.Flags().GetString("owner")
	check, _ := cmd.Flags().GetBool("check")
	ackFlags, _ := cmd.Flags().GetStringSlice("ack")
	audience, _ := cmd.Flags().GetString("audience")
	if out == "" && !check {
		return fmt.Errorf("--out is required unless --check is given")
	}
	acks, err := parseAcks(ackFlags)
	if err != nil {
		return err
	}

	env, err := loadShareEnv(cmd)
	if err != nil {
		return err
	}
	opts, err := env.exportOptions(cmd.Context(), slug, audience, include, acks, owner)
	if err != nil {
		return err
	}
	res, err := share.Export(cmd.Context(), opts)
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	if len(res.Findings) > 0 {
		printFindings(w, res.Findings)
		return fmt.Errorf("%d finding(s); nothing was written", len(res.Findings))
	}
	if check {
		_, _ = fmt.Fprintf(w, "No findings in %s @ %.12s; %d file(s) would be exported.\n", slug, res.Stamp.SHA, len(res.Files))
		return nil
	}
	if err := res.Write(out); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(w, "Exported %s @ %.12s (%s), owner %s:\n", slug, res.Stamp.SHA, res.Stamp.Date, res.Stamp.Owner)
	paths := make([]string, 0, len(res.Files))
	for p := range res.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		_, _ = fmt.Fprintf(w, "  %s\n", filepath.Join(out, filepath.FromSlash(p)))
	}
	return nil
}

// shareEnv is the workspace context every share command needs.
type shareEnv struct {
	root      string
	indexPath string // workspace-relative, slash-separated
	share     *workspace.ShareConfig
}

func loadShareEnv(cmd *cobra.Command) (*shareEnv, error) {
	root, err := workspaceRoot(cmd)
	if err != nil {
		return nil, err
	}
	cfg, err := workspace.Detect(root)
	if err != nil {
		return nil, err
	}
	env := &shareEnv{root: root, indexPath: indexProjectsPath}
	if cfg != nil {
		if p, ok := cfg.Indexes[dirProjects]; ok {
			env.indexPath = filepath.ToSlash(p)
		}
		if err := cfg.Share.Validate(); err != nil {
			return nil, err
		}
		if err := rejectOwnRepo(cmd, root, cfg.Share); err != nil {
			return nil, err
		}
		env.share = cfg.Share
	}
	return env, nil
}

// exportOptions merges the share block with the command-line overrides. The
// owner flag wins over the configured owner; includes and acknowledgements
// are the union of both; the denylist always applies. With an audience, the
// project must be one it shares, and the audience's other projects become
// link targets.
func (e *shareEnv) exportOptions(ctx context.Context, slug, audience string, include []string, acks []share.Ack, owner string) (*share.Options, error) {
	opts, a, err := e.baseOptions(slug, audience, include, acks, owner)
	if err != nil || audience == "" {
		return opts, err
	}
	indexed, err := indexedSlugs(filepath.Join(e.root, filepath.FromSlash(e.indexPath)))
	if err != nil {
		return nil, err
	}
	opts.Shared = map[string]string{}
	for _, other := range a.ProjectSlugs() {
		if other == slug || !indexed[other] {
			continue
		}
		// A link to a sibling stays live only if the sibling itself would be
		// published: its export must run and come out clean. Anything else
		// (open findings, uncommitted changes, a broken marker or include)
		// would leave a dangling link, so the sibling stays private. The
		// sibling is rendered without its own siblings, which cannot change
		// the outcome: rewritten links are relative paths no rule matches.
		sibling, _, err := e.baseOptions(other, audience, nil, nil, owner)
		if err != nil {
			continue
		}
		res, err := share.Export(ctx, sibling)
		if err != nil || len(res.Findings) > 0 {
			continue
		}
		for p := range res.Files {
			opts.Shared[dirProjects+"/"+p] = p
		}
	}
	return opts, nil
}

// baseOptions merges the share block with the command-line overrides, without
// sibling links. It returns the audience when one is named.
func (e *shareEnv) baseOptions(slug, audience string, include []string, acks []share.Ack, owner string) (*share.Options, workspace.ShareAudience, error) {
	var none workspace.ShareAudience
	project, err := indexedProject(filepath.Join(e.root, filepath.FromSlash(e.indexPath)), slug)
	if err != nil {
		return nil, none, err
	}
	opts := &share.Options{
		Root:      e.root,
		Project:   project,
		IndexPath: e.indexPath,
		Include:   include,
		Owner:     owner,
		Scan:      share.ScanOptions{Acknowledged: acks},
	}
	opts.Owner = strings.TrimSpace(opts.Owner)
	if e.share != nil {
		if opts.Owner == "" {
			opts.Owner = strings.TrimSpace(e.share.Owner)
		}
		opts.Scan.Denylist = e.share.Denylist
	}
	if audience == "" {
		return opts, none, nil
	}

	a, ok := e.audience(audience)
	if !ok {
		return nil, none, fmt.Errorf("audience %q is not in the .cg.json share block", audience)
	}
	p, ok := a.Projects[slug]
	if !ok {
		return nil, none, fmt.Errorf("audience %q does not share project %q", audience, slug)
	}
	opts.Include = append(append([]string(nil), p.Include...), include...)
	for _, ack := range p.Acknowledged {
		opts.Scan.Acknowledged = append(opts.Scan.Acknowledged, share.Ack{Rule: ack.Rule, Hash: ack.Hash})
	}
	return opts, a, nil
}

// indexedSlugs returns the project slugs that have a row in the index.
func indexedSlugs(indexPath string) (map[string]bool, error) {
	projects, err := parseProjectIndex(indexPath)
	if err != nil {
		return nil, fmt.Errorf("reading project index: %w", err)
	}
	slugs := make(map[string]bool, len(projects))
	for _, p := range projects {
		slugs[p.Folder] = true
	}
	return slugs, nil
}

// rejectOwnRepo refuses a share block whose audience points at the
// workspace's own origin: publishing there would push the private workspace's
// content into the repo it came from. Comparison is by normalized URL, so an
// alias spelling of the same repo is not caught; a publish command must check
// the resolved remote again before it pushes.
func rejectOwnRepo(cmd *cobra.Command, root string, sc *workspace.ShareConfig) error {
	origin, err := gitx.RemoteURL(cmd.Context(), root, "origin")
	if err != nil || origin == "" {
		return nil
	}
	for _, name := range sc.AudienceNames() {
		if workspace.NormalizeRepo(sc.Audiences[name].Repo) == workspace.NormalizeRepo(origin) {
			return fmt.Errorf("share: audience %q points at this workspace's own repo; a share repo must be a separate repo", name)
		}
	}
	return nil
}

func (e *shareEnv) audience(name string) (workspace.ShareAudience, bool) {
	if e.share == nil {
		return workspace.ShareAudience{}, false
	}
	a, ok := e.share.Audiences[name]
	return a, ok
}

// shareReadTimeout bounds reading one share repo, so status never hangs on a
// slow or unresponsive remote.
const shareReadTimeout = 60 * time.Second

var shareStatusCmd = &cobra.Command{
	Use:   "status [<audience>]",
	Short: "Show what each audience has, against what it would get now",
	Long: `For every audience in the .cg.json share block (or just the one named),
render each shared project, scan it, and compare it with the copy recorded in
the share repo's manifest. Read-only: nothing is written or pushed.

States: up to date, stale (the next publish would change it), not published,
blocked (open scan findings), error (the export cannot run, e.g. uncommitted
changes), and removed (published, but no longer in the config).`,
	Args: cobra.MaximumNArgs(1),
	RunE: runShareStatus,
}

func runShareStatus(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	env, err := loadShareEnv(cmd)
	if err != nil {
		return err
	}
	names := env.share.AudienceNames()
	if len(args) == 1 {
		if _, ok := env.audience(args[0]); !ok {
			return fmt.Errorf("audience %q is not in the .cg.json share block", args[0])
		}
		names = []string{args[0]}
	}
	w := cmd.OutOrStdout()
	if len(names) == 0 {
		_, _ = fmt.Fprintln(w, "No audiences in the .cg.json share block; nothing is shared. See docs/share.md.")
		return nil
	}
	for i, name := range names {
		if i > 0 {
			_, _ = fmt.Fprintln(w)
		}
		env.printAudienceStatus(cmd, w, name)
	}
	return nil
}

func (e *shareEnv) printAudienceStatus(cmd *cobra.Command, w io.Writer, name string) {
	a, _ := e.audience(name)
	_, _ = fmt.Fprintf(w, "%s → %s (%s)\n", name, share.RedactURL(a.Repo), a.BranchOrDefault())
	ctx, cancel := context.WithTimeout(cmd.Context(), shareReadTimeout)
	manifest, err := share.ReadPublished(ctx, a.Repo, a.BranchOrDefault())
	cancel()
	if err != nil {
		_, _ = fmt.Fprintf(w, "  cannot read the share repo: %s\n", firstLine(err.Error()))
	}
	if manifest != nil && manifest.Audience != name {
		// Another audience's copy: comparing against it would report its
		// projects as removed. Refuse to compare instead.
		_, _ = fmt.Fprintf(w, "  the share repo holds audience %q, not %q; not comparing\n", manifest.Audience, name)
		manifest, err = nil, fmt.Errorf("audience mismatch")
	}
	published := map[string]share.PublishedProject{}
	if manifest != nil {
		published = manifest.Projects
	}

	for _, slug := range a.ProjectSlugs() {
		state, detail := e.projectStatus(cmd, name, slug, published, err == nil)
		_, _ = fmt.Fprintf(w, "  %-32s %-16s %s\n", slug, state, detail)
	}
	var removed []string
	for slug := range published {
		if _, ok := a.Projects[slug]; !ok {
			removed = append(removed, slug)
		}
	}
	sort.Strings(removed)
	for _, slug := range removed {
		_, _ = fmt.Fprintf(w, "  %-32s %-16s %s\n", slug, "removed", "published, no longer in the config; the next publish deletes it")
	}
}

func (e *shareEnv) projectStatus(cmd *cobra.Command, audience, slug string, published map[string]share.PublishedProject, repoRead bool) (state, detail string) {
	opts, err := e.exportOptions(cmd.Context(), slug, audience, nil, nil, "")
	if err != nil {
		return "error", firstLine(err.Error())
	}
	res, err := share.Export(cmd.Context(), opts)
	if err != nil {
		return "error", firstLine(err.Error())
	}
	current := fmt.Sprintf("current %.12s (%s)", res.Stamp.SHA, res.Stamp.Date)
	if n := len(res.Findings); n > 0 {
		return "blocked", fmt.Sprintf("%d open finding(s); run cg share export %s --audience %s --check; %s", n, slug, audience, current)
	}
	if !repoRead {
		return "unknown", current
	}
	p, ok := published[slug]
	switch {
	case !ok:
		return "not published", current
	case p.ContentHash == share.ContentHash(res.Files):
		return "up to date", fmt.Sprintf("published %.12s (%s)", p.SourceCommit, p.SourceDate)
	default:
		return "stale", fmt.Sprintf("published %.12s (%s), %s", p.SourceCommit, p.SourceDate, current)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// indexedProject looks the slug up in the project index, which is the
// authoritative source of a project's status and areas.
func indexedProject(indexPath, slug string) (share.Project, error) {
	projects, err := parseProjectIndex(indexPath)
	if err != nil {
		return share.Project{}, fmt.Errorf("reading project index: %w", err)
	}
	for _, p := range projects {
		if p.Folder == slug {
			return share.Project{Slug: slug, Status: p.Status, Areas: extractAreaSlugs(p.Areas)}, nil
		}
	}
	return share.Project{}, fmt.Errorf("project %q has no row in the project index", slug)
}

// parseAcks turns rule:hash flags into acknowledgements.
func parseAcks(flags []string) ([]share.Ack, error) {
	acks := make([]share.Ack, 0, len(flags))
	for _, f := range flags {
		rule, hash, ok := strings.Cut(f, ":")
		if !ok || rule == "" || hash == "" {
			return nil, fmt.Errorf("--ack %q: want rule:hash as printed in the findings report", f)
		}
		acks = append(acks, share.Ack{Rule: rule, Hash: hash})
	}
	return acks, nil
}

func printFindings(w io.Writer, findings []share.Finding) {
	_, _ = fmt.Fprintln(w, "Confidentiality findings (line numbers are in the rendered file):")
	for _, f := range findings {
		_, _ = fmt.Fprintf(w, "  %s:%d  %-22s %s  [ack: %s:%s]\n", f.File, f.Line, f.Rule, f.Match, f.Rule, f.Hash)
	}
	_, _ = fmt.Fprintln(w, "Fix the source, exclude the passage with share:exclude markers, or --ack a false positive.")
}
