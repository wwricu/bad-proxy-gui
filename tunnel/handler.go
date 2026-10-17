package tunnel

import (
	"io"
	"net"
	"sync"

	"github.com/wwricu/bad-proxy-core/logger"
	"github.com/xjasonlyu/tun2socks/v2/core/adapter"
)

// handler turns netstack connections into local SOCKS5 calls through the
// engine's own inbound. Everything that reaches it is addressed to a fake IP,
// so the pool lookup restores the domain and the engine can still apply its
// domain rules to the connection.
//
// socks is resolved per dial because the engine is restarted under a running
// tunnel whenever the user edits a port; a fixed address would send every
// connection to a port that no longer listens.
type handler struct {
	socks func() string
	pool  *fakePool
}

func (h *handler) HandleTCP(c adapter.TCPConn) {
	id := c.ID()
	dst := net.IP(id.LocalAddress.AsSlice())
	port := id.LocalPort
	target := dst.String()
	if name, ok := h.pool.Lookup(dst); ok {
		target = name
	}
	go func() {
		defer c.Close()
		logger.Debugf("tunnel: tcp %s:%d -> %s", id.RemoteAddress, id.RemotePort, target)
		up, err := socksDial(h.socks(), target, port)
		if err != nil {
			logger.Debugf("tunnel: tcp %s dial failed: %v", target, err)
			return
		}
		defer up.Close()
		pump(c, up)
	}()
}

// HandleUDP drops the flow. Only the fake range is routed into the tunnel and
// the engine's SOCKS inbound has no UDP association, so there is nothing to
// relay a datagram to. Dropping QUIC is the point: apps fall back to TCP,
// which is proxied.
func (h *handler) HandleUDP(c adapter.UDPConn) {
	logger.Debugf("tunnel: udp dropped %s:%d", c.ID().LocalAddress, c.ID().LocalPort)
	c.Close()
}

// pump copies both directions and half-closes each side when the other is
// done, so FIN propagates like a real proxy would.
func pump(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go copyHalf(b, a, &wg)
	go copyHalf(a, b, &wg)
	wg.Wait()
}

func copyHalf(dst, src net.Conn, wg *sync.WaitGroup) {
	defer wg.Done()
	_, _ = io.Copy(dst, src)
	if cw, ok := dst.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	} else {
		_ = dst.Close()
	}
}
