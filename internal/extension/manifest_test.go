package extension

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const validManifest = `{
  "manifest": 2,
  "name": "1password",
  "version": "0.1.0",
  "description": "1Password as authoritative credential store",
  "contributes": {
    "claude-md-sections": [{ "id": "credentials-1password", "path": "fragments/credentials.md" }],
    "notes": [{ "src": "notes/credentials-policy.md", "dest": "notes/credentials-policy.md" }],
    "hooks": [{ "event": "SessionStart", "wrapper": "hooks/gate.sh", "command": "cg-1password gate" }],
    "commands": [
      { "command": "secret", "binary": "cg-1password" },
      { "command": "secret get", "binary": "cg-1password" },
      { "command": "secret-store x-2", "binary": "cg-1password" }
    ],
    "templates": [{ "src": "templates/req.md", "dest": "templates/req.md" }]
  }
}`

func TestParseAndValidateValid(t *testing.T) {
	m, err := ParseManifest([]byte(validManifest))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if m.Name != "1password" || m.Version != "0.1.0" {
		t.Errorf("unexpected name/version: %q %q", m.Name, m.Version)
	}
	if len(m.Contributes.ClaudeMDSections) != 1 || m.Contributes.ClaudeMDSections[0].ID != "credentials-1password" {
		t.Errorf("claude-md-sections not parsed: %+v", m.Contributes.ClaudeMDSections)
	}
	if len(m.Contributes.Commands) != 3 || m.Contributes.Commands[1] != (CommandContribution{Command: "secret get", Binary: "cg-1password"}) {
		t.Errorf("commands not parsed: %+v", m.Contributes.Commands)
	}
}

// cmdManifest is a minimal manifest of schema version v declaring commands,
// each served by bin/cg-a.
func cmdManifest(v int, commands ...string) Manifest {
	m := Manifest{Manifest: v, Name: "a", Version: "1.0.0", Description: "d"}
	for _, c := range commands {
		m.Contributes.Commands = append(m.Contributes.Commands, CommandContribution{Command: c, Binary: "cg-a"})
	}
	return m
}

// S1, S4: valid commands validate, including nesting inside one extension.
func TestValidateCommands(t *testing.T) {
	for _, m := range []Manifest{
		cmdManifest(2, "live run", "live watch"),
		cmdManifest(2, "shop", "shop add"),
		cmdManifest(2, "coderabbit approve", "pr", "pr merge now"),
		cmdManifest(2, "a-1 b-2 c-3"),
		cmdManifest(2, "a- 9-"), // a letter or digit first; a trailing - is fine
		cmdManifest(2),          // v2 without commands
	} {
		if err := m.Validate(); err != nil {
			t.Errorf("Validate(%v): %v", m.Contributes.Commands, err)
		}
	}
}

