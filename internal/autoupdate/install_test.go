package autoupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func makeTarGz(t *testing.T, entryName string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: entryName, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeZip(t *testing.T, entryName string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(entryName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestVerifyChecksum(t *testing.T) {
	archive := []byte("the archive bytes")
	name := "consigliere_1.2.3_linux_amd64.tar.gz"
	sums := []byte(fmt.Sprintf("%s  %s\ndeadbeef  other_file.tar.gz\n", sha256hex(archive), name))

	if err := verifyChecksum(archive, sums, name); err != nil {
		t.Errorf("matching checksum should pass: %v", err)
	}
	if err := verifyChecksum([]byte("tampered"), sums, name); err == nil {
		t.Error("mismatched checksum should fail")
	}
	if err := verifyChecksum(archive, sums, "not_listed.tar.gz"); err == nil {
		t.Error("missing entry should fail")
	}
}

func TestExtractBinaryTarGz(t *testing.T) {
	content := []byte("#!fake cg binary\n")
	archive := makeTarGz(t, exeName(), content)
	got, err := extractBinary(archive, "tar.gz")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("extracted %q, want %q", got, content)
	}
}

func TestExtractBinaryZip(t *testing.T) {
	content := []byte("#!fake cg binary zip\n")
	archive := makeZip(t, exeName(), content)
	got, err := extractBinary(archive, "zip")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("extracted %q, want %q", got, content)
	}
}

func TestExtractBinaryMissingEntry(t *testing.T) {
	archive := makeTarGz(t, "README.md", []byte("not the binary"))
	if _, err := extractBinary(archive, "tar.gz"); err == nil {
		t.Error("expected error when archive lacks the cg entry")
	}
}

func TestUnderBrewPrefix(t *testing.T) {
	cases := map[string]bool{
		"/opt/homebrew/bin/cg":               true,
		"/opt/homebrew/Caskroom/cg/1.0.0/cg": true,
		"/usr/local/bin/cg":                  true,
		"/home/linuxbrew/.linuxbrew/bin/cg":  true,
		"/home/user/.local/bin/cg":           false,
		"/usr/locallll/bin/cg":               false, // prefix must be path-boundary
	}
	for p, want := range cases {
		if got := underBrewPrefix(p); got != want {
			t.Errorf("underBrewPrefix(%q) = %v, want %v", p, got, want)
		}
	}
}

func writeInstalledState(t *testing.T, method string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	js := fmt.Sprintf(`{"version":"1.0.0","tag":"v1.0.0","method":%q,"path":"/tmp/cg"}`, method)
	if err := os.WriteFile(InstalledStatePath(), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectManagementInstallSh(t *testing.T) {
	writeInstalledState(t, methodInstallSh)
	m := DetectManagement()
	if !m.SelfManaged || m.Kind != methodInstallSh {
		t.Errorf("install.sh should be self-managed: %+v", m)
	}
	// BinaryPath targets the running executable (not the recorded st.Path,
	// which can be stale) — here that's the test binary.
	exe, _ := os.Executable()
	if resolved, e := filepath.EvalSymlinks(exe); e == nil {
		exe = resolved
	}
	if m.BinaryPath != exe {
		t.Errorf("BinaryPath = %q, want running executable %q", m.BinaryPath, exe)
	}
}

func TestDetectManagementHomebrewMethod(t *testing.T) {
	writeInstalledState(t, KindHomebrew)
	m := DetectManagement()
	if m.SelfManaged {
		t.Errorf("homebrew method must not be self-managed: %+v", m)
	}
	if m.Kind != KindHomebrew {
		t.Errorf("Kind = %q, want %q", m.Kind, KindHomebrew)
	}
}

func TestDetectManagementNoState(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // empty dir, no installed.json
	m := DetectManagement()
	if m.SelfManaged {
		t.Errorf("absent installed.json must not be self-managed: %+v", m)
	}
}

func TestInstallReleaseEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("end-to-end apply uses tar.gz; zip extraction is covered separately")
	}
	newBinary := []byte("#!/bin/sh\necho new cg\n")
	archiveName := fmt.Sprintf("consigliere_2.0.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archive := makeTarGz(t, exeName(), newBinary)
	sums := []byte(fmt.Sprintf("%s  %s\n", sha256hex(archive), archiveName))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case archiveName:
			_, _ = w.Write(archive)
		case "checksums.txt":
			_, _ = w.Write(sums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("CONSIGLIERE_GITHUB_DOWNLOAD_BASE", srv.URL)

	target := filepath.Join(t.TempDir(), "cg")
	if err := os.WriteFile(target, []byte("OLD BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := InstallRelease(context.Background(), "mnemcik/consigliere", "v2.0.0", target); err != nil {
		t.Fatalf("InstallRelease: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newBinary) {
		t.Errorf("target binary = %q, want %q", got, newBinary)
	}
}

func TestInstallReleaseChecksumMismatch(t *testing.T) {
	archiveName := fmt.Sprintf("consigliere_2.0.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		archiveName = fmt.Sprintf("consigliere_2.0.0_%s_%s.zip", runtime.GOOS, runtime.GOARCH)
	}
	archive := makeTarGz(t, exeName(), []byte("real"))
	sums := []byte("0000000000000000000000000000000000000000000000000000000000000000  " + archiveName + "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) == "checksums.txt" {
			_, _ = w.Write(sums)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer srv.Close()
	t.Setenv("CONSIGLIERE_GITHUB_DOWNLOAD_BASE", srv.URL)

	target := filepath.Join(t.TempDir(), "cg")
	_ = os.WriteFile(target, []byte("OLD"), 0o755)
	err := InstallRelease(context.Background(), "mnemcik/consigliere", "2.0.0", target)
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	// The original binary must be untouched on failure.
	got, _ := os.ReadFile(target)
	if string(got) != "OLD" {
		t.Errorf("target was modified on failed update: %q", got)
	}
}
