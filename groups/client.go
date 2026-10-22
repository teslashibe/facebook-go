package groups

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxResponseBody caps how much of a GraphQL response is read.
const maxResponseBody = 32 << 20

// gqlPayload is one incremental (@stream / @defer) payload that followed the
// first data payload of a multi-line GraphQL response.
type gqlPayload struct {
	Path  []any
	Label string
	Data  json.RawMessage
}

// gqlResult is a decoded GraphQL response.
type gqlResult struct {
	// Data is the first non-null data payload.
	Data json.RawMessage
	// Incremental holds every later payload that carried data, in order.
	Incremental []gqlPayload
}

// gateError wraps a request-gate refusal so errors.Is reaches the gate's error.
type gateError struct{ err error }

func (e *gateError) Error() string { return "facebook/groups: request gate refused: " + e.err.Error() }
func (e *gateError) Unwrap() error { return e.err }

// graphql runs a read query and returns the first data payload. It keeps the
// signature the existing read methods use.
func (c *Client) graphql(ctx context.Context, friendlyName string, variables interface{}) (json.RawMessage, error) {
	res, err := c.read(ctx, "", friendlyName, variables)
	if err != nil {
		return nil, err
	}
	return res.Data, nil
}

// read executes a GraphQL query with retries.
//
// Retriable failures (5xx, transport errors, 429) are retried up to
// maxRetries attempts with exponential backoff. An expired session triggers
// one re-bootstrap; when it succeeds the query is retried exactly once more,
// whatever maxRetries is, and a second expiry returns ErrSessionExpired. When
// expectedUser is set, identity is re-checked after the re-bootstrap.
func (c *Client) read(ctx context.Context, expectedUser, friendlyName string, variables interface{}) (*gqlResult, error) {
	docID, varsJSON, err := c.prepare(friendlyName, variables)
	if err != nil {
		return nil, err
	}

	attempts := c.maxRetries
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	rebootstrapped := false
	for i := 0; i < attempts; i++ {
		if i > 0 {
			wait := c.retryBase * time.Duration(math.Pow(2, float64(i-1)))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		res, err := c.dispatch(ctx, friendlyName, docID, varsJSON, false)
		if err == nil {
			return res, nil
		}

		if errors.Is(err, ErrSessionExpired) {
			if rebootstrapped {
				return nil, err
			}
			rebootstrapped = true
			if bErr := c.bootstrap(ctx); bErr != nil {
				return nil, sessionLost(bErr)
			}
			if expectedUser != "" {
				if idErr := c.requireUser(expectedUser); idErr != nil {
					return nil, idErr
				}
			}
			res, err = c.dispatch(ctx, friendlyName, docID, varsJSON, false)
			if err == nil {
				return res, nil
			}
			if errors.Is(err, ErrSessionExpired) {
				return nil, err
			}
		}

		if isNonRetriable(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// mutate executes a GraphQL mutation exactly once: no retry, no re-bootstrap.
// When the request may have reached Facebook without a definite answer
// (transport error after dispatch, HTTP 5xx, undecodable body) it returns
// ErrWriteUnknown, and the caller must not replay it.
func (c *Client) mutate(ctx context.Context, friendlyName string, variables interface{}) (json.RawMessage, error) {
	docID, varsJSON, err := c.prepare(friendlyName, variables)
	if err != nil {
		return nil, err
	}
	res, err := c.dispatch(ctx, friendlyName, docID, varsJSON, true)
	if err != nil {
		return nil, err
	}
	return res.Data, nil
}

// decodeWrite decodes a mutation's data payload. Facebook answered without an
// error, so a payload we cannot read means the write may have happened.
func decodeWrite(raw json.RawMessage, v interface{}) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%w: undecodable mutation payload: %v", ErrWriteUnknown, err)
	}
	return nil
}

func (c *Client) prepare(friendlyName string, variables interface{}) (string, []byte, error) {
	docID := c.docID(friendlyName)
	if docID == "" {
		return "", nil, fmt.Errorf("%w: no doc_id registered for %q", ErrInvalidParams, friendlyName)
	}
	varsJSON, err := json.Marshal(variables)
	if err != nil {
		return "", nil, fmt.Errorf("%w: marshalling variables: %v", ErrInvalidParams, err)
	}
	return docID, varsJSON, nil
}

// sessionLost maps a failed re-bootstrap. A safety stop (checkpoint, block,
// restriction, 429, gate refusal, cancellation) is returned as is; anything
// else means the session could not be recovered.
func sessionLost(bErr error) error {
	switch {
	case errors.Is(bErr, ErrSessionExpired),
		errors.Is(bErr, ErrCheckpoint),
		errors.Is(bErr, ErrFeatureBlocked),
		errors.Is(bErr, ErrAccountRestricted),
		errors.Is(bErr, ErrRateLimited),
		errors.Is(bErr, context.Canceled),
		errors.Is(bErr, context.DeadlineExceeded):
		return bErr
	}
	var ge *gateError
	if errors.As(bErr, &ge) {
		return bErr
	}
	return fmt.Errorf("%w: re-bootstrap failed: %v", ErrSessionExpired, bErr)
}

// graphqlForm builds the form body every GraphQL POST carries.
func (c *Client) graphqlForm(friendlyName, docID string, varsJSON []byte) (url.Values, string) {
	dtsg, lsd, rev, hs, hsi, spinT, spinS, jazoest := c.session.snap()
	req64 := strconv.FormatUint(c.reqCounter.Add(1), 16)

	form := url.Values{}
	form.Set("av", c.cookies.CUser)
	form.Set("__user", c.cookies.CUser)
	form.Set("__a", "1")
	form.Set("__req", req64)
	form.Set("__hs", hs)
	form.Set("dpr", "2")
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
	return form, lsd
}

// dispatch performs a single form-encoded POST to /api/graphql/ and decodes
// the answer. write selects the mutation error rules (ErrWriteUnknown).
func (c *Client) dispatch(ctx context.Context, friendlyName, docID string, varsJSON []byte, write bool) (*gqlResult, error) {
	form, lsd := c.graphqlForm(friendlyName, docID, varsJSON)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphqlEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: building request: %v", ErrRequestFailed, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setRequestHeaders(req, friendlyName, lsd)

	resp, preflight, err := c.roundTrip(ctx, req, friendlyName)
	if err != nil {
		switch {
		case preflight:
			return nil, err
		case write && !definitelyNotSent(err):
			return nil, fmt.Errorf("%w: transport error after dispatch: %w", ErrWriteUnknown, err)
		}
		return nil, fmt.Errorf("%w: %w", ErrRequestFailed, err)
	}
	defer resp.Body.Close()
	return c.handleGraphQLResponse(req.URL, resp, friendlyName, write)
}

// roundTrip sends one physical request: it waits for the request gap, asks
// the request gate, sends with redirects disabled and absorbs Set-Cookie.
// preflight is true when the error came before anything was handed to the
// transport (gate refusal or cancellation); the caller returns it as is.
func (c *Client) roundTrip(ctx context.Context, req *http.Request, friendlyName string) (*http.Response, bool, error) {
	c.waitForGap(ctx)
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	if c.gate != nil {
		if err := c.gate(ctx); err != nil {
			return nil, true, &gateError{err: err}
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.observe(0, friendlyName, "transport_error")
		return nil, false, err
	}
	c.absorbSetCookies(req.URL, resp)
	c.updateRateLimit(resp.Header)
	return resp, false, nil
}

// definitelyNotSent reports transport errors that happen before any request
// bytes can reach the server.
func definitelyNotSent(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

// handleGraphQLResponse maps one GraphQL HTTP response to data or an error
// and records LastResponse.
func (c *Client) handleGraphQLResponse(reqURL *url.URL, resp *http.Response, friendlyName string, write bool) (*gqlResult, error) {
	status := resp.StatusCode
	httpCode := "http_" + strconv.Itoa(status)

	if status >= 300 && status < 400 {
		target, _ := reqURL.Parse(resp.Header.Get("Location"))
		switch classifyLocation(target) {
		case locCheckpoint:
			c.observe(status, friendlyName, "checkpoint_redirect")
			return nil, fmt.Errorf("%w: redirected to a checkpoint", ErrCheckpoint)
		case locRestricted:
			c.observe(status, friendlyName, "restricted_redirect")
			return nil, fmt.Errorf("%w: redirected to a disabled-account page", ErrAccountRestricted)
		case locLogin:
			c.observe(status, friendlyName, "login_redirect")
			return nil, fmt.Errorf("%w: redirected to login", ErrSessionExpired)
		}
		c.observe(status, friendlyName, httpCode)
		if write {
			return nil, fmt.Errorf("%w: unexpected HTTP %d redirect", ErrWriteUnknown, status)
		}
		return nil, fmt.Errorf("%w: unexpected HTTP %d redirect", ErrRequestFailed, status)
	}

	if status == http.StatusTooManyRequests {
		wait := c.noteRetryAfter(resp.Header)
		c.observe(status, friendlyName, httpCode)
		return nil, fmt.Errorf("%w: retry after %s", ErrRateLimited, wait)
	}

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))

	if status != http.StatusOK {
		// An explicit Facebook safety or session error in the body is a
		// definite answer, whatever the status.
		if readErr == nil {
			if _, code, ferr := parseGraphQLBody(body); ferr != nil && !errors.Is(ferr, ErrRequestFailed) {
				c.observe(status, friendlyName, code)
				return nil, ferr
			}
		}
		c.observe(status, friendlyName, httpCode)
		switch {
		case status == http.StatusUnauthorized:
			return nil, fmt.Errorf("%w: HTTP 401", ErrSessionExpired)
		case status == http.StatusForbidden:
			return nil, ErrForbidden
		case status == http.StatusNotFound:
			return nil, ErrNotFound
		case status >= 500 && write:
			return nil, fmt.Errorf("%w: HTTP %d", ErrWriteUnknown, status)
		case status >= 500:
			return nil, fmt.Errorf("%w: HTTP %d", ErrRequestFailed, status)
		default:
			return nil, fmt.Errorf("%w: unexpected HTTP %d", ErrRequestFailed, status)
		}
	}

	if readErr != nil {
		c.observe(status, friendlyName, "read_error")
		if write {
			return nil, fmt.Errorf("%w: reading body: %v", ErrWriteUnknown, readErr)
		}
		return nil, fmt.Errorf("%w: reading body: %v", ErrRequestFailed, readErr)
	}

	res, code, err := parseGraphQLBody(body)
	c.observe(status, friendlyName, code)
	if err != nil {
		if write && (code == codeDecodeError || code == codeNoData) {
			return nil, fmt.Errorf("%w: %v", ErrWriteUnknown, err)
		}
		return nil, err
	}
	return res, nil
}

// noteRetryAfter records a 429's Retry-After (default 60s) in the rate-limit
// state and pushes the next allowed request time forward.
func (c *Client) noteRetryAfter(h http.Header) time.Duration {
	wait := parseRetryAfter(h.Get("Retry-After"), 60*time.Second)
	c.rlMu.Lock()
	c.rlState.Remaining = 0
	c.rlState.RetryAfter = wait
	if c.rlState.Reset.IsZero() || time.Until(c.rlState.Reset) < wait {
		c.rlState.Reset = time.Now().Add(wait)
	}
	c.rlMu.Unlock()
	c.gapMu.Lock()
	if earliest := time.Now().Add(wait); c.lastReqAt.Before(earliest) {
		c.lastReqAt = earliest
	}
	c.gapMu.Unlock()
	return wait
}

// waitForGap enforces the min request gap, honouring rate-limit state adaptively.
// It reserves the next slot atomically, releases the lock, and then sleeps
// independently so concurrent callers don't serialise behind one in-flight wait.
func (c *Client) waitForGap(ctx context.Context) {
	gap := c.adaptiveGap()
	c.gapMu.Lock()
	now := time.Now()
	next := c.lastReqAt.Add(gap)
	if now.After(next) {
		next = now
	}
	c.lastReqAt = next
	c.gapMu.Unlock()

	if wait := time.Until(next); wait > 0 {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
		case <-t.C:
		}
		t.Stop()
	}
	c.rlMu.Lock()
	c.rlState.RetryAfter = 0
	c.rlMu.Unlock()
}

// stripFBPrefix removes the "for (;;);" prefix Facebook adds for XSS protection.
func stripFBPrefix(b []byte) []byte {
	const prefix = "for (;;);"
	if len(b) >= len(prefix) && string(b[:len(prefix)]) == prefix {
		return b[len(prefix):]
	}
	return b
}

// parseRetryAfter parses the Retry-After header (seconds integer or HTTP-date).
func parseRetryAfter(val string, fallback time.Duration) time.Duration {
	if val == "" {
		return fallback
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(val); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return fallback
}

// updateRateLimit reads standard rate-limit headers and updates tracked state.
func (c *Client) updateRateLimit(h http.Header) {
	c.rlMu.Lock()
	defer c.rlMu.Unlock()
	if v := rlHeader(h, "Limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.rlState.Limit = n
		}
	}
	if v := rlHeader(h, "Remaining"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.rlState.Remaining = n
		}
	}
	if v := rlHeader(h, "Reset"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			if ts > 1_000_000_000 {
				c.rlState.Reset = time.Unix(ts, 0)
			} else {
				c.rlState.Reset = time.Now().Add(time.Duration(ts) * time.Second)
			}
		}
	}
}

