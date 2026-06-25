package core

import (
	"fmt"

	"github.com/go-viper/mapstructure/v2"
	"go.uber.org/zap"
)

type FactoryContext struct {
	raw     map[string]any
	logger  *zap.Logger
	agentID string
	dataDir string
}

func NewFactoryContext(raw map[string]any, logger *zap.Logger, agentID, dataDir string) FactoryContext {
	if raw == nil {
		raw = map[string]any{}
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return FactoryContext{
		raw:     raw,
		logger:  logger,
		agentID: agentID,
		dataDir: dataDir,
	}
}

func (c FactoryContext) DecodeConfig(target any) error {
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
		),
		ErrorUnused:      true,
		Result:           target,
		TagName:          "mapstructure",
		WeaklyTypedInput: true,
	})
	if err != nil {
		return fmt.Errorf("create plugin config decoder: %w", err)
	}
	if err := decoder.Decode(c.raw); err != nil {
		return fmt.Errorf("decode plugin config: %w", err)
	}
	return nil
}

func (c FactoryContext) RawConfig() map[string]any {
	cp := make(map[string]any, len(c.raw))
	for k, v := range c.raw {
		cp[k] = v
	}
	return cp
}

func (c FactoryContext) Logger() *zap.Logger {
	return c.logger
}

func (c FactoryContext) AgentID() string {
	return c.agentID
}

func (c FactoryContext) DataDir() string {
	return c.dataDir
}
