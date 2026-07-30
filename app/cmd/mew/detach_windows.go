//go:build kittytk && windows

package main

import "syscall"

// detachSysProcAttr starts the child detached from the parent's console, so it
// runs independently of the shell. (mew-sdl is not cross-built for Windows, but
// this keeps the launcher compiling everywhere.)
func detachSysProcAttr() *syscall.SysProcAttr {
	const detachedProcess = 0x00000008 // DETACHED_PROCESS
	return &syscall.SysProcAttr{CreationFlags: detachedProcess}
}
