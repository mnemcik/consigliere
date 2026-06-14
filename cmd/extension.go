package cmd

import (
	"context"
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
var extRemovePurge bool

func init() {
	rootCmd.AddCommand(extensionCmd)
	extensionCmd.AddCommand(extInstallCmd, extListCmd, extRemoveCmd, extUpdateCmd)
	extInstallCmd.Flags().StringVar(&extInstallRef, "ref", "",
		"git ref (tag or branch) to check out after cloning (default: the repo's default branch)")
	extListCmd.Flags().BoolVar(&extListJSON, "json", false, "output as JSON")
	extRemoveCmd.Flags().BoolVar(&extRemovePurge, "purge", false,
		"also delete the machine-shared clone under ~/.config/consigliere/extensions/")
}

var extensionCmd = &cobra.Command{
	Use:     "extension",
	Aliases: []string{"ext"},
	Short:   "Manage cg extensions",
	Long: "Install, list, update, and remove cg extensions — pluggable workspace- " +
		"or domain-specific behaviour. See docs/extensions.md for the design and " +
		"EXTENSIONS.md for authoring.",
}

var extInstallCmd = &cobra.Command{
	Use:   "install <name|repo-url>",
	Short: "Install an extension from the registry or a git repo URL",
	Long: "Install an extension and record it in .cg.json.\n\n" +
		"A bare name is resolved against the registry; a git URL or local path is " +
		"cloned directly. The cg-extension.json manifest is validated before the " +
		"extension is recorded.",
	Args: cobra.ExactArgs(1),
	RunE: runExtInstall,
}

var extListCmd = &cobra.Command{
	Use:   "list",
	Short: "List extensions installed in this workspace",
	Args:  cobra.NoArgs,
	RunE:  runExtList,
}

var extRemoveCmd = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"rm"},
	Short:   "Remove an installed extension from this workspace",
	Args:    cobra.ExactArgs(1),
	RunE:    runExtRemove,
}

var extUpdateCmd = &cobra.Command{
	Use:   "update [<name>]",
	Short: "Update one or all installed extensions to their latest version",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runExtUpdate,
}

func runExtInstall(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	arg := args[0]
	if !gitx.Available() {
		return cgerr.New(cgerr.ExitUsage, "git is required to install extensions")
	}

	root, cfg, err := requireWorkspace(cmd)
	if err != nil {
		return err
	}

	// A git URL or local path installs directly; a bare name resolves against
	// the registry.
	repo, source := arg, workspace.ExtSourceDirect
	if !looksLikeRepoSource(arg) {
		entry, rerr := resolveRegistry(cmd.Context(), arg)
		if rerr != nil {
			return rerr
		}
		repo, source = entry.Repo, workspace.ExtSourceRegistry
	}

	m, dest, err := installFrom(cmd.Context(), repo, extInstallRef)
	if err != nil {
		return err
	}
	if err := reapply(root, dest, m); err != nil {
		return err
	}
	cfg.UpsertExtension(&workspace.ExtensionRef{
		Name:      m.Name,
		Version:   m.Version,
		Source:    source,
		Repo:      repo,
		Installed: time.Now().UTC().Format(time.RFC3339),
	})
	if err := cfg.Save(root); err != nil {
		return fmt.Errorf("updating %s: %w", workspace.ConfigFile, err)
	}

	printInstallSummary(cmd, m, dest, source, repo)
	return nil
}

// reapply applies m's contributions into the workspace and persists the ledger,
// then reverses any contributions a prior install of the same extension shipped
// that the new manifest drops. It applies the NEW manifest first: Apply
// self-rolls-back on failure, so a failed reinstall/update leaves the prior
// install intact rather than half-removed.
func reapply(root, cloneDir string, m *extension.Manifest) error {
	old, err := extension.LoadLedger(root, m.Name)
	if err != nil {
		return fmt.Errorf("reading ledger for %q: %w", m.Name, err)
	}
	ledger, err := extension.Apply(root, cloneDir, m)
	if err != nil {
		return cgerr.New(cgerr.ExitUsage, "applying contributions for %q: %v", m.Name, err)
	}
	if orphan := extension.OrphanLedger(old, ledger); orphan != nil {
		if rerr := extension.Reverse(root, orphan); rerr != nil {
			return fmt.Errorf("removing dropped contributions for %q: %w", m.Name, rerr)
		}
	}
	if err := ledger.Save(root); err != nil {
		return fmt.Errorf("writing ledger for %q: %w", m.Name, err)
	}
	return nil
}

