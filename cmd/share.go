package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/share"
	"github.com/mnemcik/consigliere/internal/workspace"
)

func init() {
	shareExportCmd.Flags().String("out", "", "directory to write the export to (must not exist or be empty)")
	shareExportCmd.Flags().StringSlice("include", nil, "extra file in the project folder to export (repeatable)")
	shareExportCmd.Flags().String("owner", "", "owner display name for the mirror header (default: git user.name)")
	shareExportCmd.Flags().Bool("check", false, "render and scan only: report findings, write nothing")
	shareExportCmd.Flags().StringSlice("ack", nil, "acknowledge a finding judged safe, as rule:hash from the report (repeatable)")
	shareCmd.AddCommand(shareExportCmd)
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
--check runs the scan without writing.`,
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
	if out == "" && !check {
		return fmt.Errorf("--out is required unless --check is given")
	}
	acks, err := parseAcks(ackFlags)
	if err != nil {
		return err
	}

	root, err := workspaceRoot(cmd)
	if err != nil {
		return err
	}
	cfg, err := workspace.Detect(root)
	if err != nil {
		return err
	}
	indexPath := indexProjectsPath
	if cfg != nil {
		if p, ok := cfg.Indexes[dirProjects]; ok {
			indexPath = p
		}
	}

	project, err := indexedProject(filepath.Join(root, filepath.FromSlash(indexPath)), slug)
	if err != nil {
		return err
	}

	res, err := share.Export(cmd.Context(), &share.Options{
		Root:      root,
		Project:   project,
		IndexPath: filepath.ToSlash(indexPath),
		Include:   include,
		Owner:     owner,
		Scan:      share.ScanOptions{Acknowledged: acks},
	})
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
