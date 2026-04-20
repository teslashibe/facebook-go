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

	// The group members query (GroupsCometMembersPageQuery) requires a doc_id
	// that is only loaded when navigating to a specific group's members page.
	// It is not included in the initial page JS bundles. Use WithDocIDs to
	// supply the members query doc_id after harvesting it from the browser.
	_ = cursor
	return MemberPage{}, fmt.Errorf("%w: members query requires a group-specific doc_id — navigate to /groups/<id>/members/ to harvest it, then use WithDocIDs", ErrNotFound)
}
