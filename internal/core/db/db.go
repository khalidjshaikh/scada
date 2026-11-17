package db

import (
	"fmt"
	"sync"

	"github.com/kshaikh/scada/internal/core/event"
	"github.com/kshaikh/scada/internal/core/tag"
)

// DB is an in-memory real-time tag database.
type DB struct {
	mu    sync.RWMutex
	tags  map[string]*tag.Tag
	order []string
	bus   *event.Bus
}

func New(bus *event.Bus) *DB {
	return &DB{tags: map[string]*tag.Tag{}, bus: bus}
}

func (d *DB) Add(t *tag.Tag) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.tags[t.ID]; ok {
		return fmt.Errorf("tag %s already exists", t.ID)
	}
	d.tags[t.ID] = t
	d.order = append(d.order, t.ID)
	return nil
}

func (d *DB) Remove(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.tags, id)
	for i, v := range d.order {
		if v == id {
			d.order = append(d.order[:i], d.order[i+1:]...)
			break
		}
	}
}

func (d *DB) Get(id string) (*tag.Tag, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	t, ok := d.tags[id]
	return t, ok
}

func (d *DB) All() []*tag.Tag {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]*tag.Tag, 0, len(d.order))
	for _, id := range d.order {
		out = append(out, d.tags[id])
	}
	return out
}

func (d *DB) IDs() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := append([]string(nil), d.order...)
	return out
}

// Set updates a tag value, validates coercion, and publishes event.
func (d *DB) Set(id string, value any, q tag.Quality) error {
	t, ok := d.Get(id)
	if !ok {
		return fmt.Errorf("tag %s not found", id)
	}
	if t.Access == "ro" {
		// allow system writes? We'll permit; access enforced at API layer.
	}
	v, err := t.Coerce(value)
	if err != nil {
		return err
	}
	t.SetValue(v, q)
	if d.bus != nil {
		d.bus.Publish("tag:"+id, t.Snapshot())
		d.bus.Publish("tag:*", t.Snapshot())
	}
	return nil
}

func (d *DB) Snapshot() []tag.Snapshot {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]tag.Snapshot, 0, len(d.order))
	for _, id := range d.order {
		out = append(out, d.tags[id].Snapshot())
	}
	return out
}
