package entity

import (
	"fmt"
	"regexp"
)

type Kind string

const (
	KindSensor       Kind = "sensor"
	KindBinarySensor Kind = "binary_sensor"
	KindSwitch       Kind = "switch"
	KindSelect       Kind = "select"
	KindNumber       Kind = "number"
	KindButton       Kind = "button"
	KindText         Kind = "text"
	KindEvent        Kind = "event"
)

type Availability string

const (
	AvailabilityOnline      Availability = "online"
	AvailabilityOffline     Availability = "offline"
	AvailabilityUnavailable Availability = "unavailable"
)

type Option struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

type Entity struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Kind        Kind              `json:"kind"`
	Options     []Option          `json:"options,omitempty"`
	Icon        string            `json:"icon,omitempty"`
	DeviceClass string            `json:"device_class,omitempty"`
	StateClass  string            `json:"state_class,omitempty"`
	Unit        string            `json:"unit,omitempty"`
	Category    string            `json:"category,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)

func Validate(e Entity) error {
	if !idPattern.MatchString(e.ID) {
		return fmt.Errorf("entity id %q must match %s", e.ID, idPattern.String())
	}
	if e.Name == "" {
		return fmt.Errorf("entity %q name is required", e.ID)
	}
	if !ValidKind(e.Kind) {
		return fmt.Errorf("entity %q kind %q is not supported", e.ID, e.Kind)
	}
	if e.Kind == KindSelect && len(e.Options) == 0 {
		return fmt.Errorf("select entity %q requires at least one option", e.ID)
	}
	seen := map[string]struct{}{}
	for _, opt := range e.Options {
		if opt.Value == "" {
			return fmt.Errorf("entity %q has an option with an empty value", e.ID)
		}
		name := opt.Name
		if name == "" {
			name = opt.Value
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("entity %q has duplicate option name %q", e.ID, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func ValidKind(kind Kind) bool {
	switch kind {
	case KindSensor, KindBinarySensor, KindSwitch, KindSelect, KindNumber, KindButton, KindText, KindEvent:
		return true
	default:
		return false
	}
}

func OptionNames(options []Option) []string {
	names := make([]string, 0, len(options))
	seen := make(map[string]struct{}, len(options))
	for _, opt := range options {
		name := opt.Name
		if name == "" {
			name = opt.Value
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func OptionValue(options []Option, input string) (string, bool) {
	for _, opt := range options {
		if input == opt.Value || input == opt.Name {
			return opt.Value, true
		}
	}
	return "", false
}

func OptionName(options []Option, value string) string {
	for _, opt := range options {
		if opt.Value == value {
			if opt.Name != "" {
				return opt.Name
			}
			return opt.Value
		}
	}
	return value
}
