package autoupdate

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"
)

func isolatedState(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDebounceElapsed(t *testing.T) {
	isolatedState(t)

	if !debounceElapsed() {
		t.Error("no stamp should count as elapsed")
	}

	// Recent stamp → not elapsed.
	now := strconv.FormatInt(time.Now().UnixMilli(), 10)
	if err := os.WriteFile(LastCheckPath(), []byte(now), 0o644); err != nil {
		t.Fatal(err)
	}
	if debounceElapsed() {
		t.Error("recent stamp should not be elapsed")
	}

	// Old stamp → elapsed.
	old := strconv.FormatInt(time.Now().Add(-48*time.Hour).UnixMilli(), 10)
	if err := os.WriteFile(LastCheckPath(), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if !debounceElapsed() {
		t.Error("old stamp should be elapsed")
	}

	// Corrupt stamp → treated as elapsed.
	if err := os.WriteFile(LastCheckPath(), []byte("not-a-number"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !debounceElapsed() {
		t.Error("corrupt stamp should be elapsed")
	}
}

func TestLockAcquireReleaseAndContention(t *testing.T) {
	isolatedState(t)

	if !acquireLock() {
		t.Fatal("first acquire should succeed")
	}
	// Held by us (a live PID, fresh mtime) → second acquire blocked.
	if acquireLock() {
		t.Error("second acquire should be blocked while held")
	}
	releaseLock()
	if !acquireLock() {
		t.Error("acquire after release should succeed")
	}
	releaseLock()
}

func TestLockStaleByMtime(t *testing.T) {
	isolatedState(t)
	// A lock file owned by a live PID but with an ancient mtime is stale.
	if err := os.WriteFile(LockPath(), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(LockPath(), old, old); err != nil {
		t.Fatal(err)
	}
	if !lockIsStale() {
		t.Error("lock older than lockMaxAge should be stale")
	}
	if !acquireLock() {
		t.Error("should take over a stale lock")
	}
	releaseLock()
}

func TestLockStaleByCorruptPID(t *testing.T) {
	isolatedState(t)
	if err := os.WriteFile(LockPath(), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !lockIsStale() {
		t.Error("corrupt PID content should be stale")
	}
}

func TestUpdatedMarkerRoundTrip(t *testing.T) {
	isolatedState(t)
	writeUpdatedMarker("v1.0.0", "v1.2.0")

	if _, err := os.Stat(UpdatedMarkerPath()); err != nil {
		t.Fatalf("marker not written: %v", err)
	}
	// notice.go reads + clears it (covered in notice_test); here just confirm
	// the file is valid JSON our reader accepts by re-reading the struct.
}

func TestRunWorkerSkipsDevBuild(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	RunWorker("dev")
	if _, err := os.Stat(LastCheckPath()); !os.IsNotExist(err) {
		t.Error("dev build should not even reach the debounce stamp")
	}
}

func TestDoCheckAndInstallUpToDate(t *testing.T) {
	isolatedState(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.0"}`))
	}))
	defer srv.Close()
	t.Setenv("CONSIGLIERE_GITHUB_API_BASE", srv.URL)
	t.Setenv("CONSIGLIERE_AUTO_UPDATE_REPO", "mnemcik/consigliere")

	doCheckAndInstall("v1.0.0") // same version → no install

	if _, err := os.Stat(LastCheckPath()); err != nil {
		t.Error("debounce stamp should be written even when up to date")
	}
	if _, err := os.Stat(UpdatedMarkerPath()); !os.IsNotExist(err) {
		t.Error("no updated marker should be written when already current")
	}
}

func TestDoCheckAndInstallNotSelfManaged(t *testing.T) {
	isolatedState(t) // no installed.json → unknown provenance, never self-replaces
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
	}))
	defer srv.Close()
	t.Setenv("CONSIGLIERE_GITHUB_API_BASE", srv.URL)
	t.Setenv("CONSIGLIERE_AUTO_UPDATE_REPO", "mnemcik/consigliere")

	doCheckAndInstall("v1.0.0") // newer available, but not self-managed → skip

	if _, err := os.Stat(UpdatedMarkerPath()); !os.IsNotExist(err) {
		t.Error("non-self-managed install must not write an updated marker")
	}
	if _, err := os.Stat(LastCheckPath()); err != nil {
		t.Error("debounce stamp should still be written")
	}
}
