package extension

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// Skills an extension ships are managed by cg, like hooks: cg writes them,
// updates them and removes them. A workspace customises a skill through
// SKILL.local.md beside its SKILL.md, a file cg never touches. A managed file
// the user edited is replaced on the next update when the edit is committed
// (it stays in git history), and the run stops before anything is written
// when it isn't. The ledger records each managed file's base (the content id
// cg last wrote) in Files, and each skill's files in Skills. See
// specs/gh-300-extension-skills/spec.md.

// SkillTarget is an agent tool cg installs skills for, and the directory,
// relative to the workspace (and, for the user-scope copy the tool may also
// read, to the home directory), that holds its skills.
type SkillTarget struct {
	Name string
	Dir  string // slash-separated
}

// skillTargets is the built-in target table.
var skillTargets = []SkillTarget{
	{Name: "claude", Dir: ".claude/skills"},
	{Name: "agents", Dir: ".agents/skills"},
}

// SkillTargetNames lists the known target names.
func SkillTargetNames() []string {
	out := make([]string, len(skillTargets))
	for i, t := range skillTargets {
		out[i] = t.Name
	}
	return out
}

// SkillTargetDir returns the workspace-relative skills directory of target.
func SkillTargetDir(target string) (string, bool) {
	for _, t := range skillTargets {
		if t.Name == target {
			return t.Dir, true
		}
	}
	return "", false
}

// CheckSkillTargets refuses a target name cg doesn't know (S16).
func CheckSkillTargets(targets []string) error {
	for _, t := range targets {
		if _, ok := SkillTargetDir(t); !ok {
			return fmt.Errorf("%s skills.targets names %q, which cg doesn't know (known targets: %s)",
				workspace.ConfigFile, t, strings.Join(SkillTargetNames(), ", "))
		}
	}
	return nil
}

// SkillDir is the workspace-relative directory of skill name under target.
func SkillDir(target, name string) string {
	dir, _ := SkillTargetDir(target)
	return dir + "/" + name
}

// skillFileName is the file every skill has at its top.
const skillFileName = "SKILL.md"

// SkillLocalFile is the workspace's own file in a skill's directory.
const SkillLocalFile = "SKILL.local.md"

// SkillLocalBlock is one version of the text every skill's SKILL.md carries,
// between <!-- cg:skill-local:start=<Version> --> and the matching end marker,
// telling the agent to read SKILL.local.md.
type SkillLocalBlock struct {
	Version string
	Text    string
}

// SkillLocalBlocks are the versions cg has shipped, newest last. Rewording
// the block adds a version; every shipped one stays accepted.
var SkillLocalBlocks = []SkillLocalBlock{
	{Version: "v1", Text: "If a file named `SKILL.local.md` exists in this skill's directory, read it as well: its rules add to this skill's,\n" +
		"and replace them where the two conflict."},
}

// SkillLocalBlockMarkdown is block b with its markers, as authors paste it.
func SkillLocalBlockMarkdown(b SkillLocalBlock) string {
	return "<!-- cg:skill-local:start=" + b.Version + " -->\n" + b.Text + "\n<!-- cg:skill-local:end=" + b.Version + " -->"
}

// BlockOutcome is what CheckSkillLocalBlock found.
type BlockOutcome int

const (
	BlockOK BlockOutcome = iota
	BlockMissing
	BlockUnknownVersion
	BlockReworded
)

func (o BlockOutcome) String() string {
	switch o {
	case BlockOK:
		return "ok"
	case BlockMissing:
		return "its SKILL.md doesn't carry cg's local-file block"
	case BlockUnknownVersion:
		return "its SKILL.md carries a local-file block of a version this cg doesn't know"
	case BlockReworded:
		return "its SKILL.md rewords cg's local-file block"
	}
	return "unknown outcome"
}

var (
	blockStartRe = regexp.MustCompile(`^<!-- cg:skill-local:start=(\S+) -->$`)
	blockEndRe   = regexp.MustCompile(`^<!-- cg:skill-local:end=(\S+) -->$`)
	spaceRunRe   = regexp.MustCompile(`[ \t\n]+`)
)

// normaliseBlockText drops carriage returns, turns every run of spaces, tabs
// and newlines into one space and trims the ends.
func normaliseBlockText(s string) string {
	return strings.TrimSpace(spaceRunRe.ReplaceAllString(strings.ReplaceAll(s, "\r", ""), " "))
}

// CheckSkillLocalBlock finds the local-file block in a SKILL.md (S12c): the
// first start marker outside fenced code blocks, and the next end marker of
// the same version; the text between them must equal cg's text for that
// version, compared after normaliseBlockText.
func CheckSkillLocalBlock(content []byte) BlockOutcome {
	lines := strings.Split(strings.ReplaceAll(string(content), "\r", ""), "\n")
	var fence fenceState
	for i, line := range lines {
		if fence.step(line) {
			continue
		}
		m := blockStartRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		version := m[1]
		for j := i + 1; j < len(lines); j++ {
			e := blockEndRe.FindStringSubmatch(strings.TrimSpace(lines[j]))
			if len(e) < 2 || e[1] != version {
				continue
			}
			for _, b := range SkillLocalBlocks {
				if b.Version == version {
					if normaliseBlockText(strings.Join(lines[i+1:j], "\n")) == normaliseBlockText(b.Text) {
						return BlockOK
					}
					return BlockReworded
				}
			}
			return BlockUnknownVersion
		}
		return BlockMissing // a start without its end
	}
	return BlockMissing
}

