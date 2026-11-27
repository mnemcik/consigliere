package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/githooks"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

var extInstallRef string
var extInstallPath string
var extListJSON bool
var extRemovePurge bool
var extInstallForce bool

func init() {
	rootCmd.AddCommand(extensionCmd)
	extensionCmd.AddCommand(extInstallCmd, extListCmd, extRemoveCmd, extUpdateCmd)
	extInstallCmd.Flags().StringVar(&extInstallRef, "ref", "",
		"tag, branch or commit to install (default: the repo's default branch)")
	extInstallCmd.Flags().StringVar(&extInstallPath, "path", "",
		"subdir within the repo holding cg-extension.json (for monorepos; direct repo-url installs only)")
	extInstallCmd.Flags().BoolVar(&extInstallForce, "force", false,
		"overwrite existing files the extension ships that aren't recorded as its own")
	extListCmd.Flags().BoolVar(&extListJSON, "json", false, "output as JSON")
	extRemoveCmd.Flags().BoolVar(&extRemovePurge, "purge", false,
		"also delete a legacy name-keyed clone under ~/.config/consigliere/extensions/ (store trees are shared and kept)")
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
	// A local repo is recorded, and keyed in the store, by its absolute path,
	// so ../ext from two directories never names one mirror.
	if source == workspace.ExtSourceDirect {
		repo = absLocalRepo(repo)
	}

	m, dest, commit, err := installFrom(cmd.Context(), repo, extInstallRef, path)
	if err != nil {
		return err
	}
	if err := checkRequirements([]*extension.Manifest{m}); err != nil {
		return err
	}
	warnings, err := checkCommands(root, cfg.Extensions, map[string][]extension.CommandContribution{m.Name: m.Contributes.Commands}, false)
	if err != nil {
		return err
	}
	prev, prevCertain := "", false
	if old, ok := findExtension(cfg, m.Name); ok {
		prev, prevCertain = previousSource(old), extension.ValidCommit(old.Commit)
	}
	targets := cfg.SkillTargets()
	skillWarnings, err := checkSkills(&skillCheck{root: root, installed: cfg.Extensions,
		incoming: map[string]*extension.Manifest{m.Name: m}, targets: targets})
	if err != nil {
		return err
	}
	run := skillRun{ctx: cmd.Context(), repo: repo, targets: targets}
	if err := preflightSkills(root, []preflightItem{{m: m, manifestDir: filepath.Join(dest, path), prev: prev, prevCertain: prevCertain, force: extInstallForce, run: run}}); err != nil {
		return err
	}
	printCommandWarnings(cmd.ErrOrStderr(), append(warnings, skillWarnings...))
	res, err := reapply(root, filepath.Join(dest, path), m, prev, prevCertain, extInstallForce, run)
	if err != nil {
		return err
	}
	cfg.UpsertExtension(&workspace.ExtensionRef{
		Name:      m.Name,
		Version:   m.Version,
		Source:    source,
		Registry:  regAlias,
		Repo:      repo,
		Path:      filepath.ToSlash(path), // .cg.json is shared across OSes
		Commit:    commit,
		Installed: time.Now().UTC().Format(time.RFC3339),
	})
	if err := cfg.Save(root); err != nil {
		return fmt.Errorf("updating %s: %w", workspace.ConfigFile, err)
	}

	printInstallSummary(cmd, m, dest, source, regAlias, repo, path)
	printFileOutcomes(cmd.OutOrStdout(), m.Name, &res)
	printSkillReport(cmd.OutOrStdout(), cmd.ErrOrStderr(), &res.Skills)
	printGitHookWarnings(cmd.ErrOrStderr(), ensureGitHooks(cmd.Context(), root))
	return nil
}

// reapplyResult is what reapply left for the user: copied files it kept
// because the user changed them, and what it says about skills.
type reapplyResult struct {
	Pending []extension.PendingFile // both sides changed; awaiting `resolve`
	Kept    []string                // dropped by the new version but edited, so not deleted
	// KeptUnknown says cg had no record of what it wrote for the kept files,
	// so it can't tell whether they were edited.
	KeptUnknown bool
	Skills      extension.SkillReport
}

