package mqtt

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tarik02/home-pc-agent/internal/core/entity"
)

type DiscoveryOptions struct {
	AgentID       string
	AgentName     string
	TopicPrefix   string
	DiscoveryBase string
}

func DiscoveryTopic(e entity.Entity, opts DiscoveryOptions) string {
	base := opts.DiscoveryBase
	if base == "" {
		base = "homeassistant"
	}
	nodeID := discoveryNodeID(opts.TopicPrefix)
	if nodeID == "" {
		return fmt.Sprintf("%s/%s/%s/config", trimSlashes(base), ComponentForKind(e.Kind), objectID(opts.AgentID, e.ID))
	}
	return fmt.Sprintf("%s/%s/%s/%s/config", trimSlashes(base), ComponentForKind(e.Kind), nodeID, objectID(opts.AgentID, e.ID))
}

func LegacyDiscoveryTopic(e entity.Entity, opts DiscoveryOptions) string {
	base := opts.DiscoveryBase
	if base == "" {
		base = "homeassistant"
	}
	return fmt.Sprintf("%s/%s/%s/config", trimSlashes(base), ComponentForKind(e.Kind), objectID(opts.AgentID, e.ID))
}

func StateTopic(prefix, entityID string) string {
	return fmt.Sprintf("%s/state/%s", trimSlashes(prefix), entityID)
}

func CommandTopic(prefix, entityID string) string {
	return fmt.Sprintf("%s/command/%s", trimSlashes(prefix), entityID)
}

func AvailabilityTopic(prefix, entityID string) string {
	return fmt.Sprintf("%s/availability/%s", trimSlashes(prefix), entityID)
}

func StatusTopic(prefix string) string {
	return fmt.Sprintf("%s/status", trimSlashes(prefix))
}

func DiscoveryPayload(e entity.Entity, opts DiscoveryOptions) map[string]any {
	payload := map[string]any{
		"name":                  e.Name,
		"unique_id":             objectID(opts.AgentID, e.ID),
		"object_id":             objectID(opts.AgentID, e.ID),
		"availability_topic":    AvailabilityTopic(opts.TopicPrefix, e.ID),
		"availability_template": "{{ value_json.availability }}",
		"payload_available":     string(entity.AvailabilityOnline),
		"payload_not_available": string(entity.AvailabilityOffline),
		"device": map[string]any{
			"identifiers":  []string{opts.AgentID},
			"name":         opts.AgentName,
			"manufacturer": "home-pc-agent",
			"model":        "Windows PC control agent",
		},
	}

	if e.Icon != "" {
		payload["icon"] = e.Icon
	}
	if e.DeviceClass != "" {
		payload["device_class"] = e.DeviceClass
	}
	if e.StateClass != "" {
		payload["state_class"] = e.StateClass
	}
	if e.Unit != "" {
		payload["unit_of_measurement"] = e.Unit
	}
	if e.Category != "" {
		payload["entity_category"] = e.Category
	}

	if e.Kind != entity.KindButton {
		payload["state_topic"] = StateTopic(opts.TopicPrefix, e.ID)
	}

	if commandable(e.Kind) {
		payload["command_topic"] = CommandTopic(opts.TopicPrefix, e.ID)
	}

	switch e.Kind {
	case entity.KindSelect:
		payload["options"] = entity.OptionNames(e.Options)
		payload["command_template"] = `{"value":"{{ value }}"}`
	case entity.KindButton:
		payload["payload_press"] = `{"action":"press"}`
	case entity.KindSwitch:
		payload["payload_on"] = `{"value":true}`
		payload["payload_off"] = `{"value":false}`
		payload["state_on"] = "true"
		payload["state_off"] = "false"
	case entity.KindNumber, entity.KindText:
		payload["command_template"] = `{"value":"{{ value }}"}`
	}

	return payload
}

func ComponentForKind(kind entity.Kind) string {
	switch kind {
	case entity.KindSensor:
		return "sensor"
	case entity.KindBinarySensor:
		return "binary_sensor"
	case entity.KindSwitch:
		return "switch"
	case entity.KindSelect:
		return "select"
	case entity.KindNumber:
		return "number"
	case entity.KindButton:
		return "button"
	case entity.KindText:
		return "text"
	case entity.KindEvent:
		return "event"
	default:
		return "sensor"
	}
}

func commandable(kind entity.Kind) bool {
	switch kind {
	case entity.KindSwitch, entity.KindSelect, entity.KindNumber, entity.KindButton, entity.KindText:
		return true
	default:
		return false
	}
}

var objectIDUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_]+`)
var nodeIDUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func objectID(agentID, entityID string) string {
	value := strings.ToLower(agentID + "_" + entityID)
	value = strings.ReplaceAll(value, ".", "_")
	value = strings.ReplaceAll(value, "-", "_")
	value = objectIDUnsafe.ReplaceAllString(value, "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return "home_pc_agent_entity"
	}
	return value
}

func discoveryNodeID(topicPrefix string) string {
	value := strings.ToLower(trimSlashes(topicPrefix))
	value = strings.ReplaceAll(value, "/", "_")
	value = nodeIDUnsafe.ReplaceAllString(value, "_")
	value = strings.Trim(value, "_-")
	return value
}

func trimSlashes(value string) string {
	return strings.Trim(value, "/")
}
