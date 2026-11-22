package extension

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Conflict is one command an extension may not take. Either Other names the
// extension whose command OtherCommand is the same as Command or nests with it
// (one is the other followed by more words), or Builtin names the built-in cg
// command whose name is Command's namespace.
type Conflict struct {
	Extension    string
	Command      string
	Other        string
	OtherCommand string
	Builtin      string
}

// String describes the conflict for an error message.
func (c Conflict) String() string {
	switch {
	case c.Builtin != "":
		return fmt.Sprintf("command %q of extension %q is in the namespace of the built-in command \"cg %s\"", c.Command, c.Extension, c.Builtin)
	case c.Command == c.OtherCommand:
		return fmt.Sprintf("command %q of extension %q is already declared by extension %q", c.Command, c.Extension, c.Other)
	case commandWithin(c.Command, c.OtherCommand):
		return fmt.Sprintf("command %q of extension %q is inside command %q of extension %q", c.Command, c.Extension, c.OtherCommand, c.Other)
	default:
		return fmt.Sprintf("command %q of extension %q contains command %q of extension %q", c.Command, c.Extension, c.OtherCommand, c.Other)
	}
}

// CommandNamespace is a command's first word.
func CommandNamespace(command string) string {
	ns, _, _ := strings.Cut(command, " ")
	return ns
}

// commandWithin reports whether command a is b followed by one or more words.
func commandWithin(a, b string) bool {
	return strings.HasPrefix(a, b+" ")
}

// commandsNest reports whether two commands are the same or one is inside the
// other: a typed command matching both could not say which program runs.
func commandsNest(a, b string) bool {
	return a == b || commandWithin(a, b) || commandWithin(b, a)
}

// CheckCommands returns the conflicts the incoming commands would create, keyed
// by extension name, given the commands of the extensions already installed in
// the workspace and the built-in cg command names. An incoming extension
// replaces its own installed version, so it never conflicts with that. A
// command conflicts when another extension declares the same command, or one
// nested with it in either direction, or when its namespace is a built-in.
// Two incoming extensions are checked against each other too, each pair
// reported once. Conflicts among installed extensions alone are not reported:
// the operation doesn't create them. The result is ordered by incoming
// extension name, then by the order of its commands.
func CheckCommands(installed, incoming map[string][]CommandContribution, builtins []string) []Conflict {
	// The set after the operation: an incoming version replaces the installed
	// one of the same name.
	final := map[string][]CommandContribution{}
	for name, cs := range installed {
		final[name] = cs
	}
	for name, cs := range incoming {
		final[name] = cs
	}
	others := sortedKeys(final)
	var out []Conflict
	for _, name := range sortedKeys(incoming) {
		for _, c := range incoming[name] {
			if ns := CommandNamespace(c.Command); slices.Contains(builtins, ns) {
				out = append(out, Conflict{Extension: name, Command: c.Command, Builtin: ns})
			}
			for _, other := range others {
				if other == name {
					continue
				}
				if _, in := incoming[other]; in && other < name {
					continue // reported from other's side
				}
				for _, d := range final[other] {
					if commandsNest(c.Command, d.Command) {
						out = append(out, Conflict{Extension: name, Command: c.Command, Other: other, OtherCommand: d.Command})
					}
				}
			}
		}
	}
	return out
}

func sortedKeys(m map[string][]CommandContribution) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
