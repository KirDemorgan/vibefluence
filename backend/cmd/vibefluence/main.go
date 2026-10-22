package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"vibefluence/internal/app"
	"vibefluence/internal/config"
	"vibefluence/internal/logging"
	"vibefluence/internal/server"
	"vibefluence/internal/web"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	flags := flag.NewFlagSet("vibefluence", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", getenv("VIBEFLUENCE_CONFIG"), "path to the YAML configuration file")
	showVersion := flags.Bool("version", false, "print the version and exit")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		_, _ = fmt.Fprintln(stdout, version)
		return 0
	}

	cfg, err := config.Load(*configPath, getenv)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "vibefluence:", err)
		return 1
	}
	logger, err := logging.New(stderr, cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "vibefluence:", err)
		return 1
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.Server.Addr)
	if err != nil {
		logger.Error("listen failed", slog.String("addr", cfg.Server.Addr), slog.Any("error", err))
		return 1
	}
	logger.Info("listening",
		slog.String("addr", listener.Addr().String()),
		slog.String("version", version),
		slog.Int("repositories", len(cfg.Repositories)),
	)

	application, err := app.New(cfg, logger, app.WithVersion(version))
	if err != nil {
		logger.Error("cannot build the application", slog.Any("error", err))
		_ = listener.Close()
		return 1
	}

	runner := server.Runner{
		Handler:         application.Handler(web.Assets()),
		Logger:          logger,
		ShutdownTimeout: cfg.Server.ShutdownTimeout,
	}
	if err := runner.Serve(ctx, listener); err != nil {
		logger.Error("server stopped", slog.Any("error", err))
		return 1
	}
	logger.Info("stopped")
	return 0
}
