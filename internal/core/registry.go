package core

import (
	"errors"
	"fmt"
	"sync"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/events"
)

var ErrEntityNotFound = errors.New("entity not found")

type EntityRegistry struct {
	mu       sync.RWMutex
	entities map[string]entity.Entity
	bus      events.Bus
}

func NewEntityRegistry(bus events.Bus) *EntityRegistry {
	return &EntityRegistry{
		entities: make(map[string]entity.Entity),
		bus:      bus,
	}
}

func (r *EntityRegistry) Register(e entity.Entity) error {
	if err := entity.Validate(e); err != nil {
		return err
	}

	r.mu.Lock()
	if _, ok := r.entities[e.ID]; ok {
		r.mu.Unlock()
		return fmt.Errorf("register entity %q: duplicate entity id", e.ID)
	}
	r.entities[e.ID] = e
	r.mu.Unlock()

	r.bus.Publish(events.Event{Type: events.EntityAdded, EntityID: e.ID, Entity: e})
	return nil
}

func (r *EntityRegistry) Update(e entity.Entity) error {
	if err := entity.Validate(e); err != nil {
		return err
	}

	r.mu.Lock()
	if _, ok := r.entities[e.ID]; !ok {
		r.mu.Unlock()
		return fmt.Errorf("update entity %q: %w", e.ID, ErrEntityNotFound)
	}
	r.entities[e.ID] = e
	r.mu.Unlock()

	r.bus.Publish(events.Event{Type: events.EntityUpdated, EntityID: e.ID, Entity: e})
	return nil
}

func (r *EntityRegistry) Remove(id string) error {
	r.mu.Lock()
	e, ok := r.entities[id]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("remove entity %q: %w", id, ErrEntityNotFound)
	}
	delete(r.entities, id)
	r.mu.Unlock()

	r.bus.Publish(events.Event{Type: events.EntityRemoved, EntityID: id, Entity: e})
	return nil
}

func (r *EntityRegistry) Get(id string) (entity.Entity, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entities[id]
	return e, ok
}

func (r *EntityRegistry) Has(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.entities[id]
	return ok
}

func (r *EntityRegistry) List() []entity.Entity {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entities := make([]entity.Entity, 0, len(r.entities))
	for _, e := range r.entities {
		entities = append(entities, e)
	}
	return entities
}
