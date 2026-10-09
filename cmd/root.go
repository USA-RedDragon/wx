package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"syscall"

	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/wx/internal/config"
	"github.com/USA-RedDragon/wx/internal/ingest"
	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/web"
	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
	"github.com/ztrue/shutdown"
)

func NewCommand(version, commit string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "wx",
		Version: fmt.Sprintf("%s - %s", version, commit),
		Annotations: map[string]string{
			"version": version,
			"commit":  commit,
		},
		RunE:              runRoot,
		SilenceErrors:     true,
		DisableAutoGenTag: true,
	}
	cmd.AddCommand(newImportCommand())
	return cmd
}

func loadConfig(cmd *cobra.Command) (*config.Config, error) {
	c, err := configulator.FromContext[config.Config](cmd.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to get config from context")
	}
	cfg, err := c.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	setupLogger(cfg.LogLevel)
	return cfg, nil
}

func setupLogger(level config.LogLevel) {
	var logger *slog.Logger
	switch level {
	case config.LogLevelDebug:
		logger = slog.New(tint.NewTextHandler(os.Stdout, &tint.Options{Level: slog.LevelDebug}))
	case config.LogLevelInfo:
		logger = slog.New(tint.NewTextHandler(os.Stdout, &tint.Options{Level: slog.LevelInfo}))
	case config.LogLevelWarn:
		logger = slog.New(tint.NewTextHandler(os.Stderr, &tint.Options{Level: slog.LevelWarn}))
	case config.LogLevelError:
		logger = slog.New(tint.NewTextHandler(os.Stderr, &tint.Options{Level: slog.LevelError}))
	}
	slog.SetDefault(logger)
}

func runRoot(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}

	slog.Info("wx", "version", cmd.Annotations["version"], "commit", cmd.Annotations["commit"])

	st, err := store.Open(cmd.Context(), cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	var nws *web.NWS
	if cfg.NWS.Enabled {
		nws = web.NewNWS(cfg.NWS.UserAgent, cfg.Station.Latitude, cfg.Station.Longitude, cfg.NWS.Interval, cfg.Station.Timezone)
		go nws.Run(ctx)
	}

	srv, err := web.New(cfg, st, nws, cmd.Annotations["version"])
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create server: %w", err)
	}
	if err := srv.Start(); err != nil {
		cancel()
		return fmt.Errorf("failed to start server: %w", err)
	}

	in := ingest.New(cfg, st)
	if err := in.Start(ctx); err != nil {
		cancel()
		return fmt.Errorf("failed to start MQTT ingest: %w", err)
	}

	stop := func(sig os.Signal) {
		fmt.Println("")
		slog.Info("Received signal", "signal", sig)
		if err := in.Stop(); err != nil {
			slog.Error("Failed to stop MQTT ingest", "error", err)
		}
		cancel()
		if err := srv.Stop(); err != nil {
			slog.Error("Failed to stop server", "error", err)
		}
		if err := st.Close(); err != nil {
			slog.Error("Failed to close database", "error", err)
		}
		slog.Info("Stopped gracefully")
	}
	shutdown.AddWithParam(stop)
	shutdown.Listen(syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)

	return nil
}
