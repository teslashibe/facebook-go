# facebook-go

Go client for Facebook's internal GraphQL API. Organised into **surfaces** — each a sub-package covering a distinct area of the platform.

Zero production dependencies — stdlib only.

## Surfaces

| Surface | Package | Status |
|---------|---------|--------|
| Groups | `groups` | ✅ Complete — 14 integration tests passing against live API |

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/teslashibe/facebook-go/groups"
)

func main() {
    c, err := groups.New(groups.Cookies{
        XS:    "39%3AW11lWE...",
        CUser: "1226944",
        SB:    "YXzYZl6g...",
        DATR:  "TCeNaTXI...",
        FR:    "1pRdJlAZ...",
        PSL:   "1",
    })
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    // Search for groups
    results, _ := c.SearchGroups(ctx, "golang developers")
    for _, g := range results {
        fmt.Printf("%s — %s\n", g.Name, g.URL)
    }

    // List your joined groups
    myGroups, _ := c.MyGroups(ctx)
    fmt.Printf("You're in %d groups\n", len(myGroups))

    // Read posts from a specific group
    feed, _ := c.GetGroupPosts(ctx, myGroups[0].ID)
    for _, p := range feed.Posts {
        fmt.Printf("[%s] %s: %s\n", p.GroupID, p.AuthorName, p.Message)
    }

    // Analyze trends
    report, _ := c.ScrapeGroupTrends(ctx, myGroups[0].ID)
    fmt.Printf("Analyzed %d posts, avg engagement: %.1f\n",
        report.PostsAnalyzed, report.AvgEngagement)
    for _, kw := range report.TopKeywords[:5] {
        fmt.Printf("  %s (%d)\n", kw.Term, kw.Count)
    }
}
```

## Authentication

All surfaces use cookie-based auth. Export your cookies from a logged-in browser session (e.g. with a browser extension like "EditThisCookie") and pass them to the client constructor.

### Required Cookies

| Cookie | Description |
|--------|-------------|
| `xs` | Session token (primary auth credential) |
| `c_user` | Your Facebook user ID |
| `sb` | Browser session fingerprint |
| `datr` | Device auth token |
| `fr` | Friend-request + ad tracking token |
| `ps_l` | Presence/status |

The client performs a one-time bootstrap on construction: it loads `/groups/feed/` and extracts the CSRF tokens (`fb_dtsg`, `lsd`, `__rev`, etc.) required by every GraphQL request. It also reads the logged-in user from the page's `CurrentUserInitialData`. Use `groups.NewContext(ctx, cookies, opts...)` when the bootstrap should honour a context.

If you have the browser's whole cookie jar, pass it in `Cookies.Jar` (name to value, facebook.com cookies only). The Cookie header then carries every jar cookie. `c_user` and `xs` are still required, either as the named fields or in the jar, and a non-empty named field wins over the same cookie in the jar.

```go
c, err := groups.NewContext(ctx, groups.Cookies{Jar: cookiesFromBrowser})
```

`c.AuthSnapshot()` returns the jar after every `Set-Cookie` the client has seen, with deleted and expired cookies removed, so you can store rotated cookies.

## Groups Surface — Full API

### Discovery

```go
// Search Facebook's group directory
results, err := c.SearchGroups(ctx, "crypto traders",
    groups.WithSearchLimit(20),
)

// Facebook's personalised suggestions
suggestions, err := c.DiscoverGroups(ctx)

// List all your joined groups
myGroups, err := c.MyGroups(ctx)

// Get metadata for a specific group
group, err := c.GetGroup(ctx, "265634821388311")
```

### Joining & Membership

```go
// Join an open group
err := c.JoinGroup(ctx, groupID)

// Check if a closed group has membership questions
questions, err := c.GetMembershipQuestions(ctx, groupID)
for _, q := range questions {
    fmt.Printf("Q: %s (type: %s)\n", q.Text, q.Type)
}

// Join a gated group with answers
err = c.JoinGroupWithAnswers(ctx, groupID, []groups.MembershipAnswer{
    {QuestionID: questions[0].ID, Text: "I'm a Go developer interested in..."},
})

// Leave a group
err = c.LeaveGroup(ctx, groupID)

// Create a new group
group, err := c.CreateGroup(ctx, groups.CreateGroupParams{
    Name:        "My Community",
    Privacy:     groups.PrivacyClosed,
    Description: "A place for...",
})
```

### Reading Content

```go
// Cross-group feed (posts from all joined groups)
feed, err := c.GetGroupFeed(ctx, groupID)

// Posts from one specific group
feed, err := c.GetGroupPosts(ctx, groupID)

// Paginate through older posts
for feed.HasNext {
    feed, err = c.GetGroupFeedPage(ctx, groupID, feed.NextCursor)
}

// Get a single post by ID
post, err := c.GetPost(ctx, postID)

// List group members (paginated)
members, err := c.GetGroupMembers(ctx, groupID)

