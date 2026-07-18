//go:build windows

package sysdns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// One entry per network adapter, with the DNS servers that were set
// by hand before us. Empty means the adapter was on DHCP.
type adapterBackup struct {
	Index  int    `json:"index"`
	Alias  string `json:"alias"`
	Static string `json:"static"`
}

// powershell runs a script fragment and hands back its stdout.
func powershell(script string) ([]byte, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

// Lists active physical adapters along with any static DNS servers,
// read from the registry because DHCP assigned ones must not be
// pinned on restore.
const collectScript = `
$adapters = Get-NetAdapter -Physical | Where-Object Status -eq 'Up'
$out = foreach ($a in $adapters) {
  $key = "HKLM:\SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces\$($a.InterfaceGuid)"
  $ns = (Get-ItemProperty -Path $key -Name NameServer -ErrorAction SilentlyContinue).NameServer
  [pscustomobject]@{ index = $a.ifIndex; alias = $a.Name; static = [string]$ns }
}
ConvertTo-Json @($out) -Compress
`

// Takeover points every active adapter at 127.0.0.1.
func Takeover(dir string) error {
	out, err := powershell(collectScript)
	if err != nil {
		return err
	}
	var adapters []adapterBackup
	if err := json.Unmarshal(bytes.TrimSpace(out), &adapters); err != nil {
		return fmt.Errorf("unexpected adapter listing: %w", err)
	}
	if len(adapters) == 0 {
		return fmt.Errorf("no active network adapter found")
	}
	raw, err := json.Marshal(adapters)
	if err != nil {
		return err
	}
	if err := os.WriteFile(backupPath(dir), raw, 0o600); err != nil {
		return err
	}
	for _, a := range adapters {
		script := fmt.Sprintf("Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses 127.0.0.1", a.Index)
		if _, err := powershell(script); err != nil {
			Restore(dir)
			return fmt.Errorf("adapter %q: %w (changing DNS needs an elevated terminal)", a.Alias, err)
		}
	}
	return nil
}

// Restore hands each adapter back to DHCP or its old static servers,
// then drops the backup.
func Restore(dir string) error {
	raw, err := os.ReadFile(backupPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no DNS backup found, nothing to restore")
		}
		return err
	}
	var adapters []adapterBackup
	if err := json.Unmarshal(raw, &adapters); err != nil {
		return err
	}
	var firstErr error
	for _, a := range adapters {
		var script string
		if strings.TrimSpace(a.Static) == "" {
			script = fmt.Sprintf("Set-DnsClientServerAddress -InterfaceIndex %d -ResetServerAddresses", a.Index)
		} else {
			parts := strings.FieldsFunc(a.Static, func(r rune) bool { return r == ',' || r == ' ' })
			script = fmt.Sprintf("Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses '%s'",
				a.Index, strings.Join(parts, "','"))
		}
		if _, err := powershell(script); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("adapter %q: %w", a.Alias, err)
		}
	}
	if firstErr != nil {
		return firstErr
	}
	return os.Remove(backupPath(dir))
}