// fenceState tracks CommonMark fenced code blocks line by line: an opening
// fence is three or more backticks or tildes indented at most three spaces; it
// is closed by a fence of the same character at least as long, with nothing
// but spaces after it; an unclosed fence runs to the end of the file.
type fenceState struct {
	char byte
	n    int
}

var fenceRe = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(.*)$")

// step consumes line and reports whether it is inside a fence (or is a fence
// line itself).
func (f *fenceState) step(line string) bool {
	m := fenceRe.FindStringSubmatch(line)
	if f.n == 0 {
		if len(m) != 3 {
			return false
		}
		if m[1][0] == '`' && strings.Contains(m[2], "`") {
			return false // a backtick fence's info string can't hold a backtick
		}
		f.char, f.n = m[1][0], len(m[1])
		return true
	}
	if len(m) == 3 && m[1][0] == f.char && len(m[1]) >= f.n && strings.TrimSpace(m[2]) == "" {
		f.char, f.n = 0, 0
	}
	return true
}

// skillSource is a skill as the extension ships it.
type skillSource struct {
	name  string
	files []skillFile // sorted by rel
}

type skillFile struct {
	rel  string // relative to the skill's directory, slash-separated
	data []byte
	exec bool
}

// readSkill reads the skill sc ships from manifestDir and checks it (S7–S11).
func readSkill(manifestDir string, sc SkillContribution) (*skillSource, error) {
	name := SkillName(sc)
	sub, err := CleanSubdir(sc.Src)
	if err != nil {
		return nil, fmt.Errorf("skill %q: %w", name, err)
	}
	dir := filepath.Join(manifestDir, sub)
	info, err := os.Lstat(dir)
	switch {
	case err != nil && errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("skill %q: src %q is not a directory in the extension", name, sc.Src)
	case err != nil:
		return nil, fmt.Errorf("skill %q: %w", name, err)
	case info.Mode()&fs.ModeSymlink != 0:
		return nil, fmt.Errorf("skill %q: src %q is a symbolic link; skills can't contain symbolic links", name, sc.Src)
	case !info.IsDir():
		return nil, fmt.Errorf("skill %q: src %q is not a directory in the extension", name, sc.Src)
	}
	// A link above src (skills/ itself, say) would copy files from outside
	// the extension.
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("skill %q: %w", name, err)
	}
	if realTop, terr := filepath.EvalSymlinks(manifestDir); terr != nil || !withinDir(realTop, realDir) {
		return nil, fmt.Errorf("skill %q: src %q resolves through a symbolic link outside the extension; skills can't contain symbolic links", name, sc.Src)
	}
	s := &skillSource{name: name}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if p == dir {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			return fmt.Errorf("skill %q: %s is a symbolic link; skills can't contain symbolic links", name, rel)
		case d.IsDir():
			return nil
		case !d.Type().IsRegular():
			return fmt.Errorf("skill %q: %s is not a regular file", name, rel)
		}
		if !strings.Contains(rel, "/") && strings.EqualFold(rel, SkillLocalFile) {
			return fmt.Errorf("skill %q ships %s, a name that belongs to the workspace: rename it", name, rel)
		}
		data, rerr := os.ReadFile(p) //nolint:gosec // a file inside the extension's tree
		if rerr != nil {
			return rerr
		}
		s.files = append(s.files, skillFile{rel: rel, data: data})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(s.files, func(i, j int) bool { return s.files[i].rel < s.files[j].rel })
	var md []byte
	for _, f := range s.files {
		if f.rel == skillFileName {
			md = f.data
		}
	}
	if md == nil {
		return nil, fmt.Errorf("skill %q: no %s at the top of %s", name, skillFileName, sc.Src)
	}
	if err := checkSkillFrontmatter(name, md); err != nil {
		return nil, err
	}
	for _, e := range sc.Executables {
		clean, _ := CleanSubdir(e)
		rel := filepath.ToSlash(clean)
		found := false
		for i := range s.files {
			if s.files[i].rel == rel {
				s.files[i].exec, found = true, true
			}
		}
		if !found {
			return nil, fmt.Errorf("skill %q: executables entry %q names no file in the skill", name, e)
		}
	}
	return s, nil
}

// checkSkillFrontmatter checks the two fields Agent Skills requires (S8).
func checkSkillFrontmatter(name string, md []byte) error {
	text := strings.ReplaceAll(strings.TrimPrefix(string(md), "\ufeff"), "\r\n", "\n")
	first, rest, ok := strings.Cut(text, "\n")
	if !ok || strings.TrimRight(first, " \t") != "---" {
		return fmt.Errorf("skill %q: %s has no YAML frontmatter", name, skillFileName)
	}
	var body string
	for line := range strings.Lines(rest) {
		if strings.TrimRight(line, " \t\n") == "---" {
			ok = false
			break
		}
		body += line
	}
	if ok {
		return fmt.Errorf("skill %q: %s's frontmatter isn't closed with ---", name, skillFileName)
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(body), &fm); err != nil {
		return fmt.Errorf("skill %q: %s's frontmatter isn't valid YAML: %w", name, skillFileName, err)
	}
	got, _ := fm["name"].(string)
	if got != name {
		return fmt.Errorf("skill %q: field name in %s's frontmatter is %q; it must equal the directory name %q", name, skillFileName, got, name)
	}
	desc, isString := fm["description"].(string)
	switch {
	case !isString || strings.TrimSpace(desc) == "":
		return fmt.Errorf("skill %q: field description in %s's frontmatter is missing or empty", name, skillFileName)
	case utf8.RuneCountInString(desc) > 1024:
		return fmt.Errorf("skill %q: field description in %s's frontmatter is longer than 1024 characters", name, skillFileName)
	}
	return nil
}

