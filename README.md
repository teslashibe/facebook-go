# facebook-go

Go client for Facebook's internal GraphQL API. Organised into **surfaces** — each a sub-package covering a distinct area of the platform.

## Surfaces

| Surface | Package | Status |
|---------|---------|--------|
| Groups | `groups` | 🚧 in progress |

## Authentication

All surfaces use the same cookie-based auth. Export your cookies from a logged-in browser session (e.g. with a browser extension) and pass them to the client constructor.

Required cookies: `xs`, `c_user`, `sb`, `datr`, `fr`, `ps_l`.

```go
import "github.com/teslashibe/facebook-go/groups"

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

results, err := c.SearchGroups(ctx, "crypto traders")
```

## Design

- Zero production dependencies (stdlib only)
- Cookie-based auth; one-time session bootstrap per process
- Exponential-backoff retry; 800 ms min request gap
- Fully concurrent — atomic request counter, mutex-guarded session state
- `doc_id` overrides via `WithDocIDs(map[string]string)` for when Facebook rotates query IDs after deploys

## License

MIT
