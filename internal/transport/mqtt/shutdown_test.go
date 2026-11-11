package mqtt

import (
	"context"
	"testing"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/config"
)

func TestStopDisconnectsAfterCommandDrainTimeout(t *testing.T) {
	client := &shutdownClient{}
	transport := New(config.MQTTConfig{TopicPrefix: "pc"}, zap.NewNop())
	transport.client = client
	transport.done = make(chan struct{})
	close(transport.done)
	transport.commandDone = make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := transport.Stop(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, client.IsConnected())
	require.Equal(t, "pc/status", client.topic)
	require.Equal(t, `{"availability":"offline"}`, client.payload)
}

type shutdownClient struct {
	pahomqtt.Client
	disconnected bool
	topic        string
	payload      string
}

func (c *shutdownClient) IsConnected() bool { return !c.disconnected }

func (c *shutdownClient) Disconnect(uint) { c.disconnected = true }

func (c *shutdownClient) Publish(topic string, _ byte, _ bool, payload interface{}) pahomqtt.Token {
	c.topic, c.payload = topic, payload.(string)
	done := make(chan struct{})
	close(done)
	return shutdownToken{done}
}

type shutdownToken struct{ done chan struct{} }

func (shutdownToken) Wait() bool                     { return true }
func (shutdownToken) WaitTimeout(time.Duration) bool { return true }
func (t shutdownToken) Done() <-chan struct{}        { return t.done }
func (shutdownToken) Error() error                   { return nil }
