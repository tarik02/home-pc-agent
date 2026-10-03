package runner

import (
	"time"
)

type StateEnvelope = map[string]any

func formatHAState(kind string, value any) any {
	if kind == kindSwitch {
		enabled, err := normalizeSwitchState(value)
		if err != nil {
			return value
		}
		if enabled {
			return "true"
		}
		return "false"
	}
	return value
}

func newStateEnvelope(kind string, state any, result RunResult) StateEnvelope {
	envelope := StateEnvelope{
		"state":       formatHAState(kind, state),
		"last_run":    result.FinishedAt.UTC().Format(time.RFC3339),
		"exit_code":   result.ExitCode,
		"duration_ms": result.Duration.Milliseconds(),
	}
	if result.Stdout != "" {
		envelope["stdout"] = result.Stdout
	}
	if result.Stderr != "" {
		envelope["stderr"] = result.Stderr
	}
	if result.Truncated {
		envelope["truncated"] = true
	}
	return envelope
}

func initialEnvelope(action ActionConfig) StateEnvelope {
	if action.State.Source == stateNone && action.Kind == kindSelect {
		return StateEnvelope{"state": stateUnknown}
	}
	return StateEnvelope{"state": nil}
}
