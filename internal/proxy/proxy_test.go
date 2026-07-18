package proxy

import (
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/GuillaumeYves/notraker/internal/blocklist"
)

// Spins up a fake upstream and a real proxy, then checks that tracker
// lookups die and normal ones pass through.
func TestProxyBlocksAndForwards(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	upstream := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Answer = append(m.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
			A:   net.IPv4(1, 2, 3, 4),
		})
		w.WriteMsg(m)
	})}
	go upstream.ActivateAndServe()
	defer upstream.Shutdown()

	list := blocklist.New(nil)
	list.Replace([]string{"tracker.example.com"})

	s := New(list, "127.0.0.1:0", []string{pc.LocalAddr().String()})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	c := &dns.Client{Timeout: 2 * time.Second}

	q := new(dns.Msg)
	q.SetQuestion("pixel.tracker.example.com.", dns.TypeA)
	resp, _, err := c.Exchange(q, s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("blocked lookup: got %d answers, want 1", len(resp.Answer))
	}
	if a, ok := resp.Answer[0].(*dns.A); !ok || !a.A.Equal(net.IPv4zero) {
		t.Errorf("blocked lookup answered %v, want 0.0.0.0", resp.Answer[0])
	}

	q = new(dns.Msg)
	q.SetQuestion("fine.example.com.", dns.TypeA)
	resp, _, err = c.Exchange(q, s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("forwarded lookup: got %d answers, want 1", len(resp.Answer))
	}
	if a, ok := resp.Answer[0].(*dns.A); !ok || !a.A.Equal(net.IPv4(1, 2, 3, 4)) {
		t.Errorf("forwarded lookup answered %v, want 1.2.3.4", resp.Answer[0])
	}

	sn := s.Stats(5)
	if sn.Total != 2 || sn.Blocked != 1 {
		t.Errorf("stats = %d total %d blocked, want 2 and 1", sn.Total, sn.Blocked)
	}
	if len(sn.TopBlocked) != 1 || sn.TopBlocked[0].Domain != "pixel.tracker.example.com" {
		t.Errorf("top blocked = %v, want pixel.tracker.example.com", sn.TopBlocked)
	}
}
