package mcp

import (
	"context"

	"github.com/teslashibe/facebook-go/groups"
	"github.com/teslashibe/mcptool"
)

// GetGroupMembersInput is the typed input for facebook_get_group_members.
type GetGroupMembersInput struct {
	GroupID string `json:"group_id" jsonschema:"description=numeric Facebook group ID,required"`
	Limit   int    `json:"limit,omitempty" jsonschema:"description=cap on returned members; 0 returns the full first page,minimum=0,maximum=200,default=0"`
}

func getGroupMembers(ctx context.Context, c *groups.Client, in GetGroupMembersInput) (any, error) {
	page, err := c.GetGroupMembers(ctx, in.GroupID)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(page.Members, page.NextCursor, in.Limit), nil
}

// GetGroupMembersPageInput is the typed input for facebook_get_group_members_page.
type GetGroupMembersPageInput struct {
	GroupID string `json:"group_id" jsonschema:"description=numeric Facebook group ID,required"`
	Cursor  string `json:"cursor" jsonschema:"description=next_cursor from a prior facebook_get_group_members call,required"`
	Limit   int    `json:"limit,omitempty" jsonschema:"description=cap on returned members; 0 returns the full page,minimum=0,maximum=200,default=0"`
}

func getGroupMembersPage(ctx context.Context, c *groups.Client, in GetGroupMembersPageInput) (any, error) {
	page, err := c.GetGroupMembersPage(ctx, in.GroupID, in.Cursor)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(page.Members, page.NextCursor, in.Limit), nil
}

var memberTools = []mcptool.Tool{
	mcptool.Define[*groups.Client, GetGroupMembersInput](
		"facebook_get_group_members",
		"List the first page of members of a Facebook group",
		"GetGroupMembers",
		getGroupMembers,
	),
	mcptool.Define[*groups.Client, GetGroupMembersPageInput](
		"facebook_get_group_members_page",
		"Fetch a subsequent page of group members using a cursor from a prior facebook_get_group_members call",
		"GetGroupMembersPage",
		getGroupMembersPage,
	),
}
