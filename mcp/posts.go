package mcp

import (
	"context"

	"github.com/teslashibe/facebook-go/groups"
	"github.com/teslashibe/mcptool"
)

// CreatePostInput is the typed input for facebook_create_post.
type CreatePostInput struct {
	GroupID       string `json:"group_id" jsonschema:"description=numeric Facebook group ID to post in (the user must be a member),required"`
	Message       string `json:"message" jsonschema:"description=plain-text post body,required"`
	AttachmentURL string `json:"attachment_url,omitempty" jsonschema:"description=optional link URL to attach as a preview card"`
}

func createPost(ctx context.Context, c *groups.Client, in CreatePostInput) (any, error) {
	var opts []groups.PostOption
	if in.AttachmentURL != "" {
		opts = append(opts, groups.WithPostAttachmentURL(in.AttachmentURL))
	}
	post, err := c.CreatePost(ctx, in.GroupID, in.Message, opts...)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":          true,
		"group_id":    in.GroupID,
		"post_id":     post.ID,
		"feedback_id": post.FeedbackID,
		"post":        post,
	}, nil
}

var postTools = []mcptool.Tool{
	mcptool.Define[*groups.Client, CreatePostInput](
		"facebook_create_post",
		"Publish a text post to a Facebook group, optionally attaching a link preview",
		"CreatePost",
		createPost,
	),
}
