package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/wwricu/bad-proxy-core/logger"
	"github.com/wwricu/bad-proxy-core/router"

	"github.com/wwricu/bad-proxy-gui/tunnel"
	"github.com/wwricu/bad-proxy-gui/tunplat"
)

// TUN mode captures traffic at the IP layer instead of relying on apps honouring
// a proxy setting. The design is deliberately fake-IP-only: the adapter owns
// 198.18.0.0/16 and nothing else is routed into it, so the split-routing
// decision moves to resolution time - a name the rules send through the node
// gets a fake address and therefore reaches the tunnel, a name they send direct
// keeps its real answer and leaves through the physical card untouched.
//
// That is also the only loop-free option here. The engine's own outbound
// sockets cannot be bound to a physical interface (the core is used unmodified),
// so a default-route tunnel would send every "direct" dial back into itself.
//
// Consequence to be honest about: TUN is domain-based. A connection made
// straight to an IP address never appears in the fake block and is not proxied,
// exactly like the proxy modes.

const (
	// tunAdapterName is the wintun/utun interface as it shows up in the OS.
	tunAdapterName = "BadProxy"
	// tunRestoreFile holds the token that undoes a tunnel's system changes. It
	// is written before the tunnel is used and deleted after, so a process that
	// died cannot leave the machine's resolver pointing at a dead port.
	tunRestoreFile = "tun-restore.json"
	// tunMTU is the adapter MTU; 1400 leaves room for the tunnel headers.
	tunMTU = 1400
)

// tunPolicy answers "should this name get a fake address?" once per query. It
// reads the same rules.dat lists the engine reads, so the connection the tunnel
// hands over is routed the way the DNS answer already promised.
type tunPolicy struct {
	mu sync.RWMutex
	// all captures every name: global mode, and any mode where the rules could
	// not be loaded - the engine degrades to the same behaviour then.
	all bool
	// rt is the one rule list that decides the split, and invert says whether
	// matching it means direct (cn-whitelist) or proxy (gfw-blacklist).
	rt     *router.Router
	invert bool
	// exempt are names that must keep their real address. The node's own
	// hostname is the important one: if it were faked, the engine's dial of the
	// node would come back into the tunnel.
	exempt map[string]bool
}

func (p *tunPolicy) Proxy(name string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n := strings.ToLower(strings.TrimSuffix(name, "."))
	if !capturableName(n) || p.exempt[n] {
		return false
	}
	if p.all || p.rt == nil {
		return true
	}
	return p.rt.MatchAny(n) != p.invert
}

// load rebuilds the decision from the state the engine was just configured with.
func (p *tunPolicy) load(s GUIState) {
	var (
		rt     *router.Router
		invert bool
		all    = s.RoutingMode != modeGFWBlacklist && s.RoutingMode != modeCNWhitelist
	)
	if !all {
		rules, tag, want := []string{"geolocation-!cn"}, "proxy", false
		if s.RoutingMode == modeCNWhitelist {
			rules, tag, want = []string{"cn"}, "direct", true
		}
		// A router that fails to build leaves rt nil, which means capture
		// everything - the same thing the engine does with no rules loaded.
		if r, err := router.NewRouter(tag, rules, ensureRulesDat()); err == nil {
			rt, invert = r, want
		} else {
			logger.Debugf("tunnel: rules unavailable, capturing every name: %v", err)
		}
	}
	exempt := map[string]bool{}
	if node, err := s.activeNode(); err == nil {
		if host := strings.ToLower(node.Server); host != "" && net.ParseIP(host) == nil {
			exempt[host] = true
		}
	}
	p.mu.Lock()
	p.all, p.rt, p.invert, p.exempt = all, rt, invert, exempt
	p.mu.Unlock()
}

// capturableName keeps names that cannot meaningfully be proxied out of the fake
// block: single-label NetBIOS lookups and the local/reserved suffixes.
func capturableName(n string) bool {
	if n == "" || !strings.Contains(n, ".") {
		return false
	}
	for _, suffix := range []string{".local", ".arpa", ".lan", ".home", ".internal", ".localdomain", ".msdcs"} {
		if strings.HasSuffix(n, suffix) {
			return false
		}
	}
	return true
}

// tunRunner is the live tunnel plus the two values the stack reads on the fly:
// the engine's current SOCKS address and the current capture policy. Both are
// swapped in place instead of restarting the tunnel, because the engine is
// restarted for every setting change and re-creating the adapter would drop
// every connection each time.
type tunRunner struct {
	mu        sync.Mutex
	plat      *tunplat.Platform
	sess      *tunnel.Session
	policy    *tunPolicy
	socks     atomic.Pointer[string]
	tokenPath string
}

func (r *tunRunner) running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sess != nil
}

