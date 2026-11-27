package extension

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Q12: the skill template carries the newest local-file block, and is a valid
// skill.
func TestSkillTemplateCarriesNewestBlock(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "templates", "extension-skill", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	newest := SkillLocalBlocks[len(SkillLocalBlocks)-1]
	if !strings.Contains(strings.ReplaceAll(string(data), "\r\n", "\n"), SkillLocalBlockMarkdown(newest)) {
		t.Errorf("the template doesn't carry local-file block %s word for word", newest.Version)
	}
	if got := CheckSkillLocalBlock(data); got != BlockOK {
		t.Errorf("block check: %v", got)
	}
	if err := checkSkillFrontmatter("extension-skill", data); err != nil {
		t.Error(err)
	}
}

// The authoring guide quotes the newest block word for word.
func TestExtensionsGuideQuotesNewestBlock(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "EXTENSIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ReplaceAll(string(data), "\r\n", "\n"), SkillLocalBlockMarkdown(SkillLocalBlocks[len(SkillLocalBlocks)-1])) {
		t.Error("EXTENSIONS.md doesn't quote the newest local-file block")
	}
}
