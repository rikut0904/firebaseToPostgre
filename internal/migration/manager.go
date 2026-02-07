package migration

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// Manager coordinates a single migration run.
type Manager struct {
	mu        sync.Mutex
	running   bool
	status    Status
	cancel    context.CancelFunc
	startedCh chan struct{}
	progress  *Progress
	b         Broadcaster
	logger    *log.Logger
}

// NewManager creates a new Manager.
func NewManager(b Broadcaster) *Manager {
	return &Manager{
		b:      b,
		logger: log.Default(),
	}
}

// AttachProgress updates the broadcaster.
func (m *Manager) AttachProgress(b Broadcaster) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.b = b
}

// Start begins a migration run in a goroutine.
func (m *Manager) Start(req Request) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return errors.New("migration already running")
	}
	m.running = true
	m.startedCh = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.progress = NewProgress(m.b)
	m.status = m.progress.Status()
	m.mu.Unlock()

	close(m.startedCh)

	go func() {
		defer func() {
			m.mu.Lock()
			m.running = false
			m.cancel = nil
			m.mu.Unlock()
		}()

		migrator := NewMigrator(m.progress, m.logger)
		err := migrator.Run(ctx, req)
		if err != nil {
			m.progress.Log("migration failed: " + err.Error())
			m.progress.Finish("error")
			return
		}
		m.progress.Finish("done")
	}()

	return nil
}

// Cancel stops a running migration.
func (m *Manager) Cancel() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
	}
}

// Status returns the latest status snapshot.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.progress != nil {
		m.status = m.progress.Status()
	}
	m.status.Running = m.running
	return m.status
}

// WaitForStart waits for the migration goroutine to start.
func (m *Manager) WaitForStart(timeout time.Duration) bool {
	m.mu.Lock()
	ch := m.startedCh
	m.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	case <-time.After(timeout):
		return false
	}
}
