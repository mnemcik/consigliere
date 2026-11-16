//go:build windows

package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"

	"github.com/mnemcik/consigliere/internal/cgerr"
)

// execAgent runs the agent as a child (Windows has no exec) with the console's
// stdio, and exits with its code. Ctrl+C reaches the agent through the shared
// console; cg catches it so it stays alive until the agent exits.
func execAgent(path string, argv []string) error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	c := exec.CommandContext(context.Background(), path, argv[1:]...) //nolint:gosec // path resolved via exec.LookPath from the workspace's own setting
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return cgerr.New(ee.ExitCode(), "")
		}
		return err
	}
	return nil
}
