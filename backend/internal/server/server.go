package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const defaultShutdownTimeout = 15 * time.Second

type ReadinessCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

type Options struct {
	Logger *slog.Logger
	Assets fs.FS
	Checks []ReadinessCheck
	API    http.Handler
	MCP    http.Handler
}

func NewHandler(opts Options) http.Handler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
	mux.HandleFunc("GET /readyz", handleReady(logger, opts.Checks))
	if opts.API != nil {
		mux.Handle("/api/", opts.API)
	}
	if opts.MCP != nil {
		mux.Handle("/mcp", opts.MCP)
	}
	if opts.Assets != nil {
		mux.Handle("/", spaHandler(opts.Assets))
	}

	var handler http.Handler = mux
	handler = withSecurityHeaders(handler)
	handler = withRecover(logger, handler)
	handler = withAccessLog(logger, handler)
	handler = withRequestID(handler)
	return handler
}

type healthBody struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthBody{Status: "ok"})
}

func handleReady(logger *slog.Logger, checks []ReadinessCheck) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		body := healthBody{Status: "ok", Checks: make(map[string]string, len(checks))}
		status := http.StatusOK
		for _, check := range checks {
			if err := check.Check(ctx); err != nil {
				logger.WarnContext(ctx, "readiness check failed",
					slog.String("check", check.Name),
					slog.Any("error", err),
					slog.String("request_id", RequestID(ctx)),
				)
				body.Status = "unavailable"
				body.Checks[check.Name] = "failed"
				status = http.StatusServiceUnavailable
				continue
			}
			body.Checks[check.Name] = "ok"
		}
		writeJSON(w, status, body)
	}
}

type Runner struct {
	Handler         http.Handler
	Logger          *slog.Logger
	ShutdownTimeout time.Duration
}

func (r Runner) Serve(ctx context.Context, listener net.Listener) error {
	logger := r.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	timeout := r.ShutdownTimeout
	if timeout <= 0 {
		timeout = defaultShutdownTimeout
	}

	httpServer := &http.Server{
		Handler:           r.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	serveErr := make(chan error, 1)
	go func() {
		err := httpServer.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down", slog.Duration("timeout", timeout))
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		_ = httpServer.Close()
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return <-serveErr
}
