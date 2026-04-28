package manager

import (
	"github.com/15sheeps/webdelve/internal/metrics"
	"github.com/15sheeps/webdelve/internal/sandbox"
	"log/slog"
	"sync"
)

type Manager struct {
	pool     *sandbox.Pool
	sessions map[string]*Session
	mu       sync.RWMutex
	logger   *slog.Logger
}

func New(pool *sandbox.Pool, logger *slog.Logger) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		pool:     pool,
		logger:   logger,
	}
}

func (m *Manager) add(sess *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessions[sess.ID] = sess
	metrics.ActiveSessions.Inc()
	m.logger.Info("session created", "session_id", sess.ID)
}

func (m *Manager) Get(id string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

func (m *Manager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// remove container from pool
	sess, ok := m.sessions[id]
	if ok {
		metrics.ActiveSessions.Dec()
		m.pool.Destroy(sess.Container)
	}

	if sess.Client != nil {
		sess.Client.Disconnect(false)
	}

	delete(m.sessions, id)
	m.logger.Info("session removed", "session_id", id)
}

func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func (m *Manager) Logger() *slog.Logger {
	return m.logger
}
