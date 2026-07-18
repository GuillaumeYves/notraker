// Package control is the tiny localhost api the CLI uses to talk to
// a running daemon: are you alive, what have you blocked, please stop.
package control

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/GuillaumeYves/notraker/internal/proxy"
)

// DefaultAddr is loopback only on purpose, nothing outside this
// machine should ever reach the control api.
const DefaultAddr = "127.0.0.1:5380"

// Status is the daemon's self description.
type Status struct {
	PID        int       `json:"pid"`
	Version    string    `json:"version"`
	Addr       string    `json:"addr"`
	Domains    int       `json:"domains"`
	Protecting bool      `json:"protecting"`
	StartedAt  time.Time `json:"started_at"`
}

// Server carries the http listener so the daemon can close it.
type Server struct {
	http *http.Server
}

// Serve starts the api. The callbacks pull live data from the daemon.
func Serve(addr string, status func() Status, stats func() proxy.Snapshot, shutdown func()) (*Server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, status())
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, stats())
	})
	mux.HandleFunc("POST /shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		shutdown()
	})
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{http: &http.Server{Handler: mux}}
	go s.http.Serve(ln)
	return s, nil
}

// Close shuts the api down.
func (s *Server) Close() error { return s.http.Close() }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

var client = &http.Client{Timeout: 2 * time.Second}

// GetStatus asks a running daemon how it is doing.
func GetStatus(addr string) (Status, error) {
	var st Status
	err := getJSON(addr, "/status", &st)
	return st, err
}

// GetStats fetches the block counters.
func GetStats(addr string) (proxy.Snapshot, error) {
	var sn proxy.Snapshot
	err := getJSON(addr, "/stats", &sn)
	return sn, err
}

// Shutdown asks the daemon to stop and clean up after itself.
func Shutdown(addr string) error {
	resp, err := client.Post("http://"+addr+"/shutdown", "", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon answered %s", resp.Status)
	}
	return nil
}

func getJSON(addr, path string, v any) error {
	resp, err := client.Get("http://" + addr + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon answered %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
