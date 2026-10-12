package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/gitx"
)

const (
	noteRel      = "notes/demo.md"
	installedTxt = "note notes/demo.md"
)

func writeNote(t *testing.T, ws, body string) {
	t.Helper()
	p := filepath.Join(ws, filepath.FromSlash(noteRel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}
}

func ledgerFor(t *testing.T, ws string) *extension.Ledger {
	t.Helper()
	l, err := extension.LoadLedger(ws, "demo")
	if err != nil || l == nil {
		t.Fatalf("ledger for demo: %v %v", l, err)
	}
	return l
}

// installed returns a workspace with the demo extension installed from a fresh
// repo, plus that repo.
func installed(t *testing.T, cfgHome string) (ws, repo string) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git not available")
	}
	ws = newWorkspace(t)
	repo = makeExtRepo(t, testManifest)
	if out, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	return ws, repo
}

func TestInstallRefusesAnUnrecordedDifferentFile(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	cfgHome := t.TempDir()
	ws := newWorkspace(t)
	repo := makeExtRepo(t, testManifest)
	writeNote(t, ws, "my own note")

	out, err := runExtCfg(t, cfgHome, ws, "install", repo)
	if err == nil || !strings.Contains(err.Error()+out, noteRel) {
		t.Fatalf("install over a different file should refuse naming it; err %v, out %q", err, out)
	}
	if got := readFile(t, filepath.Join(ws, noteRel)); got != "my own note" {
		t.Errorf("refused install changed the file: %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "CLAUDE.md")); strings.Contains(string(b), "ext:demo") { //nolint:gosec // test temp dir
		t.Error("refused install still added its CLAUDE.md section")
	}

	if _, err := runExtCfg(t, cfgHome, ws, "install", repo, "--force"); err != nil {
		t.Fatalf("install --force: %v", err)
	}
	if got := readFile(t, filepath.Join(ws, noteRel)); got != installedTxt {
		t.Errorf("--force left %q", got)
	}
}

func TestInstallAdoptsAnIdenticalFile(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	cfgHome := t.TempDir()
	ws := newWorkspace(t)
	repo := makeExtRepo(t, testManifest)
	writeNote(t, ws, installedTxt)
	if _, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
		t.Fatalf("install over an identical file: %v", err)
	}
	if l := ledgerFor(t, ws); l.Files[noteRel] != extension.BlobID([]byte(installedTxt)) {
		t.Errorf("identical file not recorded as the extension's: %+v", l.Files)
	}
}

func TestUpdateRespectsUserEdits(t *testing.T) {
	cfgHome := t.TempDir()

	t.Run("unedited file is updated", func(t *testing.T) {
		ws, repo := installed(t, cfgHome)
		commitFile(t, repo, noteRel, "v2")
		if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, filepath.Join(ws, noteRel)); got != "v2" {
			t.Errorf("note = %q, want v2", got)
		}
	})

	t.Run("edited file, upstream unchanged: kept silently", func(t *testing.T) {
		ws, repo := installed(t, cfgHome)
		writeNote(t, ws, "mine")
		commitFile(t, repo, "fragments/demo.md", "other change")
		out, err := runExtCfg(t, cfgHome, ws, "update", "demo")
		if err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, filepath.Join(ws, noteRel)); got != "mine" {
			t.Errorf("note = %q, want the user's", got)
		}
		if strings.Contains(out, "kept your") || len(ledgerFor(t, ws).Pending) != 0 {
			t.Errorf("nothing upstream to take, so nothing to raise; out %q", out)
		}
	})

	t.Run("both changed: kept and pending", func(t *testing.T) {
		ws, repo := installed(t, cfgHome)
		writeNote(t, ws, "mine")
		commitFile(t, repo, noteRel, "v2")
		out, err := runExtCfg(t, cfgHome, ws, "update", "demo")
		if err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, filepath.Join(ws, noteRel)); got != "mine" {
			t.Errorf("note = %q, want the user's", got)
		}
		p := ledgerFor(t, ws).Pending
		if len(p) != 1 || p[0].Dest != noteRel || p[0].Upstream != extension.BlobID([]byte("v2")) {
			t.Fatalf("pending = %+v", p)
		}
		if !strings.Contains(out, "cg extension resolve demo "+noteRel) {
			t.Errorf("update output should name the resolve command: %q", out)
		}
	})
}

