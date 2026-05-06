package sentinel

import (
	"bufio"
	"errors"
	"net"
)

// errNotTLS indicates the connection's first bytes are not a TLS handshake.
var errNotTLS = errors.New("not a TLS handshake")

// errNoSNI indicates a valid TLS handshake without a server_name extension.
var errNoSNI = errors.New("no SNI in ClientHello")

// peekSNI reads enough of a TLS ClientHello from conn to extract SNI without
// consuming the bytes. It returns the SNI hostname and a connection that
// transparently replays the peeked bytes followed by the rest of the original
// connection. The returned net.Conn must be used in place of conn for any
// further reads.
//
// If the peek fails (not TLS, malformed, or no SNI), an error is returned but
// the returned net.Conn is still usable — callers can fall through to a
// non-SNI-based forwarding path.
func peekSNI(conn net.Conn) (string, net.Conn, error) {
	br := bufio.NewReaderSize(conn, 16389) // max TLS record (5 hdr + 16384 body)
	wrapped := &peekConn{Conn: conn, r: br}

	// Peek the 5-byte record header to learn record length.
	hdr, err := br.Peek(5)
	if err != nil || len(hdr) < 5 {
		return "", wrapped, errNotTLS
	}
	if hdr[0] != 0x16 { // TLS handshake content type
		return "", wrapped, errNotTLS
	}
	recLen := int(hdr[3])<<8 | int(hdr[4])
	total := 5 + recLen
	if total < 5 || total > 16389 {
		return "", wrapped, errNotTLS
	}

	full, err := br.Peek(total)
	if err != nil || len(full) < total {
		return "", wrapped, errNotTLS
	}

	sni, err := extractSNI(full)
	return sni, wrapped, err
}

// extractSNI parses a TLS ClientHello (record-framed, starting at byte 0)
// and returns the SNI host_name extension value. It performs strict bounds
// checks at every step so a malformed handshake returns an error rather than
// panicking on slice bounds.
func extractSNI(buf []byte) (string, error) {
	// Record header: type(1) + version(2) + length(2)
	if len(buf) < 5 || buf[0] != 0x16 {
		return "", errNotTLS
	}
	recLen := int(buf[3])<<8 | int(buf[4])
	if len(buf) < 5+recLen {
		return "", errors.New("record body truncated")
	}
	body := buf[5 : 5+recLen]

	// Handshake: type(1) + length(3) + body
	if len(body) < 4 || body[0] != 0x01 {
		return "", errors.New("not a ClientHello")
	}
	p := body[4:]

	// ClientHello: legacy_version(2) + random(32) + session_id_length(1)
	if len(p) < 2+32+1 {
		return "", errors.New("ClientHello too short")
	}
	p = p[2+32:] // skip version + random

	sidLen := int(p[0])
	p = p[1:]
	if len(p) < sidLen {
		return "", errors.New("session id truncated")
	}
	p = p[sidLen:]

	if len(p) < 2 {
		return "", errors.New("no cipher suites length")
	}
	csLen := int(p[0])<<8 | int(p[1])
	p = p[2:]
	if len(p) < csLen {
		return "", errors.New("cipher suites truncated")
	}
	p = p[csLen:]

	if len(p) < 1 {
		return "", errors.New("no compression length")
	}
	cmLen := int(p[0])
	p = p[1:]
	if len(p) < cmLen {
		return "", errors.New("compression methods truncated")
	}
	p = p[cmLen:]

	// Extensions
	if len(p) < 2 {
		return "", errNoSNI // no extensions block at all (TLS 1.0 ClientHello)
	}
	extLen := int(p[0])<<8 | int(p[1])
	p = p[2:]
	if len(p) < extLen {
		return "", errors.New("extensions truncated")
	}
	exts := p[:extLen]

	for len(exts) >= 4 {
		extType := int(exts[0])<<8 | int(exts[1])
		extDataLen := int(exts[2])<<8 | int(exts[3])
		if 4+extDataLen > len(exts) {
			return "", errors.New("extension truncated")
		}
		extData := exts[4 : 4+extDataLen]
		exts = exts[4+extDataLen:]
		if extType != 0 {
			continue
		}
		// server_name extension: list_length(2) + entries
		// Each entry: name_type(1) + name_length(2) + name(N)
		if len(extData) < 5 {
			return "", errors.New("server_name extension too short")
		}
		// Skip list_length (2 bytes); we just take the first entry.
		entry := extData[2:]
		if entry[0] != 0 { // host_name type
			return "", errors.New("server_name entry is not host_name")
		}
		nameLen := int(entry[1])<<8 | int(entry[2])
		if 3+nameLen > len(entry) {
			return "", errors.New("server_name truncated")
		}
		return string(entry[3 : 3+nameLen]), nil
	}
	return "", errNoSNI
}

// peekConn wraps a net.Conn so that Read() drains a buffered reader first,
// transparently replaying any bytes that bufio.Reader.Peek consumed from the
// underlying connection. Writes, deadlines, and Close pass through unchanged.
type peekConn struct {
	net.Conn
	r *bufio.Reader
}

func (p *peekConn) Read(b []byte) (int, error) { return p.r.Read(b) }