// installFrom clones repo (at ref, if non-empty) into a staging dir, validates
// its manifest, and atomically renames it into the machine-shared install
// location. It returns the validated manifest and the install path. It does not
// touch the workspace — callers Apply contributions and record the .cg.json entry.
func installFrom(ctx context.Context, repo, ref string) (*extension.Manifest, string, error) {
	staging := extension.StagingDir()
	if err := os.RemoveAll(staging); err != nil {
		return nil, "", fmt.Errorf("clearing staging dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(staging), 0o755); err != nil { //nolint:gosec // user config dir
		return nil, "", fmt.Errorf("creating extensions dir: %w", err)
	}
	// Best-effort cleanup: harmless once a successful install renames staging away.
	defer func() { _ = os.RemoveAll(staging) }()

	if err := gitx.Clone(ctx, repo, staging, ref); err != nil {
		return nil, "", cgerr.New(cgerr.ExitUsage, "cloning %s: %v", repo, err)
	}
	m, err := extension.LoadManifest(staging)
	if err != nil {
		return nil, "", cgerr.New(cgerr.ExitUsage, "reading %s: %v", extension.ManifestFile, err)
	}
	if err := m.Validate(); err != nil {
		return nil, "", cgerr.New(cgerr.ExitUsage, "invalid %s: %v", extension.ManifestFile, err)
	}

	dest := extension.CloneDir(m.Name)
	if err := os.RemoveAll(dest); err != nil {
		return nil, "", fmt.Errorf("clearing prior install at %s: %w", dest, err)
	}
	if err := os.Rename(staging, dest); err != nil {
		return nil, "", fmt.Errorf("installing to %s: %w", dest, err)
	}
	return m, dest, nil
}

// resolveRegistry fetches the catalogue and finds the named extension.
func resolveRegistry(ctx context.Context, name string) (*extension.RegistryEntry, error) {
	idx, err := extension.FetchIndex(ctx)
	if err != nil {
		return nil, cgerr.New(cgerr.ExitUsage, "%v", err)
	}
	entry, ok := idx.Find(name)
	if !ok {
		return nil, cgerr.New(cgerr.ExitUsage,
			"extension %q not found in the registry; pass a repo URL to install directly", name)
	}
	return entry, nil
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
// bare registry name. Bare names route to registry resolution.
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

// requireWorkspace resolves the workspace root and its config, erroring when the
// command is not run inside an initialised workspace.
func requireWorkspace(cmd *cobra.Command) (string, *workspace.Config, error) {
	root, err := workspaceRoot(cmd)
	if err != nil {
		return "", nil, err
	}
	cfg, err := workspace.Detect(root)
	if err != nil {
		return "", nil, err
	}
	if cfg == nil {
		return "", nil, cgerr.New(cgerr.ExitUsage, "%s not found in %s; run `cg init` first", workspace.ConfigFile, root)
	}
	return root, cfg, nil
}

func printInstallSummary(cmd *cobra.Command, m *extension.Manifest, dest, source, repo string) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Installed %s v%s — %s\n", m.Name, m.Version, m.Description)
	_, _ = fmt.Fprintf(out, "  source:   %s (%s)\n", source, repo)
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
		_, _ = fmt.Fprintln(out, "  no contributions")
		return
	}
	_, _ = fmt.Fprintf(out, "  applied: %s\n", strings.Join(declared, ", "))
}

func runExtRemove(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	name := args[0]

	root, cfg, err := requireWorkspace(cmd)
	if err != nil {
		return err
	}
	if _, ok := findExtension(cfg, name); !ok {
		return cgerr.New(cgerr.ExitUsage, "extension %q is not installed in this workspace", name)
	}

	// Reverse what the install applied to this workspace, recorded in the ledger,
	// then drop the ledger file.
	ledger, err := extension.LoadLedger(root, name)
	if err != nil {
		return fmt.Errorf("reading ledger for %q: %w", name, err)
	}
	if ledger != nil {
		if rerr := extension.Reverse(root, ledger); rerr != nil {
			return fmt.Errorf("reversing contributions for %q: %w", name, rerr)
		}
		if rmErr := os.Remove(extension.LedgerPath(root, name)); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("removing ledger for %q: %w", name, rmErr)
		}
	}

	cfg.RemoveExtension(name)
	if err := cfg.Save(root); err != nil {
		return fmt.Errorf("updating %s: %w", workspace.ConfigFile, err)
	}

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Removed %s from this workspace\n", name)
	clone := extension.CloneDir(name)
	if extRemovePurge {
		if err := os.RemoveAll(clone); err != nil {
			return fmt.Errorf("purging clone %s: %w", clone, err)
		}
		_, _ = fmt.Fprintf(out, "  purged shared clone: %s\n", clone)
	} else {
		_, _ = fmt.Fprintf(out, "  shared clone left in place: %s (use --purge to delete)\n", clone)
	}
	return nil
}

