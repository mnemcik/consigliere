package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// externalDispatch decides whether args invoke an external subcommand rather
// than a built-in. It returns the resolved binary path, the arguments to pass
// it, and true when an external command should run. Built-ins always win:
// `cg <verb>` only resolves externally when <verb> is not a known cg command.
//
// Resolution (git's `git foo` → `git-foo` pattern, extended): an installed
// extension whose ledger declares the namespace <verb> contributes a binary at
// <clone>/bin/<binary>; failing that, a `cg-<verb>` executable on $PATH.
func externalDispatch(args []string) (path string, rest []string, ok bool) {
	// Find the verb: the first non-flag token. Leading persistent flags (e.g.
	// --no-auto-update) are cg's, not the external command's, so they are
	// skipped. (All root persistent flags are booleans, so none consume a value.)
	verbIdx := -1
	for i, a := range args {
		if a == "" || a[0] != '-' {
			verbIdx = i
			break
		}
	}
	if verbIdx < 0 {
		return "", nil, false // no verb (flags only)
	}
	verb := args[verbIdx]
	if verb == "help" || verb == "completion" || verb == "__complete" {
		return "", nil, false
	}
	// If cobra can resolve the verb onward to a real (non-root) command, it's a
	// built-in and wins.
	if target, _, err := rootCmd.Find(args[verbIdx:]); err == nil && target != rootCmd {
		return "", nil, false
	}
	bin, found := resolveExternalSubcommand(workspaceRootForDispatch(), verb)
	if !found {
		return "", nil, false
	}
	return bin, args[verbIdx+1:], true
}

// resolveExternalSubcommand finds the executable backing `cg <namespace>`. It
// first consults the workspace's installed extensions (an extension whose ledger
// declares <namespace> ships a binary at <clone>/bin/<binary>), then falls back
// to a `cg-<namespace>` executable on $PATH.
func resolveExternalSubcommand(root, namespace string) (string, bool) {
	if root != "" {
		if cfg, err := workspace.Detect(root); err == nil && cfg != nil {
			for _, e := range cfg.Extensions {
				led, lerr := extension.LoadLedger(root, e.Name)
				if lerr != nil || led == nil {
					continue
				}
				for _, sc := range led.Subcommands {
					if sc.Namespace != namespace {
						continue
					}
					bin := filepath.Join(extension.CloneDir(e.Name), "bin", sc.Binary)
					if info, serr := os.Stat(bin); serr == nil && !info.IsDir() {
						return bin, true
					}
				}
			}
		}
	}
	if p, err := exec.LookPath("cg-" + namespace); err == nil {
		return p, true
	}
	return "", false
}

// runExternal execs the resolved binary, wiring through stdio, and propagates its
// exit code via a CodedError so main.go exits with the same status. Its stderr is
// the command's own, so no extra error line is printed on a non-zero exit.
func runExternal(path string, args []string) error {
	c := exec.CommandContext(context.Background(), path, args...) //nolint:gosec // path resolved from installed extensions / PATH
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return cgerr.New(ee.ExitCode(), "")
		}
		return err
	}
	return nil
}

// workspaceRootForDispatch resolves the workspace root from cwd, returning "" if
// not inside a workspace (PATH fallback still applies).
func workspaceRootForDispatch() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	root, _, _ := workspace.FindRoot(cwd)
	return root
}
