package engine

import (
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/wwricu/bad-proxy-gui/internal/config"
)

// A probe is over when the answer is read, and the connection it opened to the
// local inbound has to be over with it. Go's transport keeps a connection it is
// finished with, waiting for a user - and a client built for one request has no
// next one, and no idle timeout to give up on its own. So every click on Test
// left a socket, and the two goroutines reading it, behind for the life of the
// app; ten nodes probed is ten of them, and the throwaway engine they point at
// has already been stopped.
func TestProbeClosesTheConnectionItOpened(t *testing.T) {
	for _, probe := range []struct {
		name  string
		dial  Probe
		reply string
	}{
		{"the 204 ping", DialThroughProxy, "HTTP/1.1 204 No Content\r\n\r\n"},
		{"the page fetch", DialHTTPThroughProxy, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nhi"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()

			hungUp := make(chan error, 1)
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					hungUp <- err
					return
				}
				defer conn.Close()
				buf := make([]byte, 2048)
				if _, err := conn.Read(buf); err != nil {
					hungUp <- err
					return
				}
				if _, err := conn.Write([]byte(probe.reply)); err != nil {
					hungUp <- err
					return
				}
				// Whatever the probe does next, the answer is already given: this
				// read ends when the client hangs up.
				_, err = conn.Read(buf)
				hungUp <- err
			}()

			port := ln.Addr().(*net.TCPAddr).Port
			if _, err := probe.dial(config.State{HttpPort: strconv.Itoa(port)}); err != nil {
				t.Fatalf("the probe failed against a proxy that answered: %v", err)
			}
			select {
			case err := <-hungUp:
				if !errors.Is(err, io.EOF) {
					t.Errorf("the connection ended with %v, want the client closing it", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("the probe is done and still holds the connection open")
			}
		})
	}
}
