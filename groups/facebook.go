package groups

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultUserAgent   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	defaultMaxRetries  = 3
	defaultRetryBase   = 500 * time.Millisecond
	defaultMinGap      = 800 * time.Millisecond
	defaultHTTPTimeout = 30 * time.Second
	graphqlEndpoint    = "https://www.facebook.com/api/graphql/"
	referer            = "https://www.facebook.com/"
	origin             = "https://www.facebook.com"
	asbdID             = "129477"
)

// Default doc_ids keyed by the Facebook friendly query name, harvested from
// live Facebook JS bundles (2026-04-20). They rotate with Facebook deploys;
// override them with WithDocIDs.
var defaultDocIDs = map[string]string{
	// Groups — core
	"GroupsCometCrossGroupFeedContainerQuery":            "26735348219462271",
	"GroupsCometCrossGroupFeedPaginationQuery":           "26527780253581396",
	"GroupsCometLeftRailContainerQuery":                  "31152611061018930",
	"GroupsCometJoinsRootQuery":                          "24648931168042404",
	"GroupsCometDiscoverContentQuery":                    "25947833531582461",
	"GroupsCometCreateRootQuery":                         "26545572188428154",
	"GroupsCometSettingMenuQuery":                        "9746765062081053",
	"GroupsCometMoreActionMenuQuery":                     "26476435205301604",
	"GroupsCometEntityMenuEmbeddedRootQuery":             "26566099453080408",
	"GroupsCometRecAffordanceSectionQuery":               "26227921180209081",
	"GroupsCometGroupRuleEntityDialogQuery":              "25942555452109877",
	"GroupsCometHeaderInviteMenuQuery":                   "34670067305939872",
	"GroupsCometMembershipQuestionsPreloadedDialogQuery": "26323827953971452",

	// Groups — pagination & joined
	"GroupsCometAllJoinedGroupsSectionPaginationQuery":        "9974006939348139",
	"GroupsCometPendingGroupJoinsSectionPaginationQuery":      "9924911067556167",
	"GroupsCometCategoriesSectionCategoriesRefetchQuery":      "26179397388389630",
	"GroupsCometCategoriesSectionMoreSuggestionsRefetchQuery": "24774907385540983",
	"GroupsLeftRailYourGroupsPaginatedQuery":                  "9658982227546884",
	"GroupsLeftRailGroupsYouManagePaginatedQuery":             "10000015690112057",

	// Feed & posts
	"CometModernHomeFeedQuery":          "35430709819853581",
	"CometSinglePostDialogContentQuery": "26485835561067485",
	"CometFeedStoryMenuQuery":           "26578139061874750",
	"CometFeedInlineComposerQuery":      "26472956975672575",
	"ProfileCometComposerRootQuery":     "26292641347064220",
	"ProfileCometTimelineFeedQuery":     "26484140144542166",

	// Comments & reactions queries (mutations are below)
	"CometUFICommentRefetchQuery":  "26577964715169700",
	"CometUFICommentMenuQuery":     "26230165949984969",
	"CometUFIReactionsDialogQuery": "33437545572555426",
	// Alias used by GetPostComments
	"CommentsListComponentPaginationQuery": "26577964715169700",

	// Groups — utility
	"GroupsCometUFIAnonActorSwitcherMenuQuery": "25736011312767229",

	// Search
	"CometSearchKeywordDataSourceQuery":     "34279758474973265",
	"SearchCometResultsInitialResultsQuery": "35435103342769591",

	// Group detail & members
	"GroupsCometDiscussionLayoutRootQuery": "26595005356798382",
	"GroupsCometDiscussionRootQuery":       "26867841739523736",
	"GroupsCometMembersRootQuery":          "26328488663483009",
	"CometGroupRootQuery":                  "26675395318752468",

	// Membership questions (gated groups)
	"GroupsCometParticipationQuestionsDialogQuery":     "27243898798531316",
	"GroupsCometRequestToParticipateOnJoinDialogQuery": "27505387249060985",

	// Single-group feed (posts from one specific group)
	"CometGroupDiscussionRootSuccessQuery": "26867841739523736",

	// Mutations — groups (verified live via JS bundle harvest 2026-04-20)
	"useGroupRequestToJoinMutation":  "25864060869939410",
	"useGroupLeaveMutation":          "34892951610348516",
	"useGroupsCometCreateMutation":   "26410639795238208",
	"useGroupsCometFollowMutation":   "23954755464142194",
	"useGroupsCometUnfollowMutation": "9558102717592331",
	"useGroupsCometVisitMutation":    "26506406622322884",
	"useGroupHideSuggestionMutation": "9454440231347992",

	// Mutations — posts & comments (verified live)
	"GroupCometComposerCreateDialogQuery":          "27499428346323949",
	"GroupsCometInlineComposerRendererUpdateQuery": "26422734134060146",
	"ComposerStoryCreateMutation":                  "25879074401770690",
	"ComposerStoryEditMutation":                    "26629276700039334",
	"useCometUFICreateCommentMutation":             "26608664635429703",
	"CometUFIFeedbackReactMutation":                "34892649590379796",
	"ReelComposerReelPublishMutation":              "24143317328587698",
	"VideoComposerVideoPublishingMutation":         "29705123342436060",

	// Feed pagination (group-specific)
	"GroupsCometFeedRegularStoriesPaginationQuery":  "26577462205242925",
	"GroupsCometFeedHoistedStoriesnPaginationQuery": "34927333560246608",
}

