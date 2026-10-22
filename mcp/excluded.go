package mcp

// Excluded enumerates exported methods on *groups.Client that are
// intentionally not exposed via MCP. Each entry must have a non-empty reason.
//
// The coverage test in mcp_test.go fails if any exported method on *Client is
// neither wrapped by a Tool nor present in this map (or vice-versa: if an
// entry here doesn't correspond to a real method).
//
// When the underlying client gains a new method:
//   - prefer to add an MCP tool for it (see groups.go / feed.go / etc.)
//   - if the method is unsuitable for an agent (internal observability,
//     auth-only helper, etc.), add it here with a reason
var Excluded = map[string]string{
	"RateLimit":        "internal observability; surfaced via the host application's MCP middleware, not as a callable tool",
	"LastResponse":     "internal observability (status and safe code of the last response); not an agent action",
	"AuthSnapshot":     "returns live session cookies; must never reach an agent",
	"VerifiedUserID":   "session bookkeeping for the host application's identity checks, not an agent action",
	"WhoAmI":           "session health probe for the host application; identity-bound reads take it as expectedUser",
	"ListJoinedGroups": "identity-bound read for host applications that pin the sender; agents use facebook_get_my_groups",
	"ListGroupPosts":   "identity-bound read for host applications that pin the sender; agents use facebook_get_group_posts",
	"PostsSince":       "identity-bound watcher read for host applications; agents use facebook_get_group_posts",
}
