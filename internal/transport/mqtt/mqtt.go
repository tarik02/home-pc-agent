package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/config"
	"github.com/tarik02/home-pc-agent/internal/core/entity"
	"github.com/tarik02/home-pc-agent/internal/core/events"
	"github.com/tarik02/home-pc-agent/internal/core/plugin"
	coretransport "github.com/tarik02/home-pc-agent/internal/core/transport"
)

const (
	transportID                = "mqtt"
	commandQueueCapacity       = 128
	entityCommandQueueCapacity = 16
)

type Transport struct {
	cfg    config.MQTTConfig
	logger *zap.Logger

	mu          sync.Mutex
	client      pahomqtt.Client
	cancel      context.CancelFunc
	done        chan struct{}
	commandDone chan struct{}
	known       map[string]entity.Entity
	host        coretransport.Host
}

func New(cfg config.MQTTConfig, logger *zap.Logger) *Transport {
	if logger == nil {
		logger = zap.NewNop()
	}
	if cfg.DiscoveryBase == "" {
		cfg.DiscoveryBase = "homeassistant"
	}
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 5 * time.Second
	}
	return &Transport{
		cfg:    cfg,
		logger: logger.With(zap.String("transport", transportID)),
		known:  make(map[string]entity.Entity),
	}
}

func (t *Transport) ID() string {
	return transportID
}

func (t *Transport) Start(ctx context.Context, host coretransport.Host) error {
	if !t.cfg.Enabled {
		t.logger.Info("mqtt transport disabled")
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	commands := make(chan plugin.Command, commandQueueCapacity)
	commandDone := make(chan struct{})
	t.mu.Lock()
	t.cancel = cancel
	t.done = make(chan struct{})
	t.commandDone = commandDone
	t.host = host
	t.mu.Unlock()

	opts := pahomqtt.NewClientOptions().
		AddBroker(t.cfg.Broker).
		SetClientID(t.cfg.ClientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetCleanSession(true).
		SetWill(StatusTopic(t.cfg.TopicPrefix), mustJSON(map[string]string{"availability": string(entity.AvailabilityOffline)}), 1, true)

	if t.cfg.Username != "" {
		opts.SetUsername(t.cfg.Username)
		opts.SetPassword(t.cfg.Password)
	}
	opts.SetOnConnectHandler(func(client pahomqtt.Client) {
		t.logger.Info("mqtt connected")
		t.publishStatus(entity.AvailabilityOnline)
		t.subscribeCommands(ctx, client, commands)
		t.publishDiscoverySnapshot()
	})
	opts.SetConnectionLostHandler(func(client pahomqtt.Client, err error) {
		t.logger.Warn("mqtt connection lost", zap.Error(err))
	})
	opts.SetReconnectingHandler(func(client pahomqtt.Client, options *pahomqtt.ClientOptions) {
		t.logger.Info("mqtt reconnecting")
	})

	client := pahomqtt.NewClient(opts)
	t.mu.Lock()
	t.client = client
	t.mu.Unlock()

	ready := make(chan struct{})
	go t.eventLoop(ctx, host, ready)
	go t.commandLoop(ctx, host, commands, commandDone)
	<-ready

	token := client.Connect()
	if !token.WaitTimeout(t.cfg.ConnectTimeout) {
		t.logger.Warn("mqtt initial connect timed out", zap.Duration("timeout", t.cfg.ConnectTimeout))
	} else if err := token.Error(); err != nil {
		t.logger.Warn("mqtt initial connect failed; transport will continue and reconnect when possible", zap.Error(err))
	}

	return nil
}

func (t *Transport) Stop(ctx context.Context) error {
	t.mu.Lock()
	cancel := t.cancel
	done := t.done
	commandDone := t.commandDone
	client := t.client
	t.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if commandDone != nil {
		select {
		case <-commandDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if client != nil && client.IsConnected() {
		t.publishStatus(entity.AvailabilityOffline)
		client.Disconnect(250)
	}
	return nil
}

func (t *Transport) commandLoop(ctx context.Context, host coretransport.Host, commands <-chan plugin.Command, done chan<- struct{}) {
	workers := make(map[string]chan plugin.Command)
	var workerGroup sync.WaitGroup
	defer func() {
		workerGroup.Wait()
		close(done)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case command := <-commands:
			worker, ok := workers[command.EntityID]
			if !ok {
				worker = make(chan plugin.Command, entityCommandQueueCapacity)
				workers[command.EntityID] = worker
				workerGroup.Add(1)
				go t.entityCommandLoop(ctx, host, command.EntityID, worker, &workerGroup)
			}
			select {
			case worker <- command:
			case <-ctx.Done():
				return
			default:
				t.logger.Warn("mqtt entity command queue full; command dropped", zap.String("entity_id", command.EntityID))
			}
		}
	}
}

func (t *Transport) entityCommandLoop(ctx context.Context, host coretransport.Host, entityID string, commands <-chan plugin.Command, workerGroup *sync.WaitGroup) {
	defer workerGroup.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case command := <-commands:
			if err := host.RouteCommand(ctx, command); err != nil {
				t.logger.Warn("mqtt command rejected", zap.String("entity_id", entityID), zap.Error(err))
			}
		}
	}
}

func (t *Transport) eventLoop(ctx context.Context, host coretransport.Host, ready chan<- struct{}) {
	defer close(t.done)
	eventsCh := host.SubscribeEvents(ctx, 128)
	close(ready)

	for _, e := range host.Entities() {
		t.handleEntityAdded(e)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventsCh:
			if !ok {
				return
			}
			t.handleEvent(event)
		}
	}
}

