package session

import (
	"log"
	"sync"
	"github.com/15sheeps/webdelve/internal/sandbox"
)

type Manager struct {
	pool     *sandbox.SandboxPool
	sessions map[string]*Session
	mu		 sync.RWMutex
}

func NewManager(pool *sandbox.SandboxPool) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		pool: pool,
	}
}

func (m *Manager) Add(sess *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessions[sess.ID] = sess
	log.Printf("session %s created (total: %d)\n", sess.ID, len(m.sessions))
}

func (m *Manager) Get(id string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

func (m *Manager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	delete(m.sessions, id)
	log.Printf("Session %s removed (total: %d)\n", id, len(m.sessions))
}

func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}