// up configures the OS, starts the session, and takes ownership of the undo
// token. Any failure after Up leaves the machine exactly as it was found.
func (r *tunRunner) up(s GUIState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sess != nil {
		return nil
	}
	if _, err := s.activeNode(); err != nil {
		return errors.New("TUN 需要先添加并选择一个节点")
	}
	if !tunplat.Supported() {
		return tunplat.ErrUnsupported
	}
	plat, err := tunplat.Up(tunAdapterName, tunMTU)
	if err != nil {
		return err
	}
	policy := &tunPolicy{}
	policy.load(s)
	dir, dirErr := appConfigDir()
	if dirErr == nil {
		r.tokenPath = filepath.Join(dir, tunRestoreFile)
	}
	r.policy, r.plat = policy, plat
	// The token has to be on disk before the resolver goes live: everything
	// after this point is a window in which a crash would break name resolution.
	if r.tokenPath != "" {
		if err := writeTunToken(r.tokenPath, plat.Token); err != nil {
			logger.Warnf("tunnel: could not persist the undo token: %v", err)
		}
	}
	socks := s.socksAddress()
	r.socks.Store(&socks)
	sess, err := tunnel.Start(tunnel.Config{
		Device:     plat.Device,
		MTU:        plat.MTU,
		SOCKS:      func() string { return *r.socks.Load() },
		DNSListen:  plat.DNSListen,
		Forwarders: plat.Forwarders,
		AdapterIP:  plat.AdapterIP,
		Policy:     policy,
	})
	if err != nil {
		_ = tunplat.Recover(plat.Token)
		_ = os.Remove(r.tokenPath)
		plat.Device.Close()
		r.plat, r.tokenPath = nil, ""
		return fmt.Errorf("TUN 启动失败: %w", err)
	}
	r.sess = sess
	logger.Infof("tunnel: TUN up on %s", plat.AdapterName)
	return nil
}

// down restores the system resolver first and only then closes the tunnel: the
// resolver answers for the fake block, so it must outlive the adapter by as much
// as a round trip.
func (r *tunRunner) down() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.plat != nil {
		_ = tunplat.Recover(r.plat.Token)
	}
	if r.sess != nil {
		r.sess.Stop()
	} else if r.plat != nil {
		r.plat.Device.Close()
	}
	r.sess, r.plat, r.policy = nil, nil, nil
	if r.tokenPath != "" {
		_ = os.Remove(r.tokenPath)
		r.tokenPath = ""
	}
}

// refresh re-points the stack after the engine restarted: the port may have
// moved and the routing preset or active node may have changed.
func (r *tunRunner) refresh(s GUIState) {
	r.mu.Lock()
	sess, policy := r.sess, r.policy
	addr := s.socksAddress()
	r.mu.Unlock()
	if sess == nil {
		return
	}
	r.socks.Store(&addr)
	if policy != nil {
		policy.load(s)
	}
}

// socksAddress is the loopback SOCKS inbound the tunnel dials. AllowLAN widens
// the bind to 0.0.0.0, which still answers on loopback.
func (s GUIState) socksAddress() string {
	return "127.0.0.1:" + s.SocksPort
}

func writeTunToken(path string, tok map[string]string) error {
	b, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// recoverTunToken undoes the leftovers of a run that ended without shutdown - a
// crash, a kill, a power loss. It has to run before anything else touches the
// network, because the damage it repairs is a system resolver pointing at a
// port nobody listens on any more.
func (a *App) recoverTunToken() {
	dir, err := appConfigDir()
	if err != nil {
		return
	}
	path := filepath.Join(dir, tunRestoreFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var tok map[string]string
	if err := json.Unmarshal(b, &tok); err != nil {
		logger.Warnf("tunnel: unreadable restore token at %s: %v", path, err)
		return
	}
	if err := tunplat.Recover(tok); err != nil {
		logger.Warnf("tunnel: could not restore the system resolver: %v", err)
		return
	}
	_ = os.Remove(path)
	logger.Infof("tunnel: restored the system DNS settings left behind by a previous run")
}

// SetTun is the bottom-bar switch. Turning it on also leaves direct mode,
// because a tunnel whose engine dials direct carries traffic for nothing.
func (a *App) SetTun(enabled bool) error {
	if enabled {
		if a.st.ProxyMode == modeDirect {
			a.st.ProxyMode = modeProxy
			if err := a.store.save(a.st); err != nil {
				return err
			}
			if err := a.restartEngine(); err != nil {
				return err
			}
		}
		if err := a.tun.up(a.st); err != nil {
			return err
		}
		a.st.TunEnabled = true
		return a.store.save(a.st)
	}
	a.tun.down()
	a.st.TunEnabled = false
	return a.store.save(a.st)
}
