package wizard

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

// ErrNotATTY is returned when the wizard is invoked without an interactive
// stdin. The caller should surface this to the user and suggest running
// without `--wizard`.
var ErrNotATTY = errors.New("wizard requires an interactive terminal")

// Run presents the interactive wizard and returns the user's answers.
// Returns ErrNotATTY when stdin is not a TTY; huh cannot render a form in
// that case and would otherwise hang silently.
func Run() (Answers, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return Answers{}, ErrNotATTY
	}

	var a Answers
	a.InstallSlash = true

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Consigliere workspace setup").
				Description("A few questions to seed PROFILE.md and your first area.\nPress esc to abort; defaults are shown where available."),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Your name").
				Description("Recorded in PROFILE.md.").
				Value(&a.ProfileName),
			huh.NewInput().
				Title("Your role").
				Description("e.g., Staff engineer at $COMPANY.").
				Value(&a.ProfileRole),
			huh.NewText().
				Title("Primary responsibilities").
				Description("One per line. Becomes the Responsibilities section.").
				Value(&a.ProfileFocus),
		),
		huh.NewGroup(
			huh.NewNote().
				Title("First area").
				Description("Areas are long-lived reference hubs (systems, practices, domains).\nYou can skip by leaving the slug empty."),
			huh.NewInput().
				Title("Area slug").
				Description("Lowercase, dash-separated. e.g. `pension-calc`.").
				Value(&a.AreaSlug).
				Validate(validateSlug),
			huh.NewInput().
				Title("Area display name").
				Description("e.g., Pension Calculation Engine.").
				Value(&a.AreaName),
			huh.NewSelect[string]().
				Title("Area category").
				Options(
					huh.NewOption("Service/System — a microservice, API, or infrastructure component", "Service/System"),
					huh.NewOption("Practice/Platform — a process, tool, or organizational practice", "Practice/Platform"),
				).
				Value(&a.AreaCategory),
			huh.NewText().
				Title("One-line overview").
				Description("Optional. What does this area cover?").
				Value(&a.AreaOverview),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Run `git init` in this directory?").
				Affirmative("Yes").
				Negative("No").
				Value(&a.RunGitInit),
			huh.NewConfirm().
				Title("Install Claude Code slash commands (.claude/commands/)?").
				Affirmative("Yes").
				Negative("No").
				Value(&a.InstallSlash),
		),
	)

	if err := form.Run(); err != nil {
		return Answers{}, fmt.Errorf("wizard aborted: %w", err)
	}

	// Post-process free-form inputs so downstream code sees canonical shapes.
	a.AreaSlug = SanitizeSlug(a.AreaSlug)
	return a, nil
}

func validateSlug(s string) error {
	if s == "" {
		return nil // skipping the area is allowed
	}
	if SanitizeSlug(s) == "" {
		return errors.New("slug must contain at least one letter or digit")
	}
	return nil
}
