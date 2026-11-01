package ws

import (
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// Limits protect the process from a single user (or a bug) opening unbounded sockets.
const (
	MaxConnsPerUser  = 5
	MaxTopicsPerConn = 20
	sendBuffer       = 32
)

// ErrTooManyConns is returned by Hub.Add when the per-user cap is reached.
var ErrTooManyConns = errors.New("ws: too many connections for this user")

// Client is one authenticated socket subscribed to topics.
type Client struct {
	UserID string
	Conn   *Conn

	send   chan []byte
	mu     sync.Mutex
	topics map[string]struct{}
	once   sync.Once
	done   chan struct{}
}

// NewClient wraps an upgraded connection.
func NewClient(userID string, c *Conn) *Client {
	return &Client{UserID: userID, Conn: c, send: make(chan []byte, sendBuffer), topics: map[string]struct{}{}, done: make(chan struct{})}
}

// Subscribe adds a topic; false when the per-connection limit is reached.
func (c *Client) Subscribe(topic string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.topics[topic]; ok {
		return true
	}
	if len(c.topics) >= MaxTopicsPerConn {
		return false
	}
	c.topics[topic] = struct{}{}
	return true
}

func (c *Client) Unsubscribe(topic string) {
	c.mu.Lock()
	delete(c.topics, topic)
	c.mu.Unlock()
}

func (c *Client) wants(topic string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.topics[topic]
	return ok
}

// enqueue never blocks: a consumer that cannot keep up is disconnected.
func (c *Client) enqueue(b []byte) {
	select {
	case c.send <- b:
	default:
		c.Close()
	}
}

// Close shuts the client down once.
func (c *Client) Close() {
	c.once.Do(func() {
		close(c.done)
		c.Conn.CloseWith(CloseNormal)
	})
}

// Done is closed when the client is closed.
func (c *Client) Done() <-chan struct{} { return c.done }

// WriteLoop drains the send queue and pings the peer until the client closes.
func (c *Client) WriteLoop(pingEvery time.Duration) {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case b := <-c.send:
			if err := c.Conn.WriteText(b); err != nil {
				c.Close()
				return
			}
		case <-t.C:
			if err := c.Conn.WritePing(); err != nil {
				c.Close()
				return
			}
		}
	}
}

// Hub routes events to the sockets of one user.
type Hub struct {
	mu     sync.RWMutex
	byUser map[string]map[*Client]struct{}
}

func NewHub() *Hub { return &Hub{byUser: map[string]map[*Client]struct{}{}} }

// Add registers a client, enforcing the per-user connection cap.
func (h *Hub) Add(c *Client) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.byUser[c.UserID]
	if len(set) >= MaxConnsPerUser {
		return ErrTooManyConns
	}
	if set == nil {
		set = map[*Client]struct{}{}
		h.byUser[c.UserID] = set
	}
	set[c] = struct{}{}
	return nil
}

func (h *Hub) Remove(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set := h.byUser[c.UserID]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.byUser, c.UserID)
		}
	}
}

// Count returns the number of connected clients.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, s := range h.byUser {
		n += len(s)
	}
	return n
}

type outMsg struct {
	Topic string          `json:"topic"`
	Data  json.RawMessage `json:"data"`
}

// Deliver sends {topic, data} to every socket of userID that subscribed to topic.
func (h *Hub) Deliver(userID, topic string, data json.RawMessage) {
	if len(data) == 0 {
		data = json.RawMessage("null")
	}
	b, err := json.Marshal(outMsg{Topic: topic, Data: data})
	if err != nil {
		return
	}
	h.mu.RLock()
	var targets []*Client
	for c := range h.byUser[userID] {
		if c.wants(topic) {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range targets {
		c.enqueue(b)
	}
}
