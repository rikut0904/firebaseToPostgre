package migration

import (
	"sync"
	"time"
)

// Broadcaster broadcasts progress updates (WebSocket hub implements this).
type Broadcaster interface {
	Broadcast(v interface{})
}

// Progress tracks and broadcasts migration progress.
type Progress struct {
	mu     sync.Mutex
	start  time.Time
	status Status
	b      Broadcaster
}

// NewProgress creates a new progress tracker.
func NewProgress(b Broadcaster) *Progress {
	return &Progress{
		start: time.Now(),
		status: Status{
			Running:      true,
			CurrentPhase: "starting",
			Tables:       []TableStatus{},
		},
		b: b,
	}
}

// Status returns a snapshot of current status.
func (p *Progress) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refreshLocked()
	return p.status
}

// SetPhase updates the current phase.
func (p *Progress) SetPhase(phase string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.CurrentPhase = phase
	p.refreshLocked()
	p.broadcastLocked()
}

// Log updates the log message.
func (p *Progress) Log(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.LogMessage = msg
	p.refreshLocked()
	p.broadcastLocked()
}

// UpdateTable updates a table status by name.
func (p *Progress) UpdateTable(name string, update func(*TableStatus)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	idx := -1
	for i := range p.status.Tables {
		if p.status.Tables[i].Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		p.status.Tables = append(p.status.Tables, TableStatus{Name: name})
		idx = len(p.status.Tables) - 1
	}
	update(&p.status.Tables[idx])
	p.refreshLocked()
	p.broadcastLocked()
}

// AddFailed records a failed table.
func (p *Progress) AddFailed(name, errMsg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.FailedTables = append(p.status.FailedTables, FailedTable{Name: name, Error: errMsg})
	p.refreshLocked()
	p.broadcastLocked()
}

// Finish marks the migration as complete.
func (p *Progress) Finish(phase string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.Running = false
	p.status.CurrentPhase = phase
	p.refreshLocked()
	p.broadcastLocked()
}

func (p *Progress) refreshLocked() {
	p.status.ElapsedSec = int64(time.Since(p.start).Seconds())
	p.status.Overall = p.computeOverallLocked()
}

func (p *Progress) computeOverallLocked() int {
	var total int64
	var written int64
	for i := range p.status.Tables {
		t := &p.status.Tables[i]
		if t.Total > 0 {
			t.Percent = int(float64(t.Written) / float64(t.Total) * 100)
			if t.Percent > 100 {
				t.Percent = 100
			}
		}
		if t.Total > 0 {
			total += t.Total
			written += t.Written
		}
	}
	if total == 0 {
		return 0
	}
	percent := int(float64(written) / float64(total) * 100)
	if percent > 100 {
		percent = 100
	}
	return percent
}

func (p *Progress) broadcastLocked() {
	if p.b != nil {
		p.b.Broadcast(p.status)
	}
}
