//go:build kittytk && !windows

package main

import "syscall"

// detachSysProcAttr starts the child in a new session (Setsid), so the launched
// mew-sdl window has no controlling terminal and outlives the shell that ran us.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
