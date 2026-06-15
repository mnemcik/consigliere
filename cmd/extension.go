package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

var extInstallRef string
var extInstallPath string
var extListJSON bool
var extRemovePurge bool

func init() {
	rootCmd.AddCommand(extensionCmd)
	extensionCmd.AddCommand(extInstallCmd, extListCmd, extRemoveCmd, extUpdateCmd)
	extInstallCmd.Flags().StringVar(&extInstallRef, "ref", "",
		"git ref (tag or branch) to check out after cloning (default: the repo's default branch)")
	extInstallCmd.Flags().StringVar(&extInstallPath, "path", "",
		"subdir within the repo holding cg-extension.json (for monorepos; direct repo-url installs only)")
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
	Use:   "install <registry>/<name> | <repo-url>",
	Short: "Install an extension by fully-qualified name or a git repo URL",
	Long: "Install an extension and record it in .cg.json.\n\n" +
		"A fully-qualified name <registry>/<extension> (e.g. cg/1password) is resolved " +
		"against that one named registry from .cg.json (the built-in `cg` alias is the " +
		"public catalogue). A git URL or local path is cloned directly. Bare names are " +
		"rejected — the source must be unambiguous. The cg-extension.json manifest is " +
		"validated before the extension is recorded.",
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

	// A git URL or local path installs directly. Otherwise the arg must be a
	// fully-qualified "<registry>/<extension>" name, resolved against that one
	// named registry — there is no bare-name or first-match resolution. The
	// subdir holding the manifest comes from --path for direct installs and from
	// the catalogue entry for registry installs.
	repo, source, path, regAlias := arg, workspace.ExtSourceDirect, extInstallPath, ""
	if !looksLikeRepoSource(arg) {
		if extInstallPath != "" {
			return cgerr.New(cgerr.ExitUsage,
				"--path applies only to direct repo-url installs; a registry entry carries its own subdir")
		}
		alias, name, ok := extension.ParseQualifiedName(arg)
		if !ok {
			return cgerr.New(cgerr.ExitUsage,
				"install needs a fully-qualified name <registry>/<extension> (e.g. %s/%s) or a repo URL; got %q. Configured registries: %s",
				extension.BuiltinRegistryAlias, arg, arg, registryAliasList(cfg))
		}
		entry, rerr := resolveQualified(cmd.Context(), cfg, alias, name)
		if rerr != nil {
			return rerr
		}
		repo, source, path, regAlias = entry.Repo, workspace.ExtSourceRegistry, entry.Path, alias
	}
	path, err = extension.CleanSubdir(path)
	if err != nil {
		return cgerr.New(cgerr.ExitUsage, "%v", err)
	}

	m, dest, err := installFrom(cmd.Context(), repo, extInstallRef, path)
	if err != nil {
		return err
	}
	if err := reapply(root, filepath.Join(dest, path), m); err != nil {
		return err
	}
	cfg.UpsertExtension(&workspace.ExtensionRef{
		Name:      m.Name,
		Version:   m.Version,
		Source:    source,
		Registry:  regAlias,
		Repo:      repo,
		Path:      path,
		Installed: time.Now().UTC().Format(time.RFC3339),
	})
	if err := cfg.Save(root); err != nil {
		return fmt.Errorf("updating %s: %w", workspace.ConfigFile, err)
	}

	printInstallSummary(cmd, m, dest, source, regAlias, repo, path)
	return nil
}

