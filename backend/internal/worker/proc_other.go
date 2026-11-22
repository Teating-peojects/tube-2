//go:build !unix

package worker

import "os/exec"

func detach(*exec.Cmd) {}

// applyLimits is a no-op where the limits are not available (the worker targets Linux).
func applyLimits(int, Limits) error { return nil }
