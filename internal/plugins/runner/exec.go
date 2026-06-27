package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type RunResult struct {
	ExitCode   int
	Stdout     string
	Stderr     string
	Truncated  bool
	Duration   time.Duration
	StartedAt  time.Time
	FinishedAt time.Time
}

type CommandSpec struct {
	Command     string
	Args        []string
	Interpreter string
	Path        string
	StaticArgs  []string
	Delivery    string
	Params      map[string]any
	Output      OutputConfig
}

func runCommand(ctx context.Context, spec CommandSpec) (RunResult, error) {
	started := time.Now()
	name, args, stdin, env, err := buildInvocation(spec)
	if err != nil {
		return RunResult{}, err
	}

	cmd := exec.CommandContext(ctx, name, args...)
	configureCommand(cmd)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	if env != nil {
		cmd.Env = env
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	finished := time.Now()

	result := RunResult{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		Duration:   finished.Sub(started),
		StartedAt:  started,
		FinishedAt: finished,
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return result, runErr
		}
	}

	if spec.Output.Capture {
		result.Stdout, result.Stderr, result.Truncated = truncateOutput(result.Stdout, result.Stderr, spec.Output.MaxBytes)
	} else {
		result.Stdout = ""
		result.Stderr = ""
	}
	return result, nil
}

func buildInvocation(spec CommandSpec) (string, []string, *strings.Reader, []string, error) {
	if strings.TrimSpace(spec.Command) != "" {
		args := append([]string{}, spec.Args...)
		args = append(args, spec.Path)
		args = append(args, spec.StaticArgs...)
		args = append(args, deliveryArgsForPowerShell(spec.Delivery, spec.Params)...)
		stdin, env, err := deliveryExtras(spec.Delivery, spec.Params)
		return spec.Command, args, stdin, env, err
	}

	interpreter := normalizeInterpreter(spec.Interpreter)
	switch interpreter {
	case interpreterPwsh, interpreterPowerShell:
		args := []string{
			"-NoProfile",
			"-NonInteractive",
			"-ExecutionPolicy",
			"Bypass",
			"-File",
			spec.Path,
		}
		args = append(args, spec.StaticArgs...)
		args = append(args, deliveryArgsForPowerShell(spec.Delivery, spec.Params)...)
		stdin, env, err := deliveryExtras(spec.Delivery, spec.Params)
		return interpreterExecutable(interpreter), args, stdin, env, err
	case interpreterCmd:
		args := []string{"/c", spec.Path}
		args = append(args, spec.StaticArgs...)
		args = append(args, deliveryArgsFlat(spec.Delivery, spec.Params)...)
		stdin, env, err := deliveryExtras(spec.Delivery, spec.Params)
		return "cmd", args, stdin, env, err
	case interpreterSh:
		args := []string{spec.Path}
		args = append(args, spec.StaticArgs...)
		args = append(args, deliveryArgsFlat(spec.Delivery, spec.Params)...)
		stdin, env, err := deliveryExtras(spec.Delivery, spec.Params)
		return "sh", args, stdin, env, err
	case interpreterBash:
		args := []string{spec.Path}
		args = append(args, spec.StaticArgs...)
		args = append(args, deliveryArgsFlat(spec.Delivery, spec.Params)...)
		stdin, env, err := deliveryExtras(spec.Delivery, spec.Params)
		return "bash", args, stdin, env, err
	case interpreterExe:
		args := append([]string{}, spec.StaticArgs...)
		args = append(args, deliveryArgsFlat(spec.Delivery, spec.Params)...)
		stdin, env, err := deliveryExtras(spec.Delivery, spec.Params)
		return spec.Path, args, stdin, env, err
	default:
		return "", nil, nil, nil, fmt.Errorf("unsupported interpreter %q", spec.Interpreter)
	}
}

func normalizeInterpreter(interpreter string) string {
	switch strings.ToLower(strings.TrimSpace(interpreter)) {
	case interpreterPowerShellE:
		return interpreterPowerShell
	default:
		return strings.ToLower(strings.TrimSpace(interpreter))
	}
}

func interpreterExecutable(interpreter string) string {
	switch interpreter {
	case interpreterPwsh:
		return "pwsh"
	case interpreterPowerShell:
		return "powershell"
	default:
		return interpreter
	}
}

func deliveryArgsForPowerShell(delivery string, params map[string]any) []string {
	if delivery != deliveryArgs || len(params) == 0 {
		return nil
	}
	keys := sortedParamKeys(params)
	args := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		args = append(args, "-"+key, formatPowerShellArgValue(params[key]))
	}
	return args
}

func deliveryArgsFlat(delivery string, params map[string]any) []string {
	if delivery != deliveryArgs || len(params) == 0 {
		return nil
	}
	keys := sortedParamKeys(params)
	args := make([]string, 0, len(keys))
	for _, key := range keys {
		args = append(args, formatFlatArgValue(params[key]))
	}
	return args
}

func deliveryExtras(delivery string, params map[string]any) (*strings.Reader, []string, error) {
	switch delivery {
	case deliveryStdinJSON:
		payload, err := json.Marshal(params)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal stdin json: %w", err)
		}
		return strings.NewReader(string(payload)), nil, nil
	case deliveryEnv:
		env := append([]string{}, os.Environ()...)
		for _, key := range sortedParamKeys(params) {
			env = append(env, "HOMEPPC_"+strings.ToUpper(key)+"="+formatFlatArgValue(params[key]))
		}
		return nil, env, nil
	default:
		return nil, nil, nil
	}
}

func formatPowerShellArgValue(value any) string {
	switch typed := value.(type) {
	case bool:
		if typed {
			return "$true"
		}
		return "$false"
	default:
		return formatFlatArgValue(value)
	}
}

func formatFlatArgValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case json.Number:
		return typed.String()
	case float64:
		return fmt.Sprintf("%v", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	default:
		return fmt.Sprint(value)
	}
}

func truncateOutput(stdout, stderr string, maxBytes int) (string, string, bool) {
	if maxBytes <= 0 {
		return stdout, stderr, false
	}
	truncated := false
	if len(stdout) > maxBytes {
		stdout = stdout[:maxBytes]
		truncated = true
	}
	if len(stderr) > maxBytes {
		stderr = stderr[:maxBytes]
		truncated = true
	}
	return stdout, stderr, truncated
}

func parseGetterValue(stdout string, getter GetterConfig) (any, error) {
	text := strings.TrimSpace(stdout)
	if getter.Format == getterFormatJSON {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			return nil, fmt.Errorf("parse getter json: %w", err)
		}
		value, ok := decoded[getter.JSONPath]
		if !ok {
			return nil, fmt.Errorf("getter json missing key %q", getter.JSONPath)
		}
		return value, nil
	}
	if text == "" {
		return nil, fmt.Errorf("getter stdout is empty")
	}
	return text, nil
}

func normalizeSwitchState(value any) (bool, error) {
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "on", "1", "yes":
			return true, nil
		case "false", "off", "0", "no":
			return false, nil
		default:
			return false, fmt.Errorf("unknown switch state %q", typed)
		}
	default:
		return false, fmt.Errorf("switch state must be boolean or string")
	}
}
