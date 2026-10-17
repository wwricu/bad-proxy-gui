package tunnel

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
)

// socksDial performs a no-auth SOCKS5 CONNECT to target:port through the local
// engine inbound. Domains are sent as ATYP=3 so the router can apply domain
// rules; the fake address has already been turned back into the name.
func socksDial(proxyAddr, target string, port uint16) (net.Conn, error) {
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		conn.Close()
		return nil, err
	}
	b := make([]byte, 2)
	if _, err := readFull(conn, b); err != nil {
		conn.Close()
		return nil, err
	}
	if b[0] != 5 || b[1] != 0 {
		conn.Close()
		return nil, errors.New("socks5 handshake refused")
	}
	req := []byte{5, 1, 0}
	if ip := net.ParseIP(target); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			req = append(req, 1)
			req = append(req, v4...)
		} else {
			req = append(req, 4)
			req = append(req, ip.To16()...)
		}
	} else {
		if len(target) > 255 {
			conn.Close()
			return nil, errors.New("target name too long")
		}
		req = append(req, 3, byte(len(target)))
		req = append(req, target...)
	}
	var pb [2]byte
	binary.BigEndian.PutUint16(pb[:], port)
	req = append(req, pb[:]...)
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}
	reply := make([]byte, 10)
	if _, err := readFull(conn, reply); err != nil {
		conn.Close()
		return nil, err
	}
	if reply[0] != 5 || reply[1] != 0 {
		conn.Close()
		return nil, fmt.Errorf("socks5 connect refused: %d", reply[1])
	}
	return conn, nil
}

func readFull(c net.Conn, b []byte) (int, error) {
	total := 0
	for total < len(b) {
		n, err := c.Read(b[total:])
		if n <= 0 {
			if err == nil {
				err = io.EOF
			}
			return total, err
		}
		total += n
	}
	return total, nil
}