// SkillMode is how a run treats managed skill files.
type SkillMode int

const (
	// SkillsApply is install and update: an edited, committed file is
	// replaced; an uncommitted one stops the run.
	SkillsApply SkillMode = iota
	// SkillsRestore is `cg init` re-installing at the recorded commit: an
	// edited file is left as it is, with a message (S36).
	SkillsRestore
)

// SkillReport collects what a run tells the user about skills.
type SkillReport struct {
	Notices  []string // per-file outcomes (S30, S33b, S36)
	Warnings []string // S12b, S26
}

func (r *SkillReport) notice(format string, a ...any) {
	if r != nil {
		r.Notices = append(r.Notices, fmt.Sprintf(format, a...))
	}
}

func (r *SkillReport) warn(format string, a ...any) {
	if r != nil {
		r.Warnings = append(r.Warnings, fmt.Sprintf(format, a...))
	}
}

// SkillEditError stops a run before anything is written: managed skill files
// were edited and the change isn't committed (S31).
type SkillEditError struct {
	Paths   []string
	Ignored []string // of them, the ones git ignores
}

func (e *SkillEditError) Error() string {
	var parts []string
	for _, p := range e.Paths {
		if slices.Contains(e.Ignored, p) {
			parts = append(parts, fmt.Sprintf("%s: git ignores it, so it can't keep your change; move the change to %s or discard it", p, localFileFor(p)))
		} else {
			parts = append(parts, fmt.Sprintf("commit or discard your change to %s first", p))
		}
	}
	return "cg manages these skill files and would replace your change: " + strings.Join(parts, "; ") + "; then run the command again"
}

// localFileFor returns the SKILL.local.md beside the skill holding managed
// path p (<target dir>/<name>/…).
func localFileFor(p string) string {
	for _, t := range skillTargets {
		if rest, ok := strings.CutPrefix(p, t.Dir+"/"); ok {
			name, _, _ := strings.Cut(rest, "/")
			return t.Dir + "/" + name + "/" + SkillLocalFile
		}
	}
	return SkillLocalFile
}

// skillOp is what a run does with one managed path.
type skillOp int

const (
	opWrite        skillOp = iota // write the new version (unedited, or new)
	opAdopt                       // disk already holds it; record it
	opMode                        // disk holds it; set the mode
	opReplace                     // edited and committed: write over it, with the S30 message
	opKeep                        // cg init: edited, leave it (S36)
	opDelete                      // dropped and unedited: delete
	opDeleteEdited                // dropped, edited and committed: delete, with the S30 message
	opForget                      // dropped and already absent
	opLeave                       // cg init: dropped but edited, left in place unmanaged
)

type skillPathPlan struct {
	dest    string // workspace-relative, slash
	data    []byte
	mode    os.FileMode
	next    string // content id of data
	op      skillOp
	missing bool // the file was absent on disk
	// renameFrom is the old path of a rename that only changes case, on a
	// file system that ignores case: the file is renamed to dest first.
	renameFrom string
	// clearDir says dest is now a directory, every file in it dropped: it is
	// removed (once emptied) before the file is written.
	clearDir bool
}

// skillPlan is every decision for an extension's skills, made before anything
// is written.
type skillPlan struct {
	paths  []skillPathPlan
	skills []LedgerSkill
	// gone are the skill directories (target, name) the run stops installing
	// into, which it removes when empty; now says where each skill still lives.
	gone []skillDir
	now  map[string]string // skill name → a directory it is still installed in
	// dirs are skill directories in which files were deleted but which stay.
	dirs []string
	// shipped are the note and template destinations of the new version,
	// which a gone skill directory may still hold (S41).
	shipped map[string]bool
}

type skillDir struct {
	dir  string // <target dir>/<name>
	name string
}

