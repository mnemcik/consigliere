package extension

import (
	"context"
	"crypto/sha1" //nolint:gosec // git blob ids are SHA-1; not used for security
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// Ownership of copied files (notes and templates). The ledger records, per
// destination, the git blob id of the content cg last wrote (Ledger.Files).
// That is the base of a three-way comparison with what is on disk now and
// what the new version ships:
//
//	disk == new                       nothing to do; the new version is the baseline
//	disk == base                      the file is still cg's: write the new version
//	disk != base, new == base         the user changed it and upstream didn't: keep it, silently
//	disk != base, new != base         both changed: keep the user's file and record it as
//	                                  pending, for `cg extension resolve` to settle
//	no base (first install), disk != new   refuse without --force: the file isn't cg's
//
// Hook wrappers and git-hook scripts are not covered: they are code whose
// content must match the extension's source for the trust model, so cg always
// owns them.

// PendingFile is a contributed file that both the user and the extension
// changed since cg last wrote it. cg left the user's file in place.
type PendingFile struct {
	Dest string `json:"dest"`
	// Base is the blob id of the version cg last wrote; Upstream is the blob
	// id of the version the extension ships now. Both are in the extension's
	// repo, so `cg extension diff` and `resolve` can read them.
	Base     string `json:"base"`
	Upstream string `json:"upstream"`
}

// BlobID is the git blob id (SHA-1 object format) of data.
func BlobID(data []byte) string {
	h := sha1.New() //nolint:gosec // git object id, not a security use
	h.Write([]byte("blob " + strconv.Itoa(len(data)) + "\x00"))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// fileBlob returns the blob id of the file at path, or "" when it does not
// exist.
func fileBlob(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // workspace or extension file
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return BlobID(data), nil
}

// ConflictError reports existing files an install would overwrite although
// they aren't cg's: no ledger records them and their content differs from the
// extension's.
type ConflictError struct {
	Paths []string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("these files exist, differ from the extension's version, and cg has no record of writing them: %s. "+
		"Keep a copy if you need it, then run `cg extension install <source> --force` to take the extension's version",
		strings.Join(e.Paths, ", "))
}

// copyDecision is what Apply does with one copied file.
type copyDecision int

const (
	copyWrite   copyDecision = iota // write the new version
	copyAdopt                       // disk already holds the new version
	copyKeep                        // keep the user's file; nothing upstream to take
	copyPending                     // keep the user's file; upstream changed too
	copyRefuse                      // an unowned, different file is in the way
)

func decideCopy(base, disk, next string, force bool) copyDecision {
	switch {
	case disk == "":
		return copyWrite
	case disk == next:
		return copyAdopt
	case base == "":
		if force {
			return copyWrite
		}
		return copyRefuse
	case disk == base:
		return copyWrite
	case next == base:
		return copyKeep
	default:
		return copyPending
	}
}

// LegacyBases returns, for a ledger written before cg recorded file blobs, the
// blob of each copied file as the previous source (sourceDir, holding that
// version's manifest) shipped it: the best available record of what cg wrote.
// It returns nil when the previous source can't be read.
func LegacyBases(sourceDir string) map[string]string {
	m, err := LoadManifest(sourceDir)
	if err != nil || m.Validate() != nil {
		return nil
	}
	bases := map[string]string{}
	for _, c := range append(append([]CopyContribution(nil), m.Contributes.Notes...), m.Contributes.Templates...) {
		if id, err := fileBlob(filepath.Join(sourceDir, filepath.FromSlash(c.Src))); err == nil && id != "" {
			bases[c.Dest] = id
		}
	}
	return bases
}

// BlobContent reads blob id from the extension's repo: its store mirror, or
// its legacy clone for an entry that predates the store.
func BlobContent(ctx context.Context, ref *workspace.ExtensionRef, id string) ([]byte, error) {
	if !ValidCommit(id) { // a blob id has the same shape as a commit id
		return nil, fmt.Errorf("invalid blob id %q", id)
	}
	for _, dir := range []string{MirrorDir(ref.Repo), CloneDir(ref.Name)} {
		if !dirExists(dir) {
			continue
		}
		if out, err := gitx.RunRaw(ctx, dir, "cat-file", "blob", id); err == nil {
			return out, nil
		}
	}
	return nil, errors.New("blob " + id[:12] + " is not in the extension's repo on this machine; run `cg extension update " + ref.Name + "`")
}

// PendingAll returns every pending file recorded in the workspace's ledgers,
// keyed by extension name, for the extensions .cg.json records.
func PendingAll(root string, refs []workspace.ExtensionRef) map[string][]PendingFile {
	out := map[string][]PendingFile{}
	for i := range refs {
		l, err := LoadLedger(root, refs[i].Name)
		if err != nil || l == nil || len(l.Pending) == 0 {
			continue
		}
		p := append([]PendingFile(nil), l.Pending...)
		sort.Slice(p, func(a, b int) bool { return p[a].Dest < p[b].Dest })
		out[refs[i].Name] = p
	}
	return out
}
