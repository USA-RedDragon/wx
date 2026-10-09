package live

import (
	"errors"
	"sync"
)

var ErrTooManyClients = errors.New("too many live clients")

type Client struct {
	ch     chan []byte
	done   chan struct{}
	closed bool
}

func (c *Client) Messages() <-chan []byte { return c.ch }

func (c *Client) Done() <-chan struct{} { return c.done }

type Hub struct {
	mu      sync.Mutex
	clients map[*Client]struct{}
	max     int
	buffer  int
}

func NewHub(maxClients, buffer int) *Hub {
	return &Hub{clients: map[*Client]struct{}{}, max: maxClients, buffer: buffer}
}

func (h *Hub) Subscribe() (*Client, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.clients) >= h.max {
		return nil, ErrTooManyClients
	}
	c := &Client{ch: make(chan []byte, h.buffer), done: make(chan struct{})}
	h.clients[c] = struct{}{}
	return c, nil
}

func (h *Hub) drop(c *Client) {
	if c.closed {
		return
	}
	c.closed = true
	delete(h.clients, c)
	close(c.done)
}

func (h *Hub) Unsubscribe(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.drop(c)
}

func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) Broadcast(msg []byte) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	sent := 0
	for c := range h.clients {
		select {
		case c.ch <- msg:
			sent++
		default:
			h.drop(c)
		}
	}
	return sent
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		h.drop(c)
	}
}