// skillTargetsOf returns the run's targets: opts.SkillTargets, or the default
// when nil. Duplicates are dropped.
func skillTargetsOf(opts *ApplyOptions) []string {
	ts := opts.SkillTargets
	if ts == nil {
		ts = []string{workspace.DefaultSkillTarget}
	}
	var out []string
	for _, t := range ts {
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// oldSkillFiles maps every managed path the old ledger records to its skill.
func oldSkillFiles(old *Ledger) map[string]string {
	out := map[string]string{}
	if old == nil {
		return out
	}
	for _, s := range old.Skills {
		for _, f := range s.Files {
			out[f] = s.Name
		}
	}
	return out
}

// CheckSkills makes the skill plan for installing m from manifestDir and
// returns why it can't be applied, or nil, without writing anything. Callers
// applying several extensions run it for all of them first.
func CheckSkills(root, manifestDir string, m *Manifest, opts *ApplyOptions) error {
	o := *opts
	_, err := planSkills(root, manifestDir, m, &o, nil)
	return err
}

// planSkills decides what to do with every managed path of m's skills (and of
// the old ledger's) under the run's targets. report receives warnings; it may
// be nil.
func planSkills(root, manifestDir string, m *Manifest, opts *ApplyOptions, report *SkillReport) (*skillPlan, error) {
	old := opts.Old
	if (m == nil || len(m.Contributes.Skills) == 0) && (old == nil || len(old.Skills) == 0) {
		return &skillPlan{}, nil
	}
	targets := skillTargetsOf(opts)
	if err := CheckSkillTargets(targets); err != nil {
		return nil, err
	}
	if err := validLedgerSkills(old); err != nil {
		return nil, err
	}
	var sources []*skillSource
	shipped := map[string]bool{} // note and template destinations of m
	if m != nil {
		for _, sc := range m.Contributes.Skills {
			s, err := readSkill(manifestDir, sc)
			if err != nil {
				return nil, err
			}
			sources = append(sources, s)
		}
		for _, c := range append(append([]CopyContribution(nil), m.Contributes.Notes...), m.Contributes.Templates...) {
			shipped[CleanDest(c.Dest)] = true
		}
	}
	plan := &skillPlan{now: map[string]string{}, shipped: shipped}
	type want struct {
		data []byte
		mode os.FileMode
	}
	wanted := map[string]want{}
	var order []string
	installed := map[skillDir]bool{}
	for _, s := range sources {
		ls := LedgerSkill{Name: s.name, Targets: append([]string{}, targets...)}
		for _, t := range targets {
			dir := SkillDir(t, s.name)
			installed[skillDir{dir: dir, name: s.name}] = true
			if _, ok := plan.now[s.name]; !ok {
				plan.now[s.name] = dir
			}
			for c := range shipped {
				if c == dir || strings.HasPrefix(c, dir+"/") {
					return nil, fmt.Errorf("%s, which extension %q ships as a note or template, lies inside its skill %q (%s)", c, m.Name, s.name, dir)
				}
			}
			for _, f := range s.files {
				dest := dir + "/" + f.rel
				mode := os.FileMode(0o644)
				if f.exec {
					mode = 0o755
				}
				wanted[dest] = want{data: f.data, mode: mode}
				order = append(order, dest)
				ls.Files = append(ls.Files, dest)
			}
		}
		plan.skills = append(plan.skills, ls)
	}
	oldFiles := oldSkillFiles(old)
	// renamed maps a wanted path to the old path it is the same file as on
	// a case-insensitive file system (a rename that only changes case): the
	// old path isn't dropped, and its base is the wanted path's.
	renamed := map[string]string{}
	var wantedInfo []pathInfo
	if old != nil && len(old.Skills) > 0 {
		for _, w := range order {
			if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(w))); err == nil {
				wantedInfo = append(wantedInfo, pathInfo{path: w, info: fi})
			}
		}
	}
	var dropped []string
	for dest := range oldFiles {
		if _, ok := wanted[dest]; ok || shipped[dest] {
			continue
		}
		if w := sameFileAs(root, dest, wantedInfo); w != "" {
			// The same file on disk: through a case-only rename, or because
			// one target directory links to another. Not dropped; its base
			// carries over.
			renamed[w] = dest
			continue
		}
		dropped = append(dropped, dest)
	}
	sort.Strings(dropped)
	if old != nil {
		for _, s := range old.Skills {
			for _, t := range s.Targets {
				d := skillDir{dir: SkillDir(t, s.Name), name: s.Name}
				if !installed[d] && !slices.ContainsFunc(plan.gone, func(g skillDir) bool { return g == d }) {
					plan.gone = append(plan.gone, d)
				}
			}
		}
	}

	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	for _, dest := range append(append([]string(nil), order...), dropped...) {
		if err := checkInsideWorkspace(root, rootReal, dest); err != nil {
			return nil, err
		}
	}

	base := func(dest string) (id string, tracked bool) {
		if old == nil {
			return "", false
		}
		if from, ok := renamed[dest]; ok {
			dest = from
		}
		_, tracked = oldFiles[dest]
		tracked = tracked || containsStr(old.Notes, dest) || containsStr(old.Templates, dest)
		if old.HasContentIDs() {
			return old.Files[dest], tracked
		}
		return opts.LegacyBases[dest], tracked
	}
	var refused, edited, blocked []string
	// droppedSet holds the dropped paths this run removes from disk; only
	// those clear the way for a wanted path (cg init leaves edited ones).
	droppedSet := make(map[string]bool, len(dropped))
	// Dropped paths first: deleting them can clear the way for a wanted path
	// (a managed directory replaced by a file, or the reverse).
	for _, dest := range dropped {
		disk, err := skillDiskID(filepath.Join(root, filepath.FromSlash(dest)))
		if err != nil {
			return nil, err
		}
		p := skillPathPlan{dest: dest, missing: disk == ""}
		b, _ := base(dest)
		switch {
		case disk == "":
			p.op = opForget
		case disk == b:
			p.op = opDelete
		case opts.SkillMode == SkillsRestore:
			p.op = opLeave
		default:
			edited = append(edited, dest)
			p.op = opDeleteEdited
		}
		if p.op != opLeave {
			droppedSet[dest] = true
		}
		plan.paths = append(plan.paths, p)
	}
	for _, dest := range order {
		w := wanted[dest]
		abs := filepath.Join(root, filepath.FromSlash(dest))
		if why := blockedBy(root, dest, droppedSet); why != "" {
			blocked = append(blocked, why)
			continue
		}
		p := skillPathPlan{dest: dest, data: w.data, mode: w.mode, next: ContentID(w.data)}
		disk := ""
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			p.clearDir = true // a managed directory the new version makes a file
		} else {
			var err error
			if disk, err = skillDiskID(abs); err != nil {
				return nil, err
			}
		}
		p.missing = disk == ""
		if from := renamed[dest]; from != "" && strings.EqualFold(from, dest) {
			p.renameFrom = from // a case-only rename: give the file the new name
		}
		b, tracked := base(dest)
		switch {
		case disk == p.next:
			p.op = opAdopt
			if modeDiffers(abs, w.mode) {
				p.op = opMode
			}
		case !tracked:
			if disk == "" || opts.Force {
				p.op = opWrite
			} else {
				refused = append(refused, dest)
			}
		case (b != "" && disk == b) || (disk == "" && opts.SkillMode == SkillsRestore):
			p.op = opWrite // unedited, or missing at cg init (S36)
		case opts.SkillMode == SkillsRestore:
			p.op = opKeep
		default:
			edited = append(edited, dest)
			p.op = opReplace
		}
		plan.paths = append(plan.paths, p)
	}
	if len(blocked) > 0 {
		return nil, fmt.Errorf("these skill paths are blocked: %s; move what is in the way aside and run the command again", strings.Join(blocked, "; "))
	}
	if len(refused) > 0 {
		return nil, &ConflictError{Paths: refused}
	}
	if len(edited) > 0 {
		ctx := opts.Context
		if ctx == nil {
			ctx = context.Background()
		}
		// git sees a file by its real path: one reached through a symbolic
		// link (a target directory linking elsewhere in the workspace)
		// shows no status under the link's spelling.
		realPath := make(map[string]string, len(edited))
		var query []string
		for _, dest := range edited {
			if r, ok := realRel(root, rootReal, dest); ok {
				realPath[dest] = r
				query = append(query, r)
			}
		}
		states, err := gitx.PathStates(ctx, rootReal, query)
		if err != nil {
			return nil, fmt.Errorf("checking which skill files are committed: %w", err)
		}
		stop := &SkillEditError{}
		for _, dest := range edited {
			state := gitx.PathUncommitted // not resolvable: never counted as committed
			if r, ok := realPath[dest]; ok {
				state = states[r]
			}
			switch state {
			case gitx.PathCommitted:
			case gitx.PathIgnored:
				stop.Paths = append(stop.Paths, dest)
				stop.Ignored = append(stop.Ignored, dest)
			case gitx.PathUncommitted:
				stop.Paths = append(stop.Paths, dest)
			}
		}
		if len(stop.Paths) > 0 {
			return nil, stop
		}
	}
	for _, d := range plan.gone {
		if err := checkInsideWorkspace(root, rootReal, d.dir); err != nil {
			return nil, err
		}
	}
	// A directory the run stops installing into that is, on disk, one it
	// still installs into (a linked target directory) isn't gone.
	plan.gone = slices.DeleteFunc(plan.gone, func(g skillDir) bool {
		gi, err := os.Stat(filepath.Join(root, filepath.FromSlash(g.dir)))
		if err != nil {
			return false
		}
		for d := range installed {
			if di, err := os.Stat(filepath.Join(root, filepath.FromSlash(d.dir))); err == nil && os.SameFile(gi, di) {
				return true
			}
		}
		return false
	})
	for _, d := range dropped {
		dir := skillDirOf(d)
		if !slices.ContainsFunc(plan.gone, func(g skillDir) bool { return g.dir == dir }) && !slices.Contains(plan.dirs, dir) {
			plan.dirs = append(plan.dirs, dir)
		}
	}
	if opts.SkillMode != SkillsRestore {
		warnSkills(root, sources, targets, old, opts, report)
	}
	return plan, nil
}