func runExtUpdate(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	if !gitx.Available() {
		return cgerr.New(cgerr.ExitUsage, "git is required to update extensions")
	}
	root, cfg, err := requireWorkspace(cmd)
	if err != nil {
		return err
	}

	var targets []workspace.ExtensionRef
	if len(args) == 1 {
		ref, ok := findExtension(cfg, args[0])
		if !ok {
			return cgerr.New(cgerr.ExitUsage, "extension %q is not installed in this workspace", args[0])
		}
		targets = []workspace.ExtensionRef{*ref}
	} else {
		targets = cfg.Extensions
	}
	if len(targets) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no extensions installed")
		return nil
	}

	out := cmd.OutOrStdout()
	for i := range targets {
		old := targets[i].Version
		newVer, err := updateOne(cmd.Context(), root, targets[i].Name)
		if err != nil {
			return err
		}
		ref := targets[i]
		ref.Version = newVer
		cfg.UpsertExtension(&ref)
		if old == newVer {
			_, _ = fmt.Fprintf(out, "%s: already at v%s\n", ref.Name, newVer)
		} else {
			_, _ = fmt.Fprintf(out, "%s: v%s → v%s\n", ref.Name, old, newVer)
		}
	}
	if err := cfg.Save(root); err != nil {
		return fmt.Errorf("updating %s: %w", workspace.ConfigFile, err)
	}
	return nil
}

// updateOne fetches the clone of name, checks out its latest tag (or default
// branch when untagged), re-validates the manifest, re-applies its contributions
// to the workspace (reverse the prior ledger, then apply the new manifest), and
// returns the new version.
func updateOne(ctx context.Context, root, name string) (string, error) {
	clone := extension.CloneDir(name)
	if _, err := os.Stat(clone); err != nil {
		return "", cgerr.New(cgerr.ExitUsage,
			"clone for %q missing at %s; reinstall it", name, clone)
	}
	if err := gitx.Fetch(ctx, clone, "origin", "--tags"); err != nil {
		return "", cgerr.New(cgerr.ExitUsage, "fetching %q: %v", name, err)
	}
	if tag := gitx.LatestTag(ctx, clone); tag != "" {
		if err := gitx.Checkout(ctx, clone, tag); err != nil {
			return "", cgerr.New(cgerr.ExitUsage, "checking out %s of %q: %v", tag, name, err)
		}
	} else {
		db := gitx.DefaultBranch(ctx, clone)
		if err := gitx.Checkout(ctx, clone, db); err != nil {
			return "", cgerr.New(cgerr.ExitUsage, "checking out %s of %q: %v", db, name, err)
		}
		if err := gitx.MergeFFOnly(ctx, clone, "origin/"+db); err != nil {
			return "", cgerr.New(cgerr.ExitUsage, "fast-forwarding %q: %v", name, err)
		}
	}
	m, err := extension.LoadManifest(clone)
	if err != nil {
		return "", cgerr.New(cgerr.ExitUsage, "reading %s for %q: %v", extension.ManifestFile, name, err)
	}
	if err := m.Validate(); err != nil {
		return "", cgerr.New(cgerr.ExitUsage, "invalid %s for %q: %v", extension.ManifestFile, name, err)
	}

	// Re-apply: new manifest first, then reverse any contributions it dropped.
	if err := reapply(root, clone, m); err != nil {
		return "", err
	}
	return m.Version, nil
}

// findExtension returns the workspace's recorded entry for name.
func findExtension(cfg *workspace.Config, name string) (*workspace.ExtensionRef, bool) {
	for i := range cfg.Extensions {
		if cfg.Extensions[i].Name == name {
			return &cfg.Extensions[i], true
		}
	}
	return nil, false
}
