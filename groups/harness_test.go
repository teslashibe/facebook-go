package groups

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Offline test harness. Every request the client makes to facebook.com is
// rewritten to a local httptest server; anything else is refused, so no test
// can reach the network.

const testUser = "100000000000001"

var testCookies = Cookies{
	SB:    "fixture-sb",
	DATR:  "fixture-datr",
	CUser: testUser,
	XS:    "fixture-xs",
}

// rewriteTransport sends facebook.com requests to target and refuses others.
type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !isFacebookHost(req.URL.Hostname()) {
		return nil, fmt.Errorf("test transport: refusing non-facebook host %q", req.URL.Host)
	}
	r := req.Clone(req.Context())
	r.URL.Scheme = rt.target.Scheme
	r.URL.Host = rt.target.Host
	r.Host = rt.target.Host
	return rt.base.RoundTrip(r)
}

type recordedRequest struct {
	Method string
	Path   string
	Name   string // fb_api_req_friendly_name for GraphQL, "bootstrap" for /groups/feed/
	Form   url.Values
	Vars   map[string]any
	Header http.Header
}

type gqlHandler func(w http.ResponseWriter, r recordedRequest, call int)

// fakeFB is a scripted stand-in for www.facebook.com.
type fakeFB struct {
	t   *testing.T
	srv *httptest.Server

	mu        sync.Mutex
	requests  []recordedRequest
	calls     map[string]int
	bootstrap func(w http.ResponseWriter, r *http.Request, call int)
	paths     map[string]http.HandlerFunc
	gql       map[string]gqlHandler
}

func newFakeFB(t *testing.T) *fakeFB {
	t.Helper()
	f := &fakeFB{
		t:     t,
		calls: map[string]int{},
		paths: map[string]http.HandlerFunc{},
		gql:   map[string]gqlHandler{},
	}
	f.bootstrap = func(w http.ResponseWriter, _ *http.Request, _ int) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(bootstrapPage(t, testUser))
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeFB) serve(w http.ResponseWriter, r *http.Request) {
	rec := recordedRequest{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone()}
	switch {
	case r.URL.Path == "/groups/feed/":
		rec.Name = "bootstrap"
	case r.URL.Path == "/api/graphql/":
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		rec.Form = r.PostForm
		rec.Name = r.PostForm.Get("fb_api_req_friendly_name")
		_ = json.Unmarshal([]byte(r.PostForm.Get("variables")), &rec.Vars)
	default:
		rec.Name = r.URL.Path
	}

	f.mu.Lock()
	f.requests = append(f.requests, rec)
	f.calls[rec.Name]++
	call := f.calls[rec.Name]
	boot := f.bootstrap
	h := f.gql[rec.Name]
	ph := f.paths[r.URL.Path]
	f.mu.Unlock()

	switch {
	case rec.Name == "bootstrap" && ph == nil:
		boot(w, r, call)
	case ph != nil:
		ph(w, r)
	case r.URL.Path == "/api/graphql/" && h != nil:
		h(w, rec, call)
	default:
		http.Error(w, "fakeFB: no handler for "+rec.Name, http.StatusTeapot)
	}
}

// onGraphQL scripts the answer for one friendly name.
func (f *fakeFB) onGraphQL(name string, h gqlHandler) {
	f.mu.Lock()
	f.gql[name] = h
	f.mu.Unlock()
}

// onPath scripts an extra path (redirect hops, bootstrap overrides).
func (f *fakeFB) onPath(path string, h http.HandlerFunc) {
	f.mu.Lock()
	f.paths[path] = h
	f.mu.Unlock()
}

func (f *fakeFB) setBootstrap(h func(w http.ResponseWriter, r *http.Request, call int)) {
	f.mu.Lock()
	f.bootstrap = h
	f.mu.Unlock()
}

// count returns how many requests carried name ("" counts every request).
func (f *fakeFB) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if name == "" {
		return len(f.requests)
	}
	return f.calls[name]
}

// last returns the most recent request with that name.
func (f *fakeFB) last(name string) recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if f.requests[i].Name == name {
			return f.requests[i]
		}
	}
	f.t.Fatalf("no %s request recorded", name)
	return recordedRequest{}
}

func (f *fakeFB) httpClient() *http.Client {
	target, _ := url.Parse(f.srv.URL)
	return &http.Client{Transport: &rewriteTransport{target: target, base: f.srv.Client().Transport}}
}

// newClient builds a client against the fake with no request gap and no
// retries; later options override these defaults.
func (f *fakeFB) newClient(opts ...Option) (*Client, error) {
	base := []Option{WithHTTPClient(f.httpClient()), WithMinRequestGap(0), WithRetry(1, 0)}
	return New(testCookies, append(base, opts...)...)
}

func (f *fakeFB) mustClient(opts ...Option) *Client {
	f.t.Helper()
	c, err := f.newClient(opts...)
	if err != nil {
		f.t.Fatalf("New: %v", err)
	}
	return c
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

// bootstrapPage returns the bootstrap fixture with CurrentUserInitialData for
// userID ("" removes the entry).
func bootstrapPage(t *testing.T, userID string) []byte {
	t.Helper()
	page := fixture(t, "bootstrap.html")
	if userID == "" {
		return bytes.Replace(page, []byte(`"CurrentUserInitialData"`), []byte(`"SomeOtherConfig"`), 1)
	}
	return bytes.ReplaceAll(page, []byte(testUser), []byte(userID))
}

func writeBody(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func serveFixture(t *testing.T, name string) gqlHandler {
	body := fixture(t, name)
	return func(w http.ResponseWriter, _ recordedRequest, _ int) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

func serveBody(status int, body string) gqlHandler {
	return func(w http.ResponseWriter, _ recordedRequest, _ int) { writeBody(w, status, body) }
}

func varString(r recordedRequest, key string) string {
	v, _ := r.Vars[key].(string)
	return v
}

func varNumber(r recordedRequest, key string) float64 {
	v, _ := r.Vars[key].(float64)
	return v
}

var bg = context.Background()

func contains(s, sub string) bool { return strings.Contains(s, sub) }
