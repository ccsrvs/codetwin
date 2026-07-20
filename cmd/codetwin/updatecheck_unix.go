//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachProcess puts the child in its own session so it survives the
// parent's exit.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
