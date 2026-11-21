//go:build unix

package worker

import (
	"os/exec"
	"syscall"
	"time"
)

// detach puts the child in its own process group and kills the whole group on cancel or timeout, so
// a hung ffmpeg/scanner (and anything it spawned) can never outlive its job and keep eating CPU.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
}
