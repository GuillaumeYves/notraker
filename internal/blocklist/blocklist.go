// Package blocklist keeps the set of domains we refuse to resolve.
//
// Lists come from well known public sources in hosts file format.
// The merged result is cached on disk so the proxy keeps working
// when the machine is offline.
package blocklist

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultSources are community maintained lists that cover ads,
// trackers and the domains behind email tracking pixels.
var DefaultSources = []string{
	"https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
	"https://pgl.yoyo.org/adservers/serverlist.php?hostformat=hosts&showintro=0&mimetype=plaintext",
}

const cacheFile = "blocklist.cache"

// List is a thread safe set of blocked domains.
type List struct {
	mu      sync.RWMutex
	domains map[string]struct{}
	sources []string
}

// New builds an empty list. Pass nil to use the default sources.
func New(sources []string) *List {
	if len(sources) == 0 {
		sources = DefaultSources
	}
	return &List{domains: map[string]struct{}{}, sources: sources}
}

// Len reports how many domains are currently blocked.
func (l *List) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.domains)
}

// Blocked answers the proxy's one question: should this name be dropped?
// A blocked domain takes all of its subdomains down with it.
func (l *List) Blocked(name string) bool {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	l.mu.RLock()
	defer l.mu.RUnlock()
	for name != "" {
		if _, ok := l.domains[name]; ok {
			return true
		}
		i := strings.IndexByte(name, '.')
		if i < 0 {
			break
		}
		name = name[i+1:]
	}
	return false
}

// Replace swaps in a whole new domain set.
func (l *List) Replace(domains []string) {
	set := make(map[string]struct{}, len(domains))
	for _, d := range domains {
		set[d] = struct{}{}
	}
	l.mu.Lock()
	l.domains = set
	l.mu.Unlock()
}

// LoadCache brings back the last merged list from disk, if there is one.
func (l *List) LoadCache(dir string) error {
	f, err := os.Open(filepath.Join(dir, cacheFile))
	if err != nil {
		return err
	}
	defer f.Close()
	l.Replace(parse(f))
	return nil
}

// Refresh downloads every source, merges them and saves the result.
// One source failing is survivable as long as another one works.
func (l *List) Refresh(dir string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	merged := make(map[string]struct{})
	var okCount int
	var lastErr error
	for _, src := range l.sources {
		resp, err := client.Get(src)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("%s answered %s", src, resp.Status)
			continue
		}
		for _, d := range parse(resp.Body) {
			merged[d] = struct{}{}
		}
		resp.Body.Close()
		okCount++
	}
	if okCount == 0 {
		return fmt.Errorf("no blocklist source reachable: %w", lastErr)
	}
	l.mu.Lock()
	l.domains = merged
	l.mu.Unlock()
	return l.saveCache(dir)
}

// saveCache writes the merged list next to our other data, atomically.
func (l *List) saveCache(dir string) error {
	l.mu.RLock()
	lines := make([]string, 0, len(l.domains))
	for d := range l.domains {
		lines = append(lines, d)
	}
	l.mu.RUnlock()
	sort.Strings(lines)
	tmp := filepath.Join(dir, cacheFile+".tmp")
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, cacheFile))
}

// parse understands hosts files ("0.0.0.0 tracker.com") and plain
// domain lists, one entry per line, comments allowed.
func parse(r io.Reader) []string {
	var out []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		var domain string
		switch {
		case len(fields) == 0:
			continue
		case net.ParseIP(fields[0]) != nil:
			if len(fields) < 2 {
				continue
			}
			domain = fields[1]
		default:
			domain = fields[0]
		}
		domain = strings.ToLower(strings.TrimSuffix(domain, "."))
		if domain == "" || !strings.Contains(domain, ".") {
			continue
		}
		// hosts files carry a few names that are not trackers at all
		if domain == "localhost.localdomain" || net.ParseIP(domain) != nil {
			continue
		}
		out = append(out, domain)
	}
	return out
}