// reapply applies m's contributions into the workspace and persists the ledger,
// then reverses any contributions a prior install of the same extension shipped
// that the new manifest drops. It applies the NEW manifest first: Apply
// self-rolls-back on failure, so a failed reinstall/update leaves the prior
// install intact rather than half-removed.
func reapply(root, manifestDir string, m *extension.Manifest) error {
	old, err := extension.LoadLedger(root, m.Name)
	if err != nil {
		return fmt.Errorf("reading ledger for %q: %w", m.Name, err)
	}
	ledger, err := extension.Apply(root, manifestDir, m)
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

// reinstallMissingExtensions re-clones and re-applies any extension recorded in
// .cg.json whose machine-shared clone is absent (the re-cloned-workspace case),
// leaving present clones untouched. Returns short descriptions for the init
// summary.
func reinstallMissingExtensions(cmd *cobra.Command, root string, exts []workspace.ExtensionRef) ([]string, error) {
	var done []string
	for _, e := range exts {
		if _, err := os.Stat(extension.CloneDir(e.Name)); err == nil {
			continue // clone present; nothing to re-clone
		}
		if !gitx.Available() {
			return done, cgerr.New(cgerr.ExitUsage, "git is required to re-install extension %q", e.Name)
		}
		path, perr := extension.CleanSubdir(e.Path)
		if perr != nil {
			return done, cgerr.New(cgerr.ExitUsage, "extension %q: %v", e.Name, perr)
		}
		m, dest, err := installFrom(cmd.Context(), e.Repo, "", path)
		if err != nil {
			return done, err
		}
		if err := reapply(root, filepath.Join(dest, path), m); err != nil {
			return done, err
		}
		done = append(done, fmt.Sprintf("extension %q v%s (re-installed)", m.Name, m.Version))
	}
	return done, nil
}

// installFrom clones repo (at ref, if non-empty) into a staging dir, reads and
// validates the manifest at <staging>/<path> (path empty = repo root), and
// atomically renames the whole clone into the machine-shared install location
// keyed by the extension name. It returns the validated manifest and the clone
// root (the manifest and its payloads live at <cloneRoot>/<path>). It does not
// touch the workspace — callers Apply contributions and record the .cg.json entry.
func installFrom(ctx context.Context, repo, ref, path string) (*extension.Manifest, string, error) {
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
	m, err := extension.LoadManifest(filepath.Join(staging, path))
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

// resolveQualified resolves alias to its registry source, fetches that one
// catalogue, and finds the named extension in it. The alias names exactly one
// source, so resolution is unambiguous — no search across registries.
func resolveQualified(ctx context.Context, cfg *workspace.Config, alias, name string) (*extension.RegistryEntry, error) {
	src, ok := registrySource(cfg, alias)
	if !ok {
		return nil, cgerr.New(cgerr.ExitUsage,
			"unknown registry %q. Configured registries: %s", alias, registryAliasList(cfg))
	}
	idx, err := extension.FetchIndexFrom(ctx, src)
	if err != nil {
		return nil, cgerr.New(cgerr.ExitUsage, "%v", err)
	}
	entry, ok := idx.Find(name)
	if !ok {
		return nil, cgerr.New(cgerr.ExitUsage,
			"extension %q not found in registry %q; pass a repo URL to install directly", name, alias)
	}
	return entry, nil
}

// registrySource resolves a registry alias to its source string. The "cg"
// builtin alias always resolves — to the env override if set, else the
// workspace's configured source, else the public default — so first-party
// extensions work even in workspaces whose .cg.json predates the registries
// map. Any other alias must be present in .cg.json registries.
func registrySource(cfg *workspace.Config, alias string) (string, bool) {
	if alias == extension.BuiltinRegistryAlias {
		if env := strings.TrimSpace(os.Getenv("CONSIGLIERE_EXTENSIONS_REGISTRY")); env != "" {
			return env, true
		}
	}
	if cfg != nil {
		if src, ok := cfg.Registries[alias]; ok && strings.TrimSpace(src) != "" {
			return src, true
		}
	}
	if alias == extension.BuiltinRegistryAlias {
		return extension.DefaultRegistryURL, true
	}
	return "", false
}

// registryAliasList returns the configured registry aliases (plus the builtin
// "cg") as a sorted, comma-joined string for error messages.
func registryAliasList(cfg *workspace.Config) string {
	seen := map[string]bool{extension.BuiltinRegistryAlias: true}
	aliases := []string{extension.BuiltinRegistryAlias}
	if cfg != nil {
		for a := range cfg.Registries {
			if !seen[a] {
				seen[a] = true
				aliases = append(aliases, a)
			}
		}
	}
	sort.Strings(aliases)
	return strings.Join(aliases, ", ")
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
	_, _ = fmt.Fprintf(out, "%-20s %-10s %-12s %s\n", "NAME", "VERSION", "SOURCE", "REPO")
	for _, e := range exts {
		// For registry installs show the alias the extension came from (its
		// fully-qualified prefix); direct installs show "direct".
		src := e.Source
		if e.Registry != "" {
			src = "registry:" + e.Registry
		}
		_, _ = fmt.Fprintf(out, "%-20s %-10s %-12s %s\n", e.Name, e.Version, src, e.Repo)
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

func printInstallSummary(cmd *cobra.Command, m *extension.Manifest, dest, source, regAlias, repo, path string) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Installed %s v%s — %s\n", m.Name, m.Version, m.Description)
	if regAlias != "" {
		_, _ = fmt.Fprintf(out, "  source:   %s %s/%s (%s)\n", source, regAlias, m.Name, repo)
	} else {
		_, _ = fmt.Fprintf(out, "  source:   %s (%s)\n", source, repo)
	}
	if path != "" {
		_, _ = fmt.Fprintf(out, "  subdir:   %s\n", filepath.ToSlash(path))
	}
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
		newVer, err := updateOne(cmd.Context(), root, targets[i].Name, targets[i].Path)
		if err != nil {
			return err
		}
		ref := targets[i]
		ref.Version = newVer
		cfg.UpsertExtension(&ref)
		// Persist after each target so a later target's failure can't leave an
		// already-applied update unrecorded in .cg.json.
		if err := cfg.Save(root); err != nil {
			return fmt.Errorf("updating %s: %w", workspace.ConfigFile, err)
		}
		if old == newVer {
			_, _ = fmt.Fprintf(out, "%s: already at v%s\n", ref.Name, newVer)
		} else {
			_, _ = fmt.Fprintf(out, "%s: v%s → v%s\n", ref.Name, old, newVer)
		}
	}
	return nil
}

// updateOne fetches the clone of name, advances it to the right commit,
// re-validates the manifest at <clone>/<path>, re-applies its contributions to
// the workspace (reverse the prior ledger, then apply the new manifest), and
// returns the new version (always the manifest's own version field).
//
// Commit selection depends on layout. A root extension (path == "") is its own
// repo, so its git tags are its releases: check out the latest tag, or track the
// default branch when untagged. A subdir extension (path != "") shares one repo
// with siblings, so whole-repo tags don't map to a single extension's version —
// track the default branch and let the manifest's version field be the source of
// truth.
func updateOne(ctx context.Context, root, name, path string) (string, error) {
	clone := extension.CloneDir(name)
	if _, err := os.Stat(clone); err != nil {
		return "", cgerr.New(cgerr.ExitUsage,
			"clone for %q missing at %s; reinstall it", name, clone)
	}
	if err := gitx.Fetch(ctx, clone, "origin", "--tags"); err != nil {
		return "", cgerr.New(cgerr.ExitUsage, "fetching %q: %v", name, err)
	}
	if tag := gitx.LatestTag(ctx, clone); path == "" && tag != "" {
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
	manifestDir := filepath.Join(clone, path)
	m, err := extension.LoadManifest(manifestDir)
	if err != nil {
		return "", cgerr.New(cgerr.ExitUsage, "reading %s for %q: %v", extension.ManifestFile, name, err)
	}
	if err := m.Validate(); err != nil {
		return "", cgerr.New(cgerr.ExitUsage, "invalid %s for %q: %v", extension.ManifestFile, name, err)
	}

	// Re-apply: new manifest first, then reverse any contributions it dropped.
	if err := reapply(root, manifestDir, m); err != nil {
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
