package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewJSON(t *testing.T) {
	var out bytes.Buffer
	logger, err := New(&out, "info", "json")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logger.Info("hello", "key", "value")

	var record map[string]any
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatalf("output is not JSON: %v: %s", err, out.String())
	}
	if record["msg"] != "hello" || record["key"] != "value" {
		t.Errorf("unexpected record: %v", record)
	}
}

func TestNewTextRespectsLevel(t *testing.T) {
	var out bytes.Buffer
	logger, err := New(&out, "WARN", "TEXT")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logger.Info("hidden")
	logger.Warn("shown")

	if strings.Contains(out.String(), "hidden") {
		t.Errorf("info record leaked at warn level: %s", out.String())
	}
	if !strings.Contains(out.String(), "shown") {
		t.Errorf("warn record missing: %s", out.String())
	}
}

func TestNewRejectsBadSettings(t *testing.T) {
	if _, err := New(&bytes.Buffer{}, "loud", "json"); err == nil {
		t.Error("expected error for bad level")
	}
	if _, err := New(&bytes.Buffer{}, "info", "xml"); err == nil {
		t.Error("expected error for bad format")
	}
}
