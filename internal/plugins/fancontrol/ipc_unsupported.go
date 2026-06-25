//go:build !windows

package fancontrol

import (
	"context"
	"fmt"
)

type fanControlConfigs struct {
	Configs       []string
	CurrentConfig string
	ConfigFolder  string
}

type fanControlIPCConn struct{}

type fanControlRPC struct{}

func openFanControlIPC(ctx context.Context) (*fanControlIPCConn, error) {
	return nil, fmt.Errorf("%w on this platform", errFanControlIPCUnavailable)
}

func (c *fanControlIPCConn) Close() error {
	return nil
}

func newFanControlRPC(conn *fanControlIPCConn) fanControlRPC {
	return fanControlRPC{}
}

func (r fanControlRPC) ListConfigs(ctx context.Context) (fanControlConfigs, error) {
	return fanControlConfigs{}, fmt.Errorf("%w on this platform", errFanControlIPCUnavailable)
}

func (r fanControlRPC) LoadConfig(ctx context.Context, configName string) error {
	return fmt.Errorf("%w on this platform", errFanControlIPCUnavailable)
}
