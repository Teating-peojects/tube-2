package ws

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAcceptKeyMatchesRFCExample(t *testing.T) {
	// RFC 6455 section 1.3
	if got := AcceptKey("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("got %s", got)
	}
}

// ---- a tiny test client ----

type tc struct {
	c  net.Conn
	br *bufio.Reader
}

func dial(t *testing.T, srv *httptest.Server, origin string) (*tc, *http.Response) {
	t.Helper()
	c, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	key := make([]byte, 16)
	rand.Read(key)
	k := base64.StdEncoding.EncodeToString(key)
	req := "GET /ws HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: keep-alive, Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + k + "\r\n"
	if origin != "" {
		req += "Origin: " + origin + "\r\n"
	}
	io.WriteString(c, req+"\r\n")
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == 101 && resp.Header.Get("Sec-WebSocket-Accept") != AcceptKey(k) {
		t.Fatal("bad accept key")
	}
	return &tc{c: c, br: br}, resp
}

func (c *tc) send(op byte, payload []byte, mask bool, fin bool) {
	h := []byte{op}
	if fin {
		h[0] |= 0x80
	}
	mb := byte(0)
	if mask {
		mb = 0x80
	}
	switch l := len(payload); {
	case l < 126:
		h = append(h, mb|byte(l))
	case l <= 0xFFFF:
		h = append(h, mb|126, 0, 0)
		binary.BigEndian.PutUint16(h[2:], uint16(l))
	default:
		h = append(h, mb|127, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.BigEndian.PutUint64(h[2:], uint64(l))
	}
	body := append([]byte(nil), payload...)
	if mask {
		m := []byte{1, 2, 3, 4}
		h = append(h, m...)
		for i := range body {
			body[i] ^= m[i&3]
		}
	}
	c.c.Write(append(h, body...))
}

func (c *tc) read(t *testing.T) (op byte, payload []byte) {
	t.Helper()
	c.c.SetReadDeadline(time.Now().Add(3 * time.Second))
	var h [2]byte
	if _, err := io.ReadFull(c.br, h[:]); err != nil {
		t.Fatalf("read: %v", err)
	}
	op = h[0] & 0x0F
	n := int(h[1] & 0x7F)
	if n == 126 {
		var b [2]byte
		io.ReadFull(c.br, b[:])
		n = int(binary.BigEndian.Uint16(b[:]))
	}
	payload = make([]byte, n)
	io.ReadFull(c.br, payload)
	return
}

func echoServer(t *testing.T, origins []string, opt Options) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := Upgrade(w, r, origins, opt)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			m, err := conn.ReadText()
			if err != nil {
				return
			}
			conn.WriteText(append([]byte("echo:"), m...))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEchoPingAndFragmentation(t *testing.T) {
	srv := echoServer(t, []string{"https://site.example"}, Options{})
	c, resp := dial(t, srv, "https://site.example")
	if resp.StatusCode != 101 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	c.send(opText, []byte(`{"action":"subscribe"}`), true, true)
	if op, p := c.read(t); op != opText || string(p) != `echo:{"action":"subscribe"}` {
		t.Fatalf("got %d %q", op, p)
	}
	c.send(opPing, []byte("hi"), true, true)
	if op, p := c.read(t); op != opPong || string(p) != "hi" {
		t.Fatalf("want pong, got %d %q", op, p)
	}
	// fragmented text message: "he" + "llo"
	c.send(opText, []byte("he"), true, false)
	c.send(opCont, []byte("llo"), true, true)
	if op, p := c.read(t); op != opText || string(p) != "echo:hello" {
		t.Fatalf("got %d %q", op, p)
	}
	// a medium frame (16-bit length)
	big := strings.Repeat("a", 300)
	c.send(opText, []byte(big), true, true)
	if _, p := c.read(t); string(p) != "echo:"+big {
		t.Fatal("medium frame mismatch")
	}
	c.send(opClose, []byte{0x03, 0xe8}, true, true)
	if op, _ := c.read(t); op != opClose {
		t.Fatalf("want close, got %d", op)
	}
}

func TestOriginIsEnforced(t *testing.T) {
	srv := echoServer(t, []string{"https://site.example"}, Options{})
	_, resp := dial(t, srv, "https://evil.example")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site origin must be rejected, got %d", resp.StatusCode)
	}
	_, resp = dial(t, srv, "") // non-browser client
	if resp.StatusCode != 101 {
		t.Fatalf("missing Origin should be allowed, got %d", resp.StatusCode)
	}
}

func TestBadHandshakes(t *testing.T) {
	srv := echoServer(t, nil, Options{})
	for name, req := range map[string]string{
		"no upgrade": "GET /ws HTTP/1.1\r\nHost: x\r\n\r\n",
		"bad key":    "GET /ws HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: short\r\n\r\n",
		"version 8":  "GET /ws HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 8\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n",
		"post":       "POST /ws HTTP/1.1\r\nHost: x\r\nContent-Length: 0\r\n\r\n",
	} {
		c, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(c, req)
		resp, err := http.ReadResponse(bufio.NewReader(c), nil)
		c.Close()
		if err != nil || resp.StatusCode == 101 {
			t.Errorf("%s: expected an HTTP error, got %v %v", name, resp, err)
		}
	}
}

