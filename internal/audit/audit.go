// Package audit implements the read-only workspace scanners promoted from the
// shell helpers: area-tag counting and badge-color duplicate detection. Both
// read item metadata via internal/meta, which accepts YAML frontmatter or a
// `## Meta` block, and are deterministic and network-free.
package audit

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/mnemcik/consigliere/internal/meta"
)

// TagCount is a tag and the area slugs carrying it.
type TagCount struct {
	Tag   string
	Areas []string
}

// Tags scans areas/*.md (excluding INDEX.md) for the `- **Tags:** a, b` Meta
// line, normalizes each tag to lowercase, and returns counts sorted by
// descending frequency then tag name. Ports area-tags.sh.
func Tags(root string) ([]TagCount, error) {
	files, err := filepath.Glob(filepath.Join(root, "areas", "*.md"))
	if err != nil {
		return nil, err
	}
	byTag := map[string][]string{}
	for _, f := range files {
		slug := strings.TrimSuffix(filepath.Base(f), ".md")
		if slug == "INDEX" {
			continue
		}
		fields, err := meta.Read(f)
		if err != nil {
			continue
		}
		for _, raw := range fields.Tags {
			if tag := strings.ToLower(strings.TrimSpace(raw)); tag != "" {
				byTag[tag] = append(byTag[tag], slug)
			}
		}
	}

	out := make([]TagCount, 0, len(byTag))
	for tag, areas := range byTag {
		sort.Strings(areas)
		out = append(out, TagCount{Tag: tag, Areas: areas})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Areas) != len(out[j].Areas) {
			return len(out[i].Areas) > len(out[j].Areas)
		}
		return out[i].Tag < out[j].Tag
	})
	return out, nil
}

// ColorEntry is a single color assignment.
type ColorEntry struct {
	Color string
	File  string // workspace-relative path
}

// DupGroup is a color used by more than one item.
type DupGroup struct {
	Color string
	Files []string
}

// ColorReport summarizes badge-color usage across projects and areas.
type ColorReport struct {
	Assigned   []ColorEntry
	Missing    []string
	Duplicates []DupGroup
}

// ColorsCheck scans projects/*/README.md and areas/*.md (excluding INDEX.md)
// for the `**Color:** <name>` Meta field, reporting assignments, items without
// a color, and any color used by 2+ items. Ports colors-check.sh.
func ColorsCheck(root string) (ColorReport, error) {
	var files []string
	projects, err := filepath.Glob(filepath.Join(root, "projects", "*", "README.md"))
	if err != nil {
		return ColorReport{}, err
	}
	files = append(files, projects...)
	areas, err := filepath.Glob(filepath.Join(root, "areas", "*.md"))
	if err != nil {
		return ColorReport{}, err
	}
	for _, a := range areas {
		if filepath.Base(a) != "INDEX.md" {
			files = append(files, a)
		}
	}
	sort.Strings(files)

	var rep ColorReport
	byColor := map[string][]string{}
	for _, f := range files {
		rel, _ := filepath.Rel(root, f)
		if rel == "" {
			rel = f
		}
		fields, err := meta.Read(f)
		if err != nil {
			continue
		}
		color := fields.Color
		if color == "" {
			rep.Missing = append(rep.Missing, rel)
			continue
		}
		rep.Assigned = append(rep.Assigned, ColorEntry{Color: color, File: rel})
		byColor[color] = append(byColor[color], rel)
	}
	sort.Slice(rep.Assigned, func(i, j int) bool {
		if rep.Assigned[i].Color != rep.Assigned[j].Color {
			return rep.Assigned[i].Color < rep.Assigned[j].Color
		}
		return rep.Assigned[i].File < rep.Assigned[j].File
	})

	colors := make([]string, 0, len(byColor))
	for c := range byColor {
		colors = append(colors, c)
	}
	sort.Strings(colors)
	for _, c := range colors {
		if len(byColor[c]) > 1 {
			rep.Duplicates = append(rep.Duplicates, DupGroup{Color: c, Files: byColor[c]})
		}
	}
	return rep, nil
}
