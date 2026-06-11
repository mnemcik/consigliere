package autoupdate

import (
	"context"
	"os"
	"os/exec"
)

// workerEnvFlag marks a cg invocation as the detached auto-update worker so it
// takes the worker codepath instead of dispatching the user's command.
const workerEnvFlag = "CONSIGLIERE_AUTO_UPDATE_WORKER"

// IsWorkerInvocation reports whether this process is the detached worker.
func IsWorkerInvocation() bool {
	return os.Getenv(workerEnvFlag) == "1"
}

// Bootstrap spawns a detached background copy of cg that runs the auto-update
// worker, then returns immediately — the child's stdio is /dev/null and its
// lifecycle is decoupled from ours. Best-effort: any failure is swallowed so it
// can never break the user's actual command.
//
// It no-ops when disabled (caller's --no-auto-update / dev build / skipped
// command), when CONSIGLIERE_AUTO_UPDATE=0, or when already inside the worker.
func Bootstrap(disabled bool) {
	if disabled || os.Getenv("CONSIGLIERE_AUTO_UPDATE") == "0" || IsWorkerInvocation() {
		return
	}

	exe, err := os.Executable()
	if err != nil {
		return
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer func() { _ = devNull.Close() }() // child keeps its own fd post-exec

	// Background context (no cancellation): the worker is fire-and-forget and
	// must outlive this process.
	// #nosec G204 -- exe is our own path from os.Executable(), not user input.
	cmd := exec.CommandContext(context.Background(), exe)
	cmd.Env = append(os.Environ(), workerEnvFlag+"=1")
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.SysProcAttr = detachSysProcAttr()

	if err := cmd.Start(); err != nil {
		return
	}
	// Release so we don't keep a zombie/handle; the child runs on its own.
	_ = cmd.Process.Release()
}
