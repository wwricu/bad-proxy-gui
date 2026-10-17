package tunnel

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/tun"
)

// These tests cover the parts of the tunnel that do not need a real network
// adapter: the resolver's capture decision and the packet path from the virtual
// NIC to a SOCKS5 CONNECT that still carries the domain. An in-memory device
// stands in for wintun/utun, so all of it runs unprivileged.

type testPolicy struct {
	mu    sync.Mutex
	proxy map[string]bool
}

func (p *testPolicy) Proxy(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.proxy[name]
}

func buildQuery(id uint16, name string, qtype uint16) []byte {
	// Real stub resolvers always set RD, and RD is the bit a careless reply
	// builder lands inside the rcode - so the queries here do too.
	out := []byte{byte(id >> 8), byte(id), 0x01, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, label := range strings.Split(name, ".") {
		out = append(out, byte(len(label)))
		out = append(out, label...)
	}
	out = append(out, 0)
	var t [4]byte
	binary.BigEndian.PutUint16(t[:2], qtype)
	binary.BigEndian.PutUint16(t[2:], 1) // class IN
	return append(out, t[:]...)
}

// answerA returns the reply's first A record, or nil when it answers nothing.
func answerA(t *testing.T, msg []byte) net.IP {
	t.Helper()
	if len(msg) < 12 {
		t.Fatalf("reply is %d bytes, too short for a DNS header", len(msg))
	}
	end, _, _, ok := parseQuestion(msg)
	if !ok {
		t.Fatalf("reply is not a parseable DNS message")
	}
	if n := binary.BigEndian.Uint16(msg[6:8]); n == 0 || end+12 > len(msg) {
		return nil
	}
	// The answer starts with a 2-byte pointer to the question, then qtype, class,
	// ttl and the 2-byte rdlength - so the data begins at end+12.
	rdLen := int(binary.BigEndian.Uint16(msg[end+10 : end+12]))
	if end+12+rdLen > len(msg) || rdLen != 4 {
		t.Fatalf("answer rdlength %d does not fit", rdLen)
	}
	return net.IP(msg[end+12 : end+16])
}

func rcode(msg []byte) uint16 { return binary.BigEndian.Uint16(msg[2:4]) & 0xf }

func TestDNSAnswersCapturedNamesWithFakeIPs(t *testing.T) {
	pol := &testPolicy{proxy: map[string]bool{"proxied.example": true}}
	pool := newFakePool()
	srv, err := StartDNSServer("127.0.0.1:0", pol, pool, nil)
	if err != nil {
		t.Fatalf("start resolver: %v", err)
	}
	defer srv.Close()

	conn, err := net.Dial("udp", srv.udp.LocalAddr().String())
	if err != nil {
		t.Fatalf("dial resolver: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	ask := func(name string, qtype uint16) []byte {
		t.Helper()
		if _, err := conn.Write(buildQuery(0x1234, name, qtype)); err != nil {
			t.Fatalf("write query for %s: %v", name, err)
		}
		buf := make([]byte, 512)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("read reply for %s: %v", name, err)
		}
		return buf[:n]
	}

	captured := ask("proxied.example", typeA)
	if rcode(captured) != 0 {
		t.Errorf("captured name got rcode %d, want NOERROR", rcode(captured))
	}
	if rd := binary.BigEndian.Uint16(captured[2:4]) & 0x0100; rd == 0 {
		t.Errorf("reply dropped the RD bit: flags %#x", binary.BigEndian.Uint16(captured[2:4]))
	}
	ip := answerA(t, captured)
	if ip == nil {
		t.Fatal("captured name got no answer")
	}
	if !isFakeIP(ip) {
		t.Errorf("captured name answered %s, outside the fake block", ip)
	}
	if got, ok := pool.Lookup(ip); !ok || got != "proxied.example" {
		t.Errorf("pool maps %s back to %q, want proxied.example", ip, got)
	}

	// A name the policy sends direct must not be answered from the pool. With no
	// forwarders the answer is SERVFAIL, which is still not a fake address.
	reply := ask("direct.example", typeA)
	if n := binary.BigEndian.Uint16(reply[6:8]); n != 0 {
		t.Errorf("uncaptured name answered %d records, wanted it forwarded", n)
	}
	if rcode(reply) != 2 {
		t.Errorf("uncaptured name got rcode %d, want SERVFAIL (no forwarder)", rcode(reply))
	}

	// AAAA for a captured name is answered empty, which is what pushes apps onto
	// the fake IPv4 address instead of stalling on a missing v6 route.
	reply = ask("proxied.example", typeAAAA)
	if rcode(reply) != 0 {
		t.Errorf("AAAA rcode %d, want NOERROR", rcode(reply))
	}
	if n := binary.BigEndian.Uint16(reply[6:8]); n != 0 {
		t.Errorf("AAAA answered %d records, want 0", n)
	}
}

func TestWithoutSelfDropsLoopback(t *testing.T) {
	got := withoutSelf([]string{"127.0.0.1", "192.168.1.1:53", "::1", "223.5.5.5", "garbage"}, net.ParseIP("127.0.0.1"))
	want := []string{"192.168.1.1:53", "223.5.5.5:53"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestForwardersSurviveRealServers checks the resolver keeps usable upstreams
// and throws away anything that could point back at itself.
func TestForwardersSurviveRealServers(t *testing.T) {
	pol := &testPolicy{proxy: map[string]bool{}}
	srv, err := StartDNSServer("127.0.0.1:0", pol, newFakePool(), []string{"127.0.0.1:53", "1.1.1.1"})
	if err != nil {
		t.Fatalf("start resolver: %v", err)
	}
	defer srv.Close()
	if len(srv.forwarders) != 1 || srv.forwarders[0] != "1.1.1.1:53" {
		t.Fatalf("forwarders = %v, want only 1.1.1.1:53", srv.forwarders)
	}
}

// fakeDevice is an in-memory tun.Device: the test writes the packets the OS
// would and reads back the ones the netstack answers with.
type fakeDevice struct {
	in     chan []byte
	out    chan []byte
	closed chan struct{}
	once   sync.Once
}

func newFakeDevice() *fakeDevice {
	return &fakeDevice{
		in:     make(chan []byte, 16),
		out:    make(chan []byte, 64),
		closed: make(chan struct{}),
	}
}

func (d *fakeDevice) File() *os.File { return nil }

func (d *fakeDevice) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	select {
	case p, ok := <-d.in:
		if !ok {
			return 0, os.ErrClosed
		}
		sizes[0] = copy(bufs[0][offset:], p)
		return 1, nil
	case <-d.closed:
		return 0, os.ErrClosed
	}
}

func (d *fakeDevice) Write(bufs [][]byte, offset int) (int, error) {
	p := make([]byte, len(bufs[0])-offset)
	copy(p, bufs[0][offset:])
	select {
	case d.out <- p:
		return 1, nil
	case <-d.closed:
		return 0, os.ErrClosed
	}
}

func (d *fakeDevice) MTU() (int, error)        { return 1400, nil }
func (d *fakeDevice) Name() (string, error)    { return "faketun", nil }
func (d *fakeDevice) Events() <-chan tun.Event { return nil }
func (d *fakeDevice) BatchSize() int           { return 1 }

func (d *fakeDevice) Close() error {
	d.once.Do(func() {
		close(d.closed)
		close(d.in)
	})
	return nil
}

var _ tun.Device = (*fakeDevice)(nil)

// ---- hand built IPv4/TCP: the stack drops a packet whose checksums it cannot
// verify, so there is no way to test with a sloppy header.

type tcpSegment struct {
	src, dst     net.IP
	sport, dport uint16
	seq, ack     uint32
	flags        byte
	payload      []byte
}

const (
	synFlag = 0x02
	ackFlag = 0x10
)

func (s tcpSegment) marshal() []byte {
	tcp := []byte{
		byte(s.sport >> 8), byte(s.sport), byte(s.dport >> 8), byte(s.dport),
		byte(s.seq >> 24), byte(s.seq >> 16), byte(s.seq >> 8), byte(s.seq),
		byte(s.ack >> 24), byte(s.ack >> 16), byte(s.ack >> 8), byte(s.ack),
		5 << 4, s.flags, 0xff, 0xff, 0, 0, 0, 0,
	}
	tcp = append(tcp, s.payload...)

	ps := make([]byte, 0, 12+len(tcp))
	ps = append(ps, s.src.To4()...)
	ps = append(ps, s.dst.To4()...)
	ps = append(ps, 0, 6)
	var l [2]byte
	binary.BigEndian.PutUint16(l[:], uint16(len(tcp)))
	ps = append(ps, l[:]...)
	ps = append(ps, tcp...)
	binary.BigEndian.PutUint16(tcp[16:18], checksum(ps))

	ip := []byte{0x45, 0, 0, byte(20 + len(tcp)), 0, 1, 0x40, 0, 64, 6, 0, 0}
	ip = append(ip, s.src.To4()...)
	ip = append(ip, s.dst.To4()...)
	binary.BigEndian.PutUint16(ip[10:12], checksum(ip))
	return append(ip, tcp...)
}

func checksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}

// socksRecorder is a local SOCKS5 server that remembers what it was asked to
// connect to; the tunnel's dial of it is the thing under test. Failures are
// recorded too, because a CONNECT that arrives but does not parse would
// otherwise look exactly like one that was never attempted.
type socksRecorder struct {
	ln      net.Listener
	mu      sync.Mutex
	targets []string
	errs    []string
}

func startSOCKSRecorder(t *testing.T) *socksRecorder {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	r := &socksRecorder{ln: ln}
	go r.serve()
	return r
}

func (r *socksRecorder) addr() string { return r.ln.Addr().String() }

func (r *socksRecorder) fail(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func (r *socksRecorder) errors() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.errs...)
}

