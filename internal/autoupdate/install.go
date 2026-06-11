package autoupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/minio/selfupdate"
)

// binaryName is the executable name inside a release archive.
const binaryName = "cg"

// goosWindows is runtime.GOOS for Windows (archive ext + binary suffix differ).
const goosWindows = "windows"

// Management.Kind values, describing how cg was installed.
const (
	KindInstallSh = "install.sh" // installed by install.sh — the one self-replaceable provenance (DEC-011)
	KindHomebrew  = "homebrew"   // installed via Homebrew — upgrade with brew
	KindUnknown   = "unknown"    // go install, manual copy, etc. — left alone
)

// methodInstallSh is the installed.json "method" value install.sh records.
const methodInstallSh = KindInstallSh

// maxDownloadBytes bounds an asset download so a malformed/huge response can't
// exhaust memory; release archives are a few MB.
const maxDownloadBytes = 100 << 20 // 100 MiB

// brewPrefixes are the install roots that mean a binary is Homebrew-managed
// (covering both Cellar/ and Caskroom/ under each). DEC-011.
var brewPrefixes = []string{"/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew"}

// installedState mirrors the JSON install.sh writes to installed.json.
type installedState struct {
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	Method      string `json:"method"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Path        string `json:"path"`
	InstalledAt string `json:"installedAt"`
}

// Management describes how the running cg binary was installed and whether it
// is safe to self-replace.
type Management struct {
	SelfManaged bool   // true only for install.sh installs — safe to self-replace
	Kind        string // "install.sh" | "homebrew" | "unknown"
	BinaryPath  string // resolved path of the running executable
}

// DetectManagement decides whether cg may replace its own binary (DEC-011).
// Authoritative signal: install.sh writes installed.json with method
// "install.sh". Absent that, a binary under a Homebrew prefix is brew-managed;
// anything else is unknown provenance (go install, manual copy) and is left
// alone. Only install.sh installs are self-managed.
func DetectManagement() Management {
	exe, err := os.Executable()
	if err == nil {
		if resolved, e := filepath.EvalSymlinks(exe); e == nil {
			exe = resolved
		}
	}

	// Strongest signal first: a binary physically under a Homebrew prefix is
	// brew-managed regardless of any (possibly stale) installed.json — e.g. a
	// user who installed via install.sh, then later `brew install`ed over it.
	// Checking the *running* executable before trusting the recorded method
	// prevents misclassifying a brew binary as self-managed (DEC-011).
	if exe != "" && underBrewPrefix(exe) {
		return Management{SelfManaged: false, Kind: KindHomebrew, BinaryPath: exe}
	}

	st, _ := readInstalledState() // absent/unreadable → zero value, treated as unmanaged
	if st.Method == methodInstallSh {
		// Always target the running executable, never the recorded st.Path:
		// a self-replace replaces *this* binary, and st.Path can be stale.
		return Management{SelfManaged: true, Kind: KindInstallSh, BinaryPath: exe}
	}
	if strings.EqualFold(st.Method, KindHomebrew) {
		return Management{SelfManaged: false, Kind: KindHomebrew, BinaryPath: exe}
	}
	return Management{SelfManaged: false, Kind: KindUnknown, BinaryPath: exe}
}

func underBrewPrefix(p string) bool {
	for _, prefix := range brewPrefixes {
		if p == prefix || strings.HasPrefix(p, prefix+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func readInstalledState() (installedState, error) {
	var st installedState
	raw, err := os.ReadFile(InstalledStatePath())
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return st, err
	}
	return st, nil
}

// InstallRelease downloads the platform archive for version from repo's GitHub
// release, verifies its SHA-256 against the release checksums.txt, extracts the
// cg binary, and atomically replaces the executable at targetPath. version may
// be given with or without a leading "v". A non-nil error leaves the existing
// binary untouched (selfupdate.Apply rolls back on failure).
func InstallRelease(ctx context.Context, repo, version, targetPath string) error {
	tag := Normalize(version)
	fileVer := strings.TrimPrefix(tag, "v")
	ext := "tar.gz"
	if runtime.GOOS == goosWindows {
		ext = "zip"
	}
	archiveName := fmt.Sprintf("consigliere_%s_%s_%s.%s", fileVer, runtime.GOOS, runtime.GOARCH, ext)
	base := fmt.Sprintf("%s/%s/releases/download/%s", downloadBase(), repo, tag)

	archive, err := downloadFile(ctx, base+"/"+archiveName)
	if err != nil {
		return fmt.Errorf("download %s: %w", archiveName, err)
	}
	sums, err := downloadFile(ctx, base+"/checksums.txt")
	if err != nil {
		return fmt.Errorf("download checksums.txt: %w", err)
	}

	if err := verifyChecksum(archive, sums, archiveName); err != nil {
		return err
	}

	bin, err := extractBinary(archive, ext)
	if err != nil {
		return fmt.Errorf("extract %s from %s: %w", exeName(), archiveName, err)
	}

	opts := selfupdate.Options{}
	if targetPath != "" {
		opts.TargetPath = targetPath
	}
	if err := selfupdate.Apply(bytes.NewReader(bin), opts); err != nil {
		return fmt.Errorf("apply update: %w", err)
	}
	return nil
}

// exeName is the binary's filename inside the archive for this platform.
func exeName() string {
	if runtime.GOOS == goosWindows {
		return binaryName + ".exe"
	}
	return binaryName
}

func downloadFile(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes))
}

// verifyChecksum confirms archive's SHA-256 matches the entry for archiveName
// in a GoReleaser checksums.txt ("<hex>  <filename>" per line).
func verifyChecksum(archive, checksums []byte, archiveName string) error {
	want := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == archiveName {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("checksums.txt does not list %s", archiveName)
	}
	sum := sha256.Sum256(archive)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("SHA-256 mismatch for %s: expected %s, got %s", archiveName, want, got)
	}
	return nil
}

// extractBinary returns the cg binary bytes from a release archive. ext is
// "tar.gz" or "zip"; the binary is matched by base name anywhere in the archive
// (GoReleaser places it at the top level).
func extractBinary(archive []byte, ext string) ([]byte, error) {
	if ext == "zip" {
		return extractFromZip(archive)
	}
	return extractFromTarGz(archive)
}

func extractFromTarGz(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg || path.Base(hdr.Name) != exeName() {
			continue
		}
		return io.ReadAll(io.LimitReader(tr, maxDownloadBytes))
	}
	return nil, fmt.Errorf("no %s entry found", exeName())
}

func extractFromZip(archive []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if path.Base(f.Name) != exeName() {
			continue
		}
		return readZipEntry(f)
	}
	return nil, fmt.Errorf("no %s entry found", exeName())
}

func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(io.LimitReader(rc, maxDownloadBytes))
}

// RefreshInstalledState rewrites installed.json after a successful self-update
// so the recorded version/tag stay truthful, preserving install.sh's other
// fields. A missing file is left missing (the install was not install.sh-based).
func RefreshInstalledState(toVersion, nowISO string) error {
	st, err := readInstalledState()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	fileVer := strings.TrimPrefix(Normalize(toVersion), "v")
	st.Version = fileVer
	st.Tag = Normalize(toVersion)
	st.InstalledAt = nowISO

	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(InstalledStatePath(), append(raw, '\n'), 0o644)
}
