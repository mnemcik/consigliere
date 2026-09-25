package share

import (
	"strings"
	"testing"
)

func scanOne(line string, opts ScanOptions) []Finding {
	return Scan(map[string]string{"p/README.md": line + "\n"}, opts)
}

func TestScanTruePositives(t *testing.T) {
	cases := map[string]struct{ line, rule string }{
		"github token":     {"token ghp_" + strings.Repeat("a1B2", 9), RuleToken},
		"fine-grained pat": {"github_pat_" + strings.Repeat("x", 30), RuleToken},
		"slack bot token":  {"xoxb-1234567890-abcdefghij", RuleToken},
		"aws key id":       {"AKIAABCDEFGHIJKLMNOP", RuleToken},
		"anthropic key":    {"sk-ant-" + strings.Repeat("Ab3", 10), RuleToken},
		"slack webhook":    {"https://hooks.slack.com/services/T000/B000/XXXX", RuleToken},
		"private key":      {"-----BEGIN OPENSSH PRIVATE KEY-----", RulePrivateKey},
		"jwt":              {"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", RuleJWT},
		"assignment":       {`client_secret: "q8Zr2mX9vLp4Tn7Wk3Yd"`, RuleAssignment},
		"env assignment":   {"API_KEY=Zx9Qw2Er7Ty4Ui1Op6As", RuleAssignment},
		"mac user path":    {"see /Users/jane.doe/notes", RuleLocalPath},
		"linux user path":  {"at /home/jane/work", RuleLocalPath},
		"windows path":     {`C:\Users\jane\notes`, RuleLocalPath},
		"vault reference":  {"op://Employee/github-token/credential", RuleVaultRef},
		"placeholder":      {"# Decisions — {Project Title}", RulePlaceholder},
		"owner leftover":   {"| Owner | [You] |", RulePlaceholder},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := scanOne(tc.line, ScanOptions{})
			if len(got) != 1 || got[0].Rule != tc.rule {
				t.Fatalf("want one %s finding, got %+v", tc.rule, got)
			}
		})
	}
}

// Each must pass: they describe or reference credentials without containing
// one, or are conventional paths and syntax that identify nobody.
func TestScanTrueNegatives(t *testing.T) {
	for name, line := range map[string]string{
		"pilot handoff line": "provide staging URL/curl + Visma Connect client_id/secret via API management; schedule follow-up",
		"prose secret":       "the secret: stored in the vault, never in the repo",
		"placeholder value":  "API_KEY=<your-key-here>",
		"env reference":      "token=${GITHUB_TOKEN}",
		"low-entropy value":  "password=aaaaaaaaaaaaaaaa1",
		"tilde path":         "edit ~/.claude/settings.json and ~/source/repo",
		"ellipsis user path": "paths like /Users/… are private",
		"op syntax mention":  "use op://... references, the op:// syntax",
		"api path param":     "GET /sets/{setId}/parameters/{paramKey}",
		"double-brace tmpl":  "Your request for {{Rejection reason}} was declined",
		"git sha":            "merged as 30e43b8f2c1d",
	} {
		t.Run(name, func(t *testing.T) {
			if got := scanOne(line, ScanOptions{}); len(got) != 0 {
				t.Fatalf("want no findings, got %+v", got)
			}
		})
	}
}

func TestScanMasksSecrets(t *testing.T) {
	token := "ghp_" + strings.Repeat("a1B2", 9)
	got := scanOne("token "+token, ScanOptions{})
	if len(got) != 1 || strings.Contains(got[0].Match, token[8:]) || !strings.HasPrefix(got[0].Match, "ghp_") {
		t.Fatalf("secret not masked: %+v", got)
	}
}

func TestScanDenylist(t *testing.T) {
	got := scanOne("Meeting with ACME Corp about the rollout", ScanOptions{Denylist: []string{" acme corp ", ""}})
	if len(got) != 1 || got[0].Rule != RuleDenylist || got[0].Match != "acme corp" {
		t.Fatalf("want one denylist finding, got %+v", got)
	}
}

func TestScanAcknowledgement(t *testing.T) {
	line := "see /Users/jane.doe/notes"
	first := scanOne(line, ScanOptions{})
	if len(first) != 1 {
		t.Fatalf("setup: want one finding, got %+v", first)
	}
	ack := Ack{Rule: first[0].Rule, Hash: first[0].Hash}

	if got := scanOne("  "+line+"  ", ScanOptions{Acknowledged: []Ack{ack}}); len(got) != 0 {
		t.Errorf("acknowledged finding still reported (whitespace must not matter): %+v", got)
	}
	if got := scanOne(line+" edited", ScanOptions{Acknowledged: []Ack{ack}}); len(got) != 1 {
		t.Errorf("editing the line must bring the finding back, got %+v", got)
	}
	other := Ack{Rule: RuleDenylist, Hash: ack.Hash}
	if got := scanOne(line, ScanOptions{Acknowledged: []Ack{other}}); len(got) != 1 {
		t.Errorf("an ack for another rule must not clear this one, got %+v", got)
	}
}

func TestScanOrdersFindings(t *testing.T) {
	got := Scan(map[string]string{
		"b/x.md": "/Users/a1\n",
		"a/x.md": "ok\n{Project Title} /Users/b2\n",
	}, ScanOptions{})
	order := make([]string, 0, len(got))
	for _, f := range got {
		order = append(order, f.File+":"+f.Rule)
	}
	want := "a/x.md:local-path a/x.md:placeholder b/x.md:local-path"
	if strings.Join(order, " ") != want {
		t.Errorf("order = %v, want %s", order, want)
	}
}

func TestExportReportsFindings(t *testing.T) {
	ctx, root := initWorkspace(t)
	write(t, root, "projects/pilot/decisions.md", "# Decisions — {Project Title}\n")
	git(t, ctx, root, "add", ".")
	git(t, ctx, root, "commit", "-m", "placeholder")

	res, err := Export(ctx, pilotOptions(root))
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(res.Findings) != 1 || res.Findings[0].Rule != RulePlaceholder || res.Findings[0].File != "pilot/decisions.md" {
		t.Fatalf("want the placeholder finding, got %+v", res.Findings)
	}
}
