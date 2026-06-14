package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

var extInstallRef string
var extListJSON bool

func init() {
	rootCmd.AddCommand(extensionCmd)
	extensionCmd.AddCommand(extInstallCmd, extListCmd)
	extInstallCmd.Flags().StringVar(&extInstallRef, "ref", "",
		"git ref (tag or branch) to check out after cloning (default: the repo's default branch)")
	extListCmd.Flags().BoolVar(&extListJSON, "json", false, "output as JSON")
}

var extensionCmd = &cobra.Command{
	Use:     "extension",
	Aliases: []string{"ext"},
	Short:   "Manage cg extensions",
	Long: "Install, list, and manage cg extensions — pluggable workspace- or " +
		"domain-specific behaviour. See docs/extensions.md for the design and " +
		"EXTENSIONS.md for authoring.",
}

var extInstallCmd = &cobra.Command{
	Use:   "install <repo-url>",
	Short: "Install an extension from a git repo URL",
	Long: "Clone an extension from a git repo URL (or local path), validate its " +
		"cg-extension.json manifest, and record it in .cg.json.\n\n" +
		"Registry lookup by short name lands in a later milestone; for now pass a " +
		"repo URL or a local path.",
	Args: cobra.ExactArgs(1),
	RunE: runExtInstall,
}

var extListCmd = &cobra.Command{
	Use:   "list",
	Short: "List extensions installed in this workspace",
	Args:  cobra.NoArgs,
	RunE:  runExtList,
}

func runExtInstall(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	src := args[0]
	if !looksLikeRepoSource(src) {
		return cgerr.New(cgerr.ExitUsage,
			"registry lookup by name is not yet available; pass a repo URL or local path (got %q)", src)
	}
	if !gitx.Available() {
		return cgerr.New(cgerr.ExitUsage, "git is required to install extensions")
	}

	root, err := workspaceRoot(cmd)
	if err != nil {
		return err
	}
	cfg, err := workspace.Detect(root)
	if err != nil {
		return err
	}
	if cfg == nil {
		return cgerr.New(cgerr.ExitUsage, "%s not found in %s; run `cg init` first", workspace.ConfigFile, root)
	}

	ctx := cmd.Context()
	staging := extension.StagingDir()
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("clearing staging dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(staging), 0o755); err != nil { //nolint:gosec // user config dir
		return fmt.Errorf("creating extensions dir: %w", err)
	}
	// Best-effort cleanup: harmless once a successful install renames staging away.
	defer func() { _ = os.RemoveAll(staging) }()

	if err := gitx.Clone(ctx, src, staging, extInstallRef); err != nil {
		return cgerr.New(cgerr.ExitUsage, "cloning %s: %v", src, err)
	}

	m, err := extension.LoadManifest(staging)
	if err != nil {
		return cgerr.New(cgerr.ExitUsage, "reading %s: %v", extension.ManifestFile, err)
	}
	if err := m.Validate(); err != nil {
		return cgerr.New(cgerr.ExitUsage, "invalid %s: %v", extension.ManifestFile, err)
	}

	dest := extension.CloneDir(m.Name)
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("clearing prior install at %s: %w", dest, err)
	}
	if err := os.Rename(staging, dest); err != nil {
		return fmt.Errorf("installing to %s: %w", dest, err)
	}

	cfg.UpsertExtension(&workspace.ExtensionRef{
		Name:      m.Name,
		Version:   m.Version,
		Source:    workspace.ExtSourceDirect,
		Repo:      src,
		Installed: time.Now().UTC().Format(time.RFC3339),
	})
	if err := cfg.Save(root); err != nil {
		return fmt.Errorf("updating %s: %w", workspace.ConfigFile, err)
	}

	printInstallSummary(cmd, m, dest, src)
	return nil
}

func runExtList(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	root, err := workspaceRoot(cmd)
	if err != nil {
		return err
	}
	cfg, err := workspace.Detect(root)
	if err != nil {
		return err
	}
	var exts []workspace.ExtensionRef
	if cfg != nil {
		exts = cfg.Extensions
	}
	out := cmd.OutOrStdout()

	if extListJSON {
		if exts == nil {
			exts = []workspace.ExtensionRef{}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(exts)
	}

	if len(exts) == 0 {
		_, _ = fmt.Fprintln(out, "no extensions installed")
		return nil
	}
	_, _ = fmt.Fprintf(out, "%-20s %-10s %-9s %s\n", "NAME", "VERSION", "SOURCE", "REPO")
	for _, e := range exts {
		_, _ = fmt.Fprintf(out, "%-20s %-10s %-9s %s\n", e.Name, e.Version, e.Source, e.Repo)
	}
	return nil
}

// looksLikeRepoSource reports whether s is a git URL or local path rather than a
// bare registry name. Bare names route to registry resolution (a later
// milestone); until then they are rejected with a clear message.
func looksLikeRepoSource(s string) bool {
	switch {
	case strings.Contains(s, "://"),
		strings.HasPrefix(s, "git@"),
		strings.HasPrefix(s, "github.com/"),
		strings.HasPrefix(s, "/"),
		strings.HasPrefix(s, "./"),
		strings.HasPrefix(s, "../"):
		return true
	}
	// An existing local directory is a valid clone source too.
	if info, err := os.Stat(s); err == nil && info.IsDir() {
		return true
	}
	return false
}

func printInstallSummary(cmd *cobra.Command, m *extension.Manifest, dest, src string) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Installed %s v%s — %s\n", m.Name, m.Version, m.Description)
	_, _ = fmt.Fprintf(out, "  source:   direct (%s)\n", src)
	_, _ = fmt.Fprintf(out, "  location: %s\n", dest)

	c := m.Contributes
	declared := []string{}
	add := func(n int, label string) {
		if n > 0 {
			declared = append(declared, fmt.Sprintf("%d %s", n, label))
		}
	}
	add(len(c.ClaudeMDSections), "CLAUDE.md section(s)")
	add(len(c.Notes), "note(s)")
	add(len(c.Hooks), "hook(s)")
	add(len(c.Subcommands), "subcommand(s)")
	add(len(c.Templates), "template(s)")
	if len(declared) == 0 {
		_, _ = fmt.Fprintln(out, "  declares no contributions")
		return
	}
	_, _ = fmt.Fprintf(out, "  declares: %s\n", strings.Join(declared, ", "))
}
