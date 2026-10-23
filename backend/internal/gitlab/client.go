package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeout    = 20 * time.Second
	defaultMaxRetries = 3
	defaultRetryDelay = 250 * time.Millisecond
	maxRetryAfter     = 10 * time.Second
	maxResponseBytes  = 32 << 20
	userAgent         = "vibefluence"
)

var (
	ErrNotFound     = errors.New("gitlab: not found")
	ErrUnauthorized = errors.New("gitlab: unauthorized")
	ErrForbidden    = errors.New("gitlab: forbidden")
)

type APIError struct {
	Method  string
	Path    string
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("gitlab: %s %s: status %d", e.Method, e.Path, e.Status)
	}
	return fmt.Sprintf("gitlab: %s %s: status %d: %s", e.Method, e.Path, e.Status, e.Message)
}

func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	case ErrUnauthorized:
		return e.Status == http.StatusUnauthorized
	case ErrForbidden:
		return e.Status == http.StatusForbidden
	}
	return false
}

type Options struct {
	BaseURL        string
	Token          string
	HTTPClient     *http.Client
	MaxRetries     int
	RetryBaseDelay time.Duration
	Sleep          func(ctx context.Context, d time.Duration) error
}

type Client struct {
	base       *url.URL
	token      string
	http       *http.Client
	maxRetries int
	retryDelay time.Duration
	sleep      func(ctx context.Context, d time.Duration) error
}

func New(opts Options) (*Client, error) {
	base, err := url.Parse(strings.TrimRight(opts.BaseURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("gitlab: base URL %q must be an http(s) URL", opts.BaseURL)
	}
	client := &Client{
		base:       base,
		token:      opts.Token,
		http:       opts.HTTPClient,
		maxRetries: opts.MaxRetries,
		retryDelay: opts.RetryBaseDelay,
		sleep:      opts.Sleep,
	}
	if client.http == nil {
		client.http = &http.Client{Timeout: defaultTimeout}
	}
	if client.maxRetries < 0 {
		client.maxRetries = 0
	} else if opts.MaxRetries == 0 {
		client.maxRetries = defaultMaxRetries
	}
	if client.retryDelay <= 0 {
		client.retryDelay = defaultRetryDelay
	}
	if client.sleep == nil {
		client.sleep = sleepContext
	}
	return client, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func projectEndpoint(project, suffix string) string {
	return "/api/v4/projects/" + url.PathEscape(project) + suffix
}

func (c *Client) endpoint(path string, query url.Values) string {
	target := *c.base
	target.Path = strings.TrimRight(c.base.Path, "/") + path
	target.RawPath = ""
	if unescaped, err := url.PathUnescape(path); err == nil && unescaped != path {
		target.Path = strings.TrimRight(c.base.Path, "/") + unescaped
		target.RawPath = strings.TrimRight(c.base.EscapedPath(), "/") + path
	}
	target.RawQuery = query.Encode()
	return target.String()
}

func isIdempotent(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func (c *Client) send(ctx context.Context, method, path string, query url.Values, payload any) (*response, error) {
	var encoded []byte
	if payload != nil {
		var err error
		if encoded, err = json.Marshal(payload); err != nil {
			return nil, fmt.Errorf("gitlab: encode request: %w", err)
		}
	}

	attempts := 1
	if isIdempotent(method) {
		attempts += c.maxRetries
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			if err := c.sleep(ctx, c.backoff(attempt, lastErr)); err != nil {
				return nil, err
			}
		}
		resp, err := c.attempt(ctx, method, path, query, encoded)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = &transportError{err: err}
			continue
		}
		if resp.status >= 400 {
			apiErr := c.apiError(method, path, resp)
			if retryableStatus(resp.status) && attempt < attempts-1 {
				lastErr = &retryAfterError{APIError: apiErr, after: retryAfter(resp.header)}
				continue
			}
			return nil, apiErr
		}
		return resp, nil
	}
	return nil, unwrapAttemptError(lastErr)
}

type transportError struct{ err error }

func (e *transportError) Error() string { return "gitlab: request failed: " + e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

type retryAfterError struct {
	*APIError
	after time.Duration
}

func unwrapAttemptError(err error) error {
	var withDelay *retryAfterError
	if errors.As(err, &withDelay) {
		return withDelay.APIError
	}
	return err
}

func (c *Client) backoff(attempt int, previous error) time.Duration {
	var withDelay *retryAfterError
	if errors.As(previous, &withDelay) && withDelay.after > 0 {
		return min(withDelay.after, maxRetryAfter)
	}
	return c.retryDelay << (attempt - 1)
}

func retryAfter(header http.Header) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(header.Get("Retry-After")))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func (c *Client) attempt(ctx context.Context, method, path string, query url.Values, body []byte) (*response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.endpoint(path, query), reader)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		request.Header.Set("PRIVATE-TOKEN", c.token)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", userAgent)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	httpResponse, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = httpResponse.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(httpResponse.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxResponseBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", maxResponseBytes)
	}
	return &response{status: httpResponse.StatusCode, header: httpResponse.Header, body: raw}, nil
}

func (c *Client) apiError(method, path string, resp *response) *APIError {
	return &APIError{Method: method, Path: path, Status: resp.status, Message: errorMessage(resp.body)}
}

func errorMessage(body []byte) string {
	var decoded struct {
		Message any    `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return ""
	}
	if decoded.Error != "" {
		return decoded.Error
	}
	switch message := decoded.Message.(type) {
	case string:
		return message
	case nil:
		return ""
	default:
		encoded, err := json.Marshal(message)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	resp, err := c.send(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	return decodeBody(resp, out)
}

func decodeBody(resp *response, out any) error {
	if err := json.Unmarshal(resp.body, out); err != nil {
		return fmt.Errorf("gitlab: decode response: %w", err)
	}
	return nil
}
