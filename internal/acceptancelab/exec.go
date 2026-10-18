package acceptancelab

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

// ExecRunner starts an executable directly and relies on the context deadline
// to stop a stalled child process.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, executable string, arguments []string, stdout, stderr io.Writer) Execution {
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Stdout = stdout
	command.Stderr = stderr
	command.WaitDelay = 5 * time.Second
	err := command.Run()
	result := Execution{ExitCode: 0}
	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
	}
	if err != nil {
		if command.ProcessState != nil {
			result.ExitCode = command.ProcessState.ExitCode()
			result.Error = "child process exited with a non-zero status"
		} else {
			result.ExitCode = -1
			if errors.Is(ctx.Err(), context.Canceled) {
				result.Error = "child process was interrupted"
			} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				result.Error = "child process exceeded its deadline"
			} else {
				result.Error = "child process could not start"
			}
		}
	}
	return result
}
