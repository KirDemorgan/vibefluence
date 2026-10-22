package main

import (
	"bytes"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type syncBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buffer.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buffer.String()
}

func TestRunRejectsUnknownFlag(t *testing.T) {
	if code := run(context.Background(), []string{"-nope"}, &bytes.Buffer{}); code != 2 {
		t.Errorf("exit code = %d", code)
	}
}

func TestRunFailsWhenAddressIsTaken(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()

	var stderr bytes.Buffer
	if code := run(context.Background(), []string{"-addr", busy.Addr().String()}, &stderr); code != 1 {
		t.Errorf("exit code = %d", code)
	}
}

func TestRunServesDemoDataAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var stderr syncBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"-addr", "127.0.0.1:0", "-token", "t"}, &stderr) }()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(stderr.String(), "listening") {
		if time.Now().After(deadline) {
			t.Fatal("server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit code = %d, stderr: %s", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return")
	}
	if !strings.Contains(stderr.String(), "demo/platform") {
		t.Errorf("stderr = %s", stderr.String())
	}
}
