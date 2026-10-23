// Package provider holds shared HTTP plumbing for the mod platform clients.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// ErrNotFound is returned for 404 responses.
var ErrNotFound = fmt.Errorf("not found")

// Doer sends JSON requests with retries on rate limits, 5xx and network
// errors. All requests through one Doer share a pacer, so concurrent callers
// stay under the platform's rate limit together.
type Doer struct {
	HTTP        *http.Client
	Header      http.Header
	Attempts    int
	MaxBackoff  time.Duration
	MinInterval time.Duration // minimum spacing between requests

	mu   sync.Mutex
	next time.Time // no request may start before this
}

func NewDoer(header http.Header) *Doer {
	return &Doer{
		HTTP:        &http.Client{Timeout: 30 * time.Second},
		Header:      header,
		Attempts:    5,
		MaxBackoff:  60 * time.Second,
		MinInterval: 250 * time.Millisecond, // Modrinth allows 300 requests/minute
	}
}

// wait blocks until the pacer lets the next request start.
func (d *Doer) wait(ctx context.Context) error {
	d.mu.Lock()
	start := time.Now()
	if d.next.After(start) {
		start = d.next
	}
	d.next = start.Add(d.MinInterval)
	d.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Until(start)):
		return nil
	}
}

// pause delays every caller, e.g. when the server says the quota is used up.
func (d *Doer) pause(dur time.Duration) {
	d.mu.Lock()
	if until := time.Now().Add(dur); until.After(d.next) {
		d.next = until
	}
	d.mu.Unlock()
}

// retryDelay reads Retry-After or X-Ratelimit-Reset (seconds).
func retryDelay(h http.Header) time.Duration {
	for _, k := range []string{"Retry-After", "X-Ratelimit-Reset"} {
		if s, err := strconv.Atoi(h.Get(k)); err == nil && s > 0 {
			return time.Duration(s) * time.Second
		}
	}
	return 0
}

// JSON performs method on url with an optional JSON body and decodes the
// JSON response into out.
func (d *Doer) JSON(ctx context.Context, method, url string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	backoff := time.Second
	lastErr := fmt.Errorf("%s: no attempts made", url)
	for attempt := 0; attempt < max(d.Attempts, 1); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, d.MaxBackoff)
		}
		if err := d.wait(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		for k, v := range d.Header {
			req.Header[k] = v
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")
		resp, err := d.HTTP.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if resp.Header.Get("X-Ratelimit-Remaining") == "0" {
			d.pause(retryDelay(resp.Header))
		}
		switch {
		case err != nil:
			lastErr = err
			continue
		case resp.StatusCode == http.StatusNotFound:
			return ErrNotFound
		case resp.StatusCode == http.StatusTooManyRequests:
			lastErr = fmt.Errorf("%s: rate limited", url)
			if delay := retryDelay(resp.Header); delay > 0 {
				d.pause(min(delay, d.MaxBackoff))
			}
			continue
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("%s: %s", url, resp.Status)
			continue
		case resp.StatusCode >= 400:
			return fmt.Errorf("%s: %s: %s", url, resp.Status, truncate(data, 200))
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%s: decoding response: %w", url, err)
		}
		return nil
	}
	return lastErr
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}
