//go:build !windows

package lspclient

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

func configureProcess(command *exec.Cmd) func() {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var mu sync.Mutex
	var timer *time.Timer
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		mu.Lock()
		timer = time.AfterFunc(250*time.Millisecond, func() { _ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL) })
		mu.Unlock()
		return err
	}
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			// The parent may exit before its tsserver/Phpactor descendants.
			// Retain escalation while the process group still exists.
			if command.Process == nil || syscall.Kill(-command.Process.Pid, 0) == syscall.ESRCH {
				timer.Stop()
			}
		}
	}
}
