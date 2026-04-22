package mcp

import (
	"context"

	"github.com/teslashibe/facebook-go/groups"
	"github.com/teslashibe/mcptool"
)

// GetGroupFeedInput is the typed input for facebook_get_group_feed.
type GetGroupFeedInput struct {
	GroupID string `json:"group_id" jsonschema:"description=numeric Facebook group ID to read the cross-group feed for,required"`
	Limit   int    `json:"limit,omitempty" jsonschema:"description=cap on returned posts; 0 returns the full first page,minimum=0,maximum=200,default=0"`
}

func getGroupFeed(ctx context.Context, c *groups.Client, in GetGroupFeedInput) (any, error) {
	page, err := c.GetGroupFeed(ctx, in.GroupID)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(page.Posts, page.NextCursor, in.Limit), nil
}

// GetGroupFeedPageInput is the typed input for facebook_get_group_feed_page.
type GetGroupFeedPageInput struct {
	GroupID string `json:"group_id" jsonschema:"description=numeric Facebook group ID,required"`
	Cursor  string `json:"cursor" jsonschema:"description=next_cursor from a prior facebook_get_group_feed or facebook_get_group_feed_page call,required"`
	Limit   int    `json:"limit,omitempty" jsonschema:"description=cap on returned posts; 0 returns the full page,minimum=0,maximum=200,default=0"`
}

func getGroupFeedPage(ctx context.Context, c *groups.Client, in GetGroupFeedPageInput) (any, error) {
	page, err := c.GetGroupFeedPage(ctx, in.GroupID, in.Cursor)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(page.Posts, page.NextCursor, in.Limit), nil
}

// GetGroupPostsInput is the typed input for facebook_get_group_posts.
type GetGroupPostsInput struct {
	GroupID string `json:"group_id" jsonschema:"description=numeric Facebook group ID to read posts from,required"`
	Limit   int    `json:"limit,omitempty" jsonschema:"description=cap on returned posts; 0 returns the full first page,minimum=0,maximum=200,default=0"`
}

func getGroupPosts(ctx context.Context, c *groups.Client, in GetGroupPostsInput) (any, error) {
	page, err := c.GetGroupPosts(ctx, in.GroupID)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(page.Posts, page.NextCursor, in.Limit), nil
}

// GetPostInput is the typed input for facebook_get_post.
type GetPostInput struct {
	PostID string `json:"post_id" jsonschema:"description=Facebook post (story) ID,required"`
}

func getPost(ctx context.Context, c *groups.Client, in GetPostInput) (any, error) {
	return c.GetPost(ctx, in.PostID)
}

var feedTools = []mcptool.Tool{
	mcptool.Define[*groups.Client, GetGroupFeedInput](
		"facebook_get_group_feed",
		"Read the personalised cross-group feed for a Facebook group (first page)",
		"GetGroupFeed",
		getGroupFeed,
	),
	mcptool.Define[*groups.Client, GetGroupFeedPageInput](
		"facebook_get_group_feed_page",
		"Fetch a subsequent page of the cross-group feed using a cursor from a prior call",
		"GetGroupFeedPage",
		getGroupFeedPage,
	),
	mcptool.Define[*groups.Client, GetGroupPostsInput](
		"facebook_get_group_posts",
		"Read posts from a single specific Facebook group (not the cross-group home feed)",
		"GetGroupPosts",
		getGroupPosts,
	),
	mcptool.Define[*groups.Client, GetPostInput](
		"facebook_get_post",
		"Fetch a single Facebook group post by its story ID",
		"GetPost",
		getPost,
	),
}
