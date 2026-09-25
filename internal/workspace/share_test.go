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
