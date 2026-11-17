package driver

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/kshaikh/scada/internal/core/db"
	"github.com/kshaikh/scada/internal/core/tag"
)

// SimulatedDriver generates demo process values.
type SimulatedDriver struct {
	name     string
	interval time.Duration
	start    time.Time
	tags     []string
}

func NewSimulated(name string, interval time.Duration) *SimulatedDriver {
	if interval <= 0 {
		interval = time.Second
	}
	return &SimulatedDriver{name: name, interval: interval, start: time.Now()}
}

func (s *SimulatedDriver) Name() string { return s.name }

func (s *SimulatedDriver) Connect(ctx context.Context) error { return nil }
func (s *SimulatedDriver) Disconnect() error                 { return nil }
func (s *SimulatedDriver) Interval() time.Duration           { return s.interval }

func (s *SimulatedDriver) Poll(ctx context.Context, database *db.DB) error {
	t := time.Since(s.start).Seconds()
	for _, tg := range database.All() {
		// only update tags owned by this driver? We'll update all writable? Better: update tags with metadata driver=sim
		if tg.Metadata == nil || tg.Metadata["driver"] != s.name {
			continue
		}
		switch tg.ID {
		case "sim.temperature":
			v := 25 + 5*math.Sin(t/10) + (rand.Float64()-0.5)*0.5
			database.Set(tg.ID, v, tag.QualityGood)
		case "sim.pressure":
			v := 1.2 + 0.3*math.Sin(t/7+1) + (rand.Float64()-0.5)*0.05
			database.Set(tg.ID, v, tag.QualityGood)
		case "sim.flow":
			v := 100 + 20*math.Sin(t/5) + (rand.Float64()-0.5)*2
			database.Set(tg.ID, v, tag.QualityGood)
		case "sim.level":
			v := 50 + 30*math.Sin(t/15)
			if v < 0 {
				v = 0
			}
			if v > 100 {
				v = 100
			}
			database.Set(tg.ID, v, tag.QualityGood)
		case "sim.pump1":
			// keep as is (controlled)
		case "sim.status":
			database.Set(tg.ID, "running", tag.QualityGood)
		case "sim.counter":
			// increment
			cur, _ := tg.Value().(int)
			database.Set(tg.ID, cur+1, tag.QualityGood)
		default:
			// generic: if float tag, sine
			if tg.Type == tag.TypeFloat {
				v := rand.Float64() * 10
				database.Set(tg.ID, v, tag.QualityGood)
			}
		}
	}
	return nil
}

func (s *SimulatedDriver) Write(ctx context.Context, database *db.DB, tagID string, value any) error {
	tg, ok := database.Get(tagID)
	if !ok {
		return fmt.Errorf("tag not found")
	}
	if tg.Metadata == nil || tg.Metadata["driver"] != s.name {
		return fmt.Errorf("not owned")
	}
	return database.Set(tagID, value, tag.QualityGood)
}
