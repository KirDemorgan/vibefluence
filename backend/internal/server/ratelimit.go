package server

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	limiterIdleTTL      = 10 * time.Minute
	limiterSweepEvery   = time.Minute
	minLimiterBurst     = 10
	burstDivisor        = 6
	writeLimitErrorCode = "rate_limited"
)

type bucket struct {
	tokens float64
	seen   time.Time
}

type rateLimiter struct {
	mu        sync.Mutex
	perSecond float64
	burst     float64
	now       func() time.Time
	buckets   map[string]*bucket
	sweptAt   time.Time
}

func newRateLimiter(perMinute int, now func() time.Time) *rateLimiter {
	burst := perMinute / burstDivisor
	if burst < minLimiterBurst {
		burst = minLimiterBurst
	}
	return &rateLimiter{
		perSecond: float64(perMinute) / 60,
		burst:     float64(burst),
		now:       now,
		buckets:   make(map[string]*bucket),
	}
}

func (l *rateLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, seen: now}
		l.buckets[key] = b
	}
	elapsed := now.Sub(b.seen).Seconds()
	b.tokens = min(l.burst, b.tokens+elapsed*l.perSecond)
	b.seen = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.perSecond * float64(time.Second))
	return false, wait
}

func (l *rateLimiter) sweep(now time.Time) {
	if now.Sub(l.sweptAt) < limiterSweepEvery {
		return
	}
	l.sweptAt = now
	for key, b := range l.buckets {
		if now.Sub(b.seen) > limiterIdleTTL {
			delete(l.buckets, key)
		}
	}
}

func withWriteRateLimit(perMinute int, now func() time.Time, next http.Handler) http.Handler {
	if perMinute <= 0 {
		return next
	}
	limiter := newRateLimiter(perMinute, now)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLimited(r) {
			next.ServeHTTP(w, r)
			return
		}
		allowed, wait := limiter.allow(clientKey(r))
		if allowed {
			next.ServeHTTP(w, r)
			return
		}
		seconds := int((wait + time.Second - 1) / time.Second)
		w.Header().Set("Retry-After", strconv.Itoa(max(seconds, 1)))
		writeError(w, http.StatusTooManyRequests, writeLimitErrorCode, "too many requests")
	})
}

func isLimited(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/mcp") {
		return true
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
