package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/events"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestEntityRegistrationAndRemovalEvents(t *testing.T) {
	bus := events.NewBus()
	registry := NewEntityRegistry(bus)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := bus.Subscribe(ctx, 4)

	e := entity.Entity{ID: "powerplan.mode", Name: "Power Plan", Kind: entity.KindSelect, Options: []entity.Option{{Value: "dev", Name: "Development"}}}
	require.NoError(t, registry.Register(e))
	require.Error(t, registry.Register(e))

	added := readEvent(t, ch)
	require.Equal(t, events.EntityAdded, added.Type)
	require.Equal(t, "powerplan.mode", added.EntityID)

	require.NoError(t, registry.Remove(e.ID))
	require.False(t, registry.Has(e.ID))

	removed := readEvent(t, ch)
	require.Equal(t, events.EntityRemoved, removed.Type)
	require.Equal(t, "powerplan.mode", removed.EntityID)
}

func TestCommandRouting(t *testing.T) {
	router := NewCommandRouter()
	var got plugin.Command
	unsub, err := router.Subscribe("session.lock", func(ctx context.Context, command plugin.Command) error {
		got = command
		return nil
	})
	require.NoError(t, err)

	err = router.Route(context.Background(), plugin.Command{EntityID: "session.lock", Payload: "press"})
	require.NoError(t, err)
	require.Equal(t, "press", got.Payload)

	unsub()
	err = router.Route(context.Background(), plugin.Command{EntityID: "session.lock", Payload: "press"})
	require.ErrorIs(t, err, ErrNoCommandHandler)
}

func TestPluginScopeCleanup(t *testing.T) {
	bus := events.NewBus()
	registry := NewEntityRegistry(bus)
	router := NewCommandRouter()
	scope := NewPluginScope(context.Background(), "test", registry, router, bus, zap.NewNop())

	require.NoError(t, scope.RegisterEntity(entity.Entity{ID: "test.button", Name: "Test Button", Kind: entity.KindButton}))
	require.NoError(t, scope.SubscribeCommand("test.button", func(ctx context.Context, command plugin.Command) error {
		return nil
	}))

	started := make(chan struct{})
	cancelled := make(chan struct{})
	scope.Go("wait", func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil
	})
	<-started

	require.NoError(t, router.Route(context.Background(), plugin.Command{EntityID: "test.button"}))
	require.True(t, registry.Has("test.button"))

	require.NoError(t, scope.Close(context.Background()))
	require.False(t, registry.Has("test.button"))
	require.ErrorIs(t, router.Route(context.Background(), plugin.Command{EntityID: "test.button"}), ErrNoCommandHandler)

	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("plugin goroutine was not cancelled")
	}
}

func TestPluginScopeReportsTaskErrors(t *testing.T) {
	bus := events.NewBus()
	registry := NewEntityRegistry(bus)
	router := NewCommandRouter()
	scope := NewPluginScope(context.Background(), "test", registry, router, bus, zap.NewNop())

	expected := errors.New("task failed")
	scope.Go("fail", func(ctx context.Context) error {
		return expected
	})

	err := scope.Close(context.Background())
	require.ErrorIs(t, err, expected)
}

func readEvent(t *testing.T, ch <-chan events.Event) events.Event {
	t.Helper()
	select {
	case event := <-ch:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return events.Event{}
	}
}
