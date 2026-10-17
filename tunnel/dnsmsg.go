package tunnel

import (
	"encoding/binary"
	"strings"
)

// These are the handful of DNS wire-format routines the tunnel needs: read one
// question out of a query, and build a reply for it. Answers are always small
// (one A record at most), so truncation and TCP fallback never come up here.
const (
	typeA    = 1
	typeAAAA = 28
)

// parseQuestion returns the offset just past the question section, the lower
// cased name, and its query type. Compression pointers do not appear in the
// question section of a stub-resolver query, so seeing one means bail out.
func parseQuestion(query []byte) (end int, name string, qtype uint16, ok bool) {
	if len(query) < 13 || binary.BigEndian.Uint16(query[4:6]) == 0 {
		return 0, "", 0, false
	}
	i := 12
	var labels []string
	for {
		if i >= len(query) {
			return 0, "", 0, false
		}
		l := int(query[i])
		if l == 0 {
			i++
			break
		}
		if l&0xC0 != 0 {
			return 0, "", 0, false
		}
		if i+1+l > len(query) {
			return 0, "", 0, false
		}
		labels = append(labels, string(query[i+1:i+1+l]))
		i += 1 + l
	}
	if i+4 > len(query) {
		return 0, "", 0, false
	}
	qtype = binary.BigEndian.Uint16(query[i : i+2])
	return i + 4, strings.ToLower(strings.Join(labels, ".")), qtype, true
}

// dnsQR, dnsRA and dnsRDOffset are the flag bits of a reply's 16-bit flags
// field. RD is bit 0 of the *first* byte (0x0100), not of the second: putting
// it anywhere lower lands inside the 4-bit rcode and silently turns NOERROR
// into FORMERR.
const (
	dnsQR       = 0x8000
	dnsRA       = 0x0080
	dnsRDOffset = 8
)

// captureAnswer replies to a query for a name that the tunnel owns. An A query
// gets the fake address standing in for the name; AAAA gets a well-formed
// empty NOERROR, which is what pushes apps onto IPv4 (and therefore onto the
// fake address) instead of leaving them stalled on a missing v6 route.
func captureAnswer(query []byte, end int, name string, qtype uint16, p *fakePool) []byte {
	flags := dnsQR | dnsRA | uint16(query[2]&0x01)<<dnsRDOffset
	var answers []byte
	nanswers := 0
	if qtype == typeA {
		ip := p.Assign(name)
		answers = answerRecord(typeA, []byte{ip[12], ip[13], ip[14], ip[15]})
		nanswers = 1
	}
	return assemble(query, end, flags, nanswers, answers)
}

func assemble(query []byte, end int, flags uint16, nanswers int, answers []byte) []byte {
	out := make([]byte, 0, end+len(answers))
	head := make([]byte, 12)
	binary.BigEndian.PutUint16(head[0:2], binary.BigEndian.Uint16(query[0:2]))
	binary.BigEndian.PutUint16(head[2:4], flags)
	binary.BigEndian.PutUint16(head[4:6], 1)
	binary.BigEndian.PutUint16(head[6:8], uint16(nanswers))
	out = append(out, head...)
	out = append(out, query[12:end]...)
	return append(out, answers...)
}

// answerRecord emits one RR: name compressed as a pointer to the question at
// offset 12, class IN, ttl 60. The short TTL keeps a stale fake address from
// outliving the mapping table after the tunnel restarts.
func answerRecord(qtype uint16, rdata []byte) []byte {
	b := make([]byte, 10, 12+len(rdata))
	binary.BigEndian.PutUint16(b[0:2], 0xC00C)
	binary.BigEndian.PutUint16(b[2:4], qtype)
	binary.BigEndian.PutUint16(b[4:6], 1)
	binary.BigEndian.PutUint32(b[6:10], 60)
	var rl [2]byte
	binary.BigEndian.PutUint16(rl[:], uint16(len(rdata)))
	b = append(b, rl[:]...)
	return append(b, rdata...)
}
