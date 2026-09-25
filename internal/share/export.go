package share

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mnemcik/consigliere/internal/gitx"
)

// Options configures Export.
type Options struct {
	Root    string // workspace root
	Project Project
	// IndexPath is the workspace-relative project index. It is included in the
	// uncommitted-changes check because Status and Areas are read from it.
	IndexPath string
	// Include names extra files within the project folder to export beyond
	// DefaultFiles.
	Include []string
	// Owner overrides the owner's display name; empty falls back to git's
	// user.name in the workspace.
	Owner string
	// Shared is passed through to Render; see Input.Shared.
	Shared map[string]string
	// Scan configures the confidentiality scan run on the rendered output.
	Scan ScanOptions
}

// Result is a rendered export. Findings holds the unacknowledged scan
// findings; a non-empty list means the export must not be written.
type Result struct {
	Files    map[string]string // published path → content
	Stamp    Stamp
	Findings []Finding
}

// Export selects, reads and renders one project. It refuses to run while the
// project folder or the index has uncommitted changes, so the stamped source
// commit always describes exactly what was exported.
func Export(ctx context.Context, opts *Options) (*Result, error) {
	slug := opts.Project.Slug
	if slug == "" || strings.ContainsAny(slug, `/\`) || slug == "." || slug == ".." {
		return nil, fmt.Errorf("invalid project slug %q", slug)
	}
	rel := projectsDir + "/" + slug
	dir := filepath.Join(opts.Root, filepath.FromSlash(rel))
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("project folder %s not found", rel)
	}

	names, err := selectFiles(dir, opts.Include)
	if err != nil {
		return nil, err
	}
	files := make(map[string]string, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		files[name] = string(data)
	}

	stamp, err := resolveStamp(ctx, opts, rel)
	if err != nil {
		return nil, err
	}
	rendered, err := Render(&Input{Project: opts.Project, Files: files, Shared: opts.Shared, Stamp: stamp})
	if err != nil {
		return nil, err
	}
	return &Result{Files: rendered, Stamp: stamp, Findings: Scan(rendered, opts.Scan)}, nil
}

// selectFiles returns the allowlisted file names present in dir: the defaults
// that exist, plus every explicitly included file, which must exist. Every
// selected file must be a regular file whose real path stays inside the real
// project folder: a lexical check alone would let a symlink (or a symlinked
// parent directory) pull content from elsewhere into the export.
func selectFiles(dir string, include []string) ([]string, error) {
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving project folder: %w", err)
	}
	seen := map[string]bool{}
	var names []string
	for _, name := range DefaultFiles {
		if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
			continue
		}
		if err := checkContained(realDir, dir, name); err != nil {
			return nil, err
		}
		seen[name] = true
		names = append(names, name)
	}
	if !seen[readmeFile] {
		return nil, fmt.Errorf("project has no %s", readmeFile)
	}
	for _, raw := range include {
		name := path.Clean(filepath.ToSlash(raw))
		if path.IsAbs(name) || name == "." || name == ".." || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("include %q is not a file inside the project folder", raw)
		}
		if isResume(name) {
			return nil, fmt.Errorf("include %q: %s is never exported", raw, ResumeFile)
		}
		if seen[name] {
			continue
		}
		if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			return nil, fmt.Errorf("include %q: no such file in the project folder", raw)
		}
		if err := checkContained(realDir, dir, name); err != nil {
			return nil, fmt.Errorf("include %q: %w", raw, err)
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// checkContained confirms dir/name is a regular file (not a symlink) whose
// real path stays inside realDir.
func checkContained(realDir, dir, name string) error {
	full := filepath.Join(dir, filepath.FromSlash(name))
	info, err := os.Lstat(full)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", name)
	}
	// On a case-insensitive filesystem Lstat("Readme.md") succeeds for
	// README.md, so a name could select a file other than the one it spells.
	// Require an entry with exactly this name in its directory.
	if err := checkExactName(dir, name); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realDir, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s resolves outside the project folder", name)
	}
	return nil
}

func resolveStamp(ctx context.Context, opts *Options, rel string) (Stamp, error) {
	paths := []string{rel}
	if opts.IndexPath != "" {
		paths = append(paths, opts.IndexPath)
	}
	// --untracked-files=all overrides a status.showUntrackedFiles=no config,
	// which would otherwise hide an untracked file that --include selects.
	status, err := gitx.Run(ctx, opts.Root, append([]string{"status", "--porcelain", "--untracked-files=all", "--"}, paths...)...)
	if err != nil {
		return Stamp{}, fmt.Errorf("checking for uncommitted changes: %w", err)
	}
	if status != "" {
		return Stamp{}, fmt.Errorf("uncommitted changes under %s; commit them first so the export matches its stamped commit:\n%s",
			strings.Join(paths, " or "), status)
	}

	log, err := gitx.Run(ctx, opts.Root, "log", "-1", "--format=%H %cs", "--", rel)
	if err != nil {
		return Stamp{}, fmt.Errorf("reading the project's last commit: %w", err)
	}
	sha, date, ok := strings.Cut(log, " ")
	if !ok || sha == "" {
		return Stamp{}, fmt.Errorf("%s has no commits", rel)
	}

	owner := strings.TrimSpace(opts.Owner)
	if owner == "" {
		// A missing user.name makes git exit non-zero; that is the same
		// outcome as an empty one, so the error itself adds nothing.
		owner, _ = gitx.Run(ctx, opts.Root, "config", "user.name")
	}
	if owner == "" {
		return Stamp{}, fmt.Errorf("no owner name: pass --owner or set git user.name")
	}
	return Stamp{Owner: owner, SHA: sha, Date: date}, nil
}

// Write writes the export under out. It refuses while any scan finding is
// open, so no caller can write an export the scan has not cleared.
func (r *Result) Write(out string) error {
	if len(r.Findings) > 0 {
		return fmt.Errorf("%d open scan finding(s); refusing to write the export", len(r.Findings))
	}
	return writeFiles(out, r.Files)
}

// writeFiles writes rendered files under out, which must not exist yet or be an
// empty directory, so an export never overwrites or mixes with existing files.
func writeFiles(out string, files map[string]string) error {
	entries, err := os.ReadDir(out)
	switch {
	case err == nil && len(entries) > 0:
		return fmt.Errorf("output directory %s is not empty", out)
	case err != nil && !os.IsNotExist(err):
		return err
	}
	for pub, body := range files {
		dest := filepath.Join(out, filepath.FromSlash(pub))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, []byte(body), 0o644); err != nil { //nolint:gosec // exported markdown, meant to be read
			return err
		}
	}
	return nil
}

// checkExactName confirms that every element of name, below dir, is spelled
// exactly as it appears in its directory listing.
func checkExactName(dir, name string) error {
	cur := dir
	for _, elem := range strings.Split(name, "/") {
		entries, err := os.ReadDir(cur)
		if err != nil {
			return err
		}
		found := false
		for _, e := range entries {
			if e.Name() == elem {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s: no file with exactly this name (letter case differs on disk)", name)
		}
		cur = filepath.Join(cur, elem)
	}
	return nil
}
