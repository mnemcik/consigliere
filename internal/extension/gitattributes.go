package extension

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/mnemcik/consigliere/internal/githooks"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/manifest"
)

// Line endings of scripts in a workspace. Git for Windows checks text out with
// CRLF by default (core.autocrlf), and a shell script with CRLF line endings
// doesn't run: the shebang names "sh\r" and bash trips on each "\r". cg keeps a
// block in the workspace's .gitattributes that checks scripts out with LF on
// every OS. It covers only scripts; the user's own files keep whatever line
// endings their git gives them.

const (
	gitattributesStart = "# cg:gitattributes:start"
	gitattributesEnd   = "# cg:gitattributes:end"
)

// gitattributesBody is cg's block: the hook wrappers and status line, extension
// git-hook scripts (whatever their extension), and any other shell script. The
// git-hooks line is text=auto, not text, so a binary hook there is left alone.
const gitattributesBody = `# Managed by cg: shell scripts need LF line endings to run, on every OS.
*.sh text eol=lf
.cg/git-hooks/** text=auto eol=lf`

// scriptPathspecs are the files gitattributesBody covers, as git pathspecs.
var scriptPathspecs = []string{"*.sh", ".cg/git-hooks/"}

// EnsureGitattributes adds or updates cg's block in root's .gitattributes,
// creating the file when there is none. Lines outside the block are left as
// they are. It reports whether the file changes; with apply false, nothing is
// written.
func EnsureGitattributes(root string, apply bool) (bool, error) {
	path := filepath.Join(root, ".gitattributes")
	raw, err := os.ReadFile(path) //nolint:gosec // fixed name under the workspace root
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	content := string(raw)
	// A CRLF checkout is edited as LF and written back as CRLF; any other file
	// is edited as it is, so a stray CRLF line of the user's stays as it was.
	crlf := manifest.AllCRLF(content)
	lf := content
	if crlf {
		lf = strings.ReplaceAll(content, "\r\n", "\n")
	}
	updated := upsertBlock(lf, gitattributesStart, gitattributesEnd, gitattributesBody, indexLine)
	if lf == "" {
		updated = strings.TrimPrefix(updated, "\n")
	}
	if updated == lf {
		return false, nil
	}
	if !apply {
		return true, nil
	}
	if crlf {
		updated = strings.ReplaceAll(updated, "\n", "\r\n")
	}
	return true, writeFileAtomic(path, []byte(updated), 0o644)
}

// NormalizeScriptLineEndings rewrites, with LF line endings, each script that
// cg's .gitattributes block covers and that is on disk with CRLF: a checkout
// made before the block existed keeps its CRLF copies until git checks the
// files out again. Only tracked files are touched, and only their line endings
// change. It returns the workspace-relative paths it rewrites (or, with apply
// false, would rewrite). Outside a git repository it does nothing.
func NormalizeScriptLineEndings(ctx context.Context, root string, apply bool) ([]string, error) {
	if !gitx.Available() {
		return nil, nil
	}
	// RunRaw, not Run: Run trims the output, which would cut a leading or
	// trailing space off the first or last path.
	args := append([]string{"ls-files", "-z", "--"}, scriptPathspecs...)
	out, err := gitx.RunRaw(ctx, root, args...)
	if err != nil {
		return nil, nil //nolint:nilerr // not a git repository: nothing to normalise
	}
	staged := indexBlobs(ctx, root)
	var changed, refresh []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue // deleted in the working tree, or a symlink: not ours to rewrite
		}
		data, err := os.ReadFile(path) //nolint:gosec // tracked file under the workspace root
		if err != nil {
			return changed, err
		}
		if !strings.Contains(string(data), "\r\n") || binary(data) {
			continue
		}
		changed = append(changed, rel)
		if !apply {
			continue
		}
		lf := strings.ReplaceAll(string(data), "\r\n", "\n")
		if err := writeFileAtomicMode(path, []byte(lf), info.Mode().Perm()); err != nil {
			return changed, err
		}
		if matchesIndex(ctx, root, path, lf, staged[rel]) {
			refresh = append(refresh, rel)
		}
	}
	// Git goes on reporting a file it checked out with other attributes as
	// modified, though its content matches the index, until it is added again.
	// Adding a script whose content is the index's changes nothing but that; one
	// committed with CRLF is staged with LF, as git add --renormalize would; one
	// the user has edited is left unstaged.
	if len(refresh) > 0 {
		if _, err := gitx.Run(ctx, root, append([]string{"--literal-pathspecs", "add", "--"}, refresh...)...); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// matchesIndex reports whether lf, the LF content now at path, is the index's
// blob staged, either exactly or once that blob's CRLF line endings are LF.
func matchesIndex(ctx context.Context, root, path, lf, staged string) bool {
	if staged == "" {
		return false
	}
	if id, err := githooks.BlobID(ctx, root, path); err == nil && id == staged {
		return true
	}
	blob, err := gitx.RunRaw(ctx, root, "cat-file", "blob", staged)
	return err == nil && manifest.LF(string(blob)) == lf
}

// binary reports whether data looks binary the way git decides it for
// text=auto: a NUL byte in the first 8000 bytes.
func binary(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0
}

// indexBlobs returns the index's blob id for each file cg's block covers.
func indexBlobs(ctx context.Context, root string) map[string]string {
	out, err := gitx.RunRaw(ctx, root, append([]string{"ls-files", "-s", "-z", "--"}, scriptPathspecs...)...)
	if err != nil {
		return nil
	}
	blobs := map[string]string{}
	for _, entry := range strings.Split(string(out), "\x00") {
		// <mode> SP <object> SP <stage> TAB <path>
		meta, rel, ok := strings.Cut(entry, "\t")
		if f := strings.Fields(meta); ok && len(f) == 3 && f[2] == "0" {
			blobs[rel] = f[1]
		}
	}
	return blobs
}
