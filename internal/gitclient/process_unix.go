//go:build !windows

package gitclient

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

func configureCommand(command *exec.Cmd) func() {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var timer *time.Timer
	var mu sync.Mutex
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		if err == nil {
			mu.Lock()
			timer = time.AfterFunc(time.Second, func() { _ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL) })
			mu.Unlock()
		}
		return err
	}
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
	}
}
