//go:build linux

package sysdns

import (
	"encoding/json"
	"fmt"
	"os"
)

const resolvConf = "/etc/resolv.conf"

// Everything needed to undo the takeover: the old file content, and
// the symlink target when resolv.conf was one (systemd-resolved).
type backup struct {
	Symlink string `json:"symlink,omitempty"`
	Content string `json:"content"`
}

// Takeover swaps /etc/resolv.conf for one line pointing at us.
func Takeover(dir string) error {
	var b backup
	if fi, err := os.Lstat(resolvConf); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if target, err := os.Readlink(resolvConf); err == nil {
			b.Symlink = target
		}
	}
	if content, err := os.ReadFile(resolvConf); err == nil {
		b.Content = string(content)
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	if err := os.WriteFile(backupPath(dir), raw, 0o600); err != nil {
		return err
	}
	os.Remove(resolvConf)
	conf := "# managed by notraker, run notraker restore-dns to undo\nnameserver 127.0.0.1\n"
	if err := os.WriteFile(resolvConf, []byte(conf), 0o644); err != nil {
		return fmt.Errorf("%w (writing /etc/resolv.conf needs root)", err)
	}
	return nil
}

// Restore puts resolv.conf back from the backup, then drops the backup.
func Restore(dir string) error {
	raw, err := os.ReadFile(backupPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no DNS backup found, nothing to restore")
		}
		return err
	}
	var b backup
	if err := json.Unmarshal(raw, &b); err != nil {
		return err
	}
	os.Remove(resolvConf)
	if b.Symlink != "" {
		err = os.Symlink(b.Symlink, resolvConf)
	} else {
		err = os.WriteFile(resolvConf, []byte(b.Content), 0o644)
	}
	if err != nil {
		return err
	}
	return os.Remove(backupPath(dir))
}
