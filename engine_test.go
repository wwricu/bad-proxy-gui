package main

import (
	"net"
	"testing"
	"time"
)

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

func canDial(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// TestEngineLifecycle proves the GUI's in-process embedding path end to end
// without a reachable upstream: a valid node config builds, core.New accepts it,
// Start binds the two loopback listeners, and Stop releases them.
func TestEngineLifecycle(t *testing.T) {
	socks, http := freePort(t), freePort(t)
	s := GUIState{
		SocksPort: socks,
		HttpPort:  http,
		Active:    0,
		Nodes: []Node{{
			Name: "test", Protocol: "vless",
			Secret: "11111111-2222-4333-8444-555555555555",
			Server: "127.0.0.1", Port: "1", Transport: "tcp",
		}},
	}

	var e engine
	if err := e.start(s); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !e.running() {
		t.Fatal("engine should report running after start")
	}

	socksAddr := net.JoinHostPort("127.0.0.1", socks)
	httpAddr := net.JoinHostPort("127.0.0.1", http)
	waitFor(socksAddr, true)
	waitFor(httpAddr, true)
	if !canDial(socksAddr) {
		t.Errorf("socks listener %s not up", socksAddr)
	}
	if !canDial(httpAddr) {
		t.Errorf("http listener %s not up", httpAddr)
	}

	e.stop()
	if e.running() {
		t.Fatal("engine should report stopped after stop")
	}
	waitFor(socksAddr, false)
	if canDial(socksAddr) {
		t.Errorf("socks port %s still accepting after stop", socksAddr)
	}
}

// start is async (goroutine per inbound); poll the port until it reaches the
// want state, so the test is not a fixed-sleep race.
func waitFor(addr string, want bool) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if canDial(addr) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestNoActiveNodeRejects(t *testing.T) {
	var e engine
	s := GUIState{SocksPort: "10808", HttpPort: "10809", Active: -1}
	if err := e.start(s); err == nil {
		t.Fatal("expected start to fail with no active node")
	}
}
