package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// skillRun is what a run passes down for an extension's skills.
type skillRun struct {
	ctx  context.Context
	repo string
	mode extension.SkillMode
	// targets are the skill targets the run checked conflicts with; the
	// same list is applied, so a .cg.json changed meanwhile can't slip an
	// unchecked target in.
	targets []string
}

// skillTargetsFor returns the workspace's skill targets (.cg.json
// skills.targets, or the default when the key is absent). A .cg.json that
// can't be read is an error, never the default.
func skillTargetsFor(root string) ([]string, error) {
	cfg, err := workspace.Detect(root)
	if err != nil {
		return nil, fmt.Errorf("reading skills.targets: %w", err)
	}
	return cfg.SkillTargets(), nil
}

// reservedSkillNames are the names of the framework's skills and slash
// commands (frameworkClaudeFiles): a skill of that name installed for Claude
// Code would hide them (S20).
func reservedSkillNames() []string {
	var out []string
	for _, f := range frameworkClaudeFiles {
		if rest, ok := strings.CutPrefix(f.dst, ".claude/skills/"); ok {
			name, _, _ := strings.Cut(rest, "/")
			out = append(out, name)
		} else if rest, ok := strings.CutPrefix(f.dst, ".claude/commands/"); ok {
			out = append(out, strings.TrimSuffix(path.Base(rest), ".md"))
		}
	}
	return out
}

// shipsForClaude reports whether installed extension e already ships skill
// name for the claude target: its manifest at the recorded commit declares the
// skill and its ledger records claude among the skill's targets. An extension
// whose tree can't be read ships nothing.
func shipsForClaude(root string, e *workspace.ExtensionRef, name string) bool {
	_, m, err := verifiedManifest(e)
	if err != nil || !slices.ContainsFunc(m.Contributes.Skills, func(s extension.SkillContribution) bool { return extension.SkillName(s) == name }) {
		return false
	}
	led, err := extension.LoadLedger(root, e.Name)
	if err != nil || led == nil {
		return false
	}
	for _, s := range led.Skills {
		if s.Name == name && slices.Contains(s.Targets, "claude") {
			return true
		}
	}
	return false
}

// extSkills is what one extension contributes to the skill-name check after
// the run: skill name → targets, and its note and template destinations.
type extSkills struct {
	skills map[string][]string
	copies []string
}

func manifestSkills(m *extension.Manifest, targets []string) extSkills {
	out := extSkills{skills: map[string][]string{}}
	for _, s := range m.Contributes.Skills {
		out.skills[extension.SkillName(s)] = targets
	}
	for _, c := range append(append([]extension.CopyContribution(nil), m.Contributes.Notes...), m.Contributes.Templates...) {
		out.copies = append(out.copies, extension.CleanDest(c.Dest))
	}
	return out
}

func ledgerSkills(l *extension.Ledger) extSkills {
	out := extSkills{skills: map[string][]string{}}
	if l == nil {
		return out
	}
	for _, s := range l.Skills {
		out.skills[s.Name] = append(out.skills[s.Name], s.Targets...)
	}
	for _, d := range append(append([]string(nil), l.Notes...), l.Templates...) {
		out.copies = append(out.copies, extension.CleanDest(d))
	}
	return out
}

// skillCheck is one run's input to checkSkills.
type skillCheck struct {
	root      string
	installed []workspace.ExtensionRef
	incoming  map[string]*extension.Manifest // keyed by extension name
	targets   []string
	update    bool // an update run: moves are detected, hints added
	restore   bool // cg init: reserved names only warn (S20c)
}