// Client is a Facebook Groups API client. It is safe for concurrent use.
type Client struct {
	// cookies is the session as supplied. cookies.CUser is the acting user id
	// sent as av, __user and actor_id.
	cookies    Cookies
	httpClient *http.Client
	userAgent  string
	hints      clientHints
	docIDs     map[string]string
	maxRetries int
	retryBase  time.Duration
	minGap     time.Duration
	gate       func(ctx context.Context) error

	// Transport options are resolved once in NewContext, after every option
	// has run, so WithHTTPClient wins whatever the order and a bad WithProxy
	// URL fails closed instead of falling back to direct egress.
	customHTTP *http.Client
	proxyURL   string
	proxySet   bool

	reqCounter atomic.Uint64
	session    *sessionState

	// jarMu guards jar: every facebook.com cookie by name, kept current from
	// Set-Cookie on every response.
	jarMu sync.Mutex
	jar   map[string]string

	// lastMu guards last, the most recent physical response.
	lastMu sync.Mutex
	last   ResponseInfo

	// gapMu + lastReqAt implement the leaky-bucket rate limiter.
	gapMu     sync.Mutex
	lastReqAt time.Time

	// rlMu + rlState track the most recently observed rate-limit headers.
	rlMu    sync.Mutex
	rlState RateLimitState
}

// New constructs a Client and performs the session bootstrap immediately with
// context.Background(). See NewContext.
func New(cookies Cookies, opts ...Option) (*Client, error) {
	return NewContext(context.Background(), cookies, opts...)
}

