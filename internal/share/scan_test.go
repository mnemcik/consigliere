package share

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
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
		// Independent-review cases: prefixed names, where \b cannot match.
		"prefixed env name":   {"GITHUB_TOKEN=Zx9Qw2Er7Ty4Ui1Op6As", RuleAssignment},
		"db password":         {"DB_PASSWORD=Summer2024!!", RuleAssignment},
		"secret key":          {"SECRET_KEY=Zx9Qw2Er7Ty4Ui1Op6As", RuleAssignment},
		"auth token yaml":     {"auth_token: Zx9Qw2Er7Ty4Ui1Op6As", RuleAssignment},
		"azure client secret": {"AZURE_CLIENT_SECRET=abc8Q~Zx9Qw2Er7Ty4Ui1Op6", RuleAssignment},
		"aws secret key":      {"aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", RuleAssignment},
		"sas signature":       {"?sv=2022-11-02&sig=Zx9Qw2Er7Ty4Ui1Op6AsDf%3D&se=2026", RuleAssignment},
		"url credential":      {"postgres://admin:Zx9Qw2Er7Ty4@db.internal:5432/app", RuleURLCredential},
		"bearer token":        {"Authorization: Bearer Zx9Qw2Er7Ty4Ui1Op6AsQq", RuleToken},
		"stripe key":          {"sk_live_" + strings.Repeat("Ab3", 8), RuleToken},
		"gitlab token":        {"glpat-" + strings.Repeat("Ab3", 8), RuleToken},
		"google api key":      {"AIza" + strings.Repeat("Ab3", 11) + "xy", RuleToken},
		"npm token":           {"npm_" + strings.Repeat("Ab3", 12), RuleToken},
		"aws sts key id":      {"ASIAABCDEFGHIJKLMNOP", RuleToken},
		"slack app token":     {"xapp-1-A0123-4567-abcdef", RuleToken},
		"pgp private key":     {"-----BEGIN PGP PRIVATE KEY BLOCK-----", RulePrivateKey},
		"lowercase windows":   {`c:\users\jane\notes`, RuleLocalPath},
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
		"pilot handoff line":      "provide staging URL/curl + Visma Connect client_id/secret via API management; schedule follow-up",
		"prose secret":            "the secret: stored in the vault, never in the repo",
		"placeholder value":       "API_KEY=<your-key-here>",
		"env reference":           "token=${GITHUB_TOKEN}",
		"low-entropy value":       "token=aaaaaaaaaaaaaaaa1",
		"tilde path":              "edit ~/.claude/settings.json and ~/source/repo",
		"ellipsis user path":      "paths like /Users/… are private",
		"op syntax mention":       "use op://... references, the op:// syntax",
		"api path param":          "GET /sets/{setId}/parameters/{paramKey}",
		"double-brace tmpl":       "Your request for {{Rejection reason}} was declined",
		"git sha":                 "merged as 30e43b8f2c1d",
		"web route users":         "GET /Users/me returns the profile",
		"web route home":          "redirects to /home/dashboard",
		"resource path":           "secret: projects/idella-prod/secrets/db-2024",
		"url placeholder":         "postgres://user:pass@localhost/app",
		"url env password":        "postgres://app:${DB_PASSWORD}@db/app",
		"bearer placeholder":      "Authorization: Bearer <token>",
		"bearer challenge":        `WWW-Authenticate: Bearer authorization_uri="https://login.example.com/x"`,
		"bearer placeholder word": "Authorization: Bearer YOUR_ACCESS_TOKEN_HERE",
		"bearer prose":            "use Bearer authorization/authentication flows",
		"assignee":                "assignee: jane.doe2@idella.com",
		"monkey":                  "monkey=banana2024xyzQ",
		"authority url":           "authority=https://login.microsoftonline.com/0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0/v2.0",
		"key vault uri":           "keyVaultUri=https://kv-idella-prod-01.vault.azure.net/",
		"idempotency uuid":        "idempotency_key: 0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0",
		"primary key column":      "primary_key_column=customer_id2024x",
		"compass":                 "compass_key=north2south",
		"passport number":         "passport_number=AB1234567",
		"asia word":               "ASIAPACIFICDATACENTER region",
		"password placeholder":    "password=$DB_PASSWORD",
		"windows env ref":         "password=%DB_PASSWORD%",
		"api web route":           "GET /api/Users/me/settings",
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
		"b/x.md": "/Users/a1/x\n",
		"a/x.md": "ok\n{Project Title} /Users/b2/x\n",
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