// applyOptionsFor builds the options reapply applies m with. report may be
// nil.
func applyOptionsFor(root string, m *extension.Manifest, prevSource string, prevCertain, force bool, run skillRun, report *extension.SkillReport) (extension.ApplyOptions, error) {
	old, err := extension.LoadLedger(root, m.Name)
	if err != nil {
		return extension.ApplyOptions{}, fmt.Errorf("reading ledger for %q: %w", m.Name, err)
	}
	opts := extension.ApplyOptions{
		Old: old, Force: force, NotesIndex: notesIndexRel(root),
		SkillTargets: run.targets, SkillMode: run.mode, Context: run.ctx,
		ExtensionName: m.Name, Repo: run.repo, SkillReport: report,
	}
	if old != nil && !old.HasContentIDs() && prevSource != "" {
		opts.LegacyBases = extension.LegacyBases(prevSource)
		opts.LegacyCertain = prevCertain
	}
	return opts, nil
}

// reapply applies m's contributions into the workspace and persists the ledger,
// then reverses any contributions a prior install of the same extension shipped
// that the new manifest drops. It applies the NEW manifest first: Apply
// self-rolls-back on failure, so a failed reinstall/update leaves the prior
// install intact rather than half-removed. Copied files the user changed are
// kept (see extension.ApplyWith). prevSource is the manifest dir of the version
// being replaced, used as the base for a ledger that predates recorded file
// blobs, and prevCertain says it is exactly the commit that was installed;
// force overwrites unrecorded files in the way.
func reapply(root, manifestDir string, m *extension.Manifest, prevSource string, prevCertain, force bool, run skillRun) (reapplyResult, error) {
	var res reapplyResult
	opts, err := applyOptionsFor(root, m, prevSource, prevCertain, force, run, &res.Skills)
	if err != nil {
		return res, err
	}
	old := opts.Old
	// A broken block in the old index would fail its reversal after the new
	// files are written; refuse while nothing has changed.
	if err := extension.CheckIndexRows(root, old); err != nil {
		return res, cgerr.New(cgerr.ExitUsage, "notes index for %q: %v", m.Name, err)
	}
	ledger, err := extension.ApplyWith(root, manifestDir, m, opts)
	if err != nil {
		return res, cgerr.New(cgerr.ExitUsage, "applying contributions for %q: %v", m.Name, err)
	}
	if old != nil && !old.HasContentIDs() {
		// The base for reversing dropped files too. Set only after applying:
		// ApplyWith must see that the old ledger had no recorded files.
		old.Files = opts.LegacyBases
		res.KeptUnknown = old.Files == nil
	}
	if orphan := extension.OrphanLedger(old, ledger); orphan != nil {
		kept, rerr := extension.Reverse(root, orphan)
		if rerr != nil {
			return res, fmt.Errorf("removing dropped contributions for %q: %w", m.Name, rerr)
		}
		res.Kept = kept
	}
	if err := ledger.Save(root); err != nil {
		return res, fmt.Errorf("writing ledger for %q: %w", m.Name, err)
	}
	res.Pending = ledger.Pending
	return res, nil
}

// notesIndexRel is the workspace's notes index, as .cg.json names it, or the
// default one. A path that leaves the workspace gives "", so no rows are
// written.
func notesIndexRel(root string) string {
	rel := extension.DefaultNotesIndex
	if cfg, err := workspace.Detect(root); err == nil && cfg != nil {
		if p, ok := cfg.Indexes[dirNotes]; ok {
			rel = p
		}
	}
	clean, err := extension.CleanSubdir(rel)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(clean) // the ledger records slash paths
}

// previousSource is the manifest dir of the version a workspace entry records,
// or "" when its path is invalid.
func previousSource(ref *workspace.ExtensionRef) string {
	path, err := extension.CleanSubdir(ref.Path)
	if err != nil {
		return ""
	}
	return filepath.Join(extension.SourceRoot(ref), path)
}

// printFileOutcomes tells the user which copied files reapply left alone.
func printFileOutcomes(w io.Writer, name string, res *reapplyResult) {
	for _, p := range res.Pending {
		why := "the extension changed it too"
		if p.Base == "" || p.UncertainBase {
			why = "it differs from the extension's version, and cg can't tell exactly what it installed"
		}
		_, _ = fmt.Fprintf(w, "  kept your %s: %s; see `cg extension diff %s %s`, then `cg extension resolve %s %s --merge|--keep|--take`\n",
			p.Dest, why, name, p.Dest, name, p.Dest)
	}
	why := "you changed it"
	if res.KeptUnknown {
		why = "cg can't tell whether you changed it"
	}
	for _, k := range res.Kept {
		_, _ = fmt.Fprintf(w, "  kept %s: the extension no longer ships it, but %s; delete it yourself if it's not needed\n", k, why)
	}
}

