package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/config"
	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
	coretransport "github.com/tarik02/home-pc-agent/internal/core/transport"
)

func TestReloadRestoresCommandsAfterTransportShutdownError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(t.TempDir(), "agent.toml")
	require.NoError(t, os.WriteFile(path, []byte("[plugins.echo]\nenabled = true\n"), 0o600))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	first := &reloadTransport{stopError: context.DeadlineExceeded}
	factory := plugin.Factory{
		Descriptor: plugin.Descriptor{ID: "echo"},
		Build:      func(map[string]any, *zap.Logger) (plugin.Plugin, error) { return reloadPlugin{}, nil },
	}
	runtime, err := NewRuntime(path, cfg, zap.NewNop(), []plugin.Factory{factory},
		func(*config.Config, *zap.Logger) []coretransport.Transport {
			return []coretransport.Transport{&reloadTransport{}}
		},
		[]coretransport.Transport{first}, func(*config.Config) error { return nil })
	require.NoError(t, err)
	require.NoError(t, runtime.current.Start(ctx))
	t.Cleanup(func() { _ = runtime.current.Stop(context.Background()) })

	got, err := first.exchange("Remote MBP")
	require.NoError(t, err)
	require.Equal(t, "Remote MBP", got)
	runtime.reloadFromDisk(ctx)

	current := runtime.current.transports.transports["test"].(*reloadTransport)
	got, err = current.exchange("Remote FHD")
	require.NoError(t, err)
	require.Equal(t, "Remote FHD", got)
}

func TestReloadReportsUnrecoverableRollback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(t.TempDir(), "agent.toml")
	require.NoError(t, os.WriteFile(path, []byte("[plugins.echo]\nenabled = true\n"), 0o600))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	builds := 0
	factory := plugin.Factory{
		Descriptor: plugin.Descriptor{ID: "echo"},
		Build: func(map[string]any, *zap.Logger) (plugin.Plugin, error) {
			builds++
			if builds > 1 {
				return nil, errors.New("plugin unavailable")
			}
			return reloadPlugin{}, nil
		},
	}
	runtime, err := NewRuntime(path, cfg, zap.NewNop(), []plugin.Factory{factory},
		func(*config.Config, *zap.Logger) []coretransport.Transport {
			return []coretransport.Transport{&reloadTransport{}}
		},
		[]coretransport.Transport{&reloadTransport{}}, func(*config.Config) error { return nil })
	require.NoError(t, err)
	require.NoError(t, runtime.current.Start(ctx))
	t.Cleanup(func() { _ = runtime.current.Stop(context.Background()) })
	runtime.reloadFromDisk(ctx)
	select {
	case err := <-runtime.failures:
		require.ErrorContains(t, err, "restore previous runtime: create plugin")
	default:
		t.Fatal("a runtime without working plugins was left running")
	}
}

type reloadTransport struct {
	host      coretransport.Host
	ctx       context.Context
	cancel    context.CancelFunc
	stopError error
}

func (*reloadTransport) ID() string { return "test" }

func (t *reloadTransport) Start(ctx context.Context, host coretransport.Host) error {
	t.ctx, t.cancel = context.WithCancel(ctx)
	t.host = host
	return nil
}

func (t *reloadTransport) Stop(context.Context) error {
	t.cancel()
	err := t.stopError
	t.stopError = nil
	return err
}

func (t *reloadTransport) exchange(value string) (string, error) {
	if t.ctx == nil {
		return "", errors.New("transport is not running")
	}
	if err := t.ctx.Err(); err != nil {
		return "", err
	}
	reply := make(chan string, 1)
	err := t.host.RouteCommand(t.ctx, plugin.Command{EntityID: "echo.mode", Payload: echoRequest{value, reply}})
	if err != nil {
		return "", err
	}
	return <-reply, nil
}

type echoRequest struct {
	mode  string
	reply chan string
}

type reloadPlugin struct{}

func (reloadPlugin) Start(_ context.Context, host plugin.PluginHost) error {
	if err := host.RegisterEntity(entity.Entity{ID: "echo.mode", Name: "Mode", Kind: entity.KindSelect, Options: []entity.Option{{Value: "Remote FHD"}, {Value: "Remote MBP"}}}); err != nil {
		return err
	}
	return host.SubscribeCommand("echo.mode", func(_ context.Context, command plugin.Command) error {
		request := command.Payload.(echoRequest)
		request.reply <- request.mode
		return nil
	})
}

func (reloadPlugin) Stop(context.Context) error { return nil }