// checkSkills refuses, before anything is applied, skills that conflict by
// name for a shared target (S19, S21, S22), reserved names (S20), notes or
// templates inside a skill's directory (S24) and skills moving between
// extensions in one update (S37, S38). It returns the warnings to print
// (S20b, S20c).
func checkSkills(c *skillCheck) ([]string, error) {
	if err := extension.CheckSkillTargets(c.targets); err != nil {
		return nil, cgerr.New(cgerr.ExitUsage, "%v", err)
	}
	anySkills := false
	for _, m := range c.incoming {
		anySkills = anySkills || len(m.Contributes.Skills) > 0
	}
	final := map[string]extSkills{}
	old := map[string]extSkills{}
	for i := range c.installed {
		e := &c.installed[i]
		led, err := extension.LoadLedger(c.root, e.Name)
		if err != nil {
			if !anySkills {
				continue
			}
			return nil, unreadableLedgerError(c.root, e.Name, err)
		}
		old[e.Name] = ledgerSkills(led)
		if _, in := c.incoming[e.Name]; !in {
			final[e.Name] = ledgerSkills(led)
		}
	}
	for name, m := range c.incoming {
		final[name] = manifestSkills(m, c.targets)
	}
	if c.update {
		if err := checkSkillMoves(c, old); err != nil {
			return nil, err
		}
	}

	var lines, warnings []string
	reserved := reservedSkillNames()
	names := slices.Sorted(maps.Keys(c.incoming))
	others := slices.Sorted(maps.Keys(final))
	for _, name := range names {
		in := final[name]
		for _, skill := range slices.Sorted(maps.Keys(in.skills)) {
			targets := in.skills[skill]
			if slices.Contains(reserved, skill) && slices.Contains(targets, "claude") {
				ref, installed := findRefByName(c.installed, name)
				switch {
				case c.restore || (installed && shipsForClaude(c.root, ref, skill)):
					warnings = append(warnings, fmt.Sprintf("skill %q of extension %q hides the framework's skill or command %q in Claude Code", skill, name, skill))
				default:
					lines = append(lines, fmt.Sprintf("skill %q of extension %q has the name of the framework's skill or command %q, for target claude", skill, name, skill))
				}
			}
			for _, other := range others {
				if other == name {
					continue
				}
				if _, both := c.incoming[other]; both && other < name {
					continue // reported from other's side
				}
				for _, t := range targets {
					// Another target whose directory is the same one on disk
					// (a linked target directory) is the same place.
					if t2, ok := sameTargetIn(c.root, t, final[other].skills[skill]); ok {
						line := fmt.Sprintf("skill %q of extension %q is already shipped by extension %q, for target %s", skill, name, other, t)
						if t2 != t {
							line += fmt.Sprintf(" (whose directory is the same as target %s's)", t2)
						}
						if _, incoming := c.incoming[other]; c.update && !incoming {
							line += fmt.Sprintf("; if a newer version of %q drops the skill, update it first", other)
						}
						lines = append(lines, line)
					}
				}
			}
		}
	}
	for _, name := range others {
		for _, dest := range final[name].copies {
			for _, other := range others {
				if other == name {
					continue // the extension's own skills: checked when its skills are planned
				}
				if _, in := c.incoming[name]; !in {
					if _, in := c.incoming[other]; !in {
						continue // neither is changing
					}
				}
				for skill, targets := range final[other].skills {
					for _, t := range targets {
						for _, alias := range targetAliases(c.root, t) {
							dir := extension.SkillDir(alias, skill)
							if dest == dir || strings.HasPrefix(dest, dir+"/") {
								lines = append(lines, fmt.Sprintf("%s, a note or template of extension %q, lies inside skill %q of extension %q (%s)", dest, name, skill, other, extension.SkillDir(t, skill)))
							}
						}
					}
				}
			}
		}
	}
	if len(lines) == 0 {
		return warnings, nil
	}
	sort.Strings(lines)
	lines = slices.Compact(lines)
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return nil, cgerr.New(cgerr.ExitUsage, "extension skills conflict, so no extension was applied:\n%s", strings.Join(lines, "\n"))
}

// sameTargetIn returns the first target in ts that is t, or whose skills
// directory is, on disk, t's (one target directory linking to the other).
func sameTargetIn(root, t string, ts []string) (string, bool) {
	for _, t2 := range ts {
		if t2 == t || sameTargetDir(root, t, t2) {
			return t2, true
		}
	}
	return "", false
}

// targetAliases is t and every other known target whose skills directory is
// t's on disk.
func targetAliases(root, t string) []string {
	out := []string{t}
	for _, t2 := range extension.SkillTargetNames() {
		if t2 != t && sameTargetDir(root, t, t2) {
			out = append(out, t2)
		}
	}
	return out
}

// sameTargetDir reports whether the skills directories of targets a and b
// are the same directory on disk.
func sameTargetDir(root, a, b string) bool {
	da, ok1 := extension.SkillTargetDir(a)
	db, ok2 := extension.SkillTargetDir(b)
	if !ok1 || !ok2 {
		return false
	}
	ia, err1 := os.Stat(filepath.Join(root, filepath.FromSlash(da)))
	ib, err2 := os.Stat(filepath.Join(root, filepath.FromSlash(db)))
	return err1 == nil && err2 == nil && os.SameFile(ia, ib)
}

func findRefByName(exts []workspace.ExtensionRef, name string) (*workspace.ExtensionRef, bool) {
	for i := range exts {
		if exts[i].Name == name {
			return &exts[i], true
		}
	}
	return nil, false
}

