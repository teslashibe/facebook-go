package groups

import (
	"context"
	"fmt"
)

// GetGroupFeed returns the first page of posts from a group's feed in
// reverse-chronological order.
// Returns ErrForbidden for closed or secret groups the user has not joined.
func (c *Client) GetGroupFeed(ctx context.Context, groupID string) (FeedPage, error) {
	if groupID == "" {
		return FeedPage{}, fmt.Errorf("%w: groupID must not be empty", ErrInvalidParams)
	}

	type variables struct {
		GroupID         string `json:"groupID"`
		UseDefaultActor bool   `json:"useDefaultActor"`
		OrderBy         string `json:"orderBy"`
		Count           int    `json:"count"`
		FeedLocation    string `json:"feedLocation"`
	}

	raw, err := c.graphql(ctx, "GroupsCometGroupFeedQuery", variables{
		GroupID:         groupID,
		UseDefaultActor: true,
		OrderBy:         "CHRONOLOGICAL",
		Count:           10,
		FeedLocation:    "GROUP",
	})
	if err != nil {
		return FeedPage{}, err
	}

	var data feedData
	if err := unmarshalData(raw, &data); err != nil {
		return FeedPage{}, err
	}

	return data.toFeedPage(), nil
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

	type variables struct {
		GroupID string `json:"groupID"`
		Count   int    `json:"count"`
		Cursor  string `json:"cursor"`
	}

	raw, err := c.graphql(ctx, "GroupsCometGroupFeedPaginationQuery", variables{
		GroupID: groupID,
		Count:   10,
		Cursor:  cursor,
	})
	if err != nil {
		return FeedPage{}, err
	}

	var data feedPaginationData
	if err := unmarshalData(raw, &data); err != nil {
		return FeedPage{}, err
	}

	return data.toFeedPage(), nil
}

// GetPost retrieves a single group post by its story ID.
func (c *Client) GetPost(ctx context.Context, postID string) (*Post, error) {
	if postID == "" {
		return nil, fmt.Errorf("%w: postID must not be empty", ErrInvalidParams)
	}

	type variables struct {
		StoryID         string `json:"storyID"`
		UseDefaultActor bool   `json:"useDefaultActor"`
	}

	raw, err := c.graphql(ctx, "CometSinglePostRouteQuery", variables{
		StoryID:         postID,
		UseDefaultActor: true,
	})
	if err != nil {
		return nil, err
	}

	// The single-post query wraps the story inside data.story.
	var data struct {
		Story *fbStory `json:"story"`
	}
	if err := unmarshalData(raw, &data); err != nil {
		return nil, err
	}
	if data.Story == nil {
		return nil, ErrNotFound
	}

	p := data.Story.toPost("")
	return &p, nil
}
