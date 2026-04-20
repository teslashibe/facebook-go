package groups

import (
	"context"
	"fmt"
)

// GetGroupMembers returns the first page of group members.
// Returns ErrForbidden when the member list is hidden by group settings.
func (c *Client) GetGroupMembers(ctx context.Context, groupID string) (MemberPage, error) {
	return c.GetGroupMembersPage(ctx, groupID, "")
}

// GetGroupMembersPage fetches a subsequent page of group members using the
// cursor returned by a prior call. Pass an empty cursor for the first page.
func (c *Client) GetGroupMembersPage(ctx context.Context, groupID, cursor string) (MemberPage, error) {
	if groupID == "" {
		return MemberPage{}, fmt.Errorf("%w: groupID must not be empty", ErrInvalidParams)
	}

	type variables struct {
		GroupID string  `json:"groupID"`
		Count   int     `json:"count"`
		Cursor  *string `json:"cursor"`
	}

	var cur *string
	if cursor != "" {
		cur = &cursor
	}

	raw, err := c.graphql(ctx, "GroupsCometMembersPageQuery", variables{
		GroupID: groupID,
		Count:   30,
		Cursor:  cur,
	})
	if err != nil {
		return MemberPage{}, err
	}

	var data membersData
	if err := unmarshalData(raw, &data); err != nil {
		return MemberPage{}, err
	}

	return data.toMemberPage(), nil
}
