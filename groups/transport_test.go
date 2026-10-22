package groups

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeriveClientHints(t *testing.T) {
	cases := []struct {
		name, ua, brands, platform, mobile string
	}{
		{
			name:     "chrome 131 macOS",
			ua:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
			brands:   `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`,
			platform: `"macOS"`, mobile: "?0",
		},
		{
			name:     "chrome 141 windows",
			ua:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36",
			brands:   `"Google Chrome";v="141", "Not?A_Brand";v="8", "Chromium";v="141"`,
			platform: `"Windows"`, mobile: "?0",
		},
		{
			name:     "chrome 130 linux",
			ua:       "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
			brands:   `"Chromium";v="130", "Google Chrome";v="130", "Not?A_Brand";v="99"`,
			platform: `"Linux"`, mobile: "?0",
		},
		{
			name:     "chrome 120 android phone",
			ua:       "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
			brands:   `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`,
			platform: `"Android"`, mobile: "?1",
		},
		{
			name:     "edge 131 windows",
			ua:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0",
			brands:   `"Microsoft Edge";v="131", "Chromium";v="131", "Not_A Brand";v="24"`,
			platform: `"Windows"`, mobile: "?0",
		},
		{name: "firefox sends none", ua: "Mozilla/5.0 (Macintosh; Intel Mac OS X 14.7; rv:132.0) Gecko/20100101 Firefox/132.0"},
		{name: "iOS chrome sends none", ua: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/131.0.0.0 Mobile/15E148 Safari/604.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := deriveClientHints(tc.ua)
			if h.brands != tc.brands || h.platform != tc.platform || h.mobile != tc.mobile {
				t.Fatalf("hints = %+v, want brands=%s platform=%s mobile=%s", h, tc.brands, tc.platform, tc.mobile)
			}
		})
	}
}

func TestBrowserProfileHeadersOnEveryRequest(t *testing.T) {
	const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
	f := newFakeFB(t)
	f.onGraphQL("CometGroupDiscussionRootSuccessQuery", serveFixture(t, "group_feed_page1.json"))
	c := f.mustClient(WithBrowserProfile(ua))
	if _, err := c.ListGroupPosts(bg, testUser, "900000000000001", PostListOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bootstrap", "CometGroupDiscussionRootSuccessQuery"} {
		h := f.last(name).Header
		if got := h.Get("User-Agent"); got != ua {
			t.Errorf("%s User-Agent = %q", name, got)
		}
		if got := h.Get("Sec-Ch-Ua"); got != `"Google Chrome";v="141", "Not?A_Brand";v="8", "Chromium";v="141"` {
			t.Errorf("%s Sec-Ch-Ua = %q", name, got)
		}
		if got := h.Get("Sec-Ch-Ua-Platform"); got != `"Windows"` {
			t.Errorf("%s Sec-Ch-Ua-Platform = %q", name, got)
		}
		if got := h.Get("Sec-Ch-Ua-Mobile"); got != "?0" {
			t.Errorf("%s Sec-Ch-Ua-Mobile = %q", name, got)
		}
	}

	// A Firefox profile sends no client hints at all.
	f2 := newFakeFB(t)
	f2.mustClient(WithBrowserProfile("Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0"))
	if got := f2.last("bootstrap").Header.Get("Sec-Ch-Ua"); got != "" {
		t.Fatalf("firefox profile sent Sec-Ch-Ua %q", got)
	}
}

func TestWithProxyFailsClosed(t *testing.T) {
	bad := []string{"", "   ", "://missing-scheme", "not a url", "http://", "ftp://proxy.example.invalid:21", "http://proxy.example.invalid:99999"}
	for _, raw := range bad {
		var gateCalls atomic.Int32
		_, err := New(testCookies, WithProxy(raw), WithRequestGate(func(context.Context) error {
			gateCalls.Add(1)
			return errors.New("test gate: no egress")
		}))
		if !errors.Is(err, ErrInvalidParams) {
			t.Errorf("WithProxy(%q): err = %v, want ErrInvalidParams", raw, err)
		}
		if gateCalls.Load() != 0 {
			t.Errorf("WithProxy(%q): a request was attempted", raw)
		}
	}
}

