// Package tunnel runs the system's captured traffic through the embedded
// engine: a virtual NIC hands raw IP packets to a gvisor netstack, which turns
// them into SOCKS5 connections against the app's own loopback inbound.
//
// Only the 198.18.0.0/16 fake-address block is routed into the tunnel, and the
// DNS server decides which names land in it. That keeps the tunnel out of the
// default route, which is what makes split routing work here: a name the rules
// send direct keeps its real address and never reaches the stack at all, so
// the engine is never asked to dial something it would have to route to itself.
package tunnel

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"

	"github.com/wwricu/bad-proxy-core/logger"
	tunsocks "github.com/xjasonlyu/tun2socks/v2/core"
	"golang.zx2c4.com/wireguard/tun"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	gstack "gvisor.dev/gvisor/pkg/tcpip/stack"
)

// packetOffset is the headroom a tun.Device write buffer must leave in front of
// the packet: utun on macOS prepends a 4-byte family header there, and the
// other backends ignore it.
const packetOffset = 4

// Config describes one tunnel session. The device is already created and
// configured by the platform layer; this package only moves packets.
type Config struct {
	Device tun.Device
	MTU    int
	// SOCKS reports the engine inbound the stack dials, e.g. 127.0.0.1:10808.
	// It is a function because the engine restarts under a live tunnel
	// whenever a port changes; the session must not freeze the address.
	SOCKS func() string
	// DNSListen is the loopback address the resolver binds, e.g. 127.0.0.1:53.
	DNSListen string
	// Forwarders are the system DNS servers that were in place before the
	// tunnel took over, as bare addresses; loopback ones are dropped.
	Forwarders []string
	// AdapterIP is the tunnel interface's own address. It lives inside the
	// fake block, so the pool must never hand it to a name.
	AdapterIP net.IP
	Policy    Policy
}

// Session is a running tunnel. Start hands it a configured device; Stop
// releases everything it took.
type Session struct {
	cfg   Config
	ep    *channel.Endpoint
	st    *gstack.Stack
	dns   *DNSServer
	pool  *fakePool
	done  chan struct{}
	wg    sync.WaitGroup
	quit  sync.Once
	mtu   int
	errMu sync.Mutex
	err   error
}

// Start brings up the resolver and the netstack, then pumps both directions.
func Start(cfg Config) (*Session, error) {
	if cfg.Device == nil {
		return nil, errors.New("tunnel: no device")
	}
	if cfg.SOCKS == nil {
		return nil, errors.New("tunnel: no SOCKS inbound")
	}
	if cfg.MTU <= 0 || cfg.MTU > 65535-packetOffset {
		cfg.MTU = 1500
	}
	pool := newFakePool()
	pool.Reserve(cfg.AdapterIP)
	policy := cfg.Policy
	if policy == nil {
		policy = proxyAll{}
	}
	dns, err := StartDNSServer(cfg.DNSListen, policy, pool, cfg.Forwarders)
	if err != nil {
		return nil, err
	}
	ep := channel.New(1024, uint32(cfg.MTU), "")
	st, err := tunsocks.CreateStack(&tunsocks.Config{
		LinkEndpoint:     ep,
		TransportHandler: &handler{socks: cfg.SOCKS, pool: pool},
	})
	if err != nil {
		dns.Close()
		cfg.Device.Close()
		return nil, err
	}
	s := &Session{
		cfg:  cfg,
		ep:   ep,
		st:   st,
		dns:  dns,
		pool: pool,
		done: make(chan struct{}),
		mtu:  cfg.MTU,
	}
	s.wg.Add(2)
	go s.deviceToStack()
	go s.stackToDevice()
	logger.Debugf("tunnel: up, socks %s, dns %s, mtu %d", cfg.SOCKS(), cfg.DNSListen, cfg.MTU)
	return s, nil
}

// Stop tears the session down. Safe to call more than once.
func (s *Session) Stop() {
	s.quit.Do(func() {
		close(s.done)
		s.dns.Close()
		s.ep.Close()
		s.st.Close()
		s.st.Wait()
		s.cfg.Device.Close()
	})
	s.wg.Wait()
}

// deviceToStack reads packets off the virtual NIC and feeds them to the stack.
func (s *Session) deviceToStack() {
	defer s.wg.Done()
	bufs := [][]byte{make([]byte, packetOffset+s.mtu)}
	sizes := []int{0}
	for {
		n, err := s.cfg.Device.Read(bufs, sizes, packetOffset)
		if err != nil {
			if errors.Is(err, os.ErrClosed) {
				return
			}
			select {
			case <-s.done:
				return
			default:
			}
			logger.Debugf("tunnel: device read failed: %v", err)
			continue
		}
		for i := 0; i < n; i++ {
			pkt := bufs[i][packetOffset : packetOffset+sizes[i]]
			if len(pkt) < 20 {
				continue
			}
			// The device owns bufs[i] and reuses it on the next Read, so the
			// stack gets a copy it can hold on to.
			data := make([]byte, len(pkt))
			copy(data, pkt)
			pkb := gstack.NewPacketBuffer(gstack.PacketBufferOptions{
				Payload: buffer.MakeWithData(data),
			})
			switch data[0] >> 4 {
			case 4:
				s.ep.InjectInbound(ipv4.ProtocolNumber, pkb)
			case 6:
				s.ep.InjectInbound(ipv6.ProtocolNumber, pkb)
			}
			pkb.DecRef()
		}
	}
}

// stackToDevice sends the stack's outbound packets back to the virtual NIC.
func (s *Session) stackToDevice() {
	defer s.wg.Done()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-s.done:
			cancel()
		case <-ctx.Done():
		}
	}()
	for {
		pkt := s.ep.ReadContext(ctx)
		if pkt == nil {
			return
		}
		view := pkt.ToView()
		buf := make([]byte, packetOffset+view.Size())
		n, err := view.Read(buf[packetOffset:])
		view.Release()
		pkt.DecRef()
		if err != nil || n <= 0 {
			return
		}
		if _, err := s.cfg.Device.Write([][]byte{buf[:packetOffset+n]}, packetOffset); err != nil {
			select {
			case <-s.done:
			default:
				logger.Debugf("tunnel: device write failed: %v", err)
			}
			return
		}
	}
}

// proxyAll is the fallback policy: capture every name. Used only when the
// caller has no routing rules, which is the global-mode case.
type proxyAll struct{}

func (proxyAll) Proxy(string) bool { return true }
