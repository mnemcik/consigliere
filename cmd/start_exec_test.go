package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/cgerr"
)

// TestExecAgentRunsAgent runs the real platform execAgent (syscall.Exec on
// Unix, a child process on Windows) in a subprocess of the test binary. The
// subprocess starts the test binary again as the "agent" (TestExecAgentHelper),
// which prints the arguments, environment and a stdin line on stdout, a line on
// stderr, and exits 3. The parent
// checks the output and that exit code 3 reaches it on both platforms.
func TestExecAgentRunsAgent(t *testing.T) {
	if os.Getenv("CG_TEST_EXEC_AGENT") == "launch" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(98)
		}
		err = execAgent(exe, []string{exe, "-test.run=^TestExecAgentHelper$", "--", "a b", ""})
		var ce *cgerr.CodedError
		if errors.As(err, &ce) {
			os.Exit(ce.Code) // Windows: the child's exit code
		}
		fmt.Fprintln(os.Stderr, "execAgent returned:", err)
		os.Exit(99)
	}
	c := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestExecAgentRunsAgent$") //nolint:gosec // the test binary itself
	c.Env = append(os.Environ(), "CG_TEST_EXEC_AGENT=launch", "CG_TEST_AGENT_MARK=env-ok")
	c.Stdin = strings.NewReader("hello from stdin\n")
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	err := c.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("want exit 3 from the agent, got %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	if want := `AGENT args=["a b" ""] mark=env-ok stdin="hello from stdin"`; !strings.Contains(stdout.String(), want) {
		t.Errorf("agent stdout missing %q:\n%s", want, stdout.String())
	}
	if want := "AGENT stderr line"; !strings.Contains(stderr.String(), want) || strings.Contains(stdout.String(), want) {
		t.Errorf("agent stderr must reach stderr only; stdout: %q stderr: %q", stdout.String(), stderr.String())
	}
}

// TestExecAgentHelper is the agent TestExecAgentRunsAgent starts; it does
// nothing in a normal test run.
func TestExecAgentHelper(t *testing.T) {
	if os.Getenv("CG_TEST_EXEC_AGENT") != "launch" {
		t.Skip("helper process for TestExecAgentRunsAgent")
	}
	var args []string
	for i, a := range os.Args {
		if a == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	fmt.Printf("AGENT args=%q mark=%s stdin=%q\n", args, os.Getenv("CG_TEST_AGENT_MARK"), strings.TrimRight(line, "\r\n"))
	fmt.Fprintln(os.Stderr, "AGENT stderr line")
	os.Exit(3)
}