// ensureGitHooks reconciles the git-hook dispatchers of the repository
// containing dir with the scripts extensions contributed, approving only those
// that match an installed extension's machine-local clone (see package
// githooks), and returns what the user should be told. Callers run it after
// .cg.json records the extensions, since that is what approval reads.
func ensureGitHooks(ctx context.Context, dir string) []string {
	root, err := githooks.MainWorktree(ctx, dir)
	if err != nil {
		return nil
	}
	var refs []workspace.ExtensionRef
	if cfg, derr := workspace.Detect(root); derr == nil && cfg != nil {
		refs = cfg.Extensions
	}
	res, err := githooks.Ensure(ctx, root, extension.TrustedGitHookScripts(ctx, root, refs))
	var msgs []string
	switch {
	case err == nil:
	case errors.Is(err, githooks.ErrHooksPath):
		msgs = append(msgs, fmt.Sprintf("core.hooksPath is set, so cg did not install a dispatcher for the git-hook scripts in %s; unset core.hooksPath, or run those scripts from your own hooks", githooks.DirRel))
	default:
		msgs = append(msgs, fmt.Sprintf("git hooks: %v", err))
	}
	if len(res.Unapproved) > 0 {
		if inLinkedWorktree(ctx, dir, root) {
			// install and update copy the new scripts into this worktree, but
			// the dispatcher runs the main worktree's copies, which still differ.
			msgs = append(msgs, fmt.Sprintf("git-hook scripts in the main worktree (%s) that will not run, because they do not match an extension installed on this machine: %s; you ran this in a linked worktree, so the scripts it installed or updated are approved once its changes land in the main worktree and a cg command runs there (every session start does)", root, strings.Join(res.Unapproved, ", ")))
		} else {
			msgs = append(msgs, fmt.Sprintf("git-hook scripts that will not run, because they do not match an extension installed on this machine: %s; cg extension install or update for the extension that ships them approves them", strings.Join(res.Unapproved, ", ")))
		}
	}
	return msgs
}

// inLinkedWorktree reports whether dir lies in a worktree other than the main
// one at root.
func inLinkedWorktree(ctx context.Context, dir, root string) bool {
	top, err := gitx.Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}
	return canonicalPath(top) != canonicalPath(root)
}

// canonicalPath resolves symlinks (macOS /var vs /private/var) so two spellings
// of one directory compare equal; it falls back to the cleaned path.
func canonicalPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// commandContext returns cmd's context, or context.Background() when cmd is
// nil or was not run through Execute (tests call some RunE functions directly).
func commandContext(cmd *cobra.Command) context.Context {
	if cmd != nil {
		if ctx := cmd.Context(); ctx != nil {
			return ctx
		}
	}
	return context.Background()
}

// commandOut returns cmd's output writer, or os.Stdout when cmd is nil.
func commandOut(cmd *cobra.Command) io.Writer {
	if cmd != nil {
		return cmd.OutOrStdout()
	}
	return os.Stdout
}

// commandErr returns cmd's error writer, or os.Stderr when cmd is nil.
func commandErr(cmd *cobra.Command) io.Writer {
	if cmd != nil {
		return cmd.ErrOrStderr()
	}
	return os.Stderr
}

// printGitHookWarnings writes ensureGitHooks messages as warnings.
func printGitHookWarnings(w io.Writer, msgs []string) {
	for _, m := range msgs {
		_, _ = fmt.Fprintf(w, "warning: %s\n", m)
	}
}

