package clients

import (
	"sync"

	"github.com/gorilla/websocket"
)

// Client represents a set of connections belonging to one logical client
type Client struct {
	Control   *websocket.Conn
	Streams   []*websocket.Conn
	streamIdx int
}

// Manager tracks multiple clients keyed by clientID
type Manager struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

func NewManager() *Manager {
	return &Manager{clients: make(map[string]*Client)}
}

func (m *Manager) getOrCreate(id string) *Client {
	c, ok := m.clients[id]
	if !ok {
		c = &Client{}
		m.clients[id] = c
	}
	return c
}

func (m *Manager) SetControl(id string, conn *websocket.Conn) (old *websocket.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.getOrCreate(id)
	if c.Control != nil && c.Control != conn {
		old = c.Control
	}
	c.Control = conn
	return
}

func (m *Manager) AddStream(id string, conn *websocket.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.getOrCreate(id)
	c.Streams = append(c.Streams, conn)
}

func (m *Manager) RemoveStream(id string, conn *websocket.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.clients[id]
	if !ok {
		return
	}
	for i, s := range c.Streams {
		if s == conn {
			c.Streams = append(c.Streams[:i], c.Streams[i+1:]...)
			break
		}
	}
}

func (m *Manager) RemoveControl(id string, conn *websocket.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.clients[id]
	if !ok {
		return
	}
	if c.Control == conn {
		c.Control = nil
	}
}

// NextTarget returns the next stream connection (round-robin) or control as fallback.
func (m *Manager) NextTarget(id string) *websocket.Conn {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.clients[id]
	if !ok {
		return nil
	}
	if n := len(c.Streams); n > 0 {
		idx := c.streamIdx % n
		target := c.Streams[idx]
		c.streamIdx++
		return target
	}
	return c.Control
}

// ForEachClient executes fn with a snapshot of client IDs.
func (m *Manager) ForEachClient(fn func(id string)) {
	m.mu.RLock()
	ids := make([]string, 0, len(m.clients))
	for id := range m.clients {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	for _, id := range ids {
		fn(id)
	}
}
