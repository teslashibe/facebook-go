package mcp

import (
	"context"

	"github.com/teslashibe/facebook-go/groups"
	"github.com/teslashibe/mcptool"
)

// GetPostCommentsInput is the typed input for facebook_get_post_comments.
type GetPostCommentsInput struct {
	FeedbackID string `json:"feedback_id" jsonschema:"description=Post.FeedbackID (NOT Post.ID) returned by facebook_get_post or facebook_get_group_posts,required"`
	Limit      int    `json:"limit,omitempty" jsonschema:"description=cap on returned comments; 0 returns the full first page,minimum=0,maximum=100,default=0"`
}

func getPostComments(ctx context.Context, c *groups.Client, in GetPostCommentsInput) (any, error) {
	page, err := c.GetPostComments(ctx, in.FeedbackID)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(page.Comments, page.NextCursor, in.Limit), nil
}

// GetPostCommentsPageInput is the typed input for facebook_get_post_comments_page.
type GetPostCommentsPageInput struct {
	FeedbackID string `json:"feedback_id" jsonschema:"description=Post.FeedbackID for the post being read,required"`
	Cursor     string `json:"cursor" jsonschema:"description=next_cursor from a prior facebook_get_post_comments call,required"`
	Limit      int    `json:"limit,omitempty" jsonschema:"description=cap on returned comments; 0 returns the full page,minimum=0,maximum=100,default=0"`
}

func getPostCommentsPage(ctx context.Context, c *groups.Client, in GetPostCommentsPageInput) (any, error) {
	page, err := c.GetPostCommentsPage(ctx, in.FeedbackID, in.Cursor)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(page.Comments, page.NextCursor, in.Limit), nil
}

// CreateCommentInput is the typed input for facebook_create_comment.
type CreateCommentInput struct {
	FeedbackID string `json:"feedback_id" jsonschema:"description=Post.FeedbackID (NOT Post.ID) of the post to comment on,required"`
	Message    string `json:"message" jsonschema:"description=plain-text comment body,required"`
}

func createComment(ctx context.Context, c *groups.Client, in CreateCommentInput) (any, error) {
	comment, err := c.CreateComment(ctx, in.FeedbackID, in.Message)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":          true,
		"feedback_id": in.FeedbackID,
		"comment_id":  comment.ID,
		"comment":     comment,
	}, nil
}

// ReactToPostInput is the typed input for facebook_react_to_post.
type ReactToPostInput struct {
	FeedbackID string `json:"feedback_id" jsonschema:"description=Post.FeedbackID (NOT Post.ID) of the post to react to,required"`
	Reaction   string `json:"reaction" jsonschema:"description=reaction type; allowed: LIKE,LOVE,CARE,HAHA,WOW,SAD,ANGRY,required"`
}

func reactToPost(ctx context.Context, c *groups.Client, in ReactToPostInput) (any, error) {
	if err := c.ReactToPost(ctx, in.FeedbackID, groups.ReactionType(in.Reaction)); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":          true,
		"feedback_id": in.FeedbackID,
		"reaction":    in.Reaction,
	}, nil
}

var commentTools = []mcptool.Tool{
	mcptool.Define[*groups.Client, GetPostCommentsInput](
		"facebook_get_post_comments",
		"Read the first page of comments on a Facebook post (use Post.FeedbackID, not Post.ID)",
		"GetPostComments",
		getPostComments,
	),
	mcptool.Define[*groups.Client, GetPostCommentsPageInput](
		"facebook_get_post_comments_page",
		"Fetch a subsequent page of comments using a cursor from a prior facebook_get_post_comments call",
		"GetPostCommentsPage",
		getPostCommentsPage,
	),
	mcptool.Define[*groups.Client, CreateCommentInput](
		"facebook_create_comment",
		"Post a text comment on a Facebook post (use Post.FeedbackID, not Post.ID)",
		"CreateComment",
		createComment,
	),
	mcptool.Define[*groups.Client, ReactToPostInput](
		"facebook_react_to_post",
		"Set or change a reaction (LIKE/LOVE/CARE/HAHA/WOW/SAD/ANGRY) on a Facebook post",
		"ReactToPost",
		reactToPost,
	),
}
