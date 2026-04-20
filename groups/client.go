package groups

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// graphql executes an authenticated Facebook GraphQL POST request.
// It handles rate limiting, retries, session refresh, and the for(;;); prefix.
func (c *Client) graphql(ctx context.Context, friendlyName string, variables interface{}) (json.RawMessage, error) {
	docID := c.docID(friendlyName)
	if docID == "" {
		return nil, fmt.Errorf("%w: no doc_id registered for %q", ErrInvalidParams, friendlyName)
	}

	varsJSON, err := json.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("%w: marshalling variables: %v", ErrInvalidParams, err)
	}

	attempts := c.maxRetries
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	bootstrapped := false
	for i := 0; i < attempts; i++ {
		if i > 0 {
			wait := c.retryBase * time.Duration(math.Pow(2, float64(i-1)))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		data, err := c.doGraphQL(ctx, friendlyName, docID, varsJSON)
		if err == nil {
			return data, nil
		}

		// On session expiry, re-bootstrap exactly once then retry.
		if errors.Is(err, ErrSessionExpired) && !bootstrapped {
			bootstrapped = true
			if bErr := c.bootstrap(); bErr == nil {
				continue
			}
			return nil, err
		}

		if isNonRetriable(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// doGraphQL performs a single form-encoded POST to /api/graphql/.
func (c *Client) doGraphQL(ctx context.Context, friendlyName, docID string, varsJSON []byte) (json.RawMessage, error) {
	c.waitForGap(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	dtsg, lsd, rev, hs, hsi, spinT, spinS, jazoest := c.session.snap()
	req64 := strconv.FormatUint(c.reqCounter.Add(1), 16)

	form := url.Values{}
	form.Set("av", c.cookies.CUser)
	form.Set("__user", c.cookies.CUser)
	form.Set("__a", "1")
	form.Set("__req", req64)
	form.Set("__hs", hs)
	form.Set("dpr", "1")
	form.Set("__ccg", "EXCELLENT")
	form.Set("__rev", rev)
	if spinS != "" {
		form.Set("__s", spinS)
	}
	form.Set("__hsi", hsi)
	form.Set("__comet_req", "15")
	form.Set("fb_dtsg", dtsg)
	form.Set("jazoest", jazoest)
	form.Set("lsd", lsd)
	form.Set("__spin_r", rev)
	form.Set("__spin_b", "trunk")
	form.Set("__spin_t", spinT)
	form.Set("server_timestamps", "true")
	form.Set("fb_api_caller_class", "RelayModern")
	form.Set("fb_api_req_friendly_name", friendlyName)
	form.Set("variables", string(varsJSON))
	form.Set("doc_id", docID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphqlEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: building request: %v", ErrRequestFailed, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setRequestHeaders(req, friendlyName, lsd)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		// handled below
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, ErrSessionExpired
	case resp.StatusCode == http.StatusForbidden:
		return nil, ErrForbidden
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		wait := parseRetryAfter(resp.Header.Get("Retry-After"), 60*time.Second)
		time.Sleep(wait)
		return nil, ErrRateLimited
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: HTTP %d", ErrRequestFailed, resp.StatusCode)
	default:
		return nil, fmt.Errorf("%w: unexpected HTTP %d", ErrRequestFailed, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: reading body: %v", ErrRequestFailed, err)
	}

	// Strip the XSS guard prefix that Facebook prepends to every response.
	body = stripFBPrefix(body)

	var envelope gqlEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%w: decoding envelope: %v (body: %s)", ErrRequestFailed, err, truncate(string(body), 200))
	}

	if err := envelope.err(); err != nil {
		return nil, err
	}

	return envelope.Data, nil
}

// waitForGap enforces the leaky-bucket minimum request gap per client.
// It reserves the next slot atomically, releases the lock, and then sleeps
// independently so concurrent callers don't serialise behind one in-flight wait.
func (c *Client) waitForGap(ctx context.Context) {
	c.gapMu.Lock()
	now := time.Now()
	nextSlot := c.lastReqAt.Add(c.minGap)
	if now.After(nextSlot) {
		nextSlot = now
	}
	c.lastReqAt = nextSlot
	c.gapMu.Unlock()

	if wait := time.Until(nextSlot); wait > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

// stripFBPrefix removes the "for (;;);" prefix Facebook adds for XSS protection.
func stripFBPrefix(b []byte) []byte {
	prefix := []byte("for (;;);")
	if bytes.HasPrefix(b, prefix) {
		return b[len(prefix):]
	}
	return b
}

// parseRetryAfter parses the Retry-After header (seconds integer or HTTP-date).
func parseRetryAfter(val string, fallback time.Duration) time.Duration {
	if val == "" {
		return fallback
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(val); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return fallback
}

// isNonRetriable reports whether err should not be retried (4xx-class errors).
func isNonRetriable(err error) bool {
	return errors.Is(err, ErrInvalidAuth) ||
		errors.Is(err, ErrUnauthorized) ||
		errors.Is(err, ErrForbidden) ||
		errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrInvalidParams) ||
		errors.Is(err, ErrDocIDStale) ||
		errors.Is(err, ErrAlreadyMember) ||
		errors.Is(err, ErrNotMember) ||
		errors.Is(err, ErrSessionExpired)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// mutationID generates a simple client mutation ID from the current time in nanoseconds.
func mutationID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 16)
}