// Read comments on a post
comments, err := c.GetPostComments(ctx, post.FeedbackID)
```

### Watching groups

These reads take the Facebook user id you expect the session to belong to. They fail with `ErrSenderIdentityMismatch` when the bootstrap page's `USER_ID` or the `c_user` cookie is someone else, before any GraphQL request is sent.

```go
me, err := c.WhoAmI(ctx) // fresh bootstrap page: me.UserID, me.Name, me.ProfileURL
userID := c.VerifiedUserID() // USER_ID from the last bootstrap

// Joined groups, 20 per page by default (at most 50)
page, err := c.ListJoinedGroups(ctx, userID, groups.ListOptions{})
for page.HasNext {
    page, err = c.ListJoinedGroups(ctx, userID, groups.ListOptions{Cursor: page.NextCursor})
}

// One page of a group's posts, newest first (sortingSetting CHRONOLOGICAL)
feed, err := c.ListGroupPosts(ctx, userID, groupID, groups.PostListOptions{Count: 10})
next, err := c.ListGroupPosts(ctx, userID, groupID, groups.PostListOptions{Cursor: feed.NextCursor})

// Everything newer than what you stored last time, at most 5 pages
posts, err := c.PostsSince(ctx, userID, groupID, lastSeenPostID, lastSeenAt, 5)
for _, p := range posts {
    fmt.Println(p.CreatedAt, p.Permalink, p.Author.Kind, p.Author.Name)
}
```

`PostsSince` stops before the post whose `ID` or `StoryID` is `sinceID`, before the first post older than `sinceTime`, when the feed has no next page, or after `maxPages`. A zero `sinceID` or `sinceTime` turns that stop off.

Every post carries `ID`, `StoryID`, `FeedbackID`, `GroupID`, `Message`, `CreatedAt`, `Permalink` and `Author`. `Author.Kind` is `person`, `page`, `anonymous` or `unknown`, read from the story's actor (`AuthorID` and `AuthorName` mirror it). When the story has no actor, the author falls back to `feedback.owning_profile` and stays `unknown` unless its type says otherwise. A group whose `group` or `group_feed` comes back null returns `ErrNotMember`, never an empty page. `Group.Joined` and `Group.PendingJoin` come from each row's membership state.

### Writing Content

Mutations are sent once and never retried, whatever `WithRetry` says. When the request may have reached Facebook without a clear answer (a transport error after sending, an HTTP 5xx or a body that can't be decoded) the call returns `ErrWriteUnknown`. Treat that write as possibly done and don't send it again.

```go
// Post to a group
post, err := c.CreatePost(ctx, groupID, "Hello from the Go agent!")

// Post with a link attachment
post, err = c.CreatePost(ctx, groupID, "Check this out",
    groups.WithPostAttachmentURL("https://example.com"),
)

// Comment on a post (uses FeedbackID, not post ID)
comment, err := c.CreateComment(ctx, post.FeedbackID, "Great post!")

// React to a post
err = c.ReactToPost(ctx, post.FeedbackID, groups.ReactionLike)
// Also: ReactionLove, ReactionCare, ReactionHaha, ReactionWow, ReactionSad, ReactionAngry
```

### Trend Analysis

```go
report, err := c.ScrapeGroupTrends(ctx, groupID,
    groups.WithTrendMaxPosts(200),   // default 200
    groups.WithTrendTopN(20),        // top N keywords
    groups.WithTrendStopWords([]string{"buy", "sell"}),
)

fmt.Printf("Posts analyzed: %d\n", report.PostsAnalyzed)
fmt.Printf("Avg engagement: %.1f\n", report.AvgEngagement)
fmt.Printf("Sentiment: %.2f\n", report.SentimentScore)