// sameFileAs returns the path in wanted that names the same file on disk as
// old, or "": a case variant on a file system that ignores case, or the
// same file reached through a target directory that links to another (say
// .claude/skills -> ../.agents/skills). Deleting old would delete wanted.
func sameFileAs(root, old string, wanted []pathInfo) string {
	oi, err := os.Stat(filepath.Join(root, filepath.FromSlash(old)))
	if err != nil {
		return ""
	}
	for _, w := range wanted {
		if w.path != old && os.SameFile(oi, w.info) {
			return w.path
		}
	}
	return ""
}

// pathInfo is a workspace-relative path and what os.Stat returned for it.
type pathInfo struct {
	path string
	info os.FileInfo
}

// realRel returns managed path rel as git sees it: the real path of its
// longest existing part, relative to rootReal, followed by the rest, in
// slash form. It reports false when that real path leaves the workspace or
// can't be resolved.
func realRel(root, rootReal, rel string) (string, bool) {
	parts := strings.Split(rel, "/")
	n := 0
	for n < len(parts) {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(strings.Join(parts[:n+1], "/")))); err != nil {
			break
		}
		n++
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(strings.Join(parts[:n], "/"))))
	if err != nil {
		return "", false
	}
	out, ok := relWithin(rootReal, resolved)
	if !ok {
		return "", false
	}
	if rest := strings.Join(parts[n:], "/"); rest != "" {
		if out != "" {
			out += "/"
		}
		out += rest
	}
	return out, out != ""
}

