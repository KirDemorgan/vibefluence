package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func getenv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"-version"}, &stdout, &stderr, getenv(nil))
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if strings.TrimSpace(stdout.String()) != version {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestRunRejectsUnknownFlag(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(context.Background(), []string{"-nope"}, &bytes.Buffer{}, &stderr, getenv(nil)); code != 2 {
		t.Errorf("exit code = %d", code)
	}
}

func TestRunFailsOnMissingConfig(t *testing.T) {
	var stderr bytes.Buffer
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	if code := run(context.Background(), []string{"-config", missing}, &bytes.Buffer{}, &stderr, getenv(nil)); code != 1 {
		t.Errorf("exit code = %d", code)
	}
	if !strings.Contains(stderr.String(), "read config") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunReadsConfigPathFromEnvironment(t *testing.T) {
	var stderr bytes.Buffer
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	env := getenv(map[string]string{"VIBEFLUENCE_CONFIG": missing})
	if code := run(context.Background(), nil, &bytes.Buffer{}, &stderr, env); code != 1 {
		t.Errorf("exit code = %d", code)
	}
	if !strings.Contains(stderr.String(), "missing.yaml") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunFailsWhenAddressIsTaken(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()

	var stderr bytes.Buffer
	env := getenv(map[string]string{"VIBEFLUENCE_ADDR": busy.Addr().String(), "VIBEFLUENCE_LOG_FORMAT": "text"})
	if code := run(context.Background(), nil, &bytes.Buffer{}, &stderr, env); code != 1 {
		t.Errorf("exit code = %d", code)
	}
	if !strings.Contains(stderr.String(), "listen failed") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunServesAndStopsOnContextCancel(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(file, []byte("server:\n  addr: \"127.0.0.1:0\"\n  shutdownTimeout: 2s\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var stderr syncBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"-config", file}, &bytes.Buffer{}, &stderr, getenv(nil)) }()

	waitFor(t, func() bool { return strings.Contains(stderr.String(), "listening") })
	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit code = %d, stderr: %s", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after cancel")
	}
	if !strings.Contains(stderr.String(), "stopped") {
		t.Errorf("stderr = %s", stderr.String())
	}
}

func TestRunRejectsInvalidLogSettings(t *testing.T) {
	var stderr bytes.Buffer
	env := getenv(map[string]string{"VIBEFLUENCE_LOG_LEVEL": "loud"})
	if code := run(context.Background(), nil, &bytes.Buffer{}, &stderr, env); code != 1 {
		t.Errorf("exit code = %d", code)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