// rlHeader returns the value of a rate-limit header, checking four common prefix variants.
func rlHeader(h http.Header, suffix string) string {
	for _, p := range []string{"X-RateLimit-", "X-Rate-Limit-", "X-Ratelimit-", "RateLimit-"} {
		if v := strings.TrimSpace(h.Get(p + suffix)); v != "" {
			return v
		}
	}
	return ""
}

// adaptiveGap returns the delay before the next request based on rate-limit state.
func (c *Client) adaptiveGap() time.Duration {
	c.rlMu.Lock()
	rs := c.rlState
	c.rlMu.Unlock()

	if rs.Remaining == 0 && !rs.Reset.IsZero() {
		if d := time.Until(rs.Reset); d > 0 {
			return d + 50*time.Millisecond
		}
	}
	if rs.Remaining > 0 && !rs.Reset.IsZero() {
		if d := time.Until(rs.Reset); d > 0 {
			spread := d / time.Duration(float64(rs.Remaining)*0.9)
			if spread > c.minGap {
				return spread
			}
		}
	}
	return c.minGap
}

// isNonRetriable reports whether err should not be retried.
func isNonRetriable(err error) bool {
	var ge *gateError
	return errors.As(err, &ge) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrInvalidAuth) ||
		errors.Is(err, ErrUnauthorized) ||
		errors.Is(err, ErrForbidden) ||
		errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrInvalidParams) ||
		errors.Is(err, ErrDocIDStale) ||
		errors.Is(err, ErrAlreadyMember) ||
		errors.Is(err, ErrNotMember) ||
		errors.Is(err, ErrSessionExpired) ||
		errors.Is(err, ErrCheckpoint) ||
		errors.Is(err, ErrFeatureBlocked) ||
		errors.Is(err, ErrAccountRestricted) ||
		errors.Is(err, ErrSenderIdentityMismatch) ||
		errors.Is(err, ErrWriteUnknown)
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
