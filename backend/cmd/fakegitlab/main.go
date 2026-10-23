package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vibefluence/internal/testing/fakegitlab"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stderr))
}

func run(ctx context.Context, args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("fakegitlab", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", "127.0.0.1:8929", "listen address")
	public := flags.Bool("public", false, "let anonymous clients read, like a public GitLab project")
	token := flags.String("token", "dev-token", "token accepted in the PRIVATE-TOKEN header")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	logger := slog.New(slog.NewTextHandler(stderr, nil))
	fake := fakegitlab.New(*token)
	fake.SetPublicReads(*public)
	fakegitlab.SeedDemo(fake)

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", *addr)
	if err != nil {
		logger.Error("listen failed", slog.Any("error", err))
		return 1
	}
	logger.Info("fake GitLab listening",
		slog.String("url", "http://"+listener.Addr().String()),
		slog.String("project", fakegitlab.DemoProject),
	)

	httpServer := &http.Server{Handler: fake, ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(listener) }()

	select {
	case err := <-serveErr:
		logger.Error("server stopped", slog.Any("error", err))
		return 1
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown failed", slog.Any("error", err))
		return 1
	}
	return 0
}
