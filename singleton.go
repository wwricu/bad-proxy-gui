package main

import (
	"net"
	"time"
)

// singletonAddr is a loopback-only lock. The first instance binds it; a second
// launch fails to bind, pokes the running one to surface its window, then exits.
// Loopback binding does not trigger a firewall prompt.
const singletonAddr = "127.0.0.1:47821"

// acquireSingleton returns a live listener when this process is the primary
// instance (ok=true). When another instance already holds the lock, it signals
// that instance to show its window and returns ok=false.
func acquireSingleton() (net.Listener, bool) {
	ln, err := net.Listen("tcp", singletonAddr)
	if err == nil {
		return ln, true
	}
	conn, derr := net.Dial("tcp", singletonAddr)
	if derr == nil {
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		_, _ = conn.Write([]byte("show"))
		_ = conn.Close()
	}
	return nil, false
}