func (t *Transport) handleEvent(event events.Event) {
	switch event.Type {
	case events.EntityAdded, events.EntityUpdated:
		t.handleEntityAdded(event.Entity)
	case events.EntityRemoved:
		t.handleEntityRemoved(event.Entity)
	case events.StateChanged:
		t.publishState(event.EntityID, event.State)
	case events.AvailabilityChanged:
		t.publishAvailability(event.EntityID, event.Availability)
	}
}

func (t *Transport) handleEntityAdded(e entity.Entity) {
	t.mu.Lock()
	t.known[e.ID] = e
	t.mu.Unlock()

	t.publishDiscovery(e)
	t.publishAvailability(e.ID, entity.AvailabilityOnline)
}

func (t *Transport) handleEntityRemoved(e entity.Entity) {
	t.mu.Lock()
	delete(t.known, e.ID)
	t.mu.Unlock()

	opts := t.discoveryOptions()
	t.publishRaw(DiscoveryTopic(e, opts), nil, 1, true)
}

func (t *Transport) publishDiscoverySnapshot() {
	if t.host == nil {
		return
	}
	for _, e := range t.host.Entities() {
		t.handleEntityAdded(e)
	}
}

func (t *Transport) publishDiscovery(e entity.Entity) {
	opts := t.discoveryOptions()
	topic := DiscoveryTopic(e, opts)
	legacyTopic := LegacyDiscoveryTopic(e, opts)
	if legacyTopic != topic {
		t.publishRaw(legacyTopic, nil, 1, true)
	}
	if t.publishJSON(topic, DiscoveryPayload(e, opts), 1, true) {
		t.logger.Info("mqtt discovery published", zap.String("entity_id", e.ID), zap.String("topic", topic), zap.Int("options", len(e.Options)))
	}
}

func (t *Transport) publishState(entityID string, state any) {
	if !t.isKnown(entityID) {
		return
	}
	payload := statePlainBytes(state)
	t.publishRaw(StateTopic(t.cfg.TopicPrefix, entityID), payload, 1, true)
}

func statePlainBytes(state any) []byte {
	return []byte(statePlainText(state))
}

func statePlainText(state any) string {
	if payload, ok := asMapStringAny(state); ok {
		if value, ok := payload["state"]; ok {
			return formatMQTTScalar(value)
		}
	}
	return formatMQTTScalar(state)
}

func formatMQTTScalar(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(value)
	}
}

func asMapStringAny(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	}

	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil, false
	}

	out := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		out[iter.Key().String()] = iter.Value().Interface()
	}
	return out, true
}

func (t *Transport) publishAvailability(entityID string, availability entity.Availability) {
	if !t.isKnown(entityID) {
		return
	}
	t.publishJSON(AvailabilityTopic(t.cfg.TopicPrefix, entityID), map[string]string{"availability": string(availability)}, 1, true)
}