for _, kw := range report.TopKeywords {
    fmt.Printf("  %s (%d)\n", kw.Term, kw.Count)
}
for _, h := range report.TopHashtags {
    fmt.Printf("  #%s (%d)\n", h.Term, h.Count)
}
for _, a := range report.ActiveAuthors {
    fmt.Printf("  %s: %d posts\n", a.AuthorName, a.PostCount)
}
```

## Configuration

```go
c, err := groups.New(cookies,
    groups.WithBrowserProfile(userAgent),         // UA plus matching Sec-Ch-Ua headers
    groups.WithMinRequestGap(time.Second),        // default 800ms
    groups.WithRetry(5, time.Second),             // reads only: 5 attempts, 1s base
    groups.WithProxy("http://user:pass@host:port"),
    groups.WithHTTPClient(customClient),          // wins over WithProxy
    groups.WithRequestGate(func(ctx context.Context) error {
        return budget.Allow(ctx)                  // runs before every request
    }),
    groups.WithDocIDs(map[string]string{          // override rotated doc_ids
        "GroupsCometFeedRegularStoriesPaginationQuery": "new_id",
    }),
)
```

- `WithHTTPClient` is the only transport when it is set, whatever the option order, and `WithProxy` is then ignored.
- `WithProxy` fails closed. A URL that doesn't parse, or has no http, https, socks5 or socks5h scheme or no host, makes `New` return `ErrInvalidParams`. The client never falls back to direct egress.
- `WithBrowserProfile(userAgent)` sends that User-Agent with `Sec-Ch-Ua`, `Sec-Ch-Ua-Platform` and `Sec-Ch-Ua-Mobile` derived from it, the way Chromium builds them for that version and platform. Firefox and Safari user agents get no client hints, as in the real browsers. `WithUserAgent` derives the hints the same way.
- `WithRequestGate` runs before every physical request, including the bootstrap and each redirect hop. If it returns an error, nothing is sent and the caller gets that error wrapped (`errors.Is` works).
- `WithDocIDs` overrides doc_ids. A doc_id Facebook no longer knows returns `ErrDocIDStale`.
- `c.LastResponse()` reports the last response's status, friendly name and a short code such as `ok`, `http_429`, `gql_368` or `checkpoint_redirect`. It never includes a body.

## Errors and session safety

| Error | When |
|-------|------|
| `ErrSessionExpired` | HTTP 401, a not-logged-in error or a redirect to `/login`. A read re-bootstraps once and retries exactly once, whatever `WithRetry` says, and returns this when that doesn't recover the session |
| `ErrCheckpoint` | A redirect to `/checkpoint/` (or two-step verification), or an error that names a checkpoint |
| `ErrFeatureBlocked` | `api_error_code` 368, "temporarily blocked", "going too fast" or "you're blocked" |
| `ErrAccountRestricted` | An error saying the account is restricted or disabled |
| `ErrRateLimited` | HTTP 429. `c.RateLimit().RetryAfter` holds the Retry-After value |
| `ErrSenderIdentityMismatch` | An identity-bound read whose session belongs to another user |
| `ErrWriteUnknown` | A mutation that may have reached Facebook without a clear answer |
| `ErrNotMember`, `ErrForbidden`, `ErrNotFound`, `ErrDocIDStale` | Group membership, access, missing objects and rotated doc_ids |

GraphQL `errors[]` and top-level `{"error": ...}` bodies are both classified. Block, checkpoint and restriction wording is matched before the generic session wording.

## Architecture

- **Cookie-based auth** — no OAuth, no official API, no browser automation
- **Session bootstrap** — one GET to `/groups/feed/` extracts 7 CSRF tokens and the logged-in user; redirects are followed by hand so the request gate and cookie rotation see every hop
- **GraphQL over HTTP** — all operations POST to `/api/graphql/` with form-encoded bodies
- **`for(;;);` stripping** — Facebook's XSS guard prefix is stripped before JSON decode
- **Leaky-bucket rate limiter** — 800ms minimum gap between requests (per client)
- **Exponential-backoff retry** — reads only: 3 attempts on 429/5xx, one re-bootstrap and one retry on session expiry; mutations are sent once
- **WARNING-tolerant** — Facebook returns 20+ relay-provider warnings per response; only CRITICAL errors are treated as failures
- **Multi-line JSON** — streaming responses parsed line-by-line
- **Relay provider variables** — standard `__relay_internal__pv__*` vars injected automatically
- **doc_id overrides** — `WithDocIDs()` for when Facebook rotates query IDs after deploys

## Running Tests

The offline tests run against sanitized fixtures in `groups/testdata/` and a local test server, with no network access:

```bash
go test -race ./...
```

Integration tests require live Facebook session cookies:

```bash
FB_SB="..." FB_DATR="..." FB_CUSER="..." FB_XS="..." FB_FR="..." FB_PSL="..." FB_PSN="..." \
  go test -tags integration -v -timeout 120s ./groups/
```

## MCP support

This package ships an [MCP](https://modelcontextprotocol.io/) tool surface in `./mcp` for use with [`teslashibe/mcptool`](https://github.com/teslashibe/mcptool)-compatible hosts (e.g. [`teslashibe/agent-setup`](https://github.com/teslashibe/agent-setup)). 21 tools cover the full `groups.Client` API: group discovery (search, suggestions, joined-groups, fetch), lifecycle (join, leave, create, gated-join with answers), feed reads (cross-group feed, single-group posts, single post, paginated comments and members), writes (create post, comment, react), and trend analysis.

```go
import (
    "github.com/teslashibe/mcptool"
    "github.com/teslashibe/facebook-go/groups"
    fbmcp "github.com/teslashibe/facebook-go/mcp"
)

client, _ := groups.New(groups.Cookies{...})
provider := fbmcp.Provider{}
for _, tool := range provider.Tools() {
    // register tool with your MCP server, passing client as the
    // opaque client argument when invoking
}
```

The identity-bound watcher reads (`ListJoinedGroups`, `ListGroupPosts`, `PostsSince`) and the session helpers (`WhoAmI`, `VerifiedUserID`, `AuthSnapshot`, `LastResponse`) are listed in `mcp.Excluded` with a reason. `AuthSnapshot` returns live cookies, so it must never reach an agent.

A coverage test in `mcp/mcp_test.go` fails if a new exported method is added to `*groups.Client` without either being wrapped by an MCP tool or being added to `mcp.Excluded` with a reason — keeping the MCP surface in lockstep with the package API is enforced by CI rather than convention.

## License

MIT