// The gmail-digest case: a ledger from before file blobs were recorded, a
// locally edited note, and an extension that didn't change it.
func TestUpdateOfALegacyLedgerKeepsUserEdits(t *testing.T) {
	cfgHome := t.TempDir()
	ws, repo := installed(t, cfgHome)
	l := ledgerFor(t, ws)
	l.Files = nil
	if err := l.Save(ws); err != nil {
		t.Fatal(err)
	}
	writeNote(t, ws, "local additions")
	commitFile(t, repo, "fragments/demo.md", "unrelated change")

	if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(ws, noteRel)); got != "local additions" {
		t.Errorf("legacy update overwrote the user's note: %q", got)
	}
	if l := ledgerFor(t, ws); l.Files[noteRel] != extension.BlobID([]byte(installedTxt)) || len(l.Pending) != 0 {
		t.Errorf("want the installed version as the recorded base and nothing pending: %+v", l)
	}
}

func TestRemoveKeepsAnEditedFile(t *testing.T) {
	cfgHome := t.TempDir()
	ws, _ := installed(t, cfgHome)
	writeNote(t, ws, "mine")
	out, err := runExtCfg(t, cfgHome, ws, "remove", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(ws, noteRel)); got != "mine" {
		t.Errorf("remove deleted the user's note (now %q)", got)
	}
	if !strings.Contains(out, "kept "+noteRel) {
		t.Errorf("remove should say it kept the file: %q", out)
	}

	ws2, _ := installed(t, cfgHome)
	if _, err := runExtCfg(t, cfgHome, ws2, "remove", "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws2, noteRel)); !os.IsNotExist(err) {
		t.Errorf("an unedited note should be removed, stat err %v", err)
	}
}

