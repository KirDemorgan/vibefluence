package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

func New(out io.Writer, level, format string) (*slog.Logger, error) {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(strings.ToLower(level))); err != nil {
		return nil, fmt.Errorf("log level %q: %w", level, err)
	}
	options := &slog.HandlerOptions{Level: parsed}

	switch strings.ToLower(format) {
	case "json":
		return slog.New(slog.NewJSONHandler(out, options)), nil
	case "text":
		return slog.New(slog.NewTextHandler(out, options)), nil
	default:
		return nil, fmt.Errorf("log format %q is not supported", format)
	}
}
