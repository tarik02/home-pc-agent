package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/tarik02/home-pc-agent/internal/config"
	"github.com/tarik02/home-pc-agent/internal/core"
	coretransport "github.com/tarik02/home-pc-agent/internal/core/transport"
	"github.com/tarik02/home-pc-agent/internal/plugins/catalog"
	"github.com/tarik02/home-pc-agent/internal/service"
	mqtttransport "github.com/tarik02/home-pc-agent/internal/transport/mqtt"
	win "github.com/tarik02/home-pc-agent/internal/windows"
)

const (
	defaultServiceName        = "home-pc-agent"
	defaultServiceDisplayName = "home-pc-agent Agent"
)

func Execute() error {
	asService, err := win.RunningAsService()
	if err != nil {
		return err
	}
	if asService {
		configPath, err := configFlagFromArgs(os.Args)
		if err != nil {
			return err
		}
		return win.RunAgentService(defaultServiceName, func(ctx context.Context) error {
			return runAgent(ctx, configPath)
		})
	}
	return NewRootCommand().Execute()
}

func NewRootCommand() *cobra.Command {
	var configPath string

	root := &cobra.Command{
		Use:   "home-pc-agent",
		Short: "Home Assistant PC control agent",
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "path to TOML config file")

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the home-pc-agent agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runAgent(ctx, configPath)
		},
	}

	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and validate configuration",
	}
	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate the TOML config",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := loadAndValidate(configPath); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "config valid: %s\n", configPath)
			return err
		},
	}
	configCmd.AddCommand(validateCmd)

	pluginsCmd := &cobra.Command{
		Use:   "plugins",
		Short: "Inspect builtin plugins",
	}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List builtin plugins and configured status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadAndValidate(configPath)
			if err != nil {
				return err
			}
			factories := catalog.Factories()
			sort.Slice(factories, func(i, j int) bool { return factories[i].ID() < factories[j].ID() })
			for _, factory := range factories {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%-32s builtin enabled=%t\n", factory.ID(), cfg.PluginEnabled(factory.ID())); err != nil {
					return err
				}
			}
			return nil
		},
	}
	pluginsCmd.AddCommand(listCmd)

	root.AddCommand(runCmd, configCmd, pluginsCmd, serviceCommand(&configPath))
	return root
}

func validateLoadedConfig(cfg *config.Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}
	if err := catalog.Validate(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}
	return nil
}

func loadAndValidate(path string) (*config.Config, error) {
	if path == "" {
		return nil, fmt.Errorf("--config is required")
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if err := validateLoadedConfig(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func runAgent(ctx context.Context, configPath string) error {
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	cfg, err := loadAndValidate(absConfig)
	if err != nil {
		return err
	}
	logger, err := zap.NewProduction()
	if err != nil {
		return err
	}
	defer func() {
		_ = logger.Sync()
	}()

	buildTransports := func(cfg *config.Config, logger *zap.Logger) []coretransport.Transport {
		return enabledTransports(cfg, logger)
	}
	runtime, err := core.NewRuntime(absConfig, cfg, logger, catalog.Factories(), buildTransports, buildTransports(cfg, logger), validateLoadedConfig)
	if err != nil {
		return err
	}
	logger.Info("home-pc-agent runtime starting", zap.String("agent_id", cfg.Agent.ID), zap.String("config", absConfig))
	return runtime.Run(ctx)
}

func configFlagFromArgs(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--config" && i+1 < len(args):
			return args[i+1], nil
		case strings.HasPrefix(args[i], "--config="):
			value := strings.TrimPrefix(args[i], "--config=")
			if value == "" {
				return "", fmt.Errorf("--config requires a value")
			}
			return value, nil
		}
	}
	return "", fmt.Errorf("--config is required")
}

func enabledTransports(cfg *config.Config, logger *zap.Logger) []coretransport.Transport {
	transports := []coretransport.Transport{}
	if cfg.Transports.MQTT.Enabled {
		transports = append(transports, mqtttransport.New(cfg.Transports.MQTT, logger))
	}
	return transports
}

func serviceCommand(configPath *string) *cobra.Command {
	var serviceName string
	var displayName string

	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage the agent service",
	}
	cmd.PersistentFlags().StringVar(&serviceName, "name", defaultServiceName, "service name")
	cmd.PersistentFlags().StringVar(&displayName, "display-name", defaultServiceDisplayName, "service display name")

	install := &cobra.Command{
		Use:   "install",
		Short: "Install the agent service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if *configPath == "" {
				return fmt.Errorf("--config is required for service install")
			}
			exePath, err := os.Executable()
			if err != nil {
				return err
			}
			absConfig, err := filepath.Abs(*configPath)
			if err != nil {
				return err
			}
			return service.Install(serviceName, displayName, exePath, []string{"run", "--config", absConfig})
		},
	}
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the agent service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return service.Uninstall(serviceName)
		},
	}
	start := &cobra.Command{
		Use:   "start",
		Short: "Start the agent service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return service.Start(serviceName)
		},
	}
	stop := &cobra.Command{
		Use:   "stop",
		Short: "Stop the agent service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return service.Stop(serviceName)
		},
	}
	cmd.AddCommand(install, uninstall, start, stop)
	return cmd
}
