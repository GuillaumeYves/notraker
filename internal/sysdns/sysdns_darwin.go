//go:build darwin

package sysdns

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// One entry per network service, with the DNS servers that were set.
// An empty list means the service was on automatic DNS.
type serviceBackup struct {
	Name    string   `json:"name"`
	Servers []string `json:"servers"`
}

func networksetup(args ...string) (string, error) {
	out, err := exec.Command("networksetup", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func listServices() ([]string, error) {
	out, err := networksetup("-listallnetworkservices")
	if err != nil {
		return nil, err
	}
	var svcs []string
	for i, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		// first line is a banner, a leading * marks a disabled service
		if i == 0 || line == "" || strings.HasPrefix(line, "*") {
			continue
		}
		svcs = append(svcs, line)
	}
	return svcs, nil
}

// Takeover points every enabled network service at 127.0.0.1.
func Takeover(dir string) error {
	svcs, err := listServices()
	if err != nil {
		return err
	}
	if len(svcs) == 0 {
		return fmt.Errorf("no network service found")
	}
	var backups []serviceBackup
	for _, svc := range svcs {
		out, _ := networksetup("-getdnsservers", svc)
		b := serviceBackup{Name: svc}
		if !strings.Contains(out, "There aren't any") {
			for _, l := range strings.Split(out, "\n") {
				if l = strings.TrimSpace(l); l != "" {
					b.Servers = append(b.Servers, l)
				}
			}
		}
		backups = append(backups, b)
	}
	raw, err := json.Marshal(backups)
	if err != nil {
		return err
	}
	if err := os.WriteFile(backupPath(dir), raw, 0o600); err != nil {
		return err
	}
	for _, svc := range svcs {
		if out, err := networksetup("-setdnsservers", svc, "127.0.0.1"); err != nil {
			Restore(dir)
			return fmt.Errorf("service %q: %v: %s (try sudo)", svc, err, out)
		}
	}
	return nil
}

// Restore hands services back their old servers, or automatic DNS,
// then drops the backup.
func Restore(dir string) error {
	raw, err := os.ReadFile(backupPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no DNS backup found, nothing to restore")
		}
		return err
	}
	var backups []serviceBackup
	if err := json.Unmarshal(raw, &backups); err != nil {
		return err
	}
	var firstErr error
	for _, b := range backups {
		args := []string{"-setdnsservers", b.Name}
		if len(b.Servers) == 0 {
			args = append(args, "Empty")
		} else {
			args = append(args, b.Servers...)
		}
		if out, err := networksetup(args...); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("service %q: %v: %s", b.Name, err, out)
		}
	}
	if firstErr != nil {
		return firstErr
	}
	return os.Remove(backupPath(dir))
}
