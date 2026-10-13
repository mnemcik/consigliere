package extension

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeTestFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// assertNoTempFiles fails if a .cg-tmp-* file was left behind in dir.
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".cg-tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestWriteFileAtomicCreatesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "f.md")
	if err := writeFileAtomic(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "two" {
		t.Errorf("content = %q, want %q", got, "two")
	}
	assertNoTempFiles(t, filepath.Dir(path))
}

// The property #139 is about: a reader that opened the file before the write
// still reads the complete previous content, because the write replaces the
// directory entry instead of truncating the file in place.
func TestCopyFileNeverTruncatesAnOpenFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows refuses to rename over a file another handle has open")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "new.sh")
	dest := filepath.Join(dir, "hook.sh")
	old := "#!/bin/sh\necho previous version\n"
	writeTestFile(t, src, "#!/bin/sh\necho new\n", 0o644)
	writeTestFile(t, dest, old, 0o755)

	reader, err := os.Open(dest) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()

	if err := copyExecutable(src, dest); err != nil {
		t.Fatal(err)
	}
	seen, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(seen) != old {
		t.Errorf("open reader saw %q, want the complete previous content %q", seen, old)
	}
	if got := readFile(t, dest); got != "#!/bin/sh\necho new\n" {
		t.Errorf("dest = %q after copy", got)
	}
}

func TestWriteFileAtomicKeepsExistingMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	writeTestFile(t, path, "x", 0o600)
	if err := writeFileAtomic(path, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want the existing 600", got)
	}

	created := filepath.Join(dir, "new")
	if err := writeFileAtomic(created, []byte("z"), 0o640); err != nil {
		t.Fatal(err)
	}
	if info, err = os.Stat(created); err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("new file mode = %o, want 640", got)
	}
}

func TestCopyFileForcesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dest := filepath.Join(dir, "dest")
	writeTestFile(t, src, "#!/bin/sh\n", 0o644)
	writeTestFile(t, dest, "old", 0o644)
	if err := copyExecutable(src, dest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Errorf("mode = %o, want 755", got)
	}
}

func TestWriteFileAtomicFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "AGENTS.md")
	link := filepath.Join(dir, "CLAUDE.md")
	writeTestFile(t, target, "old", 0o644)
	if err := os.Symlink("AGENTS.md", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := writeFileAtomic(link, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("CLAUDE.md is no longer a symlink")
	}
	if got := readFile(t, target); got != "new" {
		t.Errorf("link target = %q, want %q", got, "new")
	}
	assertNoTempFiles(t, dir)
}

func TestWriteFileAtomicFailureKeepsPreviousFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory write permission is POSIX-specific")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	writeTestFile(t, path, "previous", 0o644)
	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // test temp dir

	if err := writeFileAtomic(path, []byte("new"), 0o644); err == nil {
		t.Fatal("expected an error writing into a read-only directory")
	}
	if got := readFile(t, path); got != "previous" {
		t.Errorf("content = %q, want the previous content intact", got)
	}
}

// A file without its owner write bit is refused, as the in-place write it
// replaces was: the rename would otherwise drop the protection silently.
func TestWriteFileAtomicRefusesReadOnlyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	writeTestFile(t, path, "protected", 0o444)
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) }) //nolint:gosec // test temp dir

	for name, write := range map[string]func() error{
		"keep mode":  func() error { return writeFileAtomic(path, []byte("new"), 0o644) },
		"force mode": func() error { return writeFileAtomicMode(path, []byte("new"), 0o644) },
	} {
		if err := write(); !errors.Is(err, fs.ErrPermission) {
			t.Errorf("%s: err = %v, want a permission error", name, err)
		}
	}
	if got := readFile(t, path); got != "protected" {
		t.Errorf("content = %q, want it untouched", got)
	}
}