// S5: a v1 manifest without commands still validates; the removed
// "subcommands" form is ignored like any unknown field (Q8).
func TestValidateV1WithoutCommands(t *testing.T) {
	m, err := ParseManifest([]byte(`{"manifest": 1, "name": "a", "version": "1", "description": "d",
	  "contributes": {"subcommands": [{"namespace": "a", "binary": "cg-a"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("v1 manifest without commands: %v", err)
	}
	if len(m.Contributes.Commands) != 0 {
		t.Errorf("subcommands must not become commands: %+v", m.Contributes.Commands)
	}
}

func TestParseManifestBadJSON(t *testing.T) {
	if _, err := ParseManifest([]byte("{not json")); err == nil {
		t.Fatal("expected parse error on malformed JSON")
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		m    Manifest
		want string // substring expected in the error
	}{
		{"wrong schema version", Manifest{Manifest: 3, Name: "a", Version: "1.0.0", Description: "d"}, "schema version"},
		{"empty name", Manifest{Manifest: 1, Name: "", Version: "1.0.0", Description: "d"}, "invalid name"},
		{"bad name chars", Manifest{Manifest: 1, Name: "Has_Caps", Version: "1.0.0", Description: "d"}, "invalid name"},
		{"missing version", Manifest{Manifest: 1, Name: "a", Version: "", Description: "d"}, "version is required"},
		{"missing description", Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: ""}, "description is required"},
		{
			"bad section id",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{ClaudeMDSections: []SectionContribution{{ID: "Bad ID", Path: "p"}}}},
			"invalid id",
		},
		{
			"multi-line note index",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Notes: []CopyContribution{{Src: "n.md", Dest: "notes/n.md", Index: "one\ntwo"}}}},
			"single line",
		},
		{
			"template index",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Templates: []CopyContribution{{Src: "t.md", Dest: "templates/t.md", Index: "x"}}}},
			"only for notes",
		},
		{
			"bad gitignore id",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Gitignore: []SectionContribution{{ID: "Bad ID", Path: "p"}}}},
			"invalid id",
		},
		{
			"gitignore missing path",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Gitignore: []SectionContribution{{ID: "ok", Path: ""}}}},
			"gitignore[0]: path is required",
		},
		{
			"section missing path",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{ClaudeMDSections: []SectionContribution{{ID: "ok", Path: ""}}}},
			"path is required",
		},
		{
			"note missing dest",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Notes: []CopyContribution{{Src: "s", Dest: ""}}}},
			"notes[0]",
		},
		{
			"hook missing command",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Hooks: []HookContribution{{Event: "e", Wrapper: "w", Command: ""}}}},
			"hooks[0]",
		},
		{"command empty", cmdManifest(2, "pr merge", "", "x"), "commands[1]: command is required"},
		{"command word with caps", cmdManifest(2, "pr Merge"), `commands[0]: invalid command "pr Merge": word "Merge" must match`},
		{"command word with slash", cmdManifest(2, "pr", "pr ../x"), `commands[1]: invalid command "pr ../x": word "../x" must match`},
		{"command double space", cmdManifest(2, "pr  merge"), `commands[0]: invalid command "pr  merge": words must be separated by single spaces`},
		{"command leading space", cmdManifest(2, " pr"), `commands[0]: invalid command " pr": no leading or trailing space`},
		{"command trailing space", cmdManifest(2, "pr "), `commands[0]: invalid command "pr ": no leading or trailing space`},
		{"command tab", cmdManifest(2, "pr\tmerge"), `commands[0]: invalid command "pr\tmerge": word "pr\tmerge" must match`},
		{"command word starting with -", cmdManifest(2, "pr --all"), `commands[0]: invalid command "pr --all": word "--all" must match ^[a-z0-9][a-z0-9-]*$`},
		{"command namespace starting with -", cmdManifest(2, "-x"), `commands[0]: invalid command "-x": word "-x" must match`},
		{
			"command bad binary",
			Manifest{Manifest: 2, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Commands: []CommandContribution{{Command: "ns", Binary: "ok"}, {Command: "ns x", Binary: "../Bad"}}}},
			`commands[1]: invalid binary "../Bad": must match`,
		},
		{
			"command empty binary",
			Manifest{Manifest: 2, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Commands: []CommandContribution{{Command: "ns", Binary: ""}}}},
			`commands[0]: invalid binary ""`,
		},
		{"command declared twice", cmdManifest(2, "pr merge", "pr watch", "pr merge"), `commands[2]: command "pr merge" is already declared by commands[0]`},
		{"commands in a v1 manifest", cmdManifest(1, "pr"), `commands need "manifest": 2`},
		{
			"two launchers",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Launcher: []LauncherContribution{{Binary: "cg-a"}, {Binary: "cg-b"}}}},
			"at most one",
		},
		{
			"launcher bad binary",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Launcher: []LauncherContribution{{Binary: "../x"}}}},
			"launcher[0]: invalid binary",
		},
		{
			"launcher empty arg",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Launcher: []LauncherContribution{{Binary: "cg-a", Args: []string{"launch", ""}}}}},
			"args[1] is empty",
		},
		{
			"template missing src",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Templates: []CopyContribution{{Src: "", Dest: "d"}}}},
			"templates[0]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.m.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Name != "1password" {
		t.Errorf("name = %q", m.Name)
	}

	if _, err := LoadManifest(t.TempDir()); err == nil {
		t.Error("expected error loading manifest from dir without one")
	}
}

func TestValidateGitHooks(t *testing.T) {
	base := func(g GitHookContribution) *Manifest {
		return &Manifest{Manifest: 1, Name: "demo", Version: "1.0.0", Description: "d",
			Contributes: Contributions{GitHooks: []GitHookContribution{g}}}
	}
	if err := base(GitHookContribution{Hook: "post-commit", Script: "s.sh"}).Validate(); err != nil {
		t.Errorf("valid git hook rejected: %v", err)
	}
	if err := base(GitHookContribution{Hook: "post-receive", Script: "s.sh"}).Validate(); err == nil {
		t.Error("server-side hook accepted")
	}
	if err := base(GitHookContribution{Hook: "post-commit"}).Validate(); err == nil {
		t.Error("missing script accepted")
	}
	for _, escape := range []string{"../outside.sh", "/etc/passwd", "a/../../b.sh"} {
		if err := base(GitHookContribution{Hook: "post-commit", Script: escape}).Validate(); err == nil {
			t.Errorf("script %q outside the extension accepted", escape)
		}
	}
	dup := base(GitHookContribution{Hook: "post-commit", Script: "a/check.sh"})
	dup.Contributes.GitHooks = append(dup.Contributes.GitHooks, GitHookContribution{Hook: "post-commit", Script: "b/check.sh"})
	if err := dup.Validate(); err == nil {
		t.Error("two scripts with the same install path accepted")
	}
	same := base(GitHookContribution{Hook: "post-commit", Script: "check.sh"})
	same.Contributes.GitHooks = append(same.Contributes.GitHooks, GitHookContribution{Hook: "post-merge", Script: "check.sh"})
	if err := same.Validate(); err != nil {
		t.Errorf("one script for two hooks rejected: %v", err)
	}
}

func TestValidateRejectsEscapingPaths(t *testing.T) {
	base := func(c Contributions) *Manifest {
		return &Manifest{Manifest: 1, Name: "demo", Version: "1.0.0", Description: "d", Contributes: c}
	}
	ok := Contributions{
		ClaudeMDSections: []SectionContribution{{ID: "rules", Path: "fragments/rules.md"}},
		Notes:            []CopyContribution{{Src: "notes/a.md", Dest: "notes/a.md"}},
		Templates:        []CopyContribution{{Src: "t/a.md", Dest: "templates/a.md"}},
		Hooks:            []HookContribution{{Event: "SessionStart", Wrapper: "hooks/gate.sh", Command: "demo gate"}},
	}
	if err := base(ok).Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	for _, escape := range []string{"../outside.md", "/etc/passwd", "a/../../b.md"} {
		cases := map[string]Contributions{
			"section path":   {ClaudeMDSections: []SectionContribution{{ID: "rules", Path: escape}}},
			"note src":       {Notes: []CopyContribution{{Src: escape, Dest: "notes/a.md"}}},
			"note dest":      {Notes: []CopyContribution{{Src: "notes/a.md", Dest: escape}}},
			"template src":   {Templates: []CopyContribution{{Src: escape, Dest: "templates/a.md"}}},
			"template dest":  {Templates: []CopyContribution{{Src: "t/a.md", Dest: escape}}},
			"gitignore path": {Gitignore: []SectionContribution{{ID: "ignore", Path: escape}}},
			"hook wrapper":   {Hooks: []HookContribution{{Event: "SessionStart", Wrapper: escape, Command: "demo gate"}}},
		}
		for name, c := range cases {
			if err := base(c).Validate(); err == nil {
				t.Errorf("%s %q accepted", name, escape)
			}
		}
	}
}

func TestParseManifestLauncher(t *testing.T) {
	m, err := ParseManifest([]byte(`{"manifest":1,"name":"tmux","version":"0.1.0","description":"d",
		"contributes":{"launcher":[{"binary":"cg-tmux","args":["launch"]}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	want := []LauncherContribution{{Binary: "cg-tmux", Args: []string{"launch"}}}
	if !reflect.DeepEqual(m.Contributes.Launcher, want) {
		t.Errorf("launcher = %+v, want %+v", m.Contributes.Launcher, want)
	}
	led, err := Apply(t.TempDir(), t.TempDir(), m)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(led.Launcher, want) {
		t.Errorf("ledger launcher = %+v, want %+v", led.Launcher, want)
	}
}

// hookManifest is a minimal manifest of schema version v with the given hook
// entries.
func hookManifest(v int, hooks ...HookContribution) Manifest {
	return Manifest{Manifest: v, Name: "a", Version: "1.0.0", Description: "d",
		Contributes: Contributions{Hooks: hooks}}
}

// H1, H4, H5, Q4: filters in v2, any matcher on any event, one wrapper in
// several entries; an empty matcher needs no v2.
func TestValidateHookFilters(t *testing.T) {
	for name, m := range map[string]Manifest{
		"v2 matcher, if, both, neither": hookManifest(2,
			HookContribution{Event: "PreToolUse", Matcher: "Bash", Wrapper: "hooks/a.sh", Command: "c"},
			HookContribution{Event: "PostToolUse", If: "Bash(gh *)", Wrapper: "hooks/b.sh", Command: "c"},
			HookContribution{Event: "PreToolUse", Matcher: "Bash", If: "Bash(gh pr *)", Wrapper: "hooks/c.sh", Command: "c"},
			HookContribution{Event: "SessionStart", Wrapper: "hooks/d.sh", Command: "c"}),
		"matcher on any event, unchecked": hookManifest(2,
			HookContribution{Event: "UserPromptSubmit", Matcher: "((not a regex", Wrapper: "hooks/a.sh", Command: "c"},
			HookContribution{Event: "NoSuchEvent", Matcher: "x", Wrapper: "hooks/b.sh", Command: "c"}),
		"if on every if event": hookManifest(2, ifOnEveryEvent()...),
		"one wrapper, several entries": hookManifest(2,
			HookContribution{Event: "PreToolUse", Matcher: "Bash", If: "Bash(gh pr *)", Wrapper: "hooks/a.sh", Command: "c"},
			HookContribution{Event: "PreToolUse", Matcher: "Bash", If: "Bash(gh api *)", Wrapper: "./hooks/a.sh", Command: "c"},
			HookContribution{Event: "PreToolUse", Matcher: "Edit", Wrapper: "hooks/a.sh", Command: "c"},
			HookContribution{Event: "SessionStart", Wrapper: "hooks/a.sh", Command: "c"}),
		"v1 without filters":    hookManifest(1, HookContribution{Event: "SessionStart", Wrapper: "hooks/a.sh", Command: "c"}),
		"v1 with empty matcher": hookManifest(1, HookContribution{Event: "SessionStart", Matcher: "", If: "", Wrapper: "hooks/a.sh", Command: "c"}),
		"v1, one wrapper in two events": hookManifest(1,
			HookContribution{Event: "SessionStart", Wrapper: "hooks/a.sh", Command: "c"},
			HookContribution{Event: "Stop", Wrapper: "hooks/a.sh", Command: "c"}),
	} {
		if err := m.Validate(); err != nil {
			t.Errorf("%s: Validate: %v", name, err)
		}
	}
}

func ifOnEveryEvent() []HookContribution {
	hs := make([]HookContribution, 0, len(IfEvents))
	for i, e := range IfEvents {
		hs = append(hs, HookContribution{Event: e, If: "Bash(x *)", Wrapper: "hooks/" + string(rune('a'+i)) + ".sh", Command: "c"})
	}
	return hs
}

// H1: the filter fields are read from the manifest JSON.
func TestParseHookFilters(t *testing.T) {
	m, err := ParseManifest([]byte(`{"manifest": 2, "name": "a", "version": "1", "description": "d",
	  "contributes": {"hooks": [{"event": "PreToolUse", "matcher": "Bash", "if": "Bash(gh pr *)",
	    "wrapper": "hooks/a.sh", "command": "c", "onFailure": "block", "timeout": 5}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := HookContribution{Event: "PreToolUse", Matcher: "Bash", If: "Bash(gh pr *)", Wrapper: "hooks/a.sh", Command: "c"}
	if len(m.Contributes.Hooks) != 1 || m.Contributes.Hooks[0] != want {
		t.Errorf("hooks = %+v, want [%+v]", m.Contributes.Hooks, want)
	}
	// H8: unknown keys are ignored.
	if err := m.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// H2, H3, H6, H7: the refusals, each naming the entries.
func TestValidateHookFiltersRejects(t *testing.T) {
	cases := []struct {
		name string
		m    Manifest
		want string
	}{
		{"H2 matcher in v1", hookManifest(1,
			HookContribution{Event: "SessionStart", Wrapper: "hooks/a.sh", Command: "c"},
			HookContribution{Event: "PreToolUse", Matcher: "Bash", Wrapper: "hooks/b.sh", Command: "c"}),
			`hooks[1]: matcher and if need "manifest": 2 (got 1)`},
		{"H2 if in v1", hookManifest(1,
			HookContribution{Event: "PreToolUse", If: "Bash(x)", Wrapper: "hooks/a.sh", Command: "c"}),
			`hooks[0]: matcher and if need "manifest": 2 (got 1)`},
		{"H3 if on a non-if event", hookManifest(2,
			HookContribution{Event: "SessionStart", If: "Bash(x)", Wrapper: "hooks/a.sh", Command: "c"}),
			`hooks[0]: if is not evaluated on event "SessionStart" (it is on PreToolUse, PostToolUse, PostToolUseFailure, PermissionRequest, PermissionDenied, PreSkillShellCommand)`},
		{"H3 case matters", hookManifest(2,
			HookContribution{Event: "pretooluse", If: "Bash(x)", Wrapper: "hooks/a.sh", Command: "c"}),
			`hooks[0]: if is not evaluated on event "pretooluse"`},
		{"H6 exact duplicate", hookManifest(2,
			HookContribution{Event: "PreToolUse", Matcher: "Bash", If: "Bash(x)", Wrapper: "hooks/a.sh", Command: "c"},
			HookContribution{Event: "Stop", Wrapper: "hooks/a.sh", Command: "c"},
			HookContribution{Event: "PreToolUse", Matcher: "Bash", If: "Bash(x)", Wrapper: "./hooks/a.sh", Command: "c"}),
			`hooks[2]: same event, matcher, if and wrapper as hooks[0]`},
		{"H6 duplicate differing only in command", hookManifest(2,
			HookContribution{Event: "PreToolUse", Matcher: "Bash", Wrapper: "hooks/a.sh", Command: "c1"},
			HookContribution{Event: "PreToolUse", Matcher: "Bash", Wrapper: "hooks/a.sh", Command: "c2"}),
			`hooks[1]: same event, matcher, if and wrapper as hooks[0]`},
		{"H6 Q5 star and unset matcher", hookManifest(2,
			HookContribution{Event: "PreToolUse", Wrapper: "hooks/a.sh", Command: "c"},
			HookContribution{Event: "PreToolUse", Matcher: "*", Wrapper: "hooks/a.sh", Command: "c"}),
			`hooks[1]: same event, matcher, if and wrapper as hooks[0]`},
		{"H6 in v1", hookManifest(1,
			HookContribution{Event: "SessionStart", Wrapper: "hooks/a.sh", Command: "c"},
			HookContribution{Event: "SessionStart", Wrapper: "hooks/a.sh", Command: "c"}),
			`hooks[1]: same event, matcher, if and wrapper as hooks[0]`},
		{"H7 same install path", hookManifest(2,
			HookContribution{Event: "PreToolUse", Wrapper: "hooks/a.sh", Command: "c"},
			HookContribution{Event: "Stop", Wrapper: "hooks/b.sh", Command: "c"},
			HookContribution{Event: "PostToolUse", Wrapper: "other/a.sh", Command: "c"}),
			`hooks[2]: wrapper "other/a.sh" installs to .claude/hooks/a-a.sh, as hooks[0] does; give it a different file name`},
		{"H7 in v1", hookManifest(1,
			HookContribution{Event: "SessionStart", Wrapper: "hooks/a.sh", Command: "c"},
			HookContribution{Event: "Stop", Wrapper: "other/a.sh", Command: "c"}),
			`hooks[1]: wrapper "other/a.sh" installs to .claude/hooks/a-a.sh, as hooks[0] does`},
	}
	for _, tc := range cases {
		err := tc.m.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to contain %q", tc.name, err, tc.want)
		}
	}
}

// Q5: matchers other than "*" compare as written.
func TestValidateHookMatchersCompareAsWritten(t *testing.T) {
	m := hookManifest(2,
		HookContribution{Event: "PreToolUse", Matcher: "Bash|Edit", Wrapper: "hooks/a.sh", Command: "c"},
		HookContribution{Event: "PreToolUse", Matcher: "Edit|Bash", Wrapper: "hooks/a.sh", Command: "c"})
	if err := m.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}
