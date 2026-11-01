// Package ws is a minimal RFC 6455 WebSocket server implementation (text
// frames only), standard library only. It exists so realtime updates need no
// extra dependency; it implements exactly what the frontend's realtime client
// uses: a text protocol with small JSON messages.
//
// Security properties:
//   - the Origin header is checked against an allow-list at upgrade time
//     (browsers send cookies on WebSocket handshakes, so without this any
//     website could open an authenticated socket: cross-site WebSocket hijacking);
//   - client frames must be masked, RSV bits must be zero, binary frames are refused;
//   - every frame and every reassembled message has a hard size limit;
//   - reads and writes have deadlines, so dead peers cannot pin goroutines.
package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const guid = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const (
	opCont  = 0x0
	opText  = 0x1
	opBin   = 0x2
	opClose = 0x8
	opPing  = 0x9
	opPong  = 0xA
)

// Close codes used here.
const (
	CloseNormal      = 1000
	CloseProtocol    = 1002
	CloseUnsupported = 1003
	ClosePolicy      = 1008
	CloseTooBig      = 1009
)

var (
	ErrBadHandshake = errors.New("ws: bad handshake")
	ErrOrigin       = errors.New("ws: origin not allowed")
	errProtocol     = errors.New("ws: protocol error")
	errTooBig       = errors.New("ws: message too large")
)

// AcceptKey computes Sec-WebSocket-Accept for a client key (RFC 6455 section 4.2.2).
func AcceptKey(key string) string {
	h := sha1.Sum([]byte(key + guid))
	return base64.StdEncoding.EncodeToString(h[:])
}

func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, p := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(p), token) {
				return true
			}
		}
	}
	return false
}

// OriginAllowed reports whether origin is in the allow-list. A missing Origin
// (non-browser clients) is allowed: those cannot be tricked into carrying a
// victim's cookies.
func OriginAllowed(origin string, allowed []string) bool {
	if origin == "" {
		return true
	}
	for _, a := range allowed {
		if strings.EqualFold(a, origin) {
			return true
		}
	}
	return false
}

// Conn is one WebSocket connection. WriteText/WriteClose are safe for concurrent use; ReadText is not.
type Conn struct {
	c       net.Conn
	br      *bufio.Reader
	wmu     sync.Mutex
	maxMsg  int64
	closed  bool
	readTTL time.Duration
}

// Options tune a connection.
type Options struct {
	MaxMessage int64         // largest accepted message in bytes (default 4096)
	ReadTTL    time.Duration // max silence before the peer is considered dead (default 90s)
}

