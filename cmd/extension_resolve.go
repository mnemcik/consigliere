package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// Copied files that both the user and the extension changed are left as the
// user's and recorded as pending (see internal/extension/ownership.go). These
// commands list them, show both changes, and settle each one.

var extPendingJSON bool

var (
	extResolveKeep  bool
	extResolveTake  bool
	extResolveMerge bool
)

var extPendingCmd = &cobra.Command{
	Use:   "pending",
	Short: "List extension files with an upstream change you haven't taken",
	Args:  cobra.NoArgs,
	RunE:  runExtPending,
}

var extDiffCmd = &cobra.Command{
	Use:   "diff <name> <file>",
	Short: "Show the extension's change and your change to a pending file",
	Args:  cobra.ExactArgs(2),
	RunE:  runExtDiff,
}

var extResolveCmd = &cobra.Command{
	Use:   "resolve <name> <file> --merge|--keep|--take",
	Short: "Settle a pending extension file",
	Long: `Settle a file that both you and the extension changed since cg last wrote it.

  --merge  three-way merge the extension's change into your file; refused unless the merge is clean
  --keep   keep your file as it is
  --take   replace your file with the extension's version

Each makes the extension's current version the new baseline, so the file is not
raised again until the extension changes it again.`,
	Args: cobra.ExactArgs(2),
	RunE: runExtResolve,
}

func init() {
	extensionCmd.AddCommand(extPendingCmd, extDiffCmd, extResolveCmd)
	extPendingCmd.Flags().BoolVar(&extPendingJSON, "json", false, "output as JSON")
	extResolveCmd.Flags().BoolVar(&extResolveMerge, "merge", false, "merge the extension's change into your file (clean merges only)")
	extResolveCmd.Flags().BoolVar(&extResolveKeep, "keep", false, "keep your file")
	extResolveCmd.Flags().BoolVar(&extResolveTake, "take", false, "take the extension's version")
	extResolveCmd.MarkFlagsMutuallyExclusive("merge", "keep", "take")
	extResolveCmd.MarkFlagsOneRequired("merge", "keep", "take")
}

// pendingEntry is one row of `cg extension pending --json`.
type pendingEntry struct {
	Extension string `json:"extension"`
	File      string `json:"file"`
}

func runExtPending(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	root, cfg, err := requireWorkspace(cmd)
	if err != nil {
		return err
	}
	rows := pendingRows(extension.PendingAll(root, cfg.Extensions))
	out := cmd.OutOrStdout()
	if extPendingJSON {
		if rows == nil {
			rows = []pendingEntry{}
		}
		data, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(out, string(data))
		return nil
	}
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(out, "no pending extension files")
		return nil
	}
	for _, r := range rows {
		_, _ = fmt.Fprintf(out, "%s\t%s\n", r.Extension, r.File)
	}
	return nil
}

func pendingRows(all map[string][]extension.PendingFile) []pendingEntry {
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)
	var rows []pendingEntry
	for _, n := range names {
		for _, p := range all[n] {
			rows = append(rows, pendingEntry{Extension: n, File: p.Dest})
		}
	}
	return rows
}

// pendingTarget loads what diff and resolve need for one pending file.
type pendingTarget struct {
	root    string
	ref     *workspace.ExtensionRef
	ledger  *extension.Ledger
	index   int // into ledger.Pending
	file    extension.PendingFile
	path    string // absolute path of the workspace file
	base    []byte
	theirs  []byte
	current []byte
}

func loadPending(cmd *cobra.Command, name, file string) (*pendingTarget, error) {
	root, cfg, err := requireWorkspace(cmd)
	if err != nil {
		return nil, err
	}
	ref, ok := findExtension(cfg, name)
	if !ok {
		return nil, cgerr.New(cgerr.ExitUsage, "extension %q is not installed in this workspace", name)
	}
	ledger, err := extension.LoadLedger(root, name)
	if err != nil {
		return nil, fmt.Errorf("reading ledger for %q: %w", name, err)
	}
	dest := filepath.ToSlash(filepath.Clean(file))
	t := &pendingTarget{root: root, ref: ref, ledger: ledger, index: -1}
	if ledger != nil {
		for i, p := range ledger.Pending {
			if p.Dest == dest {
				t.index, t.file = i, p
			}
		}
	}
	if t.index < 0 {
		return nil, cgerr.New(cgerr.ExitUsage, "%s is not pending for %q; `cg extension pending` lists what is", dest, name)
	}
	t.path = filepath.Join(root, filepath.FromSlash(dest))
	if t.base, err = extension.BlobContent(cmd.Context(), ref, t.file.Base); err != nil {
		return nil, err
	}
	if t.theirs, err = extension.BlobContent(cmd.Context(), ref, t.file.Upstream); err != nil {
		return nil, err
	}
	t.current, err = os.ReadFile(t.path) //nolint:gosec // a recorded workspace file
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return t, nil
}

