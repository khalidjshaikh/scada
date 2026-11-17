package event

import (
	"sync"
)

// Event is a generic notification.
type Event struct {
	Topic string
	Data  any
}

// Bus is a simple in-process pub/sub.
type Bus struct {
	mu   sync.RWMutex
	subs map[string][]chan Event
	all  []chan Event
	buf  int
}

func New(buf int) *Bus {
	if buf <= 0 {
		buf = 64
	}
	return &Bus{subs: map[string][]chan Event{}, buf: buf}
}

func (b *Bus) Subscribe(topic string) <-chan Event {
	ch := make(chan Event, b.buf)
	b.mu.Lock()
	defer b.mu.Unlock()
	if topic == "*" {
		b.all = append(b.all, ch)
	} else {
		b.subs[topic] = append(b.subs[topic], ch)
	}
	return ch
}

func (b *Bus) Publish(topic string, data any) {
	ev := Event{Topic: topic, Data: data}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs[topic] {
		select {
		case ch <- ev:
		default:
		}
	}
	for _, ch := range b.all {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Unsubscribe removes a channel (best-effort).
func (b *Bus) Unsubscribe(ch <-chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	filter := func(list []chan Event) []chan Event {
		out := list[:0]
		for _, c := range list {
			if c != ch {
				out = append(out, c)
			}
		}
		return out
	}
	for k, v := range b.subs {
		b.subs[k] = filter(v)
	}
	b.all = filter(b.all)
}
