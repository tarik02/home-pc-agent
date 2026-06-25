package events

import (
	"context"
	"sync"
	"time"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
)

type Type string

const (
	EntityAdded         Type = "entity_added"
	EntityUpdated       Type = "entity_updated"
	EntityRemoved       Type = "entity_removed"
	StateChanged        Type = "state_changed"
	AvailabilityChanged Type = "availability_changed"
)

type Event struct {
	Type         Type
	EntityID     string
	Entity       entity.Entity
	State        any
	Availability entity.Availability
	Time         time.Time
}

type Bus interface {
	Publish(Event)
	Subscribe(ctx context.Context, buffer int) <-chan Event
}

type EventBus struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}
}

func NewBus() *EventBus {
	return &EventBus{subs: make(map[chan Event]struct{})}
}

func (b *EventBus) Publish(event Event) {
	if event.Time.IsZero() {
		event.Time = time.Now()
	}

	b.mu.RLock()
	subs := make([]chan Event, 0, len(b.subs))
	for ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (b *EventBus) Subscribe(ctx context.Context, buffer int) <-chan Event {
	if buffer < 1 {
		buffer = 1
	}

	ch := make(chan Event, buffer)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.subs, ch)
		close(ch)
		b.mu.Unlock()
	}()

	return ch
}