func (t *Transport) publishStatus(availability entity.Availability) {
	topic := StatusTopic(t.cfg.TopicPrefix)
	if t.publishJSON(topic, map[string]string{"availability": string(availability)}, 1, true) {
		t.logger.Info("mqtt status published", zap.String("topic", topic), zap.String("availability", string(availability)))
	}
}

func (t *Transport) subscribeCommands(ctx context.Context, client pahomqtt.Client, commands chan<- plugin.Command) {
	topic := CommandTopic(t.cfg.TopicPrefix, "+")
	token := client.Subscribe(topic, 1, func(client pahomqtt.Client, msg pahomqtt.Message) {
		entityID, ok := t.commandEntityID(msg.Topic())
		if !ok {
			t.logger.Warn("mqtt command topic did not match expected layout", zap.String("topic", msg.Topic()))
			return
		}
		payload, err := decodeCommandPayload(msg.Payload())
		if err != nil {
			t.logger.Warn("mqtt command payload is invalid", zap.String("entity_id", entityID), zap.Error(err))
			return
		}
		command := plugin.Command{
			EntityID:   entityID,
			Payload:    payload,
			Raw:        append([]byte(nil), msg.Payload()...),
			Transport:  transportID,
			ReceivedAt: time.Now(),
		}
		select {
		case commands <- command:
		case <-ctx.Done():
		default:
			t.logger.Warn("mqtt command queue full; command dropped", zap.String("entity_id", entityID))
		}
	})
	if !token.WaitTimeout(5 * time.Second) {
		t.logger.Warn("mqtt command subscribe timed out", zap.String("topic", topic))
		return
	}
	if err := token.Error(); err != nil {
		t.logger.Warn("mqtt command subscribe failed", zap.String("topic", topic), zap.Error(err))
	}
}

func (t *Transport) commandEntityID(topic string) (string, bool) {
	prefix := CommandTopic(t.cfg.TopicPrefix, "")
	if !strings.HasPrefix(topic, prefix) {
		return "", false
	}
	entityID := strings.TrimPrefix(topic, prefix)
	if entityID == "" || strings.Contains(entityID, "/") {
		return "", false
	}
	return entityID, true
}

func (t *Transport) discoveryOptions() DiscoveryOptions {
	agentID := ""
	agentName := ""
	if t.host != nil {
		agentID = t.host.AgentID()
		agentName = t.host.AgentName()
	}
	return DiscoveryOptions{
		AgentID:       agentID,
		AgentName:     agentName,
		TopicPrefix:   t.cfg.TopicPrefix,
		DiscoveryBase: t.cfg.DiscoveryBase,
	}
}

func (t *Transport) isKnown(entityID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.known[entityID]
	return ok
}

func (t *Transport) publishJSON(topic string, value any, qos byte, retained bool) bool {
	payload, err := json.Marshal(value)
	if err != nil {
		t.logger.Error("marshal mqtt payload", zap.String("topic", topic), zap.Error(err))
		return false
	}
	return t.publishRaw(topic, payload, qos, retained)
}

func (t *Transport) publishRaw(topic string, payload []byte, qos byte, retained bool) bool {
	t.mu.Lock()
	client := t.client
	t.mu.Unlock()
	if client == nil || !client.IsConnected() {
		t.logger.Debug("mqtt client not connected; skipping publish", zap.String("topic", topic))
		return false
	}
	token := client.Publish(topic, qos, retained, payload)
	if !token.WaitTimeout(5 * time.Second) {
		t.logger.Warn("mqtt publish timed out", zap.String("topic", topic))
		return false
	}
	if err := token.Error(); err != nil {
		t.logger.Warn("mqtt publish failed", zap.String("topic", topic), zap.Error(err))
		return false
	}
	return true
}

func decodeCommandPayload(payload []byte) (any, error) {
	if len(payload) == 0 {
		return nil, nil
	}

	var decoded any
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("expected JSON command payload: %w", err)
	}
	if object, ok := decoded.(map[string]any); ok {
		if value, ok := object["value"]; ok {
			return value, nil
		}
		if action, ok := object["action"]; ok {
			return action, nil
		}
	}
	return decoded, nil
}

func mustJSON(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(payload)
}
