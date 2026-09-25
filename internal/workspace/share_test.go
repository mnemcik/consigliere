package workspace

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestShareConfigRoundTrips(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		Type:    TypeConsigliere,
		Version: "1.0.0",
		Share: &ShareConfig{
			Owner:    "Ada Owner",
			Denylist: []string{"acme"},
			Audiences: map[string]ShareAudience{
				"prdcfg-team": {
					Repo:        "git@github.com:org/share.git",
					AuthorEmail: "ada@example.com",
					Projects: map[string]ShareProject{
						"pilot": {Include: []string{"log.md"}, Acknowledged: []ShareAck{{Rule: "local-path", Hash: "6c54c54b471a3e54"}}},
					},
				},
			},
		},
	}
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}
	got, err := Detect(dir)
	if err != nil || got == nil {
		t.Fatalf("Detect: %v %v", got, err)
	}
	if !reflect.DeepEqual(got.Share, cfg.Share) {
		t.Errorf("share block did not round-trip:\ngot  %+v\nwant %+v", got.Share, cfg.Share)
	}
	if got.Share.Audiences["prdcfg-team"].BranchOrDefault() != DefaultShareBranch {
		t.Errorf("empty branch must default to %s", DefaultShareBranch)
	}
}

func TestShareConfigOmittedWhenNil(t *testing.T) {
	dir := t.TempDir()
	if err := (&Config{Type: TypeConsigliere, Version: "1.0.0"}).Save(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "share") {
		t.Errorf("a nil share block must not be written:\n%s", data)
	}
}

func TestShareConfigValidate(t *testing.T) {
	ok := ShareProject{}
	cases := map[string]struct {
		cfg     *ShareConfig
		wantErr string
	}{
		"nil is valid":   {nil, ""},
		"valid":          {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Repo: "r", Projects: map[string]ShareProject{"p": ok}}}}, ""},
		"bad name":       {&ShareConfig{Audiences: map[string]ShareAudience{"Team A": {Repo: "r", Projects: map[string]ShareProject{"p": ok}}}}, "names are lowercase"},
		"no repo":        {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Projects: map[string]ShareProject{"p": ok}}}}, "has no repo"},
		"no projects":    {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Repo: "r"}}}, "shares no projects"},
		"bad slug":       {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Repo: "r", Projects: map[string]ShareProject{"../x": ok}}}}, "not a project slug"},
		"incomplete ack": {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Repo: "r", Projects: map[string]ShareProject{"p": {Acknowledged: []ShareAck{{Rule: "token"}}}}}}}, "needs both rule and hash"},
		"blank repo":     {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Repo: "   ", Projects: map[string]ShareProject{"p": ok}}}}, "has no repo"},
		"dash branch":    {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Repo: "r", Branch: "-x", Projects: map[string]ShareProject{"p": ok}}}}, "not a usable branch"},
		"space branch":   {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Repo: "r", Branch: "a b", Projects: map[string]ShareProject{"p": ok}}}}, "not a usable branch"},
		"dotdot branch":  {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Repo: "r", Branch: "a..b", Projects: map[string]ShareProject{"p": ok}}}}, "not a usable branch"},
		"empty include":  {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Repo: "r", Projects: map[string]ShareProject{"p": {Include: []string{" "}}}}}}, "empty entry"},
		"same target": {&ShareConfig{Audiences: map[string]ShareAudience{
			"a": {Repo: "git@github.com:org/share.git", Projects: map[string]ShareProject{"p": ok}},
			"b": {Repo: "git@github.com:Org/Share", Branch: "main", Projects: map[string]ShareProject{"q": ok}},
		}}, "same repo and branch"},
		"option repo":    {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Repo: "--upload-pack=x", Projects: map[string]ShareProject{"p": ok}}}}, "cannot start with '-'"},
		"double slash":   {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Repo: "r", Branch: "a//b", Projects: map[string]ShareProject{"p": ok}}}}, "not a usable branch"},
		"lock branch":    {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Repo: "r", Branch: "a.lock", Projects: map[string]ShareProject{"p": ok}}}}, "not a usable branch"},
		"dot component":  {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Repo: "r", Branch: "a/.b", Projects: map[string]ShareProject{"p": ok}}}}, "not a usable branch"},
		"HEAD branch":    {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Repo: "r", Branch: "HEAD", Projects: map[string]ShareProject{"p": ok}}}}, "not a usable branch"},
		"release branch": {&ShareConfig{Audiences: map[string]ShareAudience{"team": {Repo: "r", Branch: "release/1.0", Projects: map[string]ShareProject{"p": ok}}}}, ""},
		"local paths differ by case": {&ShareConfig{Audiences: map[string]ShareAudience{
			"a": {Repo: "/srv/Share", Projects: map[string]ShareProject{"p": ok}},
			"b": {Repo: "/srv/share", Projects: map[string]ShareProject{"q": ok}},
		}}, ""},
		"same repo, other branch": {&ShareConfig{Audiences: map[string]ShareAudience{
			"a": {Repo: "r", Projects: map[string]ShareProject{"p": ok}},
			"b": {Repo: "r", Branch: "team-b", Projects: map[string]ShareProject{"q": ok}},
		}}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.cfg.Validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