// NewContext constructs a Client and performs the session bootstrap under ctx.
//
// It returns ErrInvalidAuth if the c_user or xs cookie is missing (from the
// struct fields or Cookies.Jar), ErrInvalidParams if WithProxy was given a URL
// that does not parse, ErrUnauthorized if the bootstrap page cannot supply
// valid tokens, ErrCheckpoint or ErrSessionExpired when the bootstrap is
// redirected to /checkpoint/ or /login, and the request gate's error (wrapped)
// when the gate refuses the bootstrap request.
func NewContext(ctx context.Context, cookies Cookies, opts ...Option) (*Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	jar := initialJar(cookies)
	if jar["c_user"] == "" || jar["xs"] == "" {
		return nil, fmt.Errorf("%w: CUser and XS must both be non-empty", ErrInvalidAuth)
	}
	cookies.CUser = jar["c_user"]
	cookies.XS = jar["xs"]

	docIDs := make(map[string]string, len(defaultDocIDs))
	for k, v := range defaultDocIDs {
		docIDs[k] = v
	}

	c := &Client{
		cookies:    cookies,
		userAgent:  defaultUserAgent,
		docIDs:     docIDs,
		maxRetries: defaultMaxRetries,
		retryBase:  defaultRetryBase,
		minGap:     defaultMinGap,
		session:    &sessionState{},
		jar:        jar,
	}

	for _, o := range opts {
		if o != nil {
			o(c)
		}
	}

	hc, err := c.resolveTransport()
	if err != nil {
		return nil, err
	}
	c.httpClient = hc
	c.hints = deriveClientHints(c.userAgent)

	if err := c.bootstrap(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// resolveTransport picks the single transport the client will use. Redirects
// are never followed automatically: bootstrap follows them by hand so the
// request gate and Set-Cookie handling see every hop, and GraphQL treats a
// redirect as an answer (checkpoint, login or unexpected).
func (c *Client) resolveTransport() (*http.Client, error) {
	var hc *http.Client
	switch {
	case c.customHTTP != nil:
		cp := *c.customHTTP
		hc = &cp
	case c.proxySet:
		u, err := parseProxyURL(c.proxyURL)
		if err != nil {
			return nil, err
		}
		var t *http.Transport
		if dt, ok := http.DefaultTransport.(*http.Transport); ok {
			t = dt.Clone()
		} else {
			t = &http.Transport{}
		}
		t.Proxy = http.ProxyURL(u)
		hc = &http.Client{Timeout: defaultHTTPTimeout, Transport: t}
	default:
		hc = &http.Client{Timeout: defaultHTTPTimeout}
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return hc, nil
}

// parseProxyURL validates a WithProxy URL. Error messages never echo the URL,
// which may carry proxy credentials.
func parseProxyURL(raw string) (*url.URL, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, fmt.Errorf("%w: WithProxy URL is empty", ErrInvalidParams)
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("%w: WithProxy URL does not parse", ErrInvalidParams)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("%w: WithProxy URL needs an http, https, socks5 or socks5h scheme", ErrInvalidParams)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("%w: WithProxy URL has no host", ErrInvalidParams)
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("%w: WithProxy URL has an invalid port", ErrInvalidParams)
		}
	}
	return u, nil
}

// Option configures a Client.
type Option func(*Client)

// WithUserAgent overrides the default Chrome User-Agent string. The
// Sec-Ch-Ua client hints are derived from it, as with WithBrowserProfile.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// WithBrowserProfile sends userAgent (normally navigator.userAgent from the
// browser that minted the session) with Sec-Ch-Ua, Sec-Ch-Ua-Platform and
// Sec-Ch-Ua-Mobile derived from it: the Chrome major version, the brand
// (Google Chrome, Microsoft Edge, Opera) and the platform (macOS, Windows,
// Linux, Chrome OS, Android). Non-Chromium user agents send no client hints,
// as those browsers do. An empty userAgent is ignored.
func WithBrowserProfile(userAgent string) Option {
	return func(c *Client) {
		if ua := strings.TrimSpace(userAgent); ua != "" {
			c.userAgent = ua
		}
	}
}

// WithRequestGate installs a function called before every physical HTTP
// request, bootstrap and each redirect hop included. A non-nil error aborts
// that request before anything is sent; the caller gets it wrapped, so
// errors.Is(err, gateErr) works. Gate refusals are never retried.
func WithRequestGate(gate func(ctx context.Context) error) Option {
	return func(c *Client) { c.gate = gate }
}

// WithDocIDs overrides one or more GraphQL doc_ids. The key is the Facebook
// friendly query name (e.g. "GroupsCometFeedRegularStoriesPaginationQuery").
// A doc_id Facebook no longer knows raises ErrDocIDStale.
func WithDocIDs(overrides map[string]string) Option {
	return func(c *Client) {
		for k, v := range overrides {
			c.docIDs[k] = v
		}
	}
}

// WithRetry configures retry behaviour for reads.
// Default: 3 attempts, 500ms exponential base (500ms → 1s → 2s).
// Set maxAttempts to 0 or 1 to disable retries. Whatever maxAttempts is, a
// read that hits an expired session gets exactly one retry after a successful
// re-bootstrap. Mutations are never retried.
func WithRetry(maxAttempts int, base time.Duration) Option {
	return func(c *Client) {
		c.maxRetries = maxAttempts
		c.retryBase = base
	}
}

// WithHTTPClient makes hc the only transport. It wins over WithProxy whatever
// the option order. The client is copied, so the caller's value is never
// modified. Nil is ignored.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.customHTTP = hc
		}
	}
}

