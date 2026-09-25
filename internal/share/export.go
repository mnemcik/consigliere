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
}

// Result is a rendered export.
type Result struct {
	Files map[string]string // published path → content
	Stamp Stamp
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
	return &Result{Files: rendered, Stamp: stamp}, nil
}

// selectFiles returns the allowlisted file names present in dir: the defaults
// that exist, plus every explicitly included file, which must exist.
func selectFiles(dir string, include []string) ([]string, error) {
	seen := map[string]bool{}
	var names []string
	for _, name := range DefaultFiles {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			seen[name] = true
			names = append(names, name)
		}
	}
	if !seen[readmeFile] {
		return nil, fmt.Errorf("project has no %s", readmeFile)
	}
	for _, raw := range include {
		name := path.Clean(filepath.ToSlash(raw))
		if path.IsAbs(name) || name == "." || name == ".." || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("include %q is not a file inside the project folder", raw)
		}
		if path.Base(name) == ResumeFile {
			return nil, fmt.Errorf("include %q: %s is never exported", raw, ResumeFile)
		}
		if seen[name] {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("include %q: no such file in the project folder", raw)
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func resolveStamp(ctx context.Context, opts *Options, rel string) (Stamp, error) {
	paths := []string{rel}
	if opts.IndexPath != "" {
		paths = append(paths, opts.IndexPath)
	}
	status, err := gitx.Run(ctx, opts.Root, append([]string{"status", "--porcelain", "--"}, paths...)...)
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

// WriteTo writes rendered files under out, which must not exist yet or be an
// empty directory, so an export never overwrites or mixes with existing files.
func WriteTo(out string, files map[string]string) error {
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
