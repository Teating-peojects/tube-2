//go:build unix

package inspect

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the child in its own process group and makes cancellation kill the whole group, so a
// hung ffprobe (or anything it spawned) cannot outlive its timeout.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
