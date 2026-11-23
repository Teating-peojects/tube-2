//go:build !unix

package worker

import "os/exec"

func detach(*exec.Cmd) {}
