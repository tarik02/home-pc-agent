package plugin

import (
	"context"
	"fmt"
	"time"

	"github.com/tarik02/home-pc-agent/internal/config"
	"github.com/tarik02/home-pc-agent/internal/core/entity"

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

type PluginHost interface {
	RegisterEntity(entity.Entity) error
	UpdateEntity(entity.Entity) error
	PublishState(entityID string, state any) error
	SetAvailability(entityID string, availability entity.Availability) error
	SubscribeCommand(entityID string, handler CommandHandler) error
	Go(name string, fn func(ctx context.Context) error)
}

type Descriptor struct {
	ID               string
	OperatingSystems []string
	EntityIDs        []string
}

type Factory struct {
	Descriptor          Descriptor
	Build               func(raw map[string]any, logger *zap.Logger) (Plugin, error)
	ConfiguredEntityIDs func(raw map[string]any) ([]string, error)
	ValidateConfigured  func(raw map[string]any) error
}

func ConfigFactory[T any](descriptor Descriptor, prepare func(T) (T, error), create func(T, *zap.Logger) Plugin) Factory {
	return Factory{
		Descriptor: descriptor,
		Build: func(raw map[string]any, logger *zap.Logger) (Plugin, error) {
			var cfg T
			if err := config.DecodePlugin(raw, &cfg); err != nil {
				return nil, err
			}
			if prepare != nil {
				prepared, err := prepare(cfg)
				if err != nil {
					return nil, err
				}
				cfg = prepared
			}
			return create(cfg, logger), nil
		},
	}
}

func (f Factory) ID() string {
	return f.Descriptor.ID
}

func (f Factory) New(raw map[string]any, logger *zap.Logger) (Plugin, error) {
	if f.Build == nil {
		return nil, fmt.Errorf("plugin %q has no builder", f.ID())
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return f.Build(raw, logger)
}

func (f Factory) EntityIDs(raw map[string]any) ([]string, error) {
	if f.ConfiguredEntityIDs != nil {
		return f.ConfiguredEntityIDs(raw)
	}
	return append([]string(nil), f.Descriptor.EntityIDs...), nil
}

func (f Factory) Validate(raw map[string]any) error {
	if f.ValidateConfigured == nil {
		return nil
	}
	return f.ValidateConfigured(raw)
}

type Plugin interface {
	Start(ctx context.Context, host PluginHost) error
	Stop(ctx context.Context) error
}