func TestProtocolViolationsCloseTheSocket(t *testing.T) {
	srv := echoServer(t, nil, Options{MaxMessage: 64})
	cases := map[string]func(c *tc){
		"unmasked client frame": func(c *tc) { c.send(opText, []byte("x"), false, true) },
		"binary frame":          func(c *tc) { c.send(opBin, []byte("x"), true, true) },
		"oversize frame":        func(c *tc) { c.send(opText, []byte(strings.Repeat("a", 200)), true, true) },
		"orphan continuation":   func(c *tc) { c.send(opCont, []byte("x"), true, true) },
		"fragmented ping":       func(c *tc) { c.send(opPing, []byte("x"), true, false) },
	}
	for name, f := range cases {
		c, resp := dial(t, srv, "")
		if resp.StatusCode != 101 {
			t.Fatal(resp.StatusCode)
		}
		f(c)
		if op, _ := c.read(t); op != opClose {
			t.Errorf("%s: expected a close frame, got opcode %d", name, op)
		}
	}
}

func TestHubDeliversOnlyToSubscribedSocketsOfThatUser(t *testing.T) {
	// server: upgrade, register in hub, subscribe via first message, run write loop
	hub := NewHub()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := Upgrade(w, r, nil, Options{})
		if err != nil {
			return
		}
		cl := NewClient(r.URL.Query().Get("u"), conn)
		if hub.Add(cl) != nil {
			conn.CloseWith(ClosePolicy)
			return
		}
		defer hub.Remove(cl)
		go cl.WriteLoop(time.Hour)
		defer cl.Close()
		for {
			m, err := conn.ReadText()
			if err != nil {
				return
			}
			var in struct{ Action, Topic string }
			if json.Unmarshal(m, &in) == nil && in.Action == "subscribe" {
				cl.Subscribe(in.Topic)
			}
		}
	}))
	t.Cleanup(srv.Close)

	connect := func(user string) *tc {
		c, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		io.WriteString(c, "GET /ws?u="+user+" HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n")
		br := bufio.NewReader(c)
		if resp, err := http.ReadResponse(br, nil); err != nil || resp.StatusCode != 101 {
			t.Fatalf("handshake: %v %v", resp, err)
		}
		return &tc{c: c, br: br}
	}
	alice1, alice2, bob := connect("alice"), connect("alice"), connect("bob")
	alice1.send(opText, []byte(`{"action":"subscribe","topic":"notifications"}`), true, true)
	bob.send(opText, []byte(`{"action":"subscribe","topic":"notifications"}`), true, true)
	alice2.send(opText, []byte(`{"action":"subscribe","topic":"video:1"}`), true, true)
	time.Sleep(200 * time.Millisecond)

	hub.Deliver("alice", "notifications", json.RawMessage(`{"n":1}`))
	op, p := alice1.read(t)
	if op != opText || string(p) != `{"topic":"notifications","data":{"n":1}}` {
		t.Fatalf("alice1 got %d %q", op, p)
	}
	// alice2 subscribed to a different topic and bob is a different user: neither may receive anything
	for name, c := range map[string]*tc{"alice2": alice2, "bob": bob} {
		c.c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		var b [1]byte
		if _, err := c.br.Read(b[:]); err == nil {
			t.Errorf("%s must not receive another topic's or user's event", name)
		}
	}
	if hub.Count() != 3 {
		t.Fatalf("count = %d", hub.Count())
	}
}

func TestHubCapsConnectionsPerUser(t *testing.T) {
	hub := NewHub()
	var clients []*Client
	for i := 0; i < MaxConnsPerUser; i++ {
		c := NewClient("u", &Conn{})
		if err := hub.Add(c); err != nil {
			t.Fatal(err)
		}
		clients = append(clients, c)
	}
	if err := hub.Add(NewClient("u", &Conn{})); err != ErrTooManyConns {
		t.Fatalf("want ErrTooManyConns, got %v", err)
	}
	if err := hub.Add(NewClient("other", &Conn{})); err != nil {
		t.Fatal("another user is unaffected")
	}
	hub.Remove(clients[0])
	if err := hub.Add(NewClient("u", &Conn{})); err != nil {
		t.Fatalf("a slot should be free again: %v", err)
	}
}

func TestSubscribeLimit(t *testing.T) {
	c := NewClient("u", &Conn{})
	for i := 0; i < MaxTopicsPerConn; i++ {
		if !c.Subscribe("t" + string(rune('a'+i))) {
			t.Fatal("under the limit")
		}
	}
	if c.Subscribe("one-too-many") {
		t.Fatal("limit must hold")
	}
	if !c.Subscribe("ta") { // already subscribed: idempotent
		t.Fatal("re-subscribing must succeed")
	}
}
