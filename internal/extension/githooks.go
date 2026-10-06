package extension

import (
	"context"
	"path/filepath"

	"github.com/mnemcik/consigliere/internal/githooks"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// TrustedGitHookScripts returns the scripts githooks.Ensure may approve: for
// every git-hooks contribution of every extension installed in the workspace
// (refs, from .cg.json), its workspace-relative install path mapped to the blob
// id of that script in the extension's machine-local clone. An extension whose
// clone is missing or unreadable contributes nothing, so its scripts stay
// unapproved until `cg init` or `cg extension install` restores the clone.
// dir is any directory in the workspace's repository, used for the object
// format.
func TrustedGitHookScripts(ctx context.Context, dir string, refs []workspace.ExtensionRef) map[string]string {
	trusted := map[string]string{}
	for _, ref := range refs {
		path, err := CleanSubdir(ref.Path)
		if err != nil {
			continue
		}
		manifestDir := filepath.Join(CloneDir(ref.Name), path)
		m, err := LoadManifest(manifestDir)
		if err != nil || m.Validate() != nil || m.Name != ref.Name {
			continue
		}
		for _, g := range m.Contributes.GitHooks {
			oid, err := githooks.BlobID(ctx, dir, filepath.Join(manifestDir, filepath.FromSlash(g.Script)))
			if err != nil || oid == "" {
				continue
			}
			trusted[gitHookDestRel(m.Name, g)] = oid
		}
	}
	return trusted
}
