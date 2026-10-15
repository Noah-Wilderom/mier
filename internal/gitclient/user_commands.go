package gitclient

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

type UserCommandRequest struct {
	Executable     string
	Arguments      []string
	TimeoutSeconds int
	ExpectedHead   string
	ExpectedBranch string
}

// RunUserCommand only runs a locally saved command explicitly selected by the
// user. Arguments are literal; repository configuration never supplies code.
func (c *Client) RunUserCommand(ctx context.Context, path string, request UserCommandRequest, stdout, stderr io.Writer) (int, error) {
	exitCode := -1
	err := c.withRepository(ctx, path, time.Duration(request.TimeoutSeconds+5)*time.Second, func(ctx context.Context, root string) error {
		state, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		if state.HeadSHA != request.ExpectedHead || state.CurrentBranch != request.ExpectedBranch {
			return errors.New("The repository changed before the command started. Review its checkout and run again")
		}
		if request.TimeoutSeconds < 1 || request.TimeoutSeconds > 900 {
			return errors.New("Choose a command timeout between 1 and 900 seconds")
		}
		ctx, cancel := context.WithTimeout(ctx, time.Duration(request.TimeoutSeconds)*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, request.Executable, request.Arguments...)
		command.Dir = root
		command.Env = os.Environ()
		command.Stdout = stdout
		command.Stderr = stderr
		command.Stdin = nil
		command.WaitDelay = 2 * time.Second
		cleanup := configureCommand(command)
		defer cleanup()
		err = command.Run()
		if command.ProcessState != nil {
			exitCode = command.ProcessState.ExitCode()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	})
	return exitCode, err
}
