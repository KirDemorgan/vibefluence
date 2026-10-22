package server

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func limitedHandler(perMinute int, clock *fakeClock) http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return withWriteRateLimit(perMinute, clock.Now, ok)
}

func send(handler http.Handler, method, path, remote string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	request.RemoteAddr = remote
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestWriteRateLimitRejectsAfterBurstAndRecovers(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	handler := limitedHandler(60, clock)
	for i := 0; i < minLimiterBurst; i++ {
		if got := send(handler, http.MethodPut, "/api/v1/x", "10.0.0.1:1111").Code; got != http.StatusNoContent {
			t.Fatalf("request %d = %d", i, got)
		}
	}
	blocked := send(handler, http.MethodPut, "/api/v1/x", "10.0.0.1:2222")
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d", blocked.Code)
	}
	retry, err := strconv.Atoi(blocked.Header().Get("Retry-After"))
	if err != nil || retry < 1 {
		t.Errorf("Retry-After = %q", blocked.Header().Get("Retry-After"))
	}
	clock.Advance(time.Second)
	if got := send(handler, http.MethodPut, "/api/v1/x", "10.0.0.1:3333").Code; got != http.StatusNoContent {
		t.Errorf("after refill = %d", got)
	}
}

func TestWriteRateLimitIsPerClientAndSkipsReads(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	handler := limitedHandler(6, clock)
	for i := 0; i < minLimiterBurst; i++ {
		send(handler, http.MethodPost, "/api/v1/x", "10.0.0.1:1")
	}
	if got := send(handler, http.MethodPost, "/api/v1/x", "10.0.0.1:1").Code; got != http.StatusTooManyRequests {
		t.Fatalf("same client = %d", got)
	}
	if got := send(handler, http.MethodPost, "/api/v1/x", "10.0.0.2:1").Code; got != http.StatusNoContent {
		t.Errorf("other client = %d", got)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		if got := send(handler, method, "/api/v1/x", "10.0.0.1:1").Code; got != http.StatusNoContent {
			t.Errorf("%s = %d", method, got)
		}
	}
	if got := send(handler, http.MethodGet, "/assets/a.js", "10.0.0.1:1").Code; got != http.StatusNoContent {
		t.Errorf("static = %d", got)
	}
}

func TestWriteRateLimitCoversMCPForEveryMethod(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	handler := limitedHandler(6, clock)
	for i := 0; i < minLimiterBurst; i++ {
		send(handler, http.MethodGet, "/mcp", "10.0.0.1:1")
	}
	if got := send(handler, http.MethodPost, "/mcp", "10.0.0.1:1").Code; got != http.StatusTooManyRequests {
		t.Errorf("mcp = %d", got)
	}
}

func TestWriteRateLimitDisabledWhenNotPositive(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	for _, perMinute := range []int{0, -1} {
		handler := limitedHandler(perMinute, clock)
		for i := 0; i < 100; i++ {
			if got := send(handler, http.MethodPost, "/api/v1/x", "10.0.0.1:1").Code; got != http.StatusNoContent {
				t.Fatalf("perMinute=%d request %d = %d", perMinute, i, got)
			}
		}
	}
}

func TestWriteRateLimitFallsBackToRawRemoteAddr(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/x", nil)
	request.RemoteAddr = "pipe"
	if got := clientKey(request); got != "pipe" {
		t.Errorf("key = %q", got)
	}
}

func TestRateLimiterForgetsIdleClients(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	limiter := newRateLimiter(60, clock.Now)
	limiter.allow("a")
	limiter.allow("b")
	clock.Advance(limiterIdleTTL + 2*limiterSweepEvery)
	limiter.allow("c")
	if len(limiter.buckets) != 1 {
		t.Errorf("buckets = %d, want 1", len(limiter.buckets))
	}
}
