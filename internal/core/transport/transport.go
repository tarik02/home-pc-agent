package transport

import (
	"context"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/events"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
)

type Host interface {
	AgentID() string
	AgentName() string
	Entities() []entity.Entity
	RouteCommand(ctx context.Context, command plugin.Command) error
	SubscribeEvents(ctx context.Context, buffer int) <-chan events.Event
}

type Transport interface {
	ID() string
	Start(ctx context.Context, host Host) error
	Stop(ctx context.Context) error
}
