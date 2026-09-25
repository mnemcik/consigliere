package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/share"
)

func init() {
	sharePublishCmd.Flags().Bool("dry-run", false, "prepare and summarise the publish; commit and push nothing")
	sharePublishCmd.Flags().Bool("yes", false, "confirm a republish without prompting (refused for a first publish or one that adds files)")
	shareCmd.AddCommand(sharePublishCmd)
}

var sharePublishCmd = &cobra.Command{
	Use:   "publish [<audience>]",
	Short: "Publish each audience's projects to its share repo",
	Long: `Render, scan and publish every project an audience shares (all audiences, or
the one named) to its share repo, replacing the repo's whole tree.

Nothing is published while any of the audience's projects has open scan
findings or cannot be exported. The push is fast-forward only: if the share
repo has commits cg did not make, or someone published meanwhile, publishing
stops. A share repo that shares history with this workspace is refused.

Every publish shows what changes and asks for confirmation. The first publish
of a project to an audience, one that adds files to a project the audience
already has, or one that replaces content cg did not write, must be confirmed
at an interactive terminal by typing the audience name: review the staged
copy it points to first. --yes is refused for these. Other republishes accept
--yes. --dry-run stops before committing.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSharePublish,
}

// consent asks the owner to confirm a publish. It is a variable so tests can
// stand in for a terminal.
var consent consenter = terminalConsent{}

type consenter interface {
	// Interactive reports whether a person is at the terminal.
	Interactive() bool
	// Ask prints the prompt and returns the answer line. It returns early
	// with an error when ctx is cancelled (Ctrl-C).
	Ask(ctx context.Context, w io.Writer, prompt string) (string, error)
}

type terminalConsent struct{}

func (terminalConsent) Interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) //nolint:gosec // stdin fd fits in int on every supported platform
}

func (terminalConsent) Ask(ctx context.Context, w io.Writer, prompt string) (string, error) {
	_, _ = fmt.Fprint(w, prompt)
	type answer struct {
		line string
		err  error
	}
	ch := make(chan answer, 1)
	go func() {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		ch <- answer{line, err}
	}()
	select {
	case <-ctx.Done():
		_, _ = fmt.Fprintln(w)
		return "", fmt.Errorf("interrupted")
	case a := <-ch:
		if a.err != nil && !errors.Is(a.err, io.EOF) {
			return "", a.err
		}
		return strings.TrimSpace(a.line), nil
	}
}

func runSharePublish(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	// Ctrl-C cancels the context instead of killing the process, so the
	// staged clone is removed on the way out.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")

	env, err := loadShareEnv(cmd)
	if err != nil {
		return err
	}
	names := env.share.AudienceNames()
	if len(args) == 1 {
		if _, ok := env.audience(args[0]); !ok {
			return fmt.Errorf("audience %q is not in the .cg.json share block", args[0])
		}
		names = []string{args[0]}
	}
	if len(names) == 0 {
		return fmt.Errorf("no audiences in the .cg.json share block; nothing to publish (see docs/share.md)")
	}

	w := cmd.OutOrStdout()
	var done, failed []string
	for i, name := range names {
		if i > 0 {
			_, _ = fmt.Fprintln(w)
		}
		if ctx.Err() != nil {
			// Interrupted: stop here rather than run the remaining audiences
			// into a cancelled context, and say exactly what already went out.
			failed = append(failed, names[i:]...)
			return publishSummary(done, failed, "interrupted")
		}
		if err := env.publishAudience(ctx, w, name, dryRun, yes); err != nil {
			_, _ = fmt.Fprintf(w, "%s: %s\n", name, err)
			failed = append(failed, name)
			continue
		}
		done = append(done, name)
	}
	if ctx.Err() != nil {
		return publishSummary(done, failed, "interrupted")
	}
	if len(failed) > 0 {
		return publishSummary(done, failed, "")
	}
	return nil
}

// publishSummary reports which audiences were handled and which were not, so
// a partial run (some audiences already pushed) is never reported as nothing.
func publishSummary(done, failed []string, why string) error {
	msg := "not published: " + strings.Join(failed, ", ")
	if len(done) > 0 {
		msg += "; completed: " + strings.Join(done, ", ")
	}
	if why != "" {
		msg = why + "; " + msg
	}
	return errors.New(msg)
}

func (e *shareEnv) publishAudience(ctx context.Context, w io.Writer, name string, dryRun, yes bool) error {
	a, _ := e.audience(name)
	_, _ = fmt.Fprintf(w, "%s → %s (%s)\n", name, share.RedactURL(a.Repo), a.BranchOrDefault())

	// Every shared project must export cleanly: an audience is published
	// whole or not at all, so its share repo never mixes states.
	exports := map[string]*share.Result{}
	owner := ""
	for _, slug := range a.ProjectSlugs() {
		opts, err := e.exportOptions(ctx, slug, name, nil, nil, "")
		if err != nil {
			return fmt.Errorf("%s: %w", slug, err)
		}
		res, err := share.Export(ctx, opts)
		if err != nil {
			return fmt.Errorf("%s: %s", slug, firstLine(err.Error()))
		}
		if n := len(res.Findings); n > 0 {
			return fmt.Errorf("%s has %d open finding(s); run cg share export %s --audience %s --check", slug, n, slug, name)
		}
		exports[slug] = res
		owner = res.Stamp.Owner
	}

	authorName, authorEmail, err := e.publishAuthor(ctx, a.AuthorName, a.AuthorEmail, owner)
	if err != nil {
		return err
	}
	plan, err := share.PreparePublish(ctx, &share.PublishOptions{
		Audience: name, Repo: a.Repo, Branch: a.BranchOrDefault(),
		Owner: owner, Generator: "cg " + Version,
		Exports: exports, WorkspaceRoot: e.root,
		AuthorName: authorName, AuthorEmail: authorEmail,
	})
	if err != nil {
		return err
	}
	defer plan.Close()

	printPlan(w, plan)
	if !plan.Changed {
		_, _ = fmt.Fprintln(w, "  up to date; nothing to publish")
		return nil
	}
	if dryRun {
		_, _ = fmt.Fprintln(w, "  dry run: nothing committed or pushed")
		return nil
	}
	if err := confirmPublish(ctx, w, plan, yes); err != nil {
		return err
	}
	sha, err := plan.Commit(ctx)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "  published %.12s\n", sha)
	return nil
}

// publishAuthor resolves the publish commit's identity: the audience's
// configured author, else the workspace repo's own git identity (which may be
// repo-local, and so invisible to the temporary share clone), else the owner
// name. A commit needs an email, so a missing one is an error.
func (e *shareEnv) publishAuthor(ctx context.Context, name, email, owner string) (resolvedName, resolvedEmail string, err error) {
	if name == "" {
		name, _ = gitx.Run(ctx, e.root, "config", "user.name")
	}
	if name == "" {
		name = owner
	}
	if email == "" {
		email, _ = gitx.Run(ctx, e.root, "config", "user.email")
	}
	if email == "" {
		return "", "", fmt.Errorf("no author email for the publish commit: set authorEmail on the audience or git user.email in this workspace")
	}
	return name, email, nil
}

// confirmPublish is the consent gate. The push happens inside cg, out of reach
// of Claude Code's PreToolUse push-policy gate (which only sees git commands
// Claude runs), so the gate lives here.
func confirmPublish(ctx context.Context, w io.Writer, plan *share.PublishPlan, yes bool) error {
	if plan.NeedsReview() {
		what := "a first publish"
		heading := "FIRST PUBLISH"
		if !plan.FirstPublish() {
			what = "a publish that adds files"
			heading = "NEW FILES"
		}
		if yes {
			return fmt.Errorf("--yes is refused for %s; run it at a terminal and review the content first", what)
		}
		if !consent.Interactive() {
			return fmt.Errorf("%s must be confirmed at an interactive terminal (run: cg share publish %s)", what, plan.Audience)
		}
		_, _ = fmt.Fprintf(w, "\n  %s to %q. Review the content in the staged copy before confirming:\n    %s\n", heading, plan.Audience, plan.Dir)
		answer, err := consent.Ask(ctx, w, fmt.Sprintf("  Type the audience name (%s) to publish, anything else to cancel: ", plan.Audience))
		if err != nil {
			return err
		}
		if answer != plan.Audience {
			return fmt.Errorf("cancelled")
		}
		return nil
	}
	if yes {
		return nil
	}
	if !consent.Interactive() {
		return fmt.Errorf("not an interactive terminal; pass --yes to confirm this republish")
	}
	answer, err := consent.Ask(ctx, w, "  Publish these changes? [y/N] ")
	if err != nil {
		return err
	}
	if a := strings.ToLower(answer); a != "y" && a != "yes" {
		return fmt.Errorf("cancelled")
	}
	return nil
}

func printPlan(w io.Writer, plan *share.PublishPlan) {
	slugs := make([]string, 0, len(plan.Projects))
	for s := range plan.Projects {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	for _, s := range slugs {
		c := plan.Projects[s]
		state := "unchanged"
		switch {
		case c.First:
			state = fmt.Sprintf("new (%d file(s))", len(c.Added))
		case len(c.Added)+len(c.Changed)+len(c.Removed) > 0:
			state = fmt.Sprintf("changed (+%d ~%d -%d)", len(c.Added), len(c.Changed), len(c.Removed))
		}
		_, _ = fmt.Fprintf(w, "  %-32s %s\n", s, state)
		if !c.First && len(c.Added) > 0 {
			_, _ = fmt.Fprintf(w, "  %-32s new file(s): %s\n", "", strings.Join(c.Added, ", "))
		}
	}
	for _, s := range plan.Removed {
		_, _ = fmt.Fprintf(w, "  %-32s removed: no longer shared, deleted from the share repo (git history keeps it)\n", s)
	}
	if len(plan.Foreign) > 0 {
		_, _ = fmt.Fprintf(w, "  replaces %d file(s) cg did not write: %s\n", len(plan.Foreign), strings.Join(plan.Foreign, ", "))
	}
}
