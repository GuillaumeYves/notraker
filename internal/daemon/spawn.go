package daemon

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/GuillaumeYves/notraker/internal/paths"
)

// Spawn relaunches this binary detached from the terminal, with its
// output going to a log file next to the rest of our data.
func Spawn(args []string) (pid int, logPath string, err error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, "", err
	}
	dir, err := paths.Dir()
	if err != nil {
		return 0, "", err
	}
	logPath = filepath.Join(dir, "notraker.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()

	cmd := exec.Command(exe, args...)
	cmd.Stdout = f
	cmd.Stderr = f
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return 0, "", err
	}
	pid = cmd.Process.Pid
	// let the child outlive us
	if err := cmd.Process.Release(); err != nil {
		return 0, "", err
	}
	return pid, logPath, nil
}
