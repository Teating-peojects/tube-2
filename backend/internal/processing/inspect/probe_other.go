//go:build !unix

package inspect

import "os/exec"

// setProcessGroup is a no-op where process groups are not available (the worker targets Linux).
func setProcessGroup(*exec.Cmd) {}
