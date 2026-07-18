// Package proxy is the local DNS server. Tracker lookups get a dead
// answer, everything else is forwarded to a real resolver.
package proxy

import (
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/GuillaumeYves/notraker/internal/blocklist"
)

// Short TTL on blocked answers, so nothing caches our lies for long.
const blockedTTL = 10

// Server serves DNS on a local address, over both UDP and TCP.
type Server struct {
	addr      string
	list      *blocklist.List
	upstreams []string
	udpc      *dns.Client
	tcpc      *dns.Client

	udp *dns.Server
	tcp *dns.Server

	mu        sync.Mutex
	total     uint64
	blocked   uint64
	perDomain map[string]uint64
}

// New wires a server up. Nothing listens until Start is called.
func New(list *blocklist.List, addr string, upstreams []string) *Server {
	return &Server{
		addr:      addr,
		list:      list,
		upstreams: upstreams,
		udpc:      &dns.Client{Timeout: 5 * time.Second},
		tcpc:      &dns.Client{Net: "tcp", Timeout: 5 * time.Second},
		perDomain: map[string]uint64{},
	}
}

// Start binds UDP and TCP listeners and serves in the background.
// Binding up front means a taken port fails loudly right here.
func (s *Server) Start() error {
	pc, err := net.ListenPacket("udp", s.addr)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		pc.Close()
		return err
	}
	handler := dns.HandlerFunc(s.handle)
	s.udp = &dns.Server{PacketConn: pc, Handler: handler}
	s.tcp = &dns.Server{Listener: ln, Handler: handler}
	go s.udp.ActivateAndServe()
	go s.tcp.ActivateAndServe()
	return nil
}

// Stop closes both listeners.
func (s *Server) Stop() {
	if s.udp != nil {
		s.udp.Shutdown()
	}
	if s.tcp != nil {
		s.tcp.Shutdown()
	}
}

// Addr reports the UDP address actually bound, handy when the
// requested port was 0.
func (s *Server) Addr() string {
	if s.udp == nil || s.udp.PacketConn == nil {
		return s.addr
	}
	return s.udp.PacketConn.LocalAddr().String()
}

func (s *Server) handle(w dns.ResponseWriter, req *dns.Msg) {
	if len(req.Question) == 0 {
		m := new(dns.Msg)
		m.SetRcode(req, dns.RcodeFormatError)
		w.WriteMsg(m)
		return
	}
	name := strings.TrimSuffix(strings.ToLower(req.Question[0].Name), ".")

	if s.list.Blocked(name) {
		s.count(name, true)
		w.WriteMsg(blockedReply(req))
		return
	}
	s.count(name, false)

	resp, err := s.forward(req)
	if err != nil {
		log.Printf("lookup for %s failed upstream: %v", name, err)
		m := new(dns.Msg)
		m.SetRcode(req, dns.RcodeServerFailure)
		w.WriteMsg(m)
		return
	}
	w.WriteMsg(resp)
}

// forward tries each upstream in order, switching to TCP when an
// answer comes back truncated.
func (s *Server) forward(req *dns.Msg) (*dns.Msg, error) {
	var lastErr error
	for _, up := range s.upstreams {
		resp, _, err := s.udpc.Exchange(req, up)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.Truncated {
			if tcpResp, _, err := s.tcpc.Exchange(req, up); err == nil {
				return tcpResp, nil
			}
		}
		return resp, nil
	}
	return nil, lastErr
}

// blockedReply answers with a dead address, which quietly starves the
// tracker instead of surfacing an error to the app.
func blockedReply(req *dns.Msg) *dns.Msg {
	m := new(dns.Msg)
	m.SetReply(req)
	q := req.Question[0]
	hdr := dns.RR_Header{Name: q.Name, Class: dns.ClassINET, Ttl: blockedTTL}
	switch q.Qtype {
	case dns.TypeA:
		hdr.Rrtype = dns.TypeA
		m.Answer = append(m.Answer, &dns.A{Hdr: hdr, A: net.IPv4zero})
	case dns.TypeAAAA:
		hdr.Rrtype = dns.TypeAAAA
		m.Answer = append(m.Answer, &dns.AAAA{Hdr: hdr, AAAA: net.IPv6zero})
	}
	return m
}

func (s *Server) count(name string, blocked bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	if blocked {
		s.blocked++
		s.perDomain[name]++
	}
}

// Snapshot is what the stats command shows.
type Snapshot struct {
	Total      uint64        `json:"total"`
	Blocked    uint64        `json:"blocked"`
	TopBlocked []DomainCount `json:"top_blocked"`
}

// DomainCount pairs a blocked domain with how often it was asked for.
type DomainCount struct {
	Domain string `json:"domain"`
	Count  uint64 `json:"count"`
}

// Stats returns current counters with the n most blocked domains.
func (s *Server) Stats(n int) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	top := make([]DomainCount, 0, len(s.perDomain))
	for d, c := range s.perDomain {
		top = append(top, DomainCount{Domain: d, Count: c})
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].Count != top[j].Count {
			return top[i].Count > top[j].Count
		}
		return top[i].Domain < top[j].Domain
	})
	if len(top) > n {
		top = top[:n]
	}
	return Snapshot{Total: s.total, Blocked: s.blocked, TopBlocked: top}
}