func TestScanShowsNoSecretValue(t *testing.T) {
	for line, notShown := range map[string]string{
		"DB_PASSWORD=aääääZx9Qw2Er7Ty4":           "aä",
		"postgres://admin:Zx9Qw2Er7Ty4@db/app":    "Zx9",
		"Authorization: Bearer Zx9Qw2Er7Ty4Ui1Op": "Zx9",
	} {
		got := scanOne(line, ScanOptions{})
		if len(got) != 1 {
			t.Fatalf("%q: want one finding, got %+v", line, got)
		}
		if m := got[0].Match; strings.Contains(m, notShown) || !utf8.ValidString(m) || !strings.HasPrefix(m, "***(") {
			t.Errorf("%q: match %q shows part of the value or is not valid UTF-8", line, m)
		}
	}
}

func TestScanTruncatesNonSecretMatches(t *testing.T) {
	got := scanOne(`C:\Users\jane notes token Zx9Qw2Er7Ty4Ui1Op6As`, ScanOptions{})
	if len(got) != 1 || got[0].Match != `C:\Users\jane` {
		t.Fatalf("want only the account path reported, got %+v", got)
	}
	if s := show(strings.Repeat("é", 100), showTruncated); !utf8.ValidString(s) || utf8.RuneCountInString(s) != maxShown+1 {
		t.Errorf("show did not truncate by rune: %q", s)
	}
}

func TestScanReportsOneFindingPerValue(t *testing.T) {
	for _, line := range []string{
		"token=ghp_" + strings.Repeat("a1B2", 9),
		"token: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
	} {
		if got := scanOne(line, ScanOptions{}); len(got) != 1 {
			t.Errorf("%q: want one finding, got %+v", line, got)
		}
	}
}

func TestScanPrivateKeyCannotBeAcknowledged(t *testing.T) {
	line := "-----BEGIN OPENSSH PRIVATE KEY-----"
	f := scanOne(line, ScanOptions{})
	if len(f) != 1 {
		t.Fatalf("setup: %+v", f)
	}
	if got := scanOne(line, ScanOptions{Acknowledged: []Ack{{Rule: f[0].Rule, Hash: f[0].Hash}}}); len(got) != 1 {
		t.Errorf("a private-key finding must survive an ack, got %+v", got)
	}
}

func TestResultWriteRefusesFindings(t *testing.T) {
	res := &Result{Files: map[string]string{"p/README.md": "x\n"}, Findings: []Finding{{Rule: RuleToken}}}
	out := filepath.Join(t.TempDir(), "out")
	if err := res.Write(out); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("want refusal, got %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("output directory was created despite findings")
	}
}

func TestScanDedupesAcrossPatternRules(t *testing.T) {
	for _, line := range []string{
		"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
		"Authorization: Bearer ghp_" + strings.Repeat("a1B2", 9),
		"token: op://Employee/github-token2/credential",
	} {
		if got := scanOne(line, ScanOptions{}); len(got) != 1 {
			t.Errorf("%q: want one finding, got %+v", line, got)
		}
	}
}

func TestScanChecksFileNames(t *testing.T) {
	got := Scan(map[string]string{"p/ACME-notes.md": "nothing here\n"}, ScanOptions{Denylist: []string{"acme"}})
	if len(got) != 1 || got[0].Line != 0 || got[0].Rule != RuleDenylist {
		t.Fatalf("want a line-0 denylist finding for the file name, got %+v", got)
	}
}

func TestCredentialKind(t *testing.T) {
	for name, want := range map[string]int{
		"SMTP_PASS": kindPassword, "PGPASSWORD": kindPassword, "db-passwd": kindPassword,
		"clientSecret": kindSecret, "AZURE_CLIENT_SECRET": kindSecret, "auth_token": kindSecret, "sig": kindSecret,
		"api_key": kindKey, "Ocp-Apim-Subscription-Key": kindKey,
		"compass": kindNone, "assignee": kindNone, "authority": kindNone, "passport_number": kindNone,
		"primary_key_column": kindNone, "keyVaultUri": kindNone, "monkey": kindNone,
	} {
		if got := credentialKind(name); got != want {
			t.Errorf("credentialKind(%q) = %d, want %d", name, got, want)
		}
	}
}
