package core

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/events"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type PluginScope struct {
	pluginID string
	registry *EntityRegistry
	router   *CommandRouter
	bus      events.Bus
	logger   *zap.Logger

	ctx    context.Context
	cancel context.CancelFunc
	group  *errgroup.Group

	mu        sync.Mutex
	closed    bool
	entityIDs map[string]struct{}
	unsubs    map[string][]func()
}

func NewPluginScope(parent context.Context, pluginID string, registry *EntityRegistry, router *CommandRouter, bus events.Bus, logger *zap.Logger) *PluginScope {
	if logger == nil {
		logger = zap.NewNop()
	}
	ctx, cancel := context.WithCancel(parent)
	group, groupCtx := errgroup.WithContext(ctx)
	return &PluginScope{
		pluginID:  pluginID,
		registry:  registry,
		router:    router,
		bus:       bus,
		logger:    logger.With(zap.String("plugin", pluginID)),
		ctx:       groupCtx,
		cancel:    cancel,
		group:     group,
		entityIDs: make(map[string]struct{}),
		unsubs:    make(map[string][]func()),
	}
}

func (s *PluginScope) RegisterEntity(e entity.Entity) (plugin.EntityHandle, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, fmt.Errorf("plugin %q scope is closed", s.pluginID)
	}
	s.mu.Unlock()

	if err := s.registry.Register(e); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.entityIDs[e.ID] = struct{}{}
	s.mu.Unlock()

	return &entityHandle{scope: s, id: e.ID}, nil
}

func (s *PluginScope) UpdateEntity(e entity.Entity) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("plugin %q scope is closed", s.pluginID)
	}
	s.mu.Unlock()

	if !s.ownsEntity(e.ID) {
		return fmt.Errorf("plugin %q cannot update unowned entity %q", s.pluginID, e.ID)
	}
	return s.registry.Update(e)
}

func (s *PluginScope) PublishState(entityID string, state any) error {
	if !s.ownsEntity(entityID) {
		return fmt.Errorf("plugin %q cannot publish state for unowned entity %q", s.pluginID, entityID)
	}
	s.bus.Publish(events.Event{Type: events.StateChanged, EntityID: entityID, State: state})
	return nil
}

func (s *PluginScope) SetAvailability(entityID string, availability entity.Availability) error {
	if !s.ownsEntity(entityID) {
		return fmt.Errorf("plugin %q cannot publish availability for unowned entity %q", s.pluginID, entityID)
	}
	s.bus.Publish(events.Event{Type: events.AvailabilityChanged, EntityID: entityID, Availability: availability})
	return nil
}

func (s *PluginScope) SubscribeCommand(entityID string, handler plugin.CommandHandler) error {
	if !s.ownsEntity(entityID) {
		return fmt.Errorf("plugin %q cannot subscribe command for unowned entity %q", s.pluginID, entityID)
	}
	unsub, err := s.router.Subscribe(entityID, handler)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		unsub()
		return fmt.Errorf("plugin %q scope is closed", s.pluginID)
	}
	s.unsubs[entityID] = append(s.unsubs[entityID], unsub)
	s.mu.Unlock()
	return nil
}

func (s *PluginScope) Go(name string, fn func(ctx context.Context) error) {
	if fn == nil {
		return
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	s.group.Go(func() (err error) {
		logger := s.logger.With(zap.String("task", name))
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("plugin task panicked", zap.Any("panic", recovered), zap.ByteString("stack", debug.Stack()))
				err = fmt.Errorf("plugin task %q panicked: %v", name, recovered)
			}
		}()
		if err := fn(s.ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			logger.Error("plugin task exited with error", zap.Error(err))
			return err
		}
		return nil
	})
}

func (s *PluginScope) Logger() *zap.Logger {
	return s.logger
}

func (s *PluginScope) Bus() events.Bus {
	return s.bus
}

func (s *PluginScope) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	var unsubs []func()
	for _, entityUnsubs := range s.unsubs {
		unsubs = append(unsubs, entityUnsubs...)
	}
	entityIDs := make([]string, 0, len(s.entityIDs))
	for id := range s.entityIDs {
		entityIDs = append(entityIDs, id)
	}
	s.unsubs = make(map[string][]func())
	s.entityIDs = make(map[string]struct{})
	s.mu.Unlock()

	s.cancel()
	for _, unsub := range unsubs {
		unsub()
	}
	for _, id := range entityIDs {
		if err := s.registry.Remove(id); err != nil {
			s.logger.Debug("entity already removed during scope cleanup", zap.String("entity_id", id), zap.Error(err))
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- s.group.Wait()
	}()

	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
		return fmt.Errorf("plugin %q scope cleanup timed out", s.pluginID)
	}
}

func (s *PluginScope) unregisterEntity(ctx context.Context, id string) error {
	s.mu.Lock()
	if _, ok := s.entityIDs[id]; !ok {
		s.mu.Unlock()
		return nil
	}
	delete(s.entityIDs, id)
	unsubs := append([]func(){}, s.unsubs[id]...)
	delete(s.unsubs, id)
	s.mu.Unlock()

	for _, unsub := range unsubs {
		unsub()
	}
	return s.registry.Remove(id)
}

func (s *PluginScope) ownsEntity(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	_, ok := s.entityIDs[id]
	return ok
}

type entityHandle struct {
	scope *PluginScope
	id    string
}

func (h *entityHandle) ID() string {
	return h.id
}

func (h *entityHandle) Unregister(ctx context.Context) error {
	return h.scope.unregisterEntity(ctx, h.id)
}