// Upgrade performs the server handshake. On error nothing has been written
// except (for handshake errors) an HTTP error response.
func Upgrade(w http.ResponseWriter, r *http.Request, allowedOrigins []string, opt Options) (*Conn, error) {
	fail := func(status int, err error) (*Conn, error) {
		http.Error(w, http.StatusText(status), status)
		return nil, err
	}
	if r.Method != http.MethodGet {
		return fail(http.StatusMethodNotAllowed, ErrBadHandshake)
	}
	if !headerHasToken(r.Header, "Connection", "upgrade") || !headerHasToken(r.Header, "Upgrade", "websocket") {
		return fail(http.StatusBadRequest, ErrBadHandshake)
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		return fail(http.StatusUpgradeRequired, ErrBadHandshake)
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if raw, err := base64.StdEncoding.DecodeString(key); err != nil || len(raw) != 16 {
		return fail(http.StatusBadRequest, ErrBadHandshake)
	}
	if !OriginAllowed(r.Header.Get("Origin"), allowedOrigins) {
		return fail(http.StatusForbidden, ErrOrigin)
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return fail(http.StatusInternalServerError, ErrBadHandshake)
	}
	nc, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	// The http.Server read/write deadlines no longer apply; clear them.
	_ = nc.SetDeadline(time.Time{})
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + AcceptKey(key) + "\r\n\r\n"
	_ = nc.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := rw.WriteString(resp); err != nil {
		nc.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		nc.Close()
		return nil, err
	}
	_ = nc.SetWriteDeadline(time.Time{})
	if opt.MaxMessage <= 0 {
		opt.MaxMessage = 4096
	}
	if opt.ReadTTL <= 0 {
		opt.ReadTTL = 90 * time.Second
	}
	return &Conn{c: nc, br: rw.Reader, maxMsg: opt.MaxMessage, readTTL: opt.ReadTTL}, nil
}

func (c *Conn) writeFrame(op byte, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed && op != opClose {
		return net.ErrClosed
	}
	var hdr [10]byte
	hdr[0] = 0x80 | op
	n := 2
	switch l := len(payload); {
	case l < 126:
		hdr[1] = byte(l)
	case l <= 0xFFFF:
		hdr[1] = 126
		binary.BigEndian.PutUint16(hdr[2:], uint16(l))
		n = 4
	default:
		hdr[1] = 127
		binary.BigEndian.PutUint64(hdr[2:], uint64(l))
		n = 10
	}
	_ = c.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.c.Write(hdr[:n]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := c.c.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// WriteText sends one text message.
func (c *Conn) WriteText(b []byte) error { return c.writeFrame(opText, b) }

// WritePing sends a ping (keeps NAT/proxy idle timers alive and detects dead peers).
func (c *Conn) WritePing() error { return c.writeFrame(opPing, nil) }

// CloseWith sends a close frame with a status code and closes the socket.
func (c *Conn) CloseWith(code int) {
	p := make([]byte, 2)
	binary.BigEndian.PutUint16(p, uint16(code))
	_ = c.writeFrame(opClose, p)
	c.Close()
}

// Close closes the underlying connection.
func (c *Conn) Close() {
	c.wmu.Lock()
	c.closed = true
	c.wmu.Unlock()
	_ = c.c.Close()
}

// ReadText returns the next text message. Pings are answered, pongs ignored.
// It returns io.EOF when the peer closes. Protocol violations close the
// connection with the proper status code and return an error.
func (c *Conn) ReadText() ([]byte, error) {
	var msg []byte
	started := false
	for {
		_ = c.c.SetReadDeadline(time.Now().Add(c.readTTL))
		fin, op, payload, err := c.readFrame()
		if err != nil {
			switch {
			case errors.Is(err, errTooBig):
				c.CloseWith(CloseTooBig)
			case errors.Is(err, errProtocol):
				c.CloseWith(CloseProtocol)
			}
			return nil, err
		}
		switch op {
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil {
				return nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			c.CloseWith(CloseNormal)
			return nil, io.EOF
		case opBin:
			c.CloseWith(CloseUnsupported)
			return nil, errProtocol
		case opText:
			if started {
				c.CloseWith(CloseProtocol)
				return nil, errProtocol
			}
			started = true
			msg = append(msg[:0], payload...)
		case opCont:
			if !started {
				c.CloseWith(CloseProtocol)
				return nil, errProtocol
			}
			if int64(len(msg)+len(payload)) > c.maxMsg {
				c.CloseWith(CloseTooBig)
				return nil, errTooBig
			}
			msg = append(msg, payload...)
		default:
			c.CloseWith(CloseProtocol)
			return nil, errProtocol
		}
		if fin {
			if !utf8.Valid(msg) {
				c.CloseWith(1007)
				return nil, errProtocol
			}
			return msg, nil
		}
	}
}

func (c *Conn) readFrame() (fin bool, op byte, payload []byte, err error) {
	var h [2]byte
	if _, err = io.ReadFull(c.br, h[:]); err != nil {
		return
	}
	fin = h[0]&0x80 != 0
	if h[0]&0x70 != 0 { // RSV1-3: no extensions negotiated
		return false, 0, nil, errProtocol
	}
	op = h[0] & 0x0F
	if h[1]&0x80 == 0 { // clients MUST mask
		return false, 0, nil, errProtocol
	}
	n := int64(h[1] & 0x7F)
	switch n {
	case 126:
		var b [2]byte
		if _, err = io.ReadFull(c.br, b[:]); err != nil {
			return
		}
		n = int64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err = io.ReadFull(c.br, b[:]); err != nil {
			return
		}
		u := binary.BigEndian.Uint64(b[:])
		if u > 1<<62 {
			return false, 0, nil, errTooBig
		}
		n = int64(u)
	}
	isControl := op >= 0x8
	if isControl && (n > 125 || !fin) {
		return false, 0, nil, errProtocol
	}
	if n > c.maxMsg {
		return false, 0, nil, errTooBig
	}
	var mask [4]byte
	if _, err = io.ReadFull(c.br, mask[:]); err != nil {
		return
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return
	}
	for i := range payload {
		payload[i] ^= mask[i&3]
	}
	return
}
