package tunnel

import (
	"encoding/binary"
	"io"
	"net"
	"time"

	"github.com/wwricu/bad-proxy-core/logger"
)

// Policy is the capture decision, asked once per name at resolution time:
// names it owns get a fake address and are therefore routed into the tunnel,
// the rest keep their real answer and leave through the normal stack. The
// answer is the same rules the engine applies to the resulting connection, so
// the two never disagree.
type Policy interface {
	Proxy(name string) bool
}

// DNSServer is the resolver the system is pointed at while the tunnel is up.
// It answers for captured names and forwards everything else to the DNS
// servers that were configured before the tunnel took over.
type DNSServer struct {
	addr       *net.UDPAddr
	udp        *net.UDPConn
	tcp        net.Listener
	policy     Policy
	pool       *fakePool
	forwarders []string
	done       chan struct{}
}

const dnsTimeout = 4 * time.Second

// StartDNSServer binds the resolver. It is deliberately a loopback-only
// server: apps reach it through 127.0.0.1, which the tunnel never captures, so
// name resolution cannot feed packets back into itself.
func StartDNSServer(listen string, policy Policy, pool *fakePool, forwarders []string) (*DNSServer, error) {
	udpAddr, err := net.ResolveUDPAddr("udp4", listen)
	if err != nil {
		return nil, err
	}
	udp, err := net.ListenUDP("udp4", udpAddr)
	if err != nil {
		return nil, err
	}
	tcp, err := net.Listen("tcp4", listen)
	if err != nil {
		udp.Close()
		return nil, err
	}
	s := &DNSServer{
		addr:       udpAddr,
		udp:        udp,
		tcp:        tcp,
		policy:     policy,
		pool:       pool,
		forwarders: withoutSelf(forwarders, udpAddr.IP),
		done:       make(chan struct{}),
	}
	go s.serveUDP()
	go s.serveTCP()
	return s, nil
}

// withoutSelf drops a forwarder that is this very server. Pointing the
// resolver at itself is how a tunnel deadlock starts, and a user who already
// runs a local resolver (AdGuard Home, Pi-hole) would otherwise inherit one.
// Survivors come back as host:port, ready to dial.
func withoutSelf(list []string, self net.IP) []string {
	out := make([]string, 0, len(list))
	for _, entry := range list {
		host := entry
		if h, _, err := net.SplitHostPort(entry); err == nil {
			host = h
		}
		ip := net.ParseIP(host)
		if ip == nil || ip.IsLoopback() || ip.Equal(self) {
			continue
		}
		out = append(out, net.JoinHostPort(host, "53"))
	}
	return out
}

func (s *DNSServer) Close() {
	close(s.done)
	s.udp.Close()
	s.tcp.Close()
}

func (s *DNSServer) serveUDP() {
	buf := make([]byte, 2048)
	for {
		_ = s.udp.SetReadDeadline(time.Time{})
		n, from, err := s.udp.ReadFrom(buf)
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				continue
			}
		}
		ans := s.reply(buf[:n])
		if ans == nil {
			continue
		}
		if _, err := s.udp.WriteTo(ans, from); err != nil {
			logger.Debugf("tunnel: dns udp reply to %s failed: %v", from, err)
		}
	}
}

func (s *DNSServer) serveTCP() {
	for {
		c, err := s.tcp.Accept()
		if err != nil {
			return // listener closed
		}
		go s.serveTCPConn(c)
	}
}

func (s *DNSServer) serveTCPConn(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(dnsTimeout))
	var lenBuf [2]byte
	if _, err := io.ReadFull(c, lenBuf[:]); err != nil {
		return
	}
	n := int(binary.BigEndian.Uint16(lenBuf[:]))
	if n <= 0 || n > 4096 {
		return
	}
	query := make([]byte, n)
	if _, err := io.ReadFull(c, query); err != nil {
		return
	}
	ans := s.reply(query)
	if ans == nil {
		return
	}
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(ans)))
	if _, err := c.Write(lenBuf[:]); err != nil {
		return
	}
	_, _ = c.Write(ans)
}

// reply answers one query. The error path is a nil reply (drop), which leaves
// the client to its own timeout; that is only reachable for a query we cannot
// parse at all.
func (s *DNSServer) reply(query []byte) []byte {
	end, name, qtype, ok := parseQuestion(query)
	if !ok {
		logger.Debugf("tunnel: dns query unparseable (%d bytes)", len(query))
		return nil
	}
	if qtype != typeA && qtype != typeAAAA {
		return s.forward(query)
	}
	if !s.policy.Proxy(name) {
		return s.forward(query)
	}
	return captureAnswer(query, end, name, qtype, s.pool)
}

// forward asks the upstream servers the system used before the tunnel, in
// order, and returns the first answer. These sockets go to real addresses,
// which the tunnel does not route, so forwarding leaves the machine for real.
func (s *DNSServer) forward(query []byte) []byte {
	for _, f := range s.forwarders {
		conn, err := net.DialTimeout("udp", f, dnsTimeout)
		if err != nil {
			logger.Debugf("tunnel: dns forward to %s unreachable: %v", f, err)
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(dnsTimeout))
		if _, err := conn.Write(query); err != nil {
			conn.Close()
			continue
		}
		buf := make([]byte, 2048)
		n, err := conn.Read(buf)
		conn.Close()
		if err != nil || n < 12 {
			continue
		}
		return buf[:n]
	}
	// Nothing upstream: answer SERVFAIL rather than silence, so the app's
	// resolver reports a failure instead of hanging.
	end, _, _, ok := parseQuestion(query)
	if !ok {
		return nil
	}
	flags := dnsQR | dnsRA | 2 | uint16(query[2]&0x01)<<dnsRDOffset
	return assemble(query, end, flags, 0, nil)
}
