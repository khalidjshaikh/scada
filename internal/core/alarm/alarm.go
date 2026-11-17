package alarm

import (
	"fmt"
	"sync"
	"time"

	"github.com/kshaikh/scada/internal/core/db"
	"github.com/kshaikh/scada/internal/core/event"
	"github.com/kshaikh/scada/internal/core/tag"
)

type Priority int

const (
	PrioLow Priority = iota + 1
	PrioMedium
	PrioHigh
	PrioCritical
)

func (p Priority) String() string {
	switch p {
	case PrioLow:
		return "low"
	case PrioMedium:
		return "medium"
	case PrioHigh:
		return "high"
	case PrioCritical:
		return "critical"
	default:
		return "unknown"
	}
}

type State string

const (
	StateActive   State = "active"
	StateAcked    State = "acked"
	StateCleared  State = "cleared"
)

type Rule struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	TagID     string  `json:"tag_id"`
	Type      string  `json:"type"` // high, low, highhigh, lollow? use highhigh/lowwlow
	Threshold float64 `json:"threshold"`
	Priority  Priority `json:"priority"`
	Enabled   bool    `json:"enabled"`
	Deadband  float64 `json:"deadband"`
	Message   string  `json:"message"`
}

type Alarm struct {
	ID          string    `json:"id"`
	RuleID      string    `json:"rule_id"`
	TagID       string    `json:"tag_id"`
	Name        string    `json:"name"`
	Priority    Priority  `json:"priority"`
	State       State     `json:"state"`
	Value       any       `json:"value"`
	Message     string    `json:"message"`
	RaisedAt    time.Time `json:"raised_at"`
	AckedAt     *time.Time `json:"acked_at,omitempty"`
	ClearedAt   *time.Time `json:"cleared_at,omitempty"`
}

type Manager struct {
	mu     sync.RWMutex
	db     *db.DB
	bus    *event.Bus
	rules  map[string]*Rule
	alarms map[string]*Alarm
	// active alarm key by rule id
	active map[string]string // ruleID -> alarmID
	seq    int
}

func NewManager(database *db.DB, bus *event.Bus) *Manager {
	m := &Manager{
		db:     database,
		bus:    bus,
		rules:  map[string]*Rule{},
		alarms: map[string]*Alarm{},
		active: map[string]string{},
	}
	if bus != nil {
		ch := bus.Subscribe("tag:*")
		go m.listen(ch)
	}
	return m
}

func (m *Manager) AddRule(r Rule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.Priority == 0 {
		r.Priority = PrioMedium
	}
	cp := r
	m.rules[r.ID] = &cp
}

func (m *Manager) RemoveRule(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rules, id)
}

func (m *Manager) Rules() []Rule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Rule, 0, len(m.rules))
	for _, r := range m.rules {
		out = append(out, *r)
	}
	return out
}

func (m *Manager) Alarms(state string) []Alarm {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []Alarm{}
	for _, a := range m.alarms {
		if state == "" || string(a.State) == state {
			out = append(out, *a)
		}
	}
	return out
}

func (m *Manager) Ack(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.alarms[id]
	if !ok {
		return fmt.Errorf("alarm %s not found", id)
	}
	if a.State == StateActive {
		now := time.Now()
		a.State = StateAcked
		a.AckedAt = &now
		m.publish(*a)
	}
	return nil
}

func (m *Manager) listen(ch <-chan event.Event) {
	for ev := range ch {
		snap, ok := ev.Data.(tag.Snapshot)
		if !ok {
			continue
		}
		m.evaluate(snap)
	}
}

func (m *Manager) evaluate(snap tag.Snapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rules {
		if !r.Enabled || r.TagID != snap.ID {
			continue
		}
		val, ok := toFloat(snap.Value)
		if !ok && r.Type != "change" {
			// bool equality handling
		}
		triggered := false
		switch r.Type {
		case "high":
			triggered = val > r.Threshold
		case "highhigh":
			triggered = val > r.Threshold
		case "low":
			triggered = val < r.Threshold
		case "lowlow":
			triggered = val < r.Threshold
		case "eq":
			triggered = fmt.Sprintf("%v", snap.Value) == fmt.Sprintf("%v", r.Threshold)
		}
		// deadband handling for clearing
		if triggered {
			if _, exists := m.active[r.ID]; !exists {
				m.seq++
				a := &Alarm{
					ID:       fmt.Sprintf("ALM-%d", m.seq),
					RuleID:   r.ID,
					TagID:    r.TagID,
					Name:     r.Name,
					Priority: r.Priority,
					State:    StateActive,
					Value:    snap.Value,
					Message:  r.Message,
					RaisedAt: time.Now(),
				}
				if a.Message == "" {
					a.Message = fmt.Sprintf("%s %s threshold %.4g", snap.ID, r.Type, r.Threshold)
				}
				m.alarms[a.ID] = a
				m.active[r.ID] = a.ID
				m.publish(*a)
			} else {
				// update value
				if a, ok := m.alarms[m.active[r.ID]]; ok {
					a.Value = snap.Value
				}
			}
		} else {
			if aid, exists := m.active[r.ID]; exists {
				// apply deadband for high/low
				clear := true
				if r.Type == "high" || r.Type == "highhigh" {
					clear = val <= r.Threshold-r.Deadband
				} else if r.Type == "low" || r.Type == "lowlow" {
					clear = val >= r.Threshold+r.Deadband
				}
				if clear {
					a := m.alarms[aid]
					now := time.Now()
					a.State = StateCleared
					a.ClearedAt = &now
					a.Value = snap.Value
					delete(m.active, r.ID)
					m.publish(*a)
				}
			}
		}
	}
}

func (m *Manager) publish(a Alarm) {
	if m.bus != nil {
		m.bus.Publish("alarm", a)
	}
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}