// validLedgerSkills refuses a ledger whose skills cg can't act on safely: the
// ledger is committed in the workspace, so a skill name, target or file path
// in it is checked before cg writes, deletes or prunes anything from it.
func validLedgerSkills(l *Ledger) error {
	if l == nil {
		return nil
	}
	for _, s := range l.Skills {
		if !ValidSkillName(s.Name) {
			return fmt.Errorf("%s records a skill named %q, which isn't a valid skill name; %s", ledgerRel(l), s.Name, ledgerFix(l))
		}
		for _, t := range s.Targets {
			if _, ok := SkillTargetDir(t); !ok {
				return fmt.Errorf("%s records skill %q for target %q, which this cg doesn't know (known targets: %s); if a newer cg wrote it, upgrade cg, otherwise %s",
					ledgerRel(l), s.Name, t, strings.Join(SkillTargetNames(), ", "), ledgerFix(l))
			}
		}
		for _, f := range s.Files {
			clean, err := CleanSubdir(f)
			ok := err == nil && filepath.ToSlash(clean) == f && !strings.Contains(f, `\`)
			if ok {
				ok = slices.ContainsFunc(s.Targets, func(t string) bool {
					dir := SkillDir(t, s.Name) + "/"
					// The workspace's own file is never cg's to delete.
					return strings.HasPrefix(f, dir) && !strings.EqualFold(strings.TrimPrefix(f, dir), SkillLocalFile)
				})
			}
			if !ok {
				return fmt.Errorf("%s records %q for skill %q, which isn't a path inside that skill's directory; %s", ledgerRel(l), f, s.Name, ledgerFix(l))
			}
		}
	}
	return nil
}

// ledgerRel is l's path relative to the workspace, for messages.
func ledgerRel(l *Ledger) string { return ledgerSubdir + "/" + l.Name + ".json" }

// ledgerFix says how to get past a ledger entry cg refuses.
func ledgerFix(l *Ledger) string {
	return "restore " + ledgerRel(l) + " from git, or correct or delete that skill entry in it, then run the command again"
}

// CleanDest is a manifest or ledger destination as the writer resolves it:
// slash-separated and cleaned ("./notes/a.md" is "notes/a.md").
func CleanDest(dest string) string {
	return path.Clean(strings.ReplaceAll(dest, `\`, "/"))
}

// skillDirOf returns the <target dir>/<name> part of managed path p.
func skillDirOf(p string) string {
	for _, t := range skillTargets {
		if rest, ok := strings.CutPrefix(p, t.Dir+"/"); ok {
			name, _, _ := strings.Cut(rest, "/")
			return t.Dir + "/" + name
		}
	}
	return path.Dir(p)
}

// warnSkills adds the install-time warnings: a skill without a supported
// local-file block (S12b), and a skill the agent tool may take from the
// user-scope directory instead (S26).
func warnSkills(root string, sources []*skillSource, targets []string, old *Ledger, opts *ApplyOptions, report *SkillReport) {
	for _, s := range sources {
		var md []byte
		for _, f := range s.files {
			if f.rel == skillFileName {
				md = f.data
			}
		}
		next := ContentID(md)
		var bases []string
		if old != nil {
			for _, ls := range old.Skills {
				if ls.Name != s.name {
					continue
				}
				for _, t := range ls.Targets {
					if id := old.Files[SkillDir(t, s.name)+"/"+skillFileName]; id != "" {
						bases = append(bases, id)
					}
				}
			}
		}
		if len(bases) == 0 || !slices.Contains(bases, next) {
			if out := CheckSkillLocalBlock(md); out != BlockOK {
				repo := opts.Repo
				if repo == "" {
					repo = "its repository"
				}
				report.warn("skill %q of extension %q (%s): %s, so this workspace has no supported way to customise it; report it to the extension's maintainer",
					s.name, opts.ExtensionName, repo, out)
			}
		}
		for _, t := range targets {
			if home := UserSkillDir(t, s.name); home != "" && dirExists(home) && !samePathFold(home, filepath.Join(root, filepath.FromSlash(SkillDir(t, s.name)))) {
				report.warn("skill %q: %s also exists, and the agent tool may use that one instead of %s",
					s.name, home, SkillDir(t, s.name))
			}
		}
	}
}

// UserSkillDir is the user-scope directory of skill name for target, in the
// home directory, or "" when there is no home directory.
func UserSkillDir(target, name string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, filepath.FromSlash(SkillDir(target, name)))
}

// modeDiffers reports whether the file at path lacks mode. Windows has no
// executable bit, so it never differs there.
func modeDiffers(file string, mode os.FileMode) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	info, err := os.Stat(file)
	return err == nil && info.Mode().Perm() != mode
}

// checkInsideWorkspace refuses a managed path whose existing part resolves,
// through a symbolic link, outside the workspace (S16b). Existence is decided
// with Lstat, so a dangling link is refused rather than skipped; the part
// below the longest existing prefix is created by cg.
func checkInsideWorkspace(root, rootReal, rel string) error {
	cur := root
	existing := root
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." || strings.Contains(part, `\`) {
			return fmt.Errorf("%s is not a plain path inside the workspace", rel)
		}
		next := filepath.Join(cur, part)
		info, err := os.Lstat(next)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				break
			}
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			if _, err := os.Stat(next); err != nil {
				return fmt.Errorf("%s is a symbolic link that points nowhere; cg won't write skill files through it", relTo(root, next))
			}
		}
		cur, existing = next, next
		if fi, err := os.Stat(next); err == nil && !fi.IsDir() {
			break // a file on the way: blockedBy decides whether it goes first
		}
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return err
	}
	if !withinDir(rootReal, resolved) {
		return fmt.Errorf("%s resolves through a symbolic link to %s, outside the workspace; cg won't write or delete skill files there", relTo(root, existing), resolved)
	}
	return nil
}

func relTo(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}

// withinDir reports whether p is dir or inside it, by location: p's
// spelling under dir, or else an ancestor of p that is dir on disk (the same
// directory spelled with another case). Case-folded spelling alone never
// counts, so two directories that differ only in case stay apart.
func withinDir(dir, p string) bool {
	_, ok := relWithin(dir, p)
	return ok
}

// relWithin returns p relative to dir (slash-separated, "" for dir itself)
// when p is dir or inside it, decided as withinDir says.
func relWithin(dir, p string) (string, bool) {
	// The exact spelling first, compared byte for byte (filepath.Rel would
	// ignore case on Windows, where a directory can be case-sensitive).
	dir, p = filepath.Clean(dir), filepath.Clean(p)
	if p == dir {
		return "", true
	}
	prefix := dir
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if rest, ok := strings.CutPrefix(p, prefix); ok {
		return filepath.ToSlash(rest), true
	}
	di, err := os.Stat(dir)
	if err != nil {
		return "", false
	}
	var rest []string
	for cand := p; ; {
		if ci, err := os.Stat(cand); err == nil && os.SameFile(di, ci) {
			slices.Reverse(rest)
			return strings.Join(rest, "/"), true
		}
		parent := filepath.Dir(cand)
		if parent == cand {
			return "", false
		}
		rest = append(rest, filepath.Base(cand))
		cand = parent
	}
}

// skillDiskID is fileID for a managed path, treating a path under a file
// (which can't exist) as missing.
func skillDiskID(file string) (string, error) {
	id, err := fileID(file)
	if err != nil && errors.Is(err, syscall.ENOTDIR) {
		return "", nil
	}
	return id, err
}

// blockedBy says why wanted path dest can't be written, or "": an existing
// part of its way that is a file and not a path the run drops, or an
// existing directory at dest holding files the run doesn't drop.
func blockedBy(root, dest string, dropped map[string]bool) string {
	parts := strings.Split(dest, "/")
	for n := 1; n < len(parts); n++ {
		rel := strings.Join(parts[:n], "/")
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			break
		}
		if !info.IsDir() {
			if dropped[rel] {
				break // deleted first
			}
			return fmt.Sprintf("%s needs %s to be a directory, but it is a file", dest, rel)
		}
	}
	abs := filepath.Join(root, filepath.FromSlash(dest))
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		files, err := filesUnder(abs)
		if err != nil {
			return fmt.Sprintf("%s: %v", dest, err)
		}
		for _, f := range files {
			if !dropped[dest+"/"+f] {
				return fmt.Sprintf("%s is a directory holding %s/%s, which the extension doesn't manage", dest, dest, f)
			}
		}
	}
	return ""
}

func samePathFold(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	return withinDir(ra, rb) && withinDir(rb, ra)
}

// skillWriteHook, when set (tests only), runs before each managed file is
// written; an error fails the write.
var skillWriteHook func(dest string) error

// applySkills carries out plan, recording each managed file in l. snap must
// already hold every path in plan (captureSkills).
func applySkills(root string, plan *skillPlan, l *Ledger, snap *snapshot, report *SkillReport) error {
	if plan == nil {
		return nil
	}
	for _, p := range plan.paths {
		if err := applySkillPath(root, &p, l, snap, report); err != nil {
			return err
		}
	}
	l.Skills = append(l.Skills, plan.skills...)
	for _, d := range plan.dirs {
		if err := pruneEmptyDirs(root, d, false); err != nil {
			return err
		}
	}
	for _, d := range plan.gone {
		if err := pruneEmptyDirs(root, d.dir, true); err != nil {
			return err
		}
		left, err := filesUnder(filepath.Join(root, filepath.FromSlash(d.dir)))
		if err != nil {
			return err
		}
		for _, f := range left {
			if plan.shipped[d.dir+"/"+f] {
				continue
			}
			where := ""
			if now, ok := plan.now[d.name]; ok && now != d.dir {
				where = fmt.Sprintf("; the skill now lives in %s/, move it there if you still want it", now)
			}
			report.notice("left %s/%s: it isn't the extension's, so cg didn't delete it%s", d.dir, f, where)
		}
	}
	return nil
}

// applySkillPath carries out one managed path's decision.
func applySkillPath(root string, p *skillPathPlan, l *Ledger, snap *snapshot, report *SkillReport) error {
	abs := filepath.Join(root, filepath.FromSlash(p.dest))
	if p.renameFrom != "" {
		// The same file on disk under another case: give it the new name.
		if err := os.Rename(filepath.Join(root, filepath.FromSlash(p.renameFrom)), abs); err != nil {
			return fmt.Errorf("renaming %s to %s: %w", p.renameFrom, p.dest, err)
		}
		report.notice("renamed %s to %s (the extension changed only its case); git may keep the old spelling, so run `git mv --force \"%s\" \"%s\"` from the workspace root before you commit",
			p.renameFrom, p.dest, p.renameFrom, p.dest)
	}
	write := func() error {
		if p.clearDir {
			if err := pruneEmptyDirs(root, p.dest, true); err != nil {
				return err
			}
		}
		if err := snap.captureDirs(root, abs); err != nil {
			return err
		}
		if skillWriteHook != nil {
			if err := skillWriteHook(p.dest); err != nil {
				return fmt.Errorf("writing %s: %w", p.dest, err)
			}
		}
		if err := writeFileAtomicMode(abs, p.data, p.mode); err != nil {
			return fmt.Errorf("writing %s: %w", p.dest, err)
		}
		l.Files[p.dest] = p.next
		return nil
	}
	remove := func() error {
		if err := removeIfPresent(abs); err != nil {
			return fmt.Errorf("deleting %s: %w", p.dest, err)
		}
		return nil
	}
	switch p.op {
	case opWrite:
		return write()
	case opReplace:
		if err := write(); err != nil {
			return err
		}
		if p.missing {
			report.notice("%s was missing, so cg restored the extension's version; to customise this skill, put your additions in %s", p.dest, localFileFor(p.dest))
		} else {
			report.notice("your change to %s was replaced by the extension's version; it is in git history; to customise this skill, put your additions in %s", p.dest, localFileFor(p.dest))
		}
	case opAdopt:
		l.Files[p.dest] = p.next
	case opMode:
		if err := os.Chmod(abs, p.mode); err != nil {
			return fmt.Errorf("setting the mode of %s: %w", p.dest, err)
		}
		l.Files[p.dest] = p.next
	case opKeep:
		l.Files[p.dest] = p.next // not adopted: the base stays the extension's content (S36)
		report.notice("%s differs from the extension's version; the next `cg extension update` replaces it (or asks you to commit first)", p.dest)
	case opDelete:
		return remove()
	case opDeleteEdited:
		if err := remove(); err != nil {
			return err
		}
		report.notice("your change to %s was deleted with the file, which the extension no longer ships there; it is in git history", p.dest)
	case opForget:
	case opLeave:
		report.notice("%s differs from the extension's version, which no longer ships it there; cg left it and no longer manages it", p.dest)
	}
	return nil
}

// captureSkills snapshots every path plan may write or delete.
func captureSkills(root string, plan *skillPlan, snap *snapshot) error {
	if plan == nil {
		return nil
	}
	for _, p := range plan.paths {
		if p.renameFrom != "" {
			if err := snap.capture(filepath.Join(root, filepath.FromSlash(p.renameFrom))); err != nil {
				return err
			}
		}
		if err := snap.capture(filepath.Join(root, filepath.FromSlash(p.dest))); err != nil {
			return err
		}
	}
	return nil
}

// pruneEmptyDirs removes every empty directory under root/dir, and dir itself
// when self, deepest first. A directory still holding a file stays.
func pruneEmptyDirs(root, dir string, self bool) error {
	top := filepath.Join(root, filepath.FromSlash(dir))
	if !dirExists(top) {
		return nil
	}
	var dirs []string
	err := filepath.WalkDir(top, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (self || p != top) {
			dirs = append(dirs, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		entries, err := os.ReadDir(dirs[i])
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			if err := os.Remove(dirs[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// filesUnder lists the files below dir, relative and slash-separated.
func filesUnder(dir string) ([]string, error) {
	if !dirExists(dir) {
		return nil, nil
	}
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

// RemoveSkills deletes the managed skill files ledger l records, as
// `cg extension remove` does (S35): unedited and edited-but-committed files
// go, an uncommitted edit stops it before anything is deleted, local files
// stay. opts.Old is set to l.
func RemoveSkills(root string, l *Ledger, opts *ApplyOptions, report *SkillReport) error {
	if l == nil || len(l.Skills) == 0 {
		return nil
	}
	o := *opts
	o.Old = l
	plan, err := planSkills(root, "", nil, &o, report)
	if err != nil {
		return err
	}
	snap := newSnapshot()
	if err := captureSkills(root, plan, snap); err != nil {
		return err
	}
	scratch := &Ledger{Files: map[string]string{}}
	if err := applySkills(root, plan, scratch, snap, report); err != nil {
		snap.restore()
		return err
	}
	return nil
}

// CheckSkillPaths runs, without a fetched tree, the skill checks `cg init`
// runs before fetching (S36b): unknown targets, and S16b on each target
// directory and on the skill directories ledger l records.
func CheckSkillPaths(root string, targets []string, l *Ledger) error {
	if err := CheckSkillTargets(targets); err != nil {
		return err
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	for _, t := range targets {
		dir, _ := SkillTargetDir(t)
		if err := checkInsideWorkspace(root, rootReal, dir); err != nil {
			return err
		}
	}
	if err := validLedgerSkills(l); err != nil {
		return err
	}
	if l == nil {
		return nil
	}
	for _, s := range l.Skills {
		for _, t := range s.Targets {
			if err := checkInsideWorkspace(root, rootReal, SkillDir(t, s.Name)); err != nil {
				return err
			}
		}
	}
	return nil
}
