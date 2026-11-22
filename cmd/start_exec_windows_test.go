//go:build windows

package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/mnemcik/consigliere/internal/cgerr"
)

// TestExecAgentSurvivesInterrupt checks that on Windows, where cg stays the
// agent's parent, a console interrupt does not end cg before the agent: cg
// must wait for the agent and exit with its code. Ctrl+C cannot be sent to a
// single process group, so the test sends Ctrl+Break (Go delivers both as
// os.Interrupt) to a new process group holding a launcher (running the real
// execAgent) and its agent, as Go's own os/signal tests do.
func TestExecAgentSurvivesInterrupt(t *testing.T) {
	if os.Getenv("CG_TEST_EXEC_AGENT") == "ctrlbreak" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(98)
		}
		err = execAgent(exe, []string{exe, "-test.run=^TestExecAgentInterruptHelper$"}, os.Environ())
		var ce *cgerr.CodedError
		if errors.As(err, &ce) {
			os.Exit(ce.Code)
		}
		fmt.Fprintln(os.Stderr, "execAgent returned:", err)
		os.Exit(99)
	}

	c := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestExecAgentSurvivesInterrupt$") //nolint:gosec // the test binary itself
	c.Env = append(os.Environ(), "CG_TEST_EXEC_AGENT=ctrlbreak")
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	c.Stderr = os.Stderr
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	var got []string
	waitFor := func(want string) {
		t.Helper()
		deadline := time.After(30 * time.Second)
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatalf("output ended before %q; got %q", want, got)
				}
				got = append(got, l)
				if strings.Contains(l, want) {
					return
				}
			case <-deadline:
				t.Fatalf("timed out waiting for %q; got %q", want, got)
			}
		}
	}
	waitFor("AGENT ready")
	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(c.Process.Pid)); err != nil { //nolint:gosec // pid fits in uint32
		t.Skipf("cannot send a console control event here: %v", err)
	}
	waitFor("AGENT got interrupt")
	err = c.Wait()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 4 {
		t.Fatalf("cg must wait for the agent and exit with its code 4, got %v; output %q", err, got)
	}
}

// TestExecAgentInterruptHelper is the agent TestExecAgentSurvivesInterrupt
// starts: it reports when it is ready, waits for the interrupt, then exits 4
// a moment later (so a launcher killed by the interrupt would exit first).
func TestExecAgentInterruptHelper(t *testing.T) {
	if os.Getenv("CG_TEST_EXEC_AGENT") != "ctrlbreak" {
		t.Skip("helper process for TestExecAgentSurvivesInterrupt")
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	fmt.Println("AGENT ready")
	select {
	case <-sig:
		fmt.Println("AGENT got interrupt")
		time.Sleep(300 * time.Millisecond)
		os.Exit(4)
	case <-time.After(20 * time.Second):
		fmt.Println("AGENT timed out")
		os.Exit(9)
	}
}