// reinstallMissingExtensions restores, for every extension recorded in
// .cg.json, the source this machine is missing (the re-cloned-workspace case)
// and re-applies its contributions. An entry with a recorded commit is
// restored at exactly that commit, so every machine applies the same content.
// An entry without one predates the store: when its legacy clone is missing it
// is installed from the default branch and its commit is recorded. Present
// sources are left as they are. Every source is fetched, and the commands of
// the whole set checked, before the first is applied, so a conflict applies
// none. It returns the entries, with any newly recorded commits, and short
// descriptions for the init summary.
func reinstallMissingExtensions(cmd *cobra.Command, root string, exts []workspace.ExtensionRef) (_ []workspace.ExtensionRef, _ []string, retErr error) {
	out := append([]workspace.ExtensionRef(nil), exts...)
	// The trees this run fetches for pinned entries were missing, which is
	// how cg init tells "not restored yet". If it stops with an error, every
	// one of them that some entry didn't get applied from is removed again,
	// so the next cg init restores that entry instead of taking its source
	// for present. (A tree two entries share stays only when both applied.)
	var fetched []string
	fetchedSet := map[string]bool{} // fetched, as a set
	// Sources missing when the run starts. A tree this run creates for an
	// earlier entry (two extensions of one repo at one commit share it,
	// also when the earlier one is legacy) still counts as missing; a tree
	// that was already there is never removed.
	missing := map[string]bool{}
	for i := range out {
		if t := extension.SourceRoot(&out[i]); t != "" {
			if _, err := os.Stat(t); err != nil {
				missing[t] = true
			}
		}
	}
	users := map[string]int{}   // entries to restore from each fetched tree
	applied := map[string]int{} // of them, the ones already applied
	defer func() {
		if retErr == nil {
			return
		}
		for _, t := range fetched {
			if applied[t] == users[t] {
				continue
			}
			if rerr := os.RemoveAll(t); rerr != nil {
				// Left in place, the tree would look restored to the next
				// cg init.
				retErr = fmt.Errorf("%w; also couldn't remove %s (%w): remove it before the next cg init", retErr, t, rerr)
			}
		}
	}()
	type restore struct {
		e           *workspace.ExtensionRef
		m           *extension.Manifest
		manifestDir string
		prev        string // base for a ledger that predates content ids
		commit      string
		tree        string // the store tree the entry is restored from
	}
	var (
		todo    []restore
		present []workspace.ExtensionRef
	)
	// S36b: the skill checks that need no fetched tree run before the first
	// fetch, so after fixing what they report a plain cg init runs again.
	targets, terr := skillTargetsFor(root)
	if terr != nil {
		return out, nil, cgerr.New(cgerr.ExitUsage, "%v", terr)
	}
	for i := range out {
		e := &out[i]
		if !missing[extension.SourceRoot(e)] {
			continue
		}
		led, lerr := extension.LoadLedger(root, e.Name)
		if lerr != nil {
			return out, nil, unreadableLedgerError(root, e.Name, lerr)
		}
		if err := extension.CheckSkillPaths(root, targets, led); err != nil {
			return out, nil, cgerr.New(cgerr.ExitUsage, "extension %q: %v", e.Name, err)
		}
	}
	incoming := map[string][]extension.CommandContribution{}
	for i := range out {
		e := &out[i]
		if !missing[extension.SourceRoot(e)] {
			present = append(present, *e)
			continue // source present; nothing to restore
		}
		if !gitx.Available() {
			return out, nil, cgerr.New(cgerr.ExitUsage, "git is required to re-install extension %q", e.Name)
		}
		path, perr := extension.CleanSubdir(e.Path)
		if perr != nil {
			return out, nil, cgerr.New(cgerr.ExitUsage, "extension %q: %v", e.Name, perr)
		}
		var (
			m      *extension.Manifest
			tree   string
			commit = e.Commit
			err    error
		)
		if extension.ValidCommit(commit) {
			tree, err = extension.FetchTree(cmd.Context(), e.Repo, commit)
			if err != nil {
				return out, nil, cgerr.New(cgerr.ExitUsage, "restoring %q at %s: %v", e.Name, commit[:12], err)
			}
			if t := extension.SourceRoot(e); !fetchedSet[t] {
				fetchedSet[t] = true
				fetched = append(fetched, t)
			}
			m, err = loadValidManifest(filepath.Join(tree, path), e.Name)
		} else {
			m, tree, commit, err = installFrom(cmd.Context(), e.Repo, "", path)
		}
		if err != nil {
			return out, nil, err
		}
		// A pinned commit is exactly what was installed, so its tree is the
		// base for a ledger that predates content ids. A legacy entry's base
		// is gone with its clone, so there is none.
		prev := ""
		if extension.ValidCommit(e.Commit) {
			prev = filepath.Join(tree, path)
		}
		todo = append(todo, restore{e: e, m: m, manifestDir: filepath.Join(tree, path), prev: prev, commit: commit, tree: tree})
		users[tree]++
		incoming[m.Name] = m.Contributes.Commands
	}
	// #212 S9: an extension this cg is too old for restores none, before
	// anything is applied; the deferred clean-up removes the fetched trees.
	ms := make([]*extension.Manifest, len(todo))
	for i := range todo {
		ms[i] = todo[i].m
	}
	if err := checkRequirements(ms); err != nil {
		return out, nil, err
	}
	// S9: every command cg init restores counts as owned, so a built-in
	// namespace only warns.
	warnings, err := checkCommands(root, present, incoming, true)
	if err != nil {
		return out, nil, err
	}
	// S23: skill names over the whole set, then each extension's own skill
	// checks, before the first is applied. Reserved names only warn (S20c).
	manifests := map[string]*extension.Manifest{}
	var items []preflightItem
	for _, r := range todo {
		manifests[r.m.Name] = r.m
		items = append(items, preflightItem{m: r.m, manifestDir: r.manifestDir, prev: r.prev, prevCertain: r.prev != "",
			run: skillRun{ctx: commandContext(cmd), repo: r.e.Repo, mode: extension.SkillsRestore, targets: targets}})
	}
	skillWarnings, err := checkSkills(&skillCheck{root: root, installed: present, incoming: manifests, targets: targets, restore: true})
	if err != nil {
		return out, nil, err
	}
	if err := preflightSkills(root, items); err != nil {
		return out, nil, err
	}
	printCommandWarnings(commandErr(cmd), append(warnings, skillWarnings...))
	var done []string
	for i, r := range todo {
		res, err := reapply(root, r.manifestDir, r.m, r.prev, r.prev != "", false, items[i].run)
		if err != nil {
			return out, done, err
		}
		printSkillReport(commandOut(cmd), commandErr(cmd), &res.Skills)
		applied[r.tree]++
		r.e.Commit = r.commit
		done = append(done, fmt.Sprintf("extension %q v%s (re-installed at %s)", r.m.Name, r.m.Version, r.commit[:12]))
	}
	return out, done, nil
}