// connectRecorder is a local HTTP proxy that refuses every CONNECT and
// records the requested authority, so nothing leaves the machine.
func connectRecorder(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.Host)
		mu.Unlock()
		http.Error(w, "refused by test proxy", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func TestWithProxyRoutesThroughProxy(t *testing.T) {
	proxy, seen := connectRecorder(t)
	_, err := New(testCookies, WithProxy(proxy.URL), WithMinRequestGap(0))
	if !errors.Is(err, ErrRequestFailed) {
		t.Fatalf("err = %v, want ErrRequestFailed from the refusing proxy", err)
	}
	got := seen()
	if len(got) != 1 || got[0] != "CONNECT www.facebook.com:443" {
		t.Fatalf("proxy saw %v, want one CONNECT www.facebook.com:443", got)
	}
}

func TestWithHTTPClientWinsOverWithProxy(t *testing.T) {
	proxy, seen := connectRecorder(t)
	f := newFakeFB(t)
	orders := map[string][]Option{
		"proxy first":       {WithProxy(proxy.URL), WithHTTPClient(f.httpClient())},
		"http client first": {WithHTTPClient(f.httpClient()), WithProxy(proxy.URL)},
		"bad proxy ignored": {WithProxy("://bad"), WithHTTPClient(f.httpClient())},
	}
	for name, opts := range orders {
		c, err := New(testCookies, append(opts, WithMinRequestGap(0))...)
		if err != nil {
			t.Fatalf("%s: New: %v", name, err)
		}
		if c.VerifiedUserID() != testUser {
			t.Fatalf("%s: VerifiedUserID = %q", name, c.VerifiedUserID())
		}
	}
	if got := seen(); len(got) != 0 {
		t.Fatalf("proxy was used: %v", got)
	}
	if f.count("bootstrap") != len(orders) {
		t.Fatalf("bootstrap count = %d, want %d", f.count("bootstrap"), len(orders))
	}
}

func TestRequestGateRunsBeforeEveryRequest(t *testing.T) {
	f := newFakeFB(t)
	// Every bootstrap goes through one redirect hop.
	f.onPath("/groups/feed/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://www.facebook.com/home.php?ref=hop", http.StatusFound)
	})
	f.onPath("/home.php", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bootstrapPage(t, testUser))
	})
	f.onGraphQL("CometGroupDiscussionRootSuccessQuery", func(w http.ResponseWriter, r recordedRequest, call int) {
		if call == 1 {
			writeBody(w, http.StatusUnauthorized, `{}`)
			return
		}
		_, _ = w.Write(fixture(t, "group_feed_page1.json"))
	})

	var gateCalls atomic.Int32
	var early []int
	var mu sync.Mutex
	gate := func(ctx context.Context) error {
		n := int(gateCalls.Add(1))
		if seen := f.count(""); seen != n-1 {
			mu.Lock()
			early = append(early, n)
			mu.Unlock()
		}
		return nil
	}
	c := f.mustClient(WithRequestGate(gate))
	if _, err := c.ListGroupPosts(bg, testUser, "900000000000001", PostListOptions{}); err != nil {
		t.Fatal(err)
	}
	// bootstrap (2 hops) + 401 + re-bootstrap (2 hops) + retry = 6 requests.
	if got, want := int(gateCalls.Load()), f.count(""); got != want || want != 6 {
		t.Fatalf("gate calls = %d, server requests = %d, want 6 and 6", got, want)
	}
	if len(early) != 0 {
		t.Fatalf("gate ran after its request for calls %v", early)
	}
}

func TestRequestGateRefusalAbortsAndWraps(t *testing.T) {
	errBudget := errors.New("read allowance used up")
	f := newFakeFB(t)
	_, err := f.newClient(WithRequestGate(func(context.Context) error { return errBudget }))
	if !errors.Is(err, errBudget) {
		t.Fatalf("New err = %v, want the gate error", err)
	}
	if f.count("") != 0 {
		t.Fatalf("server saw %d requests after a refusal", f.count(""))
	}

	// A refusal mid-session is not retried and sends nothing.
	var refuse atomic.Bool
	f.onGraphQL("CometGroupDiscussionRootSuccessQuery", serveFixture(t, "group_feed_page1.json"))
	c := f.mustClient(WithRetry(5, 0), WithRequestGate(func(context.Context) error {
		if refuse.Load() {
			return errBudget
		}
		return nil
	}))
	refuse.Store(true)
	_, err = c.ListGroupPosts(bg, testUser, "900000000000001", PostListOptions{})
	if !errors.Is(err, errBudget) {
		t.Fatalf("ListGroupPosts err = %v, want the gate error", err)
	}
	if f.count("CometGroupDiscussionRootSuccessQuery") != 0 {
		t.Fatal("a refused request reached the server")
	}
}