// checkSkillMoves refuses an update in which a skill moves between two of the
// extensions being updated: one's new version adds a skill another's new
// version drops (S37). It prints the order that works, droppers first, or,
// for a cycle, the ways out (S38).
func checkSkillMoves(c *skillCheck, old map[string]extSkills) error {
	type move struct{ skill, from, to string }
	var moves []move
	// Per target: an extension gains a skill for a target its old ledger
	// doesn't record it for, and another drops it there (its new version no
	// longer ships the skill).
	seen := map[move]bool{}
	for to, m := range c.incoming {
		for _, s := range m.Contributes.Skills {
			skill := extension.SkillName(s)
			for _, t := range c.targets {
				if _, had := sameTargetIn(c.root, t, old[to].skills[skill]); had {
					continue
				}
				for from, fm := range c.incoming {
					if from == to {
						continue
					}
					mv := move{skill: skill, from: from, to: to}
					if seen[mv] {
						continue
					}
					// The other extension shipped the skill there, and its new
					// version doesn't.
					_, had := sameTargetIn(c.root, t, old[from].skills[skill])
					drops := had && !slices.ContainsFunc(fm.Contributes.Skills, func(x extension.SkillContribution) bool { return extension.SkillName(x) == skill })
					// Or it shipped files inside the skill's directory as notes
					// or templates, which its new version drops: the gainer
					// would adopt them and the dropper's cleanup delete them.
					if !drops {
						newCopies := manifestSkills(fm, c.targets).copies
						for _, alias := range targetAliases(c.root, t) {
							dir := extension.SkillDir(alias, skill)
							for _, d := range old[from].copies {
								if (d == dir || strings.HasPrefix(d, dir+"/")) && !slices.Contains(newCopies, d) {
									drops = true
								}
							}
						}
					}
					if drops {
						seen[mv] = true
						moves = append(moves, mv)
					}
				}
			}
		}
	}
	if len(moves) == 0 {
		return nil
	}
	sort.Slice(moves, func(i, j int) bool {
		if moves[i].skill != moves[j].skill {
			return moves[i].skill < moves[j].skill
		}
		return moves[i].from < moves[j].from
	})
	var desc []string
	nodes := map[string]bool{}
	after := map[string][]string{} // dropper → gainers, which must update after it
	indeg := map[string]int{}
	for _, mv := range moves {
		desc = append(desc, fmt.Sprintf("  skill %q moves from extension %q to extension %q", mv.skill, mv.from, mv.to))
		nodes[mv.from], nodes[mv.to] = true, true
		if !slices.Contains(after[mv.from], mv.to) {
			after[mv.from] = append(after[mv.from], mv.to)
			indeg[mv.to]++
		}
	}
	// Kahn's algorithm, alphabetical among ready extensions.
	var order []string
	var ready []string
	for n := range nodes {
		if indeg[n] == 0 {
			ready = append(ready, n)
		}
	}
	for len(ready) > 0 {
		sort.Strings(ready)
		n := ready[0]
		ready = ready[1:]
		order = append(order, n)
		for _, g := range after[n] {
			indeg[g]--
			if indeg[g] == 0 {
				ready = append(ready, g)
			}
		}
	}
	if len(order) < len(nodes) {
		// An extension left over is in a cycle when it can reach itself;
		// the others only wait on one.
		reaches := func(from, to string) bool {
			seen := map[string]bool{}
			stack := append([]string(nil), after[from]...)
			for len(stack) > 0 {
				n := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if n == to {
					return true
				}
				if !seen[n] {
					seen[n] = true
					stack = append(stack, after[n]...)
				}
			}
			return false
		}
		var cycle, waiting []string
		for n := range nodes {
			switch {
			case slices.Contains(order, n):
			case reaches(n, n):
				cycle = append(cycle, n)
			default:
				waiting = append(waiting, n)
			}
		}
		sort.Strings(cycle)
		sort.Strings(waiting)
		// Separate cycles: members that reach each other.
		var cycles []string
		grouped := map[string]bool{}
		for _, n := range cycle {
			if grouped[n] {
				continue
			}
			group := []string{n}
			grouped[n] = true
			for _, m := range cycle {
				if !grouped[m] && reaches(n, m) && reaches(m, n) {
					group = append(group, m)
					grouped[m] = true
				}
			}
			cycles = append(cycles, "  "+strings.Join(group, ", "))
		}
		msg := fmt.Sprintf("extension skills move between extensions in a cycle, so no extension was applied:\n%s\n"+
			"No order of updates can apply it. Each group below holds one or more cycles; every one of them must be broken:\n%s\n"+
			"In each group, wait for a release of one of its extensions that no longer takes part in a cycle, or remove one of them with "+
			"`cg extension remove` (which also drops its notes, templates and pending state from cg's records), then run "+
			"`cg extension update` again; repeat until no cycle is left, and install the removed extensions again after the others are updated.",
			strings.Join(desc, "\n"), strings.Join(cycles, "\n"))
		if len(waiting) > 0 {
			msg += fmt.Sprintf(" Extensions %s only wait on the cycle; removing them doesn't help.", strings.Join(waiting, ", "))
		}
		return cgerr.New(cgerr.ExitUsage, "%s", msg)
	}
	var cmds []string
	for _, n := range order {
		cmds = append(cmds, "  cg extension update "+n)
	}
	cmds = append(cmds, "  cg extension update")
	return cgerr.New(cgerr.ExitUsage, "extension skills move between extensions, which takes one update per extension, so no extension was applied:\n%s\n"+
		"Run, in this order:\n%s", strings.Join(desc, "\n"), strings.Join(cmds, "\n"))
}

