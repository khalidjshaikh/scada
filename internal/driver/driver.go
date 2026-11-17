package driver

import (
	"context"
	"time"

	"github.com/kshaikh/scada/internal/core/db"
)

// Driver is a data acquisition driver.
type Driver interface {
	Name() string
	Connect(ctx context.Context) error
	Disconnect() error
	// Poll reads from device and updates tags in db. Called periodically.
	Poll(ctx context.Context, database *db.DB) error
	// Write sends a value to device for given tag.
	Write(ctx context.Context, database *db.DB, tagID string, value any) error
	Interval() time.Duration
}

// Manager runs drivers.
type Manager struct {
	drivers []Driver
	db      *db.DB
	cancel  context.CancelFunc
}

func NewManager(database *db.DB) *Manager {
	return &Manager{db: database}
}

func (m *Manager) Add(d Driver) { m.drivers = append(m.drivers, d) }

func (m *Manager) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	for _, d := range m.drivers {
		drv := d
		go func() {
			// connect
			if err := drv.Connect(ctx); err != nil {
				// log? 
				_ = err
			}
			ticker := time.NewTicker(drv.Interval())
			defer ticker.Stop()
			// initial poll
			_ = drv.Poll(ctx, m.db)
			for {
				select {
				case <-ctx.Done():
					drv.Disconnect()
					return
				case <-ticker.C:
					_ = drv.Poll(ctx, m.db)
				}
			}
		}()
	}
}

func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Manager) Write(ctx context.Context, tagID string, value any) error {
	for _, d := range m.drivers {
		// try all; ignore errors? We'll attempt and return first success
		if err := d.Write(ctx, m.db, tagID, value); err == nil {
			return nil
		}
	}
	return nil
}
