//go:build !race

package engine

import (
	"testing"

	"github.com/wwricu/bad-proxy-gui/internal/config"
)

// A binding call runs on a goroutine of its own, so two clicks that both move the
// engine really do overlap. Queued, each restart sees the proxy the one before
// it left; interleaved, one meets "already running" from the proxy this app
// owns and reports a restart failure the user did nothing wrong to earn.
//
// It is built only without -race because the second core.New cannot be made safe
// from this side: core v1.2.0-beta-3 configures its logger through package
// globals, so building a proxy while another is still logging races on them, and
// a running inbound logs on the same clock as every restart. The engine here is
// correct either way - the race is core's, and core already publishes its sinks
// as an immutable snapshot on its release branch. Re-run this under -race once
// go.mod moves past beta-3.
func TestConcurrentRestartsQueue(t *testing.T) {
	httpPort, socksPort, err := freePortPair()
	if err != nil {
		t.Fatal(err)
	}
	st := config.DefaultState()
	st.HttpPort, st.SocksPort = httpPort, socksPort

	var e Engine
	const clicks = 4
	errs := make(chan error, clicks)
	done := make(chan struct{})
	go func() { // the bar asks whether the proxy is up while the clicks land
		for {
			select {
			case <-done:
				return
			default:
				e.Running()
			}
		}
	}()
	for i := 0; i < clicks; i++ {
		go func() { errs <- e.Restart(st, "") }()
	}
	for i := 0; i < clicks; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent restart %d failed: %v", i, err)
		}
	}
	close(done)
	e.Stop()
}
