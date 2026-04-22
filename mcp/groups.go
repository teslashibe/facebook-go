package mcp

import (
	"context"

	"github.com/teslashibe/facebook-go/groups"
	"github.com/teslashibe/mcptool"
)

// SearchGroupsInput is the typed input for facebook_search_groups.
type SearchGroupsInput struct {
	Query    string `json:"query" jsonschema:"description=keywords to search Facebook Groups for,required"`
	Location string `json:"location,omitempty" jsonschema:"description=optional city or region name to filter group results by"`
	Limit    int    `json:"limit,omitempty" jsonschema:"description=maximum results to return,minimum=1,maximum=100,default=20"`
}

func searchGroups(ctx context.Context, c *groups.Client, in SearchGroupsInput) (any, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	opts := []groups.SearchOption{groups.WithSearchLimit(limit)}
	if in.Location != "" {
		opts = append(opts, groups.WithSearchLocation(in.Location))
	}
	res, err := c.SearchGroups(ctx, in.Query, opts...)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(res, "", limit), nil
}

// DiscoverGroupsInput is the typed input for facebook_discover_groups.
type DiscoverGroupsInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"description=cap on suggestions returned,minimum=1,maximum=100,default=20"`
}

func discoverGroups(ctx context.Context, c *groups.Client, in DiscoverGroupsInput) (any, error) {
	res, err := c.DiscoverGroups(ctx)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(res, "", in.Limit), nil
}

// GetMyGroupsInput is the typed input for facebook_get_my_groups.
type GetMyGroupsInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"description=cap on returned groups; 0 returns every joined group,minimum=0,maximum=500,default=0"`
}

func getMyGroups(ctx context.Context, c *groups.Client, in GetMyGroupsInput) (any, error) {
	res, err := c.MyGroups(ctx)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(res, "", in.Limit), nil
}

// GetGroupInput is the typed input for facebook_get_group.
type GetGroupInput struct {
	GroupID string `json:"group_id" jsonschema:"description=numeric Facebook group ID,required"`
}

func getGroup(ctx context.Context, c *groups.Client, in GetGroupInput) (any, error) {
	return c.GetGroup(ctx, in.GroupID)
}

// JoinGroupInput is the typed input for facebook_join_group.
type JoinGroupInput struct {
	GroupID string `json:"group_id" jsonschema:"description=numeric Facebook group ID to join (use facebook_join_group_with_answers if the group has membership questions),required"`
}

func joinGroup(ctx context.Context, c *groups.Client, in JoinGroupInput) (any, error) {
	if err := c.JoinGroup(ctx, in.GroupID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "group_id": in.GroupID}, nil
}

// LeaveGroupInput is the typed input for facebook_leave_group.
type LeaveGroupInput struct {
	GroupID string `json:"group_id" jsonschema:"description=numeric Facebook group ID to leave,required"`
}

func leaveGroup(ctx context.Context, c *groups.Client, in LeaveGroupInput) (any, error) {
	if err := c.LeaveGroup(ctx, in.GroupID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "group_id": in.GroupID}, nil
}

// CreateGroupInput is the typed input for facebook_create_group.
type CreateGroupInput struct {
	Name        string `json:"name" jsonschema:"description=name of the new group,required"`
	Privacy     string `json:"privacy,omitempty" jsonschema:"description=visibility level; allowed: PUBLIC,CLOSED,SECRET,default=CLOSED"`
	Description string `json:"description,omitempty" jsonschema:"description=optional group description"`
}

func createGroup(ctx context.Context, c *groups.Client, in CreateGroupInput) (any, error) {
	g, err := c.CreateGroup(ctx, groups.CreateGroupParams{
		Name:        in.Name,
		Privacy:     groups.Privacy(in.Privacy),
		Description: in.Description,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "group": g}, nil
}

var groupTools = []mcptool.Tool{
	mcptool.Define[*groups.Client, SearchGroupsInput](
		"facebook_search_groups",
		"Search Facebook's group directory by keyword, optionally filtered by city or region",
		"SearchGroups",
		searchGroups,
	),
	mcptool.Define[*groups.Client, DiscoverGroupsInput](
		"facebook_discover_groups",
		"Fetch Facebook's personalised group suggestions for the authenticated user",
		"DiscoverGroups",
		discoverGroups,
	),
	mcptool.Define[*groups.Client, GetMyGroupsInput](
		"facebook_get_my_groups",
		"List every Facebook group the authenticated user has joined (including pending requests)",
		"MyGroups",
		getMyGroups,
	),
	mcptool.Define[*groups.Client, GetGroupInput](
		"facebook_get_group",
		"Fetch a Facebook group's metadata (name, member count, viewer join state) by ID",
		"GetGroup",
		getGroup,
	),
	mcptool.Define[*groups.Client, JoinGroupInput](
		"facebook_join_group",
		"Join (or request to join) a Facebook group; for gated groups use facebook_join_group_with_answers",
		"JoinGroup",
		joinGroup,
	),
	mcptool.Define[*groups.Client, LeaveGroupInput](
		"facebook_leave_group",
		"Leave a Facebook group the authenticated user is currently a member of",
		"LeaveGroup",
		leaveGroup,
	),
	mcptool.Define[*groups.Client, CreateGroupInput](
		"facebook_create_group",
		"Create a new Facebook group with the given name, privacy (PUBLIC/CLOSED/SECRET), and description",
		"CreateGroup",
		createGroup,
	),
}
