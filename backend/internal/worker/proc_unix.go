//go:build unix

package worker

import (
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

// detach puts the child in its own process group and kills the whole group on cancel or timeout, so
// a hung ffmpeg/scanner (and anything it spawned) can never outlive its job and keep eating CPU.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
}

type rlimit struct{ Cur, Max uint64 }

const rlimitAS = 9 // RLIMIT_AS on Linux

// applyLimits lowers the priority of a running child and caps its address space. It is applied just after the child
// starts (before it can allocate much); lowering a limit and raising niceness need no privileges.
func applyLimits(pid int, l Limits) error {
	if l.Nice > 0 {
		if err := syscall.Setpriority(syscall.PRIO_PROCESS, pid, l.Nice); err != nil {
			return err
		}
	}
	if l.MemoryMB > 0 {
		b := uint64(l.MemoryMB) << 20
		r := rlimit{Cur: b, Max: b}
		if _, _, e := syscall.Syscall6(syscall.SYS_PRLIMIT64, uintptr(pid), rlimitAS, uintptr(unsafe.Pointer(&r)), 0, 0, 0); e != 0 {
			return e
		}
	}
	return nil
}
