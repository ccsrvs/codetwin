//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detachProcess starts the child without a console and in a new process
// group so it survives the parent's exit.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// 0x00000008 is DETACHED_PROCESS.
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008,
	}
}