func (r *socksRecorder) serve() {
	for {
		c, err := r.ln.Accept()
		if err != nil {
			return
		}
		go r.handle(c)
	}
}

func (r *socksRecorder) handle(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))

	// Greeting: version, method count, then that many methods.
	var g [2]byte
	if _, err := io.ReadFull(c, g[:]); err != nil {
		r.fail("greeting: %v", err)
		return
	}
	if _, err := io.ReadFull(c, make([]byte, g[1])); err != nil {
		r.fail("methods: %v", err)
		return
	}
	if _, err := c.Write([]byte{5, 0}); err != nil {
		r.fail("method reply: %v", err)
		return
	}

	// Request: version, command, reserved, address type.
	var head [4]byte
	if _, err := io.ReadFull(c, head[:]); err != nil {
		r.fail("request: %v", err)
		return
	}
	var host string
	switch head[3] {
	case 1:
		var b [4]byte
		if _, err := io.ReadFull(c, b[:]); err != nil {
			r.fail("v4 address: %v", err)
			return
		}
		host = net.IP(b[:]).String()
	case 3:
		var n [1]byte
		if _, err := io.ReadFull(c, n[:]); err != nil {
			r.fail("name length: %v", err)
			return
		}
		b := make([]byte, n[0])
		if _, err := io.ReadFull(c, b); err != nil {
			r.fail("name: %v", err)
			return
		}
		host = string(b)
	default:
		r.fail("unsupported ATYP %d", head[3])
		return
	}
	var pb [2]byte
	if _, err := io.ReadFull(c, pb[:]); err != nil {
		r.fail("port: %v", err)
		return
	}
	if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		r.fail("reply: %v", err)
		return
	}
	port := strconv.Itoa(int(binary.BigEndian.Uint16(pb[:])))
	r.mu.Lock()
	r.targets = append(r.targets, host+":"+port)
	r.mu.Unlock()
	_, _ = io.Copy(io.Discard, c)
}

