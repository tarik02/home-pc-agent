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
	subs map[*subscription]struct{}
}

func NewBus() *EventBus {
	return &EventBus{subs: make(map[*subscription]struct{})}
}

func (b *EventBus) Publish(event Event) {
	if event.Time.IsZero() {
		event.Time = time.Now()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for sub := range b.subs {
		sub.enqueue(event)
	}
}

func (b *EventBus) Subscribe(ctx context.Context, buffer int) <-chan Event {
	if buffer < 1 {
		buffer = 1
	}

	sub := newSubscription(buffer)
	b.mu.Lock()
	b.subs[sub] = struct{}{}
	b.mu.Unlock()
	go sub.run()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.subs, sub)
		sub.close()
		b.mu.Unlock()
	}()

	return sub.events
}

type subscription struct {
	events chan Event
	ready  chan struct{}
	done   chan struct{}

	mu        sync.Mutex
	queue     []Event
	closed    bool
	closeOnce sync.Once
}

func newSubscription(buffer int) *subscription {
	return &subscription{
		events: make(chan Event, buffer),
		ready:  make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
}

func (s *subscription) enqueue(event Event) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.queue = append(s.queue, event)
	s.mu.Unlock()

	select {
	case s.ready <- struct{}{}:
	default:
	}
}

func (s *subscription) close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.queue = nil
		s.mu.Unlock()
		close(s.done)
	})
}

func (s *subscription) run() {
	defer close(s.events)
	for {
		event, ok := s.next()
		if !ok {
			select {
			case <-s.done:
				return
			case <-s.ready:
				continue
			}
		}

		select {
		case <-s.done:
			return
		case s.events <- event:
		}
	}
}

func (s *subscription) next() (Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return Event{}, false
	}
	event := s.queue[0]
	if len(s.queue) == 1 {
		s.queue = nil
		return event, true
	}
	s.queue[0] = Event{}
	s.queue = s.queue[1:]
	return event, true
}