// installFrom fetches repo into the machine-shared store, resolves ref (empty
// means the default branch) to a commit, exports that commit's tree, and reads
// and validates the manifest at <tree>/<path> (path empty = repo root). It
// returns the manifest, the tree root and the commit. It does not touch the
// workspace: callers apply the contributions and record the .cg.json entry.
func installFrom(ctx context.Context, repo, ref, path string) (m *extension.Manifest, tree, commit string, err error) {
	mirror, err := extension.EnsureMirror(ctx, repo, true)
	if err != nil {
		return nil, "", "", cgerr.New(cgerr.ExitUsage, "fetching %s: %v", repo, err)
	}
	commit, err = extension.ResolveCommit(ctx, mirror, ref)
	if err != nil {
		return nil, "", "", cgerr.New(cgerr.ExitUsage, "resolving %s: %v", repo, err)
	}
	tree, err = extension.EnsureTree(ctx, repo, mirror, commit)
	if err != nil {
		return nil, "", "", fmt.Errorf("exporting %s at %s: %w", repo, commit[:12], err)
	}
	m, err = loadValidManifest(filepath.Join(tree, path), "")
	if err != nil {
		return nil, "", "", err
	}
	return m, tree, commit, nil
}

// loadValidManifest reads and validates the manifest in dir. A non-empty want
// must match the manifest's name, so a recorded entry can't pick up another
// extension's manifest.
func loadValidManifest(dir, want string) (*extension.Manifest, error) {
	m, err := extension.LoadManifest(dir)
	if err != nil {
		return nil, cgerr.New(cgerr.ExitUsage, "reading %s: %v", extension.ManifestFile, err)
	}
	if err := m.Validate(); err != nil {
		return nil, cgerr.New(cgerr.ExitUsage, "invalid %s: %v", extension.ManifestFile, err)
	}
	if want != "" && m.Name != want {
		return nil, cgerr.New(cgerr.ExitUsage, "%s in %s names %q, but .cg.json records %q", extension.ManifestFile, dir, m.Name, want)
	}
	return m, nil
}