// targetSeen waits for want to show up in the recorded CONNECTs.
func (r *socksRecorder) targetSeen(want string, d time.Duration) []string {
	deadline := time.Now().Add(d)
	for {
		r.mu.Lock()
		got := append([]string(nil), r.targets...)
		r.mu.Unlock()
		for _, t := range got {
			if t == want {
				return got
			}
		}
		if time.Now().After(deadline) {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// readTCP pulls the next IPv4/TCP packet the stack produced and decodes enough
// of it to check the addressing and the flags.
func readTCP(t *testing.T, dev *fakeDevice) tcpSegment {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case p := <-dev.out:
			if len(p) < 40 || p[0]>>4 != 4 || p[9] != 6 {
				continue
			}
			ihl := int(p[0]&0xf) * 4
			tcp := p[ihl:]
			return tcpSegment{
				src:   net.IP(p[12:16]),
				dst:   net.IP(p[16:20]),
				sport: binary.BigEndian.Uint16(tcp[0:2]),
				dport: binary.BigEndian.Uint16(tcp[2:4]),
				seq:   binary.BigEndian.Uint32(tcp[4:8]),
				ack:   binary.BigEndian.Uint32(tcp[8:12]),
				flags: tcp[13] & 0x3f,
			}
		case <-deadline:
			t.Fatal("the stack produced no packet")
		}
	}
}

// TestTunnelConnectsTheDomainNotTheFakeAddress is the end-to-end of the packet
// path: a SYN to a fake address, seen from the far side of the virtual NIC, has
// to arrive at the engine's inbound as a CONNECT for the domain that address
// stands in for. That mapping is the only reason domain rules still work in TUN
// mode, so it is the behaviour worth pinning down.
func TestTunnelConnectsTheDomainNotTheFakeAddress(t *testing.T) {
	rec := startSOCKSRecorder(t)
	defer rec.ln.Close()

	dev := newFakeDevice()
	sess, err := Start(Config{
		Device:    dev,
		MTU:       1400,
		SOCKS:     rec.addr,
		DNSListen: "127.0.0.1:0",
		AdapterIP: net.IPv4(198, 18, 0, 1),
		Policy:    &testPolicy{proxy: map[string]bool{"proxied.example": true}},
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer sess.Stop()

	// The pool is filled directly rather than through the resolver: what is
	// under test here is the packet path, and the resolver's own decision is
	// covered by the DNS tests above.
	fake := sess.pool.Assign("proxied.example")
	if !isFakeIP(fake) {
		t.Fatalf("pool handed out %s, not a fake address", fake)
	}

	client := net.IPv4(192, 168, 50, 7)
	dev.in <- tcpSegment{
		src: client, dst: fake, sport: 4000, dport: 443,
		seq: 1000, flags: synFlag,
	}.marshal()

	synack := readTCP(t, dev)
	if synack.flags&(synFlag|ackFlag) != synFlag|ackFlag {
		t.Fatalf("first answer is not a SYN-ACK: flags %#x", synack.flags)
	}
	if !synack.src.Equal(fake) || synack.sport != 443 {
		t.Errorf("SYN-ACK came from %s:%d, want %s:443", synack.src, synack.sport, fake)
	}
	if !synack.dst.Equal(client) || synack.dport != 4000 {
		t.Errorf("SYN-ACK addressed %s:%d, want %s:4000", synack.dst, synack.dport, client)
	}

	// The forwarder only hands the connection over once the handshake is done,
	// so the ACK is part of the path under test, not a formality.
	dev.in <- tcpSegment{
		src: client, dst: fake, sport: 4000, dport: 443,
		seq: 1001, ack: synack.seq + 1, flags: ackFlag,
	}.marshal()

	if got := rec.targetSeen("proxied.example:443", 5*time.Second); len(got) == 0 {
		t.Fatalf("the inbound never received a CONNECT, recorder errors: %v", rec.errors())
	} else if got[len(got)-1] != "proxied.example:443" {
		t.Fatalf("inbound saw %v, want a CONNECT for proxied.example:443", got)
	}
}

// TestTunnelDropsUDP documents the deliberate half: only TCP is relayed, so QUIC
// has to fall back rather than leak.
func TestTunnelDropsUDP(t *testing.T) {
	dev := newFakeDevice()
	sess, err := Start(Config{
		Device:    dev,
		MTU:       1400,
		SOCKS:     func() string { return "127.0.0.1:1" },
		DNSListen: "127.0.0.1:0",
		AdapterIP: net.IPv4(198, 18, 0, 1),
		Policy:    &testPolicy{proxy: map[string]bool{}},
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer sess.Stop()

	fake := sess.pool.Assign("udp.example")
	udp := []byte{0x45, 0, 0, 0, 0, 1, 0, 0x40, 64, 17, 0, 0}
	udp = append(udp, net.IPv4(192, 168, 50, 7).To4()...)
	udp = append(udp, fake.To4()...)
	body := []byte{byte(5000 >> 8), byte(5000 & 0xff), 0, 34, 0, 8, 0, 0}
	binary.BigEndian.PutUint16(udp[2:4], uint16(len(udp)+len(body)))
	binary.BigEndian.PutUint16(udp[10:12], checksum(udp))
	dev.in <- append(udp, body...)

	// A dropped flow must not answer anything: no ICMP, no reply, silence.
	select {
	case p := <-dev.out:
		t.Errorf("UDP produced a packet back (%d bytes), want it dropped", len(p))
	case <-time.After(500 * time.Millisecond):
	}
}
