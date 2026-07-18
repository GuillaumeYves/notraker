// Package sysdns points the OS resolver at our proxy and, just as
// important, puts it back exactly how it was.
//
// A backup is written before touching anything, so even after a crash
// the machine can be repaired with notraker restore-dns.
package sysdns

import (
	"os"
	"path/filepath"
)

const backupFile = "dns-backup.json"

func backupPath(dir string) string { return filepath.Join(dir, backupFile) }

// HasBackup tells whether a takeover is still in effect on disk.
func HasBackup(dir string) bool {
	_, err := os.Stat(backupPath(dir))
	return err == nil
}