// checkRequirements refuses the run when this cg is older than any of ms
// requires (requires.cg), naming each such extension on its own line, so one
// update of cg fixes the whole run. Callers check before applying anything.
// --force doesn't skip it: the extension is known not to work on this cg.
func checkRequirements(ms []*extension.Manifest) error {
	var lines []string
	for _, m := range ms {
		if need, have, unmet := extension.UnmetRequirement(m, Version); unmet {
			lines = append(lines, extension.RequirementMessage(m.Name, need, have))
		}
	}
	if len(lines) == 0 {
		return nil
	}
	return cgerr.New(cgerr.ExitUsage, "%s", strings.Join(lines, "\n"))
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
// builtin alias is immutable: it resolves to the env override if set (a test /
// fork hook), else the public default — and is deliberately NOT overridable via
// .cg.json. That keeps cg/<name> a trustworthy reference to the public
// catalogue: a committed or tampered workspace config can't silently repoint it
// to another registry (which would reintroduce the name-shadowing risk that
// fully-qualified names exist to prevent). Any other alias is resolved from
// .cg.json registries.
func registrySource(cfg *workspace.Config, alias string) (string, bool) {
	if alias == extension.BuiltinRegistryAlias {
		if env := strings.TrimSpace(os.Getenv("CONSIGLIERE_EXTENSIONS_REGISTRY")); env != "" {
			return env, true
		}
		return extension.DefaultRegistryURL, true
	}
	if cfg != nil {
		if src, ok := cfg.Registries[alias]; ok && strings.TrimSpace(src) != "" {
			return src, true
		}
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
	listed := make([]listedExtension, len(exts))
	for i := range exts {
		listed[i] = listedExtension{ExtensionRef: exts[i], Commands: []listedCommand{}, Skills: []listedSkill{}}
		// #212 S13c: read for every extension, with or without a ledger.
		dir, m, verr := verifiedManifest(&exts[i])
		if verr == nil {
			if need, have, unmet := extension.UnmetRequirement(m, Version); unmet {
				listed[i].RequirementUnmet = extension.RequirementMessage(m.Name, need, have)
				listed[i].requirementReason = extension.RequirementReason(need, have)
			}
		}
		led, lerr := extension.LoadLedger(root, exts[i].Name)
		if lerr != nil {
			listed[i].LedgerError = lerr.Error()
			continue
		}
		if led == nil {
			continue
		}
		listed[i].Skills = listSkills(led)
		for _, c := range led.Commands {
			lc := listedCommand{Command: c.Command, Binary: c.Binary, ShadowedByBuiltin: builtinFor(c.Command)}
			switch {
			case verr != nil:
				lc.Unverified = verr.Error()
			case !verifiedCommand(dir, m, c):
				lc.Unverified = "its manifest doesn't declare it with that binary, or the binary is missing"
			}
			listed[i].Commands = append(listed[i].Commands, lc)
		}
	}

	if extListJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(listed)
	}

	if len(listed) == 0 {
		_, _ = fmt.Fprintln(out, "no extensions installed")
		return nil
	}
	_, _ = fmt.Fprintf(out, "%-20s %-10s %-12s %s\n", "NAME", "VERSION", "SOURCE", "REPO")
	for i := range listed {
		l := &listed[i]
		// For registry installs show the alias the extension came from (its
		// fully-qualified prefix); direct installs show "direct".
		src := l.Source
		if l.Registry != "" {
			src = "registry:" + l.Registry
		}
		_, _ = fmt.Fprintf(out, "%-20s %-10s %-12s %s\n", l.Name, l.Version, src, l.Repo)
		if len(l.Commands) > 0 {
			cs := make([]string, len(l.Commands))
			for i, c := range l.Commands {
				cs[i] = c.Command
				if c.ShadowedByBuiltin != "" {
					cs[i] += fmt.Sprintf(" (can't run: the built-in \"cg %s\" wins)", c.ShadowedByBuiltin)
				}
				if c.Unverified != "" {
					cs[i] += fmt.Sprintf(" (not verified: %s; it won't run)", c.Unverified)
				}
			}
			_, _ = fmt.Fprintf(out, "  commands: %s\n", strings.Join(cs, ", "))
		}
		if len(l.Skills) > 0 {
			_, _ = fmt.Fprintf(out, "  skills: %s\n", formatSkills(l.Skills))
		}
		if l.LedgerError != "" {
			_, _ = fmt.Fprintf(out, "  commands: (can't read %s: %s)\n", extension.LedgerPath(root, l.Name), l.LedgerError)
		}
		if l.requirementReason != "" {
			_, _ = fmt.Fprintf(out, "  %s\n", l.requirementReason)
		}
	}
	return nil
}

// listedExtension is one row of `cg extension list`: the .cg.json entry and
// the commands its ledger records.
type listedExtension struct {
	workspace.ExtensionRef
	Commands []listedCommand `json:"commands"`
	Skills   []listedSkill   `json:"skills,omitempty"`
	// LedgerError says the extension's ledger can't be read, so its
	// commands are unknown (and dispatch stops on it).
	LedgerError string `json:"ledgerError,omitempty"`
	// RequirementUnmet says this cg is older than the extension's pinned
	// manifest requires (requires.cg), so its commands and launcher don't
	// run: the requirement message. Its commands still count as verified.
	RequirementUnmet string `json:"requirementUnmet,omitempty"`
	// requirementReason is the text line's form of RequirementUnmet.
	requirementReason string
}

// listedCommand is a command an installed extension declares. ShadowedByBuiltin
// names the built-in cg command its namespace resolves to, so the extension's
// command can't run: the built-in wins.
type listedCommand struct {
	Command           string `json:"command"`
	Binary            string `json:"binary"`
	ShadowedByBuiltin string `json:"shadowedByBuiltin,omitempty"`
	// Unverified says why dispatch refuses the entry (S17), "" when it runs.
	Unverified string `json:"unverified,omitempty"`
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

// absLocalRepo returns repo as an absolute, symlink-resolved path when it is
// an existing local directory, and unchanged otherwise (a URL).
func absLocalRepo(repo string) string {
	if strings.Contains(repo, "://") || strings.HasPrefix(repo, "git@") {
		return repo
	}
	if info, err := os.Stat(repo); err != nil || !info.IsDir() { //nolint:gosec // the user named this local repo to install from
		return repo
	}
	abs, err := filepath.Abs(repo)
	if err != nil {
		return repo
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
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
	add(len(c.Gitignore), ".gitignore block(s)")
	add(len(c.Notes), "note(s)")
	add(len(c.Hooks), "hook(s)")
	add(len(c.GitHooks), "git hook script(s)")
	add(len(c.Commands), "command(s)")
	add(len(c.Launcher), "launcher")
	add(len(c.Templates), "template(s)")
	add(len(c.Skills), "skill(s)")
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
		if !ledger.HasContentIDs() {
			if ref, ok := findExtension(cfg, name); ok {
				ledger.Files = extension.LegacyBases(previousSource(ref))
			}
		}
		// Skill files first: an uncommitted edit stops the remove before
		// anything is deleted (S35).
		var skills extension.SkillReport
		if err := extension.RemoveSkills(root, ledger, &extension.ApplyOptions{Context: cmd.Context(), ExtensionName: name}, &skills); err != nil {
			return cgerr.New(cgerr.ExitUsage, "removing skills of %q: %v", name, err)
		}
		printSkillReport(cmd.OutOrStdout(), cmd.ErrOrStderr(), &skills)
		kept, rerr := extension.Reverse(root, ledger)
		if rerr != nil {
			return fmt.Errorf("reversing contributions for %q: %w", name, rerr)
		}
		for _, k := range kept {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  kept %s: you changed it since install; delete it yourself if it's not needed\n", k)
		}
		if rmErr := os.Remove(extension.LedgerPath(root, name)); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("removing ledger for %q: %w", name, rmErr)
		}
	}

	cfg.RemoveExtension(name)
	if err := cfg.Save(root); err != nil {
		return fmt.Errorf("updating %s: %w", workspace.ConfigFile, err)
	}
	printGitHookWarnings(cmd.ErrOrStderr(), ensureGitHooks(cmd.Context(), root))

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Removed %s from this workspace\n", name)
	// The store's trees are shared by every workspace on the machine and keyed
	// by commit, so they stay; --purge deletes only a legacy name-keyed clone.
	clone := extension.CloneDir(name)
	if _, err := os.Stat(clone); err != nil {
		return nil
	}
	if extRemovePurge {
		if err := os.RemoveAll(clone); err != nil {
			return fmt.Errorf("purging clone %s: %w", clone, err)
		}
		_, _ = fmt.Fprintf(out, "  purged legacy clone: %s\n", clone)
	} else {
		_, _ = fmt.Fprintf(out, "  legacy clone left in place: %s (use --purge to delete)\n", clone)
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

	// Resolve every target and check the commands of the whole new set before
	// applying any, so a conflict leaves every installed version in place.
	updates := make([]pendingUpdate, len(targets))
	incoming := map[string][]extension.CommandContribution{}
	for i := range targets {
		u, err := resolveUpdate(cmd.Context(), &targets[i])
		if err != nil {
			return err
		}
		updates[i] = u
		incoming[u.ref.Name] = u.m.Contributes.Commands
	}
	ms := make([]*extension.Manifest, len(updates))
	for i := range updates {
		ms[i] = updates[i].m
	}
	if err := checkRequirements(ms); err != nil {
		return err
	}
	warnings, err := checkCommands(root, cfg.Extensions, incoming, false)
	if err != nil {
		return err
	}
	manifests := map[string]*extension.Manifest{}
	skillTargets := cfg.SkillTargets()
	var items []preflightItem
	for i := range updates {
		u := &updates[i]
		manifests[u.ref.Name] = u.m
		u.run = skillRun{ctx: cmd.Context(), repo: u.ref.Repo, targets: skillTargets}
		items = append(items, preflightItem{m: u.m, manifestDir: u.manifestDir, prev: previousSource(&u.ref),
			prevCertain: extension.ValidCommit(u.ref.Commit), run: u.run})
	}
	skillWarnings, err := checkSkills(&skillCheck{root: root, installed: cfg.Extensions, incoming: manifests,
		targets: skillTargets, update: true})
	if err != nil {
		return err
	}
	if err := preflightSkills(root, items); err != nil {
		return err
	}
	printCommandWarnings(cmd.ErrOrStderr(), append(warnings, skillWarnings...))

	out := cmd.OutOrStdout()
	for i := range updates {
		u := &updates[i]
		old := u.ref.Version
		if err := applyUpdate(cmd, root, u); err != nil {
			return err
		}
		ref := u.ref
		ref.Version = u.m.Version
		ref.Commit = u.commit
		cfg.UpsertExtension(&ref)
		// Persist after each target so a later target's failure can't leave an
		// already-applied update unrecorded in .cg.json.
		if err := cfg.Save(root); err != nil {
			return fmt.Errorf("updating %s: %w", workspace.ConfigFile, err)
		}
		if old == ref.Version {
			_, _ = fmt.Fprintf(out, "%s: already at v%s\n", ref.Name, ref.Version)
		} else {
			_, _ = fmt.Fprintf(out, "%s: v%s → v%s\n", ref.Name, old, ref.Version)
		}
	}
	printGitHookWarnings(cmd.ErrOrStderr(), ensureGitHooks(cmd.Context(), root))
	return nil
}

// pendingUpdate is an extension's next version, fetched and validated but not
// yet applied.
type pendingUpdate struct {
	ref         workspace.ExtensionRef // the entry as recorded before the update
	m           *extension.Manifest
	manifestDir string
	commit      string
	run         skillRun // set when the run's skill checks are made
}

// resolveUpdate fetches the extension's repo into the store, picks the commit
// to move to, and re-validates the manifest at <tree>/<path>. It doesn't touch
// the workspace; applyUpdate does.
//
// Commit selection depends on layout. A root extension (path == "") is its own
// repo, so its git tags are its releases: take the latest tag, or the default
// branch when untagged. A subdir extension (path != "") shares one repo with
// siblings, so whole-repo tags don't map to a single extension's version:
// take the default branch and let the manifest's version field be the source
// of truth.
func resolveUpdate(ctx context.Context, ref *workspace.ExtensionRef) (pendingUpdate, error) {
	path, err := extension.CleanSubdir(ref.Path)
	if err != nil {
		return pendingUpdate{}, cgerr.New(cgerr.ExitUsage, "extension %q: %v", ref.Name, err)
	}
	mirror, err := extension.EnsureMirror(ctx, ref.Repo, true)
	if err != nil {
		return pendingUpdate{}, cgerr.New(cgerr.ExitUsage, "fetching %q: %v", ref.Name, err)
	}
	target := ""
	if tag := gitx.LatestTag(ctx, mirror); path == "" && tag != "" {
		target = tag
	}
	commit, err := extension.ResolveCommit(ctx, mirror, target)
	if err != nil {
		return pendingUpdate{}, cgerr.New(cgerr.ExitUsage, "resolving %q: %v", ref.Name, err)
	}
	tree, err := extension.EnsureTree(ctx, ref.Repo, mirror, commit)
	if err != nil {
		return pendingUpdate{}, fmt.Errorf("exporting %q at %s: %w", ref.Name, commit[:12], err)
	}
	manifestDir := filepath.Join(tree, path)
	m, err := loadValidManifest(manifestDir, ref.Name)
	if err != nil {
		return pendingUpdate{}, err
	}
	return pendingUpdate{ref: *ref, m: m, manifestDir: manifestDir, commit: commit}, nil
}

// applyUpdate re-applies the new version's contributions (new manifest first,
// then reverse what it dropped).
func applyUpdate(cmd *cobra.Command, root string, u *pendingUpdate) error {
	res, err := reapply(root, u.manifestDir, u.m, previousSource(&u.ref), extension.ValidCommit(u.ref.Commit), false, u.run)
	if err != nil {
		return err
	}
	printFileOutcomes(cmd.OutOrStdout(), u.ref.Name, &res)
	printSkillReport(cmd.OutOrStdout(), cmd.ErrOrStderr(), &res.Skills)
	return nil
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
