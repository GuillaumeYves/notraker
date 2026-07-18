//go:build windows

package daemon

import (
	"os/exec"
	"syscall"
)

// Not exposed by the syscall package, value straight from the
// Windows process creation flags.
const detachedProcess = 0x00000008

// detach starts the child in its own process group with no console,
// which is as close to a daemon as Windows gets without a service.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess,
		HideWindow:    true,
	}
}