func runExtDiff(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	t, err := loadPending(cmd, args[0], args[1])
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "## The extension's change to %s (what cg installed → its new version)\n\n", t.file.Dest)
	if err := writeDiff(cmd.Context(), out, t.base, t.theirs, "installed", "extension"); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "\n## Your change to %s (what cg installed → your file)\n\n", t.file.Dest)
	if err := writeDiff(cmd.Context(), out, t.base, t.current, "installed", "yours"); err != nil {
		return err
	}
	if _, clean, err := mergeFile(cmd.Context(), t.current, t.base, t.theirs); err == nil {
		if clean {
			_, _ = fmt.Fprintln(out, "\nA three-way merge is clean: `--merge` would apply.")
		} else {
			_, _ = fmt.Fprintln(out, "\nThe changes overlap: a three-way merge is not clean, so `--merge` would be refused.")
		}
	}
	return nil
}

func runExtResolve(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	t, err := loadPending(cmd, args[0], args[1])
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	switch {
	case extResolveTake:
		if err := writeWorkspaceFile(t.path, t.theirs); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "%s: took the extension's version\n", t.file.Dest)
	case extResolveMerge:
		merged, clean, err := mergeFile(cmd.Context(), t.current, t.base, t.theirs)
		if err != nil {
			return err
		}
		if !clean {
			return cgerr.New(cgerr.ExitUsage,
				"%s: your change and the extension's overlap, so the merge isn't clean; edit the file by hand (see `cg extension diff %s %s`), then run --keep",
				t.file.Dest, args[0], t.file.Dest)
		}
		if err := writeWorkspaceFile(t.path, merged); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "%s: merged the extension's change into your file\n", t.file.Dest)
	default: // --keep
		_, _ = fmt.Fprintf(out, "%s: kept your file\n", t.file.Dest)
	}
	// The extension's current version is the new baseline: the file is raised
	// again only when the extension changes it again.
	if t.ledger.Files == nil {
		t.ledger.Files = map[string]string{}
	}
	t.ledger.Files[t.file.Dest] = t.file.Upstream
	t.ledger.Pending = append(t.ledger.Pending[:t.index], t.ledger.Pending[t.index+1:]...)
	if err := t.ledger.Save(t.root); err != nil {
		return fmt.Errorf("writing ledger for %q: %w", args[0], err)
	}
	return nil
}

// writeWorkspaceFile writes data to path atomically, keeping its mode.
func writeWorkspaceFile(path string, data []byte) error {
	return extension.WriteFileAtomic(path, data, 0o644)
}

// mergeFile three-way merges theirs into ours over base with git merge-file and
// reports whether the result is clean.
func mergeFile(ctx context.Context, ours, base, theirs []byte) (merged []byte, clean bool, err error) {
	dir, err := os.MkdirTemp("", "cg-merge-")
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	paths := []string{filepath.Join(dir, "yours"), filepath.Join(dir, "installed"), filepath.Join(dir, "extension")}
	for i, data := range [][]byte{ours, base, theirs} {
		if err := os.WriteFile(paths[i], data, 0o600); err != nil { //nolint:gosec // fixed names in our own temp dir
			return nil, false, err
		}
	}
	// #nosec G204 -- fixed "git" binary; the arguments are temp file paths.
	c := exec.CommandContext(ctx, "git", "merge-file", "-p", "-L", "yours", "-L", "installed", "-L", "extension", paths[0], paths[1], paths[2])
	merged, err = c.Output()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return merged, true, nil
	case errors.As(err, &exit) && exit.ExitCode() > 0 && exit.ExitCode() < 128:
		return merged, false, nil // the exit code counts the conflicts
	default:
		return nil, false, fmt.Errorf("git merge-file: %w", err)
	}
}

// writeDiff prints a unified diff from a to b with git diff --no-index.
func writeDiff(ctx context.Context, w io.Writer, a, b []byte, aName, bName string) error {
	dir, err := os.MkdirTemp("", "cg-diff-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	pa, pb := filepath.Join(dir, aName), filepath.Join(dir, bName)
	if err := os.WriteFile(pa, a, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(pb, b, 0o600); err != nil {
		return err
	}
	// #nosec G204 -- fixed "git" binary; the arguments are temp file paths.
	c := exec.CommandContext(ctx, "git", "diff", "--no-index", "--no-color", "--src-prefix=", "--dst-prefix=", aName, bName)
	c.Dir = dir
	out, err := c.Output()
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		return fmt.Errorf("git diff: %w", err)
	}
	if len(out) == 0 {
		_, _ = fmt.Fprintln(w, "(no change)")
		return nil
	}
	_, err = w.Write(out)
	return err
}
