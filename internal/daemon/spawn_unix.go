//go:build !windows

package daemon

import (
	"os/exec"
	"syscall"
)

// detach gives the child its own session, so closing the terminal
// does not take the daemon down with it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
