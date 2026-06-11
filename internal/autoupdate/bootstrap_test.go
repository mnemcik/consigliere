package autoupdate

import (
	"os"
	"testing"
)

func TestIsWorkerInvocation(t *testing.T) {
	t.Setenv(workerEnvFlag, "")
	if IsWorkerInvocation() {
		t.Error("unset flag should not be a worker invocation")
	}
	t.Setenv(workerEnvFlag, "1")
	if !IsWorkerInvocation() {
		t.Error("flag=1 should be a worker invocation")
	}
}

// Bootstrap must be a no-op in its disabled paths — note we deliberately never
// call Bootstrap(false) in tests, since that would spawn the test binary itself
// as the worker. These cases just confirm the guards return without spawning.
func TestBootstrapNoOpWhenDisabled(t *testing.T) {
	t.Setenv(workerEnvFlag, "")
	Bootstrap(true) // explicitly disabled — must return cleanly
}

func TestBootstrapNoOpInsideWorker(t *testing.T) {
	t.Setenv(workerEnvFlag, "1") // already the worker — must not re-spawn
	Bootstrap(false)
}

func TestBootstrapNoOpWhenEnvDisabled(t *testing.T) {
	t.Setenv(workerEnvFlag, "")
	t.Setenv("CONSIGLIERE_AUTO_UPDATE", "0")
	Bootstrap(false)
}

func TestDetachSysProcAttrNonNil(t *testing.T) {
	if detachSysProcAttr() == nil {
		t.Error("detachSysProcAttr must return a non-nil SysProcAttr")
	}
}

func TestProcessAliveSelf(t *testing.T) {
	// The current process is, definitionally, alive. (On Windows processAlive
	// is conservative and also returns true.)
	if !processAlive(os.Getpid()) {
		t.Error("current process should be reported alive")
	}
}