func TestAuthSnapshotTracksSetCookie(t *testing.T) {
	f := newFakeFB(t)
	f.setBootstrap(func(w http.ResponseWriter, _ *http.Request, _ int) {
		h := w.Header()
		h.Add("Set-Cookie", "fr=rotated-fr; Domain=.facebook.com; Path=/; Secure; HttpOnly")
		h.Add("Set-Cookie", "xs=rotated-xs; Domain=.facebook.com; Path=/; Secure; HttpOnly")
		h.Add("Set-Cookie", "presence=gone; Domain=.facebook.com; Path=/; Max-Age=0")
		h.Add("Set-Cookie", "sb=deleted; expires=Thu, 01-Jan-1970 00:00:01 GMT; Path=/; Domain=.facebook.com")
		h.Add("Set-Cookie", "tracker=1; Domain=.example.com; Path=/")
		h.Add("Set-Cookie", "wd=1280x720; Path=/")
		_, _ = w.Write(bootstrapPage(t, testUser))
	})
	f.onGraphQL("CometGroupDiscussionRootSuccessQuery", serveFixture(t, "group_feed_page1.json"))

	cookies := testCookies
	cookies.Jar = map[string]string{"presence": "p1", "dpr": "2", "xs": "jar-xs-loses-to-field"}
	c, err := New(cookies, WithHTTPClient(f.httpClient()), WithMinRequestGap(0))
	if err != nil {
		t.Fatal(err)
	}
	sent := f.last("bootstrap").Header.Get("Cookie")
	for _, want := range []string{"sb=fixture-sb", "c_user=" + testUser, "xs=fixture-xs", "dpr=2", "presence=p1"} {
		if !contains(sent, want) {
			t.Errorf("bootstrap Cookie header %q lacks %q", sent, want)
		}
	}

	snap := c.AuthSnapshot()
	want := map[string]string{"c_user": testUser, "xs": "rotated-xs", "fr": "rotated-fr", "datr": "fixture-datr", "dpr": "2", "wd": "1280x720"}
	for k, v := range want {
		if snap[k] != v {
			t.Errorf("AuthSnapshot[%s] = %q, want %q", k, snap[k], v)
		}
	}
	for _, gone := range []string{"presence", "sb", "tracker"} {
		if _, ok := snap[gone]; ok {
			t.Errorf("AuthSnapshot still has %s", gone)
		}
	}
	snap["xs"] = "mutated"
	if c.AuthSnapshot()["xs"] != "rotated-xs" {
		t.Fatal("AuthSnapshot returned the live map")
	}

	if _, err := c.ListGroupPosts(bg, testUser, "900000000000001", PostListOptions{}); err != nil {
		t.Fatal(err)
	}
	next := f.last("CometGroupDiscussionRootSuccessQuery").Header.Get("Cookie")
	if !contains(next, "xs=rotated-xs") || contains(next, "presence=") || contains(next, "sb=") {
		t.Fatalf("GraphQL Cookie header did not follow rotation: %q", next)
	}
}

func TestLastResponseCarriesSafeCodes(t *testing.T) {
	f := newFakeFB(t)
	f.onGraphQL("CometGroupDiscussionRootSuccessQuery", func(w http.ResponseWriter, _ recordedRequest, call int) {
		if call == 1 {
			_, _ = w.Write(fixture(t, "group_feed_page1.json"))
			return
		}
		w.Header().Set("Retry-After", "120")
		writeBody(w, http.StatusTooManyRequests, `{"error":"slow down, Jordan Sample"}`)
	})
	c := f.mustClient()
	if got := c.LastResponse(); got != (ResponseInfo{Status: 200, FriendlyName: "bootstrap", Code: "ok"}) {
		t.Fatalf("after New: %+v", got)
	}
	if _, err := c.ListGroupPosts(bg, testUser, "900000000000001", PostListOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := c.LastResponse(); got != (ResponseInfo{Status: 200, FriendlyName: "CometGroupDiscussionRootSuccessQuery", Code: "ok"}) {
		t.Fatalf("after read: %+v", got)
	}
	_, err := c.ListGroupPosts(bg, testUser, "900000000000001", PostListOptions{})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	got := c.LastResponse()
	if got.Status != 429 || got.Code != "http_429" || strings.Contains(got.Code, "Jordan") {
		t.Fatalf("after 429: %+v", got)
	}
	if rl := c.RateLimit(); rl.RetryAfter != 120*time.Second {
		t.Fatalf("RateLimit().RetryAfter = %s, want 2m0s", rl.RetryAfter)
	}
}

func TestWithDocIDsOverride(t *testing.T) {
	f := newFakeFB(t)
	f.onGraphQL("CometGroupDiscussionRootSuccessQuery", serveFixture(t, "group_feed_page1.json"))
	c := f.mustClient(WithDocIDs(map[string]string{"CometGroupDiscussionRootSuccessQuery": "1234567890123456"}))
	if _, err := c.ListGroupPosts(bg, testUser, "900000000000001", PostListOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := f.last("CometGroupDiscussionRootSuccessQuery").Form.Get("doc_id"); got != "1234567890123456" {
		t.Fatalf("doc_id = %q", got)
	}
}
