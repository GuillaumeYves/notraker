// Package daemon runs the whole show: proxy, blocklist refresh,
// control api and system DNS takeover, plus a clean shutdown path.
package daemon

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/GuillaumeYves/notraker/internal/blocklist"
	"github.com/GuillaumeYves/notraker/internal/control"
	"github.com/GuillaumeYves/notraker/internal/paths"
	"github.com/GuillaumeYves/notraker/internal/proxy"
	"github.com/GuillaumeYves/notraker/internal/sysdns"
)

// Options is everything the daemon needs to know before starting.
type Options struct {
	Port         int
	ControlAddr  string
	Upstreams    []string
	Sources      []string
	SetSystemDNS bool
	Version      string
}

// Run blocks until the daemon is told to stop, by a signal or by the
// control api. It always tries to leave system DNS as it found it.
func Run(opts Options) error {
	dir, err := paths.Dir()
	if err != nil {
		return err
	}

	list := blocklist.New(opts.Sources)
	if err := list.LoadCache(dir); err == nil {
		log.Printf("loaded %d domains from cache", list.Len())
	}
	go refreshLoop(list, dir)

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.Port))
	srv := proxy.New(list, addr, opts.Upstreams)
	if err := srv.Start(); err != nil {
		return fmt.Errorf("cannot serve DNS on %s: %w (port 53 needs an elevated terminal on Windows, root elsewhere)", addr, err)
	}
	defer srv.Stop()
	log.Printf("dns proxy listening on %s", addr)

	if err := writePid(dir); err != nil {
		return err
	}
	defer removePid(dir)

	if opts.SetSystemDNS {
		if err := sysdns.Takeover(dir); err != nil {
			return fmt.Errorf("could not point system DNS at the proxy: %w", err)
		}
		log.Print("system DNS now points at the proxy")
		defer func() {
			if err := sysdns.Restore(dir); err != nil {
				log.Printf("could not restore system DNS: %v (run notraker restore-dns)", err)
			} else {
				log.Print("system DNS restored")
			}
		}()
	}

	started := time.Now()
	var stopOnce sync.Once
	stopc := make(chan struct{})
	ctl, err := control.Serve(opts.ControlAddr,
		func() control.Status {
			return control.Status{
				PID:        os.Getpid(),
				Version:    opts.Version,
				Addr:       addr,
				Domains:    list.Len(),
				Protecting: opts.SetSystemDNS,
				StartedAt:  started,
			}
		},
		func() proxy.Snapshot { return srv.Stats(10) },
		func() { stopOnce.Do(func() { close(stopc) }) },
	)
	if err != nil {
		return fmt.Errorf("control api on %s: %w", opts.ControlAddr, err)
	}
	defer ctl.Close()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	select {
	case s := <-sigc:
		log.Printf("caught %s, shutting down", s)
	case <-stopc:
		log.Print("stop requested, shutting down")
	}
	return nil
}

// refreshLoop fetches the lists right away, then once a day. A failed
// fetch is retried hourly, the cached list keeps working meanwhile.
func refreshLoop(list *blocklist.List, dir string) {
	for {
		wait := 24 * time.Hour
		if err := list.Refresh(dir); err != nil {
			log.Printf("blocklist refresh failed: %v", err)
			wait = time.Hour
		} else {
			log.Printf("blocklist refreshed, %d domains", list.Len())
		}
		time.Sleep(wait)
	}
}

func pidPath(dir string) string { return filepath.Join(dir, "notraker.pid") }

func writePid(dir string) error {
	return os.WriteFile(pidPath(dir), []byte(strconv.Itoa(os.Getpid())), 0o644)
}

func removePid(dir string) { os.Remove(pidPath(dir)) }

// ReadPid reports the pid of the last started daemon, if any.
func ReadPid(dir string) (int, error) {
	b, err := os.ReadFile(pidPath(dir))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}
