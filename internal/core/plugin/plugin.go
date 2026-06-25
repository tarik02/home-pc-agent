package plugin

import (
	"context"
	"time"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/events"

	"go.uber.org/zap"
)

type Command struct {
	EntityID   string
	Payload    any
	Raw        []byte
	Transport  string
	ReceivedAt time.Time
}

type CommandHandler func(ctx context.Context, command Command) error

type EntityHandle interface {
	ID() string
	Unregister(ctx context.Context) error
}

type PluginHost interface {
	RegisterEntity(entity.Entity) (EntityHandle, error)
	UpdateEntity(entity.Entity) error
	PublishState(entityID string, state any) error
	SetAvailability(entityID string, availability entity.Availability) error
	SubscribeCommand(entityID string, handler CommandHandler) error
	Go(name string, fn func(ctx context.Context) error)
	Logger() *zap.Logger
	Bus() events.Bus
}

type PluginFactoryContext interface {
	DecodeConfig(target any) error
	RawConfig() map[string]any
	Logger() *zap.Logger
	AgentID() string
	DataDir() string
}

type PluginFactory interface {
	ID() string
	New(ctx PluginFactoryContext) (Plugin, error)
}

type Plugin interface {
	ID() string
	Start(ctx context.Context, host PluginHost) error
	Stop(ctx context.Context) error
}
