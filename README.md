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

The client performs a one-time bootstrap on construction: it loads `/groups/feed/` and extracts the CSRF tokens (`fb_dtsg`, `lsd`, `__rev`, etc.) required by every GraphQL request.

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

### Writing Content

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
    groups.WithUserAgent("custom UA"),
    groups.WithMinRequestGap(time.Second),      // default 800ms
    groups.WithRetry(5, time.Second),            // 5 attempts, 1s base
    groups.WithProxy("http://user:pass@host:port"),
    groups.WithHTTPClient(customClient),
    groups.WithDocIDs(map[string]string{         // override rotated doc_ids
        "ComposerStoryCreateMutation": "new_id",
    }),
)
```

## Architecture

- **Cookie-based auth** — no OAuth, no official API, no browser automation
- **Session bootstrap** — one GET to `/groups/feed/` extracts 7 CSRF tokens via regex
- **GraphQL over HTTP** — all operations POST to `/api/graphql/` with form-encoded bodies
- **`for(;;);` stripping** — Facebook's XSS guard prefix is stripped before JSON decode
- **Leaky-bucket rate limiter** — 800ms minimum gap between requests (per client)
- **Exponential-backoff retry** — 3 attempts on 429/5xx, single re-bootstrap on session expiry
- **WARNING-tolerant** — Facebook returns 20+ relay-provider warnings per response; only CRITICAL errors are treated as failures
- **Multi-line JSON** — streaming responses parsed line-by-line
- **Relay provider variables** — standard `__relay_internal__pv__*` vars injected automatically
- **doc_id overrides** — `WithDocIDs()` for when Facebook rotates query IDs after deploys

## Running Tests

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

A coverage test in `mcp/mcp_test.go` fails if a new exported method is added to `*groups.Client` without either being wrapped by an MCP tool or being added to `mcp.Excluded` with a reason — keeping the MCP surface in lockstep with the package API is enforced by CI rather than convention.

## License

MIT