func TestResolve(t *testing.T) {
	cfgHome := t.TempDir()
	pending := func(t *testing.T, mine, upstream string) (ws, repo string) {
		t.Helper()
		ws, repo = installed(t, cfgHome)
		writeNote(t, ws, mine)
		commitFile(t, repo, noteRel, upstream)
		if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
			t.Fatal(err)
		}
		if len(ledgerFor(t, ws).Pending) != 1 {
			t.Fatal("setup: expected one pending file")
		}
		return ws, repo
	}
	resolved := func(t *testing.T, ws, upstream string) {
		t.Helper()
		l := ledgerFor(t, ws)
		if len(l.Pending) != 0 || l.Files[noteRel] != extension.BlobID([]byte(upstream)) {
			t.Errorf("after resolve want no pending and the upstream version as baseline: %+v", l)
		}
	}

	t.Run("keep", func(t *testing.T) {
		ws, repo := pending(t, "mine", "v2")
		if _, err := runExtCfg(t, cfgHome, ws, "resolve", "demo", noteRel, "--keep"); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, filepath.Join(ws, noteRel)); got != "mine" {
			t.Errorf("--keep changed the file: %q", got)
		}
		resolved(t, ws, "v2")
		// Not raised again until upstream changes the file again.
		commitFile(t, repo, "fragments/demo.md", "unrelated")
		if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
			t.Fatal(err)
		}
		if p := ledgerFor(t, ws).Pending; len(p) != 0 {
			t.Errorf("a kept file was raised again without an upstream change: %+v", p)
		}
	})

	t.Run("take", func(t *testing.T) {
		ws, _ := pending(t, "mine", "v2")
		if _, err := runExtCfg(t, cfgHome, ws, "resolve", "demo", noteRel, "--take"); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, filepath.Join(ws, noteRel)); got != "v2" {
			t.Errorf("--take left %q", got)
		}
		resolved(t, ws, "v2")
	})

	t.Run("clean merge", func(t *testing.T) {
		base := "a\nb\nc\nd\ne\n"
		ws, repo := installed(t, cfgHome)
		commitFile(t, repo, noteRel, base)
		if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
			t.Fatal(err)
		}
		writeNote(t, ws, "A\nb\nc\nd\ne\n")
		commitFile(t, repo, noteRel, "a\nb\nc\nd\nE\n")
		if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
			t.Fatal(err)
		}
		out, err := runExtCfg(t, cfgHome, ws, "diff", "demo", noteRel)
		if err != nil || !strings.Contains(out, "+E") || !strings.Contains(out, "+A") || !strings.Contains(out, "merge is clean") {
			t.Errorf("diff should show both changes and a clean merge: err %v\n%s", err, out)
		}
		if _, err := runExtCfg(t, cfgHome, ws, "resolve", "demo", noteRel, "--merge"); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, filepath.Join(ws, noteRel)); got != "A\nb\nc\nd\nE\n" {
			t.Errorf("merged = %q", got)
		}
		resolved(t, ws, "a\nb\nc\nd\nE\n")
	})

	t.Run("overlapping merge refused", func(t *testing.T) {
		ws, _ := pending(t, "mine", "v2")
		if _, err := runExtCfg(t, cfgHome, ws, "resolve", "demo", noteRel, "--merge"); err == nil {
			t.Error("an overlapping merge should be refused")
		}
		if got := readFile(t, filepath.Join(ws, noteRel)); got != "mine" {
			t.Errorf("refused merge changed the file: %q", got)
		}
		if len(ledgerFor(t, ws).Pending) != 1 {
			t.Error("refused merge cleared the pending record")
		}
	})

	t.Run("not pending", func(t *testing.T) {
		ws, _ := installed(t, cfgHome)
		if _, err := runExtCfg(t, cfgHome, ws, "resolve", "demo", noteRel, "--keep"); err == nil {
			t.Error("resolving a file that isn't pending should fail")
		}
	})
}

// The session-start hook raises pending files: a one-line systemMessage for
// the user, and additionalContext for Claude naming the commands to use.
func TestSessionStartRaisesPendingFiles(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	cfgHome := t.TempDir()
	ws := newGitRepo(t, filepath.Join(t.TempDir(), "ws"), ".", `{"type":"consigliere","version":"1.3.0"}`)
	repo := makeExtRepo(t, testManifest)
	if _, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
		t.Fatal(err)
	}
	writeNote(t, ws, "mine")
	commitFile(t, repo, noteRel, "v2")
	if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
		t.Fatal(err)
	}

	hook := func() string {
		t.Helper()
		var out bytes.Buffer
		rootCmd.SetArgs([]string{"session", "pull-latest"})
		rootCmd.SetIn(strings.NewReader("{}"))
		rootCmd.SetOut(&out)
		rootCmd.SetErr(new(bytes.Buffer))
		if err := rootCmd.Execute(); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	raw := hook()
	if strings.Count(raw, "\n") != 1 {
		t.Fatalf("want exactly one JSON object, got:\n%s", raw)
	}
	var got struct {
		SystemMessage      string `json:"systemMessage"`
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("hook output is not JSON: %v\n%s", err, raw)
	}
	if !strings.Contains(got.SystemMessage, "demo "+noteRel) {
		t.Errorf("systemMessage = %q", got.SystemMessage)
	}
	ctx := got.HookSpecificOutput.AdditionalContext
	if got.HookSpecificOutput.HookEventName != "SessionStart" || !strings.Contains(ctx, "cg extension diff") || !strings.Contains(ctx, "--merge") {
		t.Errorf("additionalContext = %q", ctx)
	}

	if _, err := runExtCfg(t, cfgHome, ws, "resolve", "demo", noteRel, "--keep"); err != nil {
		t.Fatal(err)
	}
	if out := hook(); strings.Contains(out, "additionalContext") {
		t.Errorf("nothing pending, but the hook still raised it: %s", out)
	}
}
