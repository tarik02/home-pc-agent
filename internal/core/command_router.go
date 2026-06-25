package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/tarik02/home-pc-agent/internal/core/plugin"
)

var ErrNoCommandHandler = errors.New("no command handler registered")

type CommandRouter struct {
	mu       sync.RWMutex
	handlers map[string]plugin.CommandHandler
}

func NewCommandRouter() *CommandRouter {
	return &CommandRouter{handlers: make(map[string]plugin.CommandHandler)}
}

func (r *CommandRouter) Subscribe(entityID string, handler plugin.CommandHandler) (func(), error) {
	if entityID == "" {
		return nil, errors.New("entity id is required")
	}
	if handler == nil {
		return nil, errors.New("command handler is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.handlers[entityID]; ok {
		return nil, fmt.Errorf("subscribe command %q: duplicate handler", entityID)
	}
	r.handlers[entityID] = handler

	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			delete(r.handlers, entityID)
			r.mu.Unlock()
		})
	}, nil
}

func (r *CommandRouter) Route(ctx context.Context, command plugin.Command) error {
	if command.EntityID == "" {
		return errors.New("command entity id is required")
	}
	if command.ReceivedAt.IsZero() {
		command.ReceivedAt = time.Now()
	}

	r.mu.RLock()
	handler, ok := r.handlers[command.EntityID]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("route command %q: %w", command.EntityID, ErrNoCommandHandler)
	}

	return handler(ctx, command)
}
