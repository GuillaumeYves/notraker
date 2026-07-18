// notraker is a small local DNS shield. Lookups for known tracker
// domains get a dead answer, so browsers and mail clients never
// reach the pixel. Everything else passes through untouched.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/GuillaumeYves/notraker/internal/control"
	"github.com/GuillaumeYves/notraker/internal/daemon"
	"github.com/GuillaumeYves/notraker/internal/paths"
	"github.com/GuillaumeYves/notraker/internal/sysdns"
)

// version is stamped at build time, see the Makefile. Builds made
// with go install carry the tag in build info instead, use that.
var version = "dev"

func init() {
	if version != "dev" {
		return
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		version = bi.Main.Version
	}
}

const usage = `notraker, a local shield against web and email trackers

usage: notraker <command> [flags]

commands:
  run          run in the foreground, Ctrl+C to stop
  start        run in the background
  stop         stop the background daemon
  status       show whether the shield is up
  stats        show what got blocked
  restore-dns  repair system DNS if a crash left it pointing at us
  version      print the version

flags for run and start:
  -port n       port to serve DNS on (default 53)
  -control a    address of the local control api (default 127.0.0.1:5380)
  -upstream s   comma separated upstream resolvers (default 1.1.1.1:53,9.9.9.9:53)
  -lists s      comma separated blocklist urls, replaces the default set
  -keep-dns     leave system DNS settings alone, just serve on the port
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = cmdRun(os.Args[2:])
	case "start":
		err = cmdStart(os.Args[2:])
	case "stop":
		err = cmdStop(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "stats":
		err = cmdStats(os.Args[2:])
	case "restore-dns":
		err = cmdRestoreDNS()
	case "version":
		fmt.Println("notraker", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		fmt.Print(usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func parseOpts(args []string) (daemon.Options, error) {
	fs := flag.NewFlagSet("notraker", flag.ContinueOnError)
	port := fs.Int("port", 53, "port to serve DNS on")
	ctl := fs.String("control", control.DefaultAddr, "control api address")
	upstream := fs.String("upstream", "1.1.1.1:53,9.9.9.9:53", "upstream resolvers")
	lists := fs.String("lists", "", "blocklist urls")
	keep := fs.Bool("keep-dns", false, "leave system DNS alone")
	if err := fs.Parse(args); err != nil {
		return daemon.Options{}, err
	}
	opts := daemon.Options{
		Port:         *port,
		ControlAddr:  *ctl,
		Upstreams:    splitList(*upstream),
		SetSystemDNS: !*keep,
		Version:      version,
	}
	if *lists != "" {
		opts.Sources = splitList(*lists)
	}
	return opts, nil
}

// parseControl is for the commands that only talk to a running daemon.
func parseControl(args []string) (string, error) {
	fs := flag.NewFlagSet("notraker", flag.ContinueOnError)
	ctl := fs.String("control", control.DefaultAddr, "control api address")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	return *ctl, nil
}

func cmdRun(args []string) error {
	opts, err := parseOpts(args)
	if err != nil {
		return err
	}
	return daemon.Run(opts)
}

func cmdStart(args []string) error {
	opts, err := parseOpts(args)
	if err != nil {
		return err
	}
	if st, err := control.GetStatus(opts.ControlAddr); err == nil {
		fmt.Printf("already running (pid %d)\n", st.PID)
		return nil
	}

	spawnArgs := []string{"run",
		"-port", strconv.Itoa(opts.Port),
		"-control", opts.ControlAddr,
		"-upstream", strings.Join(opts.Upstreams, ","),
	}
	if len(opts.Sources) > 0 {
		spawnArgs = append(spawnArgs, "-lists", strings.Join(opts.Sources, ","))
	}
	if !opts.SetSystemDNS {
		spawnArgs = append(spawnArgs, "-keep-dns")
	}
	pid, logPath, err := daemon.Spawn(spawnArgs)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		if st, err := control.GetStatus(opts.ControlAddr); err == nil {
			if st.Domains > 0 {
				fmt.Printf("shield is up (pid %d), blocking %d domains\n", st.PID, st.Domains)
			} else {
				fmt.Printf("shield is up (pid %d), blocklist is downloading\n", st.PID)
			}
			return nil
		}
	}
	return fmt.Errorf("daemon (pid %d) did not report back, see the log at %s", pid, logPath)
}

func cmdStop(args []string) error {
	addr, err := parseControl(args)
	if err != nil {
		return err
	}
	st, err := control.GetStatus(addr)
	if err != nil {
		return stopByPid()
	}
	if err := control.Shutdown(addr); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		if _, err := control.GetStatus(addr); err != nil {
			if st.Protecting {
				fmt.Println("stopped, system DNS restored")
			} else {
				fmt.Println("stopped")
			}
			return nil
		}
	}
	fmt.Println("stop requested, the daemon is taking its time; check notraker status")
	return nil
}

// stopByPid is the fallback for a daemon whose control api is gone.
func stopByPid() error {
	dir, err := paths.Dir()
	if err != nil {
		return err
	}
	pid, err := daemon.ReadPid(dir)
	if err != nil {
		fmt.Println("notraker is not running")
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err == nil {
		if err := proc.Kill(); err == nil {
			fmt.Printf("daemon (pid %d) was not answering, killed it\n", pid)
			fmt.Println("if lookups fail now, run notraker restore-dns")
			return nil
		}
	}
	fmt.Println("notraker is not running")
	return nil
}

func cmdStatus(args []string) error {
	addr, err := parseControl(args)
	if err != nil {
		return err
	}
	st, err := control.GetStatus(addr)
	if err != nil {
		fmt.Println("notraker is not running")
		if dir, derr := paths.Dir(); derr == nil && sysdns.HasBackup(dir) {
			fmt.Println("warning: a DNS backup exists, a previous run may have died; run notraker restore-dns")
		}
		return nil
	}
	fmt.Printf("notraker %s is running (pid %d)\n", st.Version, st.PID)
	fmt.Printf("blocking %d domains, serving DNS on %s\n", st.Domains, st.Addr)
	if st.Protecting {
		fmt.Println("system DNS points at the proxy")
	} else {
		fmt.Println("system DNS untouched (keep-dns mode)")
	}
	fmt.Printf("up for %s\n", time.Since(st.StartedAt).Round(time.Second))
	return nil
}

func cmdStats(args []string) error {
	addr, err := parseControl(args)
	if err != nil {
		return err
	}
	sn, err := control.GetStats(addr)
	if err != nil {
		fmt.Println("notraker is not running")
		return nil
	}
	if sn.Total == 0 {
		fmt.Println("no lookups seen yet")
		return nil
	}
	pct := float64(sn.Blocked) / float64(sn.Total) * 100
	fmt.Printf("%d lookups, %d blocked (%.1f%%)\n", sn.Total, sn.Blocked, pct)
	if len(sn.TopBlocked) > 0 {
		fmt.Println("\ntop blocked:")
		for _, d := range sn.TopBlocked {
			fmt.Printf("  %6d  %s\n", d.Count, d.Domain)
		}
	}
	return nil
}

func cmdRestoreDNS() error {
	dir, err := paths.Dir()
	if err != nil {
		return err
	}
	if !sysdns.HasBackup(dir) {
		fmt.Println("nothing to restore, system DNS was never taken over")
		return nil
	}
	if err := sysdns.Restore(dir); err != nil {
		return err
	}
	fmt.Println("system DNS restored")
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
