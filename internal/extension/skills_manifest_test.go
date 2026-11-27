package extension

import (
	"strings"
	"testing"
)

func skillManifest(skills ...SkillContribution) *Manifest {
	return &Manifest{Manifest: 2, Name: "demo", Version: "1.0.0", Description: "d",
		Contributes: Contributions{Skills: skills}}
}

// S1: relative src paths inside the extension validate.
func TestSkillsValidate(t *testing.T) {
	m := skillManifest(SkillContribution{Src: "skills/github-digest", Executables: []string{"bin/run.py"}},
		SkillContribution{Src: "other"})
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// S2–S6: refusals name the entry and the rule.
func TestSkillsValidateRefusals(t *testing.T) {
	cases := []struct {
		name   string
		m      *Manifest
		expect []string
	}{
		{"empty src", skillManifest(SkillContribution{Src: ""}), []string{"skills[0]", "src"}},
		{"absolute src", skillManifest(SkillContribution{Src: "/etc/skill"}), []string{"skills[0]", "inside the extension"}},
		{"climbing src", skillManifest(SkillContribution{Src: "a"}, SkillContribution{Src: "../x"}), []string{"skills[1]", "inside the extension"}},
		{"uppercase name", skillManifest(SkillContribution{Src: "skills/Digest"}), []string{"skills[0]", "Digest", "[a-z0-9-]"}},
		{"leading hyphen", skillManifest(SkillContribution{Src: "skills/-x"}), []string{"skills[0]", "-x"}},
		{"trailing hyphen", skillManifest(SkillContribution{Src: "x-"}), []string{"skills[0]", "x-"}},
		{"double hyphen", skillManifest(SkillContribution{Src: "a--b"}), []string{"skills[0]", "a--b"}},
		{"too long", skillManifest(SkillContribution{Src: strings.Repeat("a", 65)}), []string{"skills[0]", "1-64"}},
		{"duplicate", skillManifest(SkillContribution{Src: "a/s"}, SkillContribution{Src: "b/s"}), []string{"skills[1]", "skills[0]", `"s"`}},
		{"manifest 1", func() *Manifest { m := skillManifest(SkillContribution{Src: "s"}); m.Manifest = 1; return m }(), []string{`"manifest": 2`}},
		{"empty executable", skillManifest(SkillContribution{Src: "s", Executables: []string{""}}), []string{"skills[0]", "executables[0]"}},
		{"absolute executable", skillManifest(SkillContribution{Src: "s", Executables: []string{"/bin/sh"}}), []string{"executables[0]"}},
		{"climbing executable", skillManifest(SkillContribution{Src: "s", Executables: []string{"ok", "../x"}}), []string{"executables[1]"}},
		{"duplicate executable", skillManifest(SkillContribution{Src: "s", Executables: []string{"a.py", "./a.py"}}), []string{"executables[1]", "twice"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.m.Validate()
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, want := range c.expect {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

// S5: a v1 manifest without skills still validates.
func TestSkillsV1WithoutSkills(t *testing.T) {
	m := skillManifest()
	m.Manifest = 1
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidSkillName(t *testing.T) {
	for name, want := range map[string]bool{
		"a": true, "github-digest": true, "a1-b2": true, strings.Repeat("a", 64): true,
		"": false, "A": false, "-a": false, "a-": false, "a--b": false, "a_b": false, strings.Repeat("a", 65): false,
	} {
		if got := ValidSkillName(name); got != want {
			t.Errorf("ValidSkillName(%q) = %v, want %v", name, got, want)
		}
	}
}

const v1Block = "<!-- cg:skill-local:start=v1 -->\n" +
	"If a file named `SKILL.local.md` exists in this skill's directory, read it as well: its rules add to this skill's,\n" +
	"and replace them where the two conflict.\n" +
	"<!-- cg:skill-local:end=v1 -->\n"

// S12c: each outcome of finding the local-file block.
func TestCheckSkillLocalBlock(t *testing.T) {
	text := "If a file named `SKILL.local.md` exists in this skill's directory, read it as well: its rules add to this skill's, and replace them where the two conflict."
	cases := []struct {
		name string
		md   string
		want BlockOutcome
	}{
		{"exact", "# Skill\n\n" + v1Block + "\nbody\n", BlockOK},
		{"indented markers", "  <!-- cg:skill-local:start=v1 -->  \n" + text + "\n\t<!-- cg:skill-local:end=v1 -->\n", BlockOK},
		{"re-wrapped", "<!-- cg:skill-local:start=v1 -->\nIf a file named `SKILL.local.md` exists in\nthis skill's directory,   read it as well:\n\tits rules add to this skill's, and replace them where the two conflict.\n<!-- cg:skill-local:end=v1 -->\n", BlockOK},
		{"crlf", strings.ReplaceAll(v1Block, "\n", "\r\n"), BlockOK},
		{"none", "# Skill\nbody\n", BlockMissing},
		{"text without markers", text + "\n", BlockMissing},
		{"start without end", "<!-- cg:skill-local:start=v1 -->\n" + text + "\n", BlockMissing},
		{"end of another version", "<!-- cg:skill-local:start=v1 -->\n" + text + "\n<!-- cg:skill-local:end=v2 -->\n", BlockMissing},
		{"unknown version", "<!-- cg:skill-local:start=v9 -->\nwhatever\n<!-- cg:skill-local:end=v9 -->\n", BlockUnknownVersion},
		{"reworded", "<!-- cg:skill-local:start=v1 -->\nRead SKILL.local.md too.\n<!-- cg:skill-local:end=v1 -->\n", BlockReworded},
		{"further pairs ignored", v1Block + "<!-- cg:skill-local:start=v1 -->\nother\n<!-- cg:skill-local:end=v1 -->\n", BlockOK},
		{"first pair decides", "<!-- cg:skill-local:start=v1 -->\nother\n<!-- cg:skill-local:end=v1 -->\n" + v1Block, BlockReworded},
		{"in backtick fence", "```markdown\n" + v1Block + "```\n", BlockMissing},
		{"in tilde fence", "~~~\n" + v1Block + "~~~\n", BlockMissing},
		{"in unclosed fence", "````\n" + v1Block, BlockMissing},
		{"after a closed fence", "```\ncode\n```\n" + v1Block, BlockOK},
		{"shorter fence doesn't close", "````\n```\n" + v1Block + "````\n", BlockMissing},
		{"fence indented four is not a fence", "    ```\n" + v1Block, BlockOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CheckSkillLocalBlock([]byte(c.md)); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// The markdown form authors paste passes the check, for every version.
func TestSkillLocalBlockMarkdownPasses(t *testing.T) {
	for _, b := range SkillLocalBlocks {
		if got := CheckSkillLocalBlock([]byte(SkillLocalBlockMarkdown(b))); got != BlockOK {
			t.Errorf("version %s: %v", b.Version, got)
		}
	}
}