// WithProxy routes all HTTP traffic through the given proxy URL
// (e.g. "http://user:pass@host:port"). A URL that does not parse, or has no
// supported scheme or host, makes New and NewContext return ErrInvalidParams;
// the client never falls back to direct egress. Ignored when WithHTTPClient
// is set.
func WithProxy(proxyURL string) Option {
	return func(c *Client) {
		c.proxyURL = proxyURL
		c.proxySet = true
	}
}

// WithMinRequestGap sets the minimum time between consecutive requests.
// Default: 800ms. Lower values risk triggering Facebook's rate limiter.
func WithMinRequestGap(d time.Duration) Option {
	return func(c *Client) { c.minGap = d }
}

// RateLimit returns a snapshot of the most recently observed rate-limit state.
func (c *Client) RateLimit() RateLimitState {
	c.rlMu.Lock()
	defer c.rlMu.Unlock()
	return c.rlState
}

// LastResponse reports the most recent physical response: its HTTP status
// (0 when the transport failed), the friendly name of the call ("bootstrap"
// for the page load) and a short safe code such as "ok", "http_429",
// "gql_368" or "checkpoint_redirect". It never carries a body.
func (c *Client) LastResponse() ResponseInfo {
	c.lastMu.Lock()
	defer c.lastMu.Unlock()
	return c.last
}

func (c *Client) observe(status int, friendlyName, code string) {
	c.lastMu.Lock()
	c.last = ResponseInfo{Status: status, FriendlyName: friendlyName, Code: code}
	c.lastMu.Unlock()
}

// docID returns the doc_id for the given friendly name.
func (c *Client) docID(name string) string {
	if id, ok := c.docIDs[name]; ok {
		return id
	}
	return ""
}

// setRequestHeaders sets the common headers (User-Agent, client hints,
// cookies, etc.) on a GraphQL request.
func (c *Client) setRequestHeaders(req *http.Request, friendlyName, lsd string) {
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", referer)
	req.Header.Set("Origin", origin)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	c.hints.apply(req.Header)
	req.Header.Set("Cookie", c.cookieHeader())
	if friendlyName != "" {
		req.Header.Set("X-FB-Friendly-Name", friendlyName)
	}
	if lsd != "" {
		req.Header.Set("X-FB-LSD", lsd)
	}
	if friendlyName != "" || lsd != "" {
		req.Header.Set("X-ASBD-ID", asbdID)
	}
}

// SearchOption configures SearchGroups.
type SearchOption func(*searchOptions)

type searchOptions struct {
	location string
	limit    int
}

// WithSearchLocation filters group search results by city or region name.
func WithSearchLocation(cityOrRegion string) SearchOption {
	return func(o *searchOptions) { o.location = cityOrRegion }
}

// WithSearchLimit caps the number of groups returned. Default: 20.
func WithSearchLimit(n int) SearchOption {
	return func(o *searchOptions) { o.limit = n }
}

// PostOption configures CreatePost.
type PostOption func(*postOptions)

type postOptions struct {
	attachmentURL string
}

// WithPostAttachmentURL appends a link attachment to the created post.
func WithPostAttachmentURL(rawURL string) PostOption {
	return func(o *postOptions) { o.attachmentURL = rawURL }
}

// TrendOption configures ScrapeGroupTrends.
type TrendOption func(*trendOptions)

type trendOptions struct {
	maxPosts  int
	topN      int
	stopWords []string
}

// WithTrendMaxPosts caps the total posts fetched for trend analysis. Default: 200.
func WithTrendMaxPosts(n int) TrendOption {
	return func(o *trendOptions) { o.maxPosts = n }
}

// WithTrendTopN sets the number of top keywords and hashtags returned. Default: 20.
func WithTrendTopN(n int) TrendOption {
	return func(o *trendOptions) { o.topN = n }
}

// WithTrendStopWords appends domain-specific stop words to the bundled English
// stop list before keyword extraction.
func WithTrendStopWords(words []string) TrendOption {
	return func(o *trendOptions) { o.stopWords = append(o.stopWords, words...) }
}
