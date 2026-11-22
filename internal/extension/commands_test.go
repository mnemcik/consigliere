package extension

import (
	"reflect"
	"testing"
)

// cmds builds command entries served by one binary.
func cmds(commands ...string) []CommandContribution {
	out := make([]CommandContribution, len(commands))
	for i, c := range commands {
		out[i] = CommandContribution{Command: c, Binary: "cg-x"}
	}
	return out
}

var testBuiltins = []string{"start", "extension", "ext", "help", "completion", "__complete"}

func TestCheckCommands(t *testing.T) {
	cases := []struct {
		name      string
		installed map[string][]CommandContribution
		incoming  map[string][]CommandContribution
		want      []Conflict
	}{
		{
			name:      "S6 same command",
			installed: map[string][]CommandContribution{"a": cmds("shop add")},
			incoming:  map[string][]CommandContribution{"b": cmds("shop add")},
			want:      []Conflict{{Extension: "b", Command: "shop add", Other: "a", OtherCommand: "shop add"}},
		},
		{
			name:      "S7 incoming inside installed",
			installed: map[string][]CommandContribution{"a": cmds("pr")},
			incoming:  map[string][]CommandContribution{"b": cmds("pr merge")},
			want:      []Conflict{{Extension: "b", Command: "pr merge", Other: "a", OtherCommand: "pr"}},
		},
		{
			name:      "S7 installed inside incoming",
			installed: map[string][]CommandContribution{"a": cmds("pr merge")},
			incoming:  map[string][]CommandContribution{"b": cmds("pr")},
			want:      []Conflict{{Extension: "b", Command: "pr", Other: "a", OtherCommand: "pr merge"}},
		},
		{
			name:      "S8 different commands in one namespace",
			installed: map[string][]CommandContribution{"a": cmds("pr merge")},
			incoming:  map[string][]CommandContribution{"b": cmds("pr watch", "pr merged")},
		},
		{
			name:      "word prefix is not a command prefix",
			installed: map[string][]CommandContribution{"a": cmds("pr")},
			incoming:  map[string][]CommandContribution{"b": cmds("prx", "pr-merge")},
		},
		{
			name:     "S9 built-in namespace",
			incoming: map[string][]CommandContribution{"b": cmds("start x", "help", "ext list", "fine")},
			want: []Conflict{
				{Extension: "b", Command: "start x", Builtin: "start"},
				{Extension: "b", Command: "help", Builtin: "help"},
				{Extension: "b", Command: "ext list", Builtin: "ext"},
			},
		},
		{
			name:      "S12 an extension's previous version is not a conflict",
			installed: map[string][]CommandContribution{"a": cmds("shop", "shop add")},
			incoming:  map[string][]CommandContribution{"a": cmds("shop", "shop add", "shop list")},
		},
		{
			name:      "S10 two incoming extensions conflict with each other",
			installed: map[string][]CommandContribution{"c": cmds("other")},
			incoming:  map[string][]CommandContribution{"b": cmds("pr merge"), "a": cmds("pr")},
			want:      []Conflict{{Extension: "a", Command: "pr", Other: "b", OtherCommand: "pr merge"}},
		},
		{
			name: "S10 moving a command between two incoming extensions",
			// a's installed version had "pr merge"; its new version drops it and
			// b's new version takes it.
			installed: map[string][]CommandContribution{"a": cmds("pr merge"), "b": cmds("pr watch")},
			incoming:  map[string][]CommandContribution{"a": cmds("pr list"), "b": cmds("pr watch", "pr merge")},
		},
		{
			name:      "S13 a removed extension's commands are free",
			installed: map[string][]CommandContribution{},
			incoming:  map[string][]CommandContribution{"b": cmds("shop add")},
		},
		{
			name:      "conflicts among installed extensions alone are not reported",
			installed: map[string][]CommandContribution{"a": cmds("x"), "c": cmds("x")},
			incoming:  map[string][]CommandContribution{"b": cmds("y")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckCommands(tc.installed, tc.incoming, testBuiltins)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("CheckCommands:\n got  %+v\n want %+v", got, tc.want)
			}
		})
	}
}

func TestConflictString(t *testing.T) {
	for _, tc := range []struct {
		c    Conflict
		want string
	}{
		{Conflict{Extension: "b", Command: "shop add", Other: "a", OtherCommand: "shop add"},
			`command "shop add" of extension "b" is already declared by extension "a"`},
		{Conflict{Extension: "b", Command: "pr merge", Other: "a", OtherCommand: "pr"},
			`command "pr merge" of extension "b" is inside command "pr" of extension "a"`},
		{Conflict{Extension: "b", Command: "pr", Other: "a", OtherCommand: "pr merge"},
			`command "pr" of extension "b" contains command "pr merge" of extension "a"`},
		{Conflict{Extension: "b", Command: "start x", Builtin: "start"},
			`command "start x" of extension "b" is in the namespace of the built-in command "cg start"`},
	} {
		if got := tc.c.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}
