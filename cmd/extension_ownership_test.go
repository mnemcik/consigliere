package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
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

// legacyEntry turns the installed demo entry into one from before the store
// and before content ids: no commit in .cg.json, no files in the ledger. With
// clone set, a legacy clone of repo at its current HEAD stands in for the old
// name-keyed clone.
func legacyEntry(t *testing.T, cfgHome, ws, repo string, clone bool) {
	t.Helper()
	cfg, err := workspace.Detect(ws)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Extensions[0].Commit = ""
	if err := cfg.Save(ws); err != nil {
		t.Fatal(err)
	}
	l := ledgerFor(t, ws)
	l.Files = nil
	if err := l.Save(ws); err != nil {
		t.Fatal(err)
	}
	if clone {
		if _, err := gitx.Run(context.Background(), "", "clone", "--quiet", repo, legacyCloneDir(cfgHome)); err != nil {
			t.Fatal(err)
		}
	}
}

// Review finding 2: a legacy clone may have moved since the install, so a base
// read from it is uncertain. A file that matches neither it nor the new
// version is raised, never kept silently.
func TestLegacyCloneBaseIsUncertain(t *testing.T) {
	cfgHome := t.TempDir()
	ws, repo := installed(t, cfgHome) // note at v1 (installedTxt)
	commitFile(t, repo, noteRel, "v2")
	legacyEntry(t, cfgHome, ws, repo, true) // the shared legacy clone is already at v2

	if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(ws, noteRel)); got != installedTxt {
		t.Errorf("note = %q, want it left for the user to decide", got)
	}
	p := ledgerFor(t, ws).Pending
	if len(p) != 1 || !p[0].UncertainBase {
		t.Fatalf("want the note pending with an uncertain base, got %+v", p)
	}
	out, err := runExtCfg(t, cfgHome, ws, "diff", "demo", noteRel)
	if err != nil || !strings.Contains(out, "may not be exactly what was installed") {
		t.Errorf("diff should flag the uncertain base: err %v\n%s", err, out)
	}
}

// Review finding 3: a legacy entry whose clone is gone has no base at all.
// update must not refuse (or advise --force, which would destroy the edit);
// the file is raised, and only keep or take apply.
func TestLegacyEntryWithoutCloneRaisesInsteadOfRefusing(t *testing.T) {
	cfgHome := t.TempDir()
	ws, repo := installed(t, cfgHome)
	legacyEntry(t, cfgHome, ws, repo, false)
	writeNote(t, ws, "local edits")

	if out, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
		t.Fatalf("update refused: %v\n%s", err, out)
	}
	if got := readFile(t, filepath.Join(ws, noteRel)); got != "local edits" {
		t.Errorf("note = %q, want the user's edits", got)
	}
	p := ledgerFor(t, ws).Pending
	if len(p) != 1 || p[0].Base != "" {
		t.Fatalf("want the note pending with no base, got %+v", p)
	}
	out, err := runExtCfg(t, cfgHome, ws, "diff", "demo", noteRel)
	if err != nil || !strings.Contains(out, "no record of the version it installed") {
		t.Errorf("diff should say the base is unknown: err %v\n%s", err, out)
	}
	if _, err := runExtCfg(t, cfgHome, ws, "resolve", "demo", noteRel, "--merge"); err == nil {
		t.Error("--merge without a base should be refused")
	}
	if _, err := runExtCfg(t, cfgHome, ws, "resolve", "demo", noteRel, "--keep"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(ws, noteRel)); got != "local edits" {
		t.Errorf("--keep changed the note: %q", got)
	}
}

// A deleted copied file is the user's change: not recreated silently, and
// raised when the extension changes it.
func TestDeletedFileIsTheUsersChange(t *testing.T) {
	cfgHome := t.TempDir()
	ws, repo := installed(t, cfgHome)
	if err := os.Remove(filepath.Join(ws, noteRel)); err != nil {
		t.Fatal(err)
	}
	commitFile(t, repo, "fragments/demo.md", "unrelated")
	if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, noteRel)); !os.IsNotExist(err) {
		t.Fatalf("update recreated a note the user deleted (stat err %v)", err)
	}

	commitFile(t, repo, noteRel, "v2")
	if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
		t.Fatal(err)
	}
	if len(ledgerFor(t, ws).Pending) != 1 {
		t.Fatal("a deleted note the extension changed should be pending")
	}
	if _, err := runExtCfg(t, cfgHome, ws, "resolve", "demo", noteRel, "--merge"); err == nil {
		t.Error("--merge into a deleted file should be refused")
	}
	abs := filepath.Join(ws, noteRel) // resolve also takes an absolute path
	if _, err := runExtCfg(t, cfgHome, ws, "resolve", "demo", abs, "--take"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, abs); got != "v2" {
		t.Errorf("--take restored %q", got)
	}
}

