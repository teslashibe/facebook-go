package groups

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// GetGroupFeed returns the first page of posts from a group's feed in
// reverse-chronological order.
// Returns ErrForbidden for closed or secret groups the user has not joined.
func (c *Client) GetGroupFeed(ctx context.Context, groupID string) (FeedPage, error) {
	if groupID == "" {
		return FeedPage{}, fmt.Errorf("%w: groupID must not be empty", ErrInvalidParams)
	}

	vars := mergeVars(map[string]interface{}{
		"feedLocation":                  "GROUP",
		"feedbackSource":                69,
		"focusCommentID":                nil,
		"privacySelectorRenderLocation": "COMET_STREAM",
		"renderLocation":                "groups_tab",
		"scale":                         2,
		"suppress_xac_groups":           false,
	})

	raw, err := c.graphql(ctx, "GroupsCometCrossGroupFeedContainerQuery", vars)
	if err != nil {
		return FeedPage{}, err
	}

	var data feedData
	if err := unmarshalData(raw, &data); err != nil {
		return FeedPage{}, err
	}

	page := data.toFeedPage()
	// Filter to the requested group if specified
	if groupID != "" {
		var filtered []Post
		for _, p := range page.Posts {
			if p.GroupID == groupID || p.GroupID == "" {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) > 0 {
			page.Posts = filtered
		}
	}
	return page, nil
}

// GetGroupFeedPage fetches a subsequent page of group feed posts using the
// cursor returned by a prior GetGroupFeed or GetGroupFeedPage call.
// When FeedPage.HasNext is false, all available posts have been returned.
func (c *Client) GetGroupFeedPage(ctx context.Context, groupID, cursor string) (FeedPage, error) {
	if groupID == "" {
		return FeedPage{}, fmt.Errorf("%w: groupID must not be empty", ErrInvalidParams)
	}
	if cursor == "" {
		return FeedPage{}, fmt.Errorf("%w: cursor must not be empty; use GetGroupFeed for the first page", ErrInvalidParams)
	}

	vars := map[string]interface{}{
		"after":          cursor,
		"feedLocation":   "GROUP",
		"feedbackSource": 69,
		"renderLocation": "groups_tab",
		"scale":          1,
	}

	raw, err := c.graphql(ctx, "GroupsCometCrossGroupFeedPaginationQuery", vars)
	if err != nil {
		return FeedPage{}, err
	}

	var data feedData
	if err := unmarshalData(raw, &data); err != nil {
		return FeedPage{}, err
	}

	return data.toFeedPage(), nil
}

// GetGroupPosts returns the first few posts (3, "Most relevant" order) from
// a single specific group, as opposed to [Client.GetGroupFeed] which returns
// the cross-group personalised feed. For watching a group use
// [Client.ListGroupPosts] or [Client.PostsSince], which read newest first.
//
// A null group or group_feed returns ErrNotMember.
func (c *Client) GetGroupPosts(ctx context.Context, groupID string) (FeedPage, error) {
	return c.listGroupPosts(ctx, "", groupID, PostListOptions{Sort: SortTopPosts, Count: 3})
}

const (
	defaultPostPageCount = 10
	maxPostPageCount     = 20
)

// ListGroupPosts returns one page of a group's posts, newest first by default
// (sortingSetting CHRONOLOGICAL). The first page is read with
// CometGroupDiscussionRootSuccessQuery and later pages, from opts.Cursor,
// with GroupsCometFeedRegularStoriesPaginationQuery.
//
// It fails with ErrSenderIdentityMismatch unless the session belongs to
// expectedUser, and with ErrNotMember when the group or its group_feed comes
// back null (never an empty page).
func (c *Client) ListGroupPosts(ctx context.Context, expectedUser, groupID string, opts PostListOptions) (FeedPage, error) {
	if err := c.requireUser(expectedUser); err != nil {
		return FeedPage{}, err
	}
	return c.listGroupPosts(ctx, expectedUser, groupID, opts)
}

// PostsSince pages a group newest first and returns the posts newer than the
// caller's stored position, newest first. It stops before the post whose ID
// (or StoryID) is sinceID, before the first post older than sinceTime, when
// the feed has no next page, or after maxPages pages (at least 1). A zero
// sinceID or sinceTime disables that stop. A post without a creation time
// never triggers the time stop.
func (c *Client) PostsSince(ctx context.Context, expectedUser, groupID, sinceID string, sinceTime time.Time, maxPages int) ([]Post, error) {
	if err := c.requireUser(expectedUser); err != nil {
		return nil, err
	}
	if maxPages < 1 {
		maxPages = 1
	}
	out := []Post{}
	seen := make(map[string]bool)
	cursor := ""
	for page := 0; page < maxPages; page++ {
		fp, err := c.listGroupPosts(ctx, expectedUser, groupID, PostListOptions{Sort: SortChronological, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, p := range fp.Posts {
			if sinceID != "" && (p.ID == sinceID || p.StoryID == sinceID) {
				return out, nil
			}
			if !sinceTime.IsZero() && !p.CreatedAt.IsZero() && p.CreatedAt.Before(sinceTime) {
				return out, nil
			}
			if seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			out = append(out, p)
		}
		if !fp.HasNext || fp.NextCursor == "" {
			break
		}
		cursor = fp.NextCursor
	}
	return out, nil
}

// listGroupPosts reads one page of a group feed. expectedUser, when set, is
// re-checked after any re-bootstrap.
func (c *Client) listGroupPosts(ctx context.Context, expectedUser, groupID string, opts PostListOptions) (FeedPage, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return FeedPage{}, fmt.Errorf("%w: groupID must not be empty", ErrInvalidParams)
	}
	sortSetting := opts.Sort
	switch sortSetting {
	case "":
		sortSetting = SortChronological
	case SortChronological, SortRecentActivity, SortTopPosts:
	default:
		return FeedPage{}, fmt.Errorf("%w: unknown sort %q", ErrInvalidParams, string(opts.Sort))
	}
	count := clampCount(opts.Count, defaultPostPageCount, maxPostPageCount)

	var name string
	var vars map[string]interface{}
	if opts.Cursor == "" {
		name = "CometGroupDiscussionRootSuccessQuery"
		vars = mergeVars(map[string]interface{}{
			"autoOpenChat":                         false,
			"creative_provider_id":                 nil,
			"feedbackSource":                       0,
			"feedLocation":                         "GROUP",
			"feedType":                             "DISCUSSION",
			"filter_topic_id":                      nil,
			"focusCommentID":                       nil,
			"groupID":                              groupID,
			"hasHoistStories":                      false,
			"hoistedSectionHeaderType":             "notifications",
			"hoistStories":                         []interface{}{},
			"hoistStoriesCount":                    0,
			"privacySelectorRenderLocation":        "COMET_STREAM",
			"regular_stories_count":                count,
			"regular_stories_stream_initial_count": count,
			"renderLocation":                       "group",
			"scale":                                2,
			"shouldDeferMainFeed":                  false,
			"sortingSetting":                       string(sortSetting),
			"threadID":                             "",
			"useDefaultActor":                      false,
		})
	} else {
		name = "GroupsCometFeedRegularStoriesPaginationQuery"
		vars = mergeVars(map[string]interface{}{
			"count":                         count,
			"cursor":                        opts.Cursor,
			"feedLocation":                  "GROUP",
			"feedType":                      "DISCUSSION",
			"feedbackSource":                0,
			"focusCommentID":                nil,
			"privacySelectorRenderLocation": "COMET_STREAM",
			"renderLocation":                "group",
			"scale":                         2,
			"sortingSetting":                string(sortSetting),
			"stream_initial_count":          count,
			"useDefaultActor":               false,
			"id":                            groupID,
		})
	}

	res, err := c.read(ctx, expectedUser, name, vars)
	if err != nil {
		return FeedPage{}, err
	}
	return parseGroupFeed(res, groupID)
}

// parseGroupFeed decodes a group feed response. The group sits at data.group
// (first page) or data.node (pagination). Streamed edges (path ending in
// "edges", <n>) and a deferred page_info arriving on later lines are merged.
func parseGroupFeed(res *gqlResult, groupID string) (FeedPage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(res.Data, &top); err != nil {
		return FeedPage{}, fmt.Errorf("%w: decoding group feed: %v", ErrRequestFailed, err)
	}
	groupRaw, present := top["group"]
	if !present {
		groupRaw, present = top["node"]
	}
	if !present {
		return FeedPage{}, fmt.Errorf("%w: group feed response has no group", ErrRequestFailed)
	}
	if !hasData(groupRaw) {
		return FeedPage{}, fmt.Errorf("%w: group is null", ErrNotMember)
	}

	var g struct {
		ID        string          `json:"id"`
		GroupFeed json.RawMessage `json:"group_feed"`
	}
	if err := json.Unmarshal(groupRaw, &g); err != nil {
		return FeedPage{}, fmt.Errorf("%w: decoding group: %v", ErrRequestFailed, err)
	}
	feedRaw := g.GroupFeed
	if feedRaw == nil {
		// group_feed may arrive in a deferred payload.
		for _, inc := range res.Incremental {
			var d struct {
				GroupFeed json.RawMessage `json:"group_feed"`
			}
			if json.Unmarshal(inc.Data, &d) == nil && d.GroupFeed != nil {
				feedRaw = d.GroupFeed
				break
			}
		}
	}
	if feedRaw == nil {
		return FeedPage{}, fmt.Errorf("%w: group feed response has no group_feed", ErrRequestFailed)
	}
	if !hasData(feedRaw) {
		return FeedPage{}, fmt.Errorf("%w: group_feed is null", ErrNotMember)
	}

	var feed struct {
		Edges    []json.RawMessage `json:"edges"`
		PageInfo *fbPageInfo       `json:"page_info"`
	}
	if err := json.Unmarshal(feedRaw, &feed); err != nil {
		return FeedPage{}, fmt.Errorf("%w: decoding group_feed: %v", ErrRequestFailed, err)
	}
	edges := feed.Edges
	pageInfo := feed.PageInfo
	for _, inc := range res.Incremental {
		if isStreamedEdge(inc.Path) {
			edges = append(edges, inc.Data)
			continue
		}
		var d struct {
			PageInfo *fbPageInfo `json:"page_info"`
		}
		if json.Unmarshal(inc.Data, &d) == nil && d.PageInfo != nil {
			pageInfo = d.PageInfo
		}
	}

	gid := g.ID
	if gid == "" {
		gid = groupID
	}
	page := FeedPage{Posts: []Post{}}
	for _, e := range edges {
		var edge struct {
			Node json.RawMessage `json:"node"`
		}
		if json.Unmarshal(e, &edge) != nil || !hasData(edge.Node) {
			continue
		}
		if p, ok := parseStory(edge.Node, gid, true); ok {
			page.Posts = append(page.Posts, p)
		}
	}
	if pageInfo != nil {
		page.NextCursor = pageInfo.EndCursor
		page.HasNext = pageInfo.HasNextPage && pageInfo.EndCursor != ""
	}
	return page, nil
}

// isStreamedEdge reports a @stream payload path such as
// ["node","group_feed","edges",3].
func isStreamedEdge(path []any) bool {
	if len(path) < 2 {
		return false
	}
	if _, ok := toInt64(path[len(path)-1]); !ok {
		return false
	}
	key, _ := path[len(path)-2].(string)
	return key == "edges"
}

func clampCount(n, def, max int) int {
	if n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// GetPost retrieves a single group post by its story ID.
func (c *Client) GetPost(ctx context.Context, postID string) (*Post, error) {
	if postID == "" {
		return nil, fmt.Errorf("%w: postID must not be empty", ErrInvalidParams)
	}

	vars := mergeVars(map[string]interface{}{
		"storyID":                       postID,
		"hashtag":                       "",
		"useDefaultActor":               false,
		"feedbackSource":                2,
		"focusCommentID":                nil,
		"privacySelectorRenderLocation": "COMET_STREAM",
		"renderLocation":                "permalink",
		"scale":                         2,
	})

	raw, err := c.graphql(ctx, "CometSinglePostDialogContentQuery", vars)
	if err != nil {
		return nil, err
	}

	// The single-post query wraps the story inside data.node or data.story.
	var data struct {
		Node  json.RawMessage `json:"node"`
		Story json.RawMessage `json:"story"`
	}
	if err := unmarshalData(raw, &data); err != nil {
		return nil, err
	}
	story := data.Node
	if !hasData(story) {
		story = data.Story
	}
	if !hasData(story) {
		return nil, ErrNotFound
	}
	p, ok := parseStory(story, "", false)
	if !ok {
		return nil, ErrNotFound
	}
	return &p, nil
}