// preflightSkills runs every extension's own skill checks (reading the
// skills, targets, paths, unowned files, uncommitted edits) before the first
// is applied. Uncommitted edits are collected over all of them (S31); any
// other refusal stops at the first.
func preflightSkills(root string, items []preflightItem) error {
	stop := &extension.SkillEditError{}
	for _, it := range items {
		opts, err := applyOptionsFor(root, it.m, it.prev, it.prevCertain, it.force, it.run, nil)
		if err != nil {
			return err
		}
		err = extension.CheckSkills(root, it.manifestDir, it.m, &opts)
		var e *extension.SkillEditError
		if errors.As(err, &e) {
			stop.Paths = append(stop.Paths, e.Paths...)
			stop.Ignored = append(stop.Ignored, e.Ignored...)
			continue
		}
		if err != nil {
			return cgerr.New(cgerr.ExitUsage, "extension %q: %v", it.m.Name, err)
		}
	}
	if len(stop.Paths) > 0 {
		return cgerr.New(cgerr.ExitUsage, "%v", stop)
	}
	return nil
}

// preflightItem is one extension a run is about to apply.
type preflightItem struct {
	m           *extension.Manifest
	manifestDir string
	prev        string
	prevCertain bool
	force       bool
	run         skillRun
}

// printSkillReport writes what a run says about skills.
func printSkillReport(out, errw io.Writer, r *extension.SkillReport) {
	if r == nil {
		return
	}
	for _, n := range r.Notices {
		_, _ = fmt.Fprintf(out, "  %s\n", n)
	}
	for _, w := range r.Warnings {
		_, _ = fmt.Fprintf(errw, "warning: %s\n", w)
	}
}

// listedSkill is a skill an installed extension's ledger records, for
// `cg extension list`.
type listedSkill struct {
	Name    string   `json:"name"`
	Targets []string `json:"targets"`
	// HidesFramework names the framework skill or command this skill hides
	// in Claude Code (S20b).
	HidesFramework string `json:"hidesFramework,omitempty"`
	// UserScope lists user-scope directories of the same name the agent tool
	// may use instead (S26).
	UserScope []string `json:"userScope,omitempty"`
}

func listSkills(led *extension.Ledger) []listedSkill {
	out := make([]listedSkill, 0, len(led.Skills))
	reserved := reservedSkillNames()
	for _, s := range led.Skills {
		ls := listedSkill{Name: s.Name, Targets: append([]string{}, s.Targets...)}
		if slices.Contains(reserved, s.Name) && slices.Contains(s.Targets, "claude") {
			ls.HidesFramework = s.Name
		}
		for _, t := range s.Targets {
			if home := extension.UserSkillDir(t, s.Name); home != "" && pathExists(home) {
				ls.UserScope = append(ls.UserScope, home)
			}
		}
		out = append(out, ls)
	}
	return out
}

func formatSkills(skills []listedSkill) string {
	parts := make([]string, len(skills))
	for i, s := range skills {
		if len(s.Targets) == 0 {
			parts[i] = s.Name + " (not installed: no targets)"
			continue
		}
		parts[i] = s.Name + " (" + strings.Join(s.Targets, ", ") + ")"
		if s.HidesFramework != "" {
			parts[i] += fmt.Sprintf(" (hides the framework's %q in Claude Code)", s.HidesFramework)
		}
		for _, u := range s.UserScope {
			parts[i] += fmt.Sprintf(" (the agent tool may use %s instead)", u)
		}
	}
	return strings.Join(parts, ", ")
}