// Review finding 1: with core.autocrlf the exported files carry CRLF while the
// repo stores LF. Content ids must still be the repo's blob ids, so diff and
// resolve can read them, and an unedited file must not look edited.
func TestOwnershipUnderAutocrlf(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	cfgHome := t.TempDir()
	ws := newWorkspace(t)
	repo := makeExtRepo(t, testManifest)
	commitFile(t, repo, noteRel, "a\nb\nc\nd\ne\n")

	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, []byte("[core]\n\tautocrlf = true\n"), 0o644); err != nil { //nolint:gosec // test config
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)

	if _, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(ws, noteRel)); !strings.Contains(got, "\r\n") {
		t.Skipf("git didn't export CRLF here (%q); nothing to test", got)
	}
	// Unedited: an upstream change is simply taken.
	commitFile(t, repo, "fragments/demo.md", "unrelated")
	if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
		t.Fatal(err)
	}
	if p := ledgerFor(t, ws).Pending; len(p) != 0 {
		t.Fatalf("an unedited CRLF note looked edited: %+v", p)
	}

	writeNote(t, ws, "A\r\nb\r\nc\r\nd\r\ne\r\n")
	commitFile(t, repo, noteRel, "a\nb\nc\nd\nE\n")
	if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := runExtCfg(t, cfgHome, ws, "diff", "demo", noteRel); err != nil {
		t.Fatalf("diff under autocrlf: %v", err)
	}
	if _, err := runExtCfg(t, cfgHome, ws, "resolve", "demo", noteRel, "--merge"); err != nil {
		t.Fatalf("merge under autocrlf: %v", err)
	}
	if got := strings.ReplaceAll(readFile(t, filepath.Join(ws, noteRel)), "\r\n", "\n"); got != "A\nb\nc\nd\nE\n" {
		t.Errorf("merged = %q", got)
	}
}

// CodeRabbit: a destination that moves from notes to templates is still
// shipped, so it isn't removed as an orphan.
func TestDestinationMovedBetweenNotesAndTemplatesIsKept(t *testing.T) {
	cfgHome := t.TempDir()
	ws, repo := installed(t, cfgHome)
	moved := strings.Replace(testManifest, `"notes": [{ "src": "notes/demo.md", "dest": "notes/demo.md" }]`,
		`"templates": [{ "src": "notes/demo.md", "dest": "notes/demo.md" }]`, 1)
	if moved == testManifest {
		t.Fatal("fixture manifest did not change")
	}
	updateExtRepo(t, repo, moved, "")
	if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(ws, noteRel)); got != installedTxt {
		t.Errorf("a destination still shipped was removed (now %q)", got)
	}
}

// A legacy ledger's dropped note is removed when unedited (its base comes from
// the pinned tree) and kept when edited.
func TestLegacyLedgerDroppedNote(t *testing.T) {
	dropped := strings.Replace(testManifest, `,
    "notes": [{ "src": "notes/demo.md", "dest": "notes/demo.md" }]`, "", 1)
	if dropped == testManifest {
		t.Fatal("fixture manifest did not change")
	}
	for _, edited := range []bool{false, true} {
		cfgHome := t.TempDir()
		ws, repo := installed(t, cfgHome)
		l := ledgerFor(t, ws)
		l.Files = nil
		if err := l.Save(ws); err != nil {
			t.Fatal(err)
		}
		if edited {
			writeNote(t, ws, "mine")
		}
		updateExtRepo(t, repo, dropped, "")
		if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
			t.Fatal(err)
		}
		_, err := os.Stat(filepath.Join(ws, noteRel))
		if edited && err != nil {
			t.Errorf("an edited dropped note was deleted: %v", err)
		}
		if !edited && !os.IsNotExist(err) {
			t.Errorf("an unedited dropped note was kept (stat err %v)", err)
		}
	}
}

// An extension repo that stores a file with CRLF: its content id is not a blob
// in the repo, so diff can't show it and says why, while --keep still works.
func TestCRLFStoredRepoStillResolvesWithKeep(t *testing.T) {
	cfgHome := t.TempDir()
	ws, repo := installed(t, cfgHome)
	writeNote(t, ws, "mine\n")
	commitFile(t, repo, noteRel, "v2\r\nstored with CRLF\r\n")
	if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
		t.Fatal(err)
	}
	if len(ledgerFor(t, ws).Pending) != 1 {
		t.Fatal("setup: expected one pending file")
	}
	out, err := runExtCfg(t, cfgHome, ws, "diff", "demo", noteRel)
	if err == nil || !strings.Contains(err.Error()+out, "CRLF") {
		t.Errorf("diff should explain it can't read the version: err %v\n%s", err, out)
	}
	if _, err := runExtCfg(t, cfgHome, ws, "resolve", "demo", noteRel, "--keep"); err != nil {
		t.Fatalf("--keep must work without the versions: %v", err)
	}
	if len(ledgerFor(t, ws).Pending) != 0 {
		t.Error("--keep didn't clear the pending file")
	}
}
