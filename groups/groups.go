package groups

import (
	"context"
	"errors"
	"fmt"
)

// SearchGroups searches Facebook Groups by keyword and returns matching results.
// Use WithSearchLocation and WithSearchLimit to narrow the query.
func (c *Client) SearchGroups(ctx context.Context, query string, opts ...SearchOption) ([]GroupSearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("%w: query must not be empty", ErrInvalidParams)
	}

	so := &searchOptions{limit: 20}
	for _, o := range opts {
		o(so)
	}

	type variables struct {
		Query          string   `json:"query"`
		Count          int      `json:"count"`
		Cursor         *string  `json:"cursor"`
		SearchSections []string `json:"search_sections"`
		ContextSource  string   `json:"context_source"`
		Location       string   `json:"location,omitempty"`
	}

	vars := variables{
		Query:          query,
		Count:          so.limit,
		SearchSections: []string{"GROUPS"},
		ContextSource:  "GROUP_DIRECTORY",
		Location:       so.location,
	}

	raw, err := c.graphql(ctx, "GroupSearchResultsPageQuery", vars)
	if err != nil {
		return nil, err
	}

	var data searchData
	if err := unmarshalData(raw, &data); err != nil {
		return nil, err
	}

	results := data.groups()
	if len(results) == 0 {
		return nil, ErrNotFound
	}
	return results, nil
}

// DiscoverGroups returns Facebook's personalised group suggestions for the
// authenticated user.
func (c *Client) DiscoverGroups(ctx context.Context) ([]GroupSearchResult, error) {
	type variables struct {
		Count  int `json:"count"`
		Scale  int `json:"scale"`
	}

	raw, err := c.graphql(ctx, "GroupsDiscoverSuggestionsQuery", variables{Count: 20, Scale: 1})
	if err != nil {
		return nil, err
	}

	var data discoverData
	if err := unmarshalData(raw, &data); err != nil {
		return nil, err
	}

	return data.groups(), nil
}

// MyGroups returns all groups the authenticated user is a member of, including
// those with pending approval (PendingJoin: true).
func (c *Client) MyGroups(ctx context.Context) ([]Group, error) {
	type variables struct {
		Count int `json:"count"`
	}

	raw, err := c.graphql(ctx, "GroupsHomeNewQuery", variables{Count: 50})
	if err != nil {
		return nil, err
	}

	var data myGroupsData
	if err := unmarshalData(raw, &data); err != nil {
		return nil, err
	}

	return data.groups(), nil
}

// GetGroup retrieves full metadata for a single group by its numeric ID.
// Returns ErrNotFound for nonexistent groups and ErrForbidden for secret groups
// the user is not a member of.
func (c *Client) GetGroup(ctx context.Context, groupID string) (*Group, error) {
	if groupID == "" {
		return nil, fmt.Errorf("%w: groupID must not be empty", ErrInvalidParams)
	}

	type variables struct {
		GroupID         string `json:"groupID"`
		UseDefaultActor bool   `json:"useDefaultActor"`
	}

	raw, err := c.graphql(ctx, "GroupsCometGroupPageQuery", variables{
		GroupID:         groupID,
		UseDefaultActor: true,
	})
	if err != nil {
		return nil, err
	}

	var data groupData
	if err := unmarshalData(raw, &data); err != nil {
		return nil, err
	}
	if data.Group == nil {
		return nil, ErrNotFound
	}

	g := data.Group.toGroup()
	return &g, nil
}

// JoinGroup sends a join request or immediately joins the group (public groups).
// Returns ErrAlreadyMember if the user is already a member.
func (c *Client) JoinGroup(ctx context.Context, groupID string) error {
	if groupID == "" {
		return fmt.Errorf("%w: groupID must not be empty", ErrInvalidParams)
	}

	type variables struct {
		GroupID     string `json:"groupID"`
		Surface     string `json:"surface"`
		IsSuggested bool   `json:"isSuggested"`
	}

	raw, err := c.graphql(ctx, "GroupJoinMutation", variables{
		GroupID: groupID,
		Surface: "GROUP_PAGE",
	})
	if err != nil {
		if errors.Is(err, ErrAlreadyMember) {
			return ErrAlreadyMember
		}
		return err
	}

	var data joinData
	return unmarshalData(raw, &data)
}

// LeaveGroup removes the authenticated user from the group.
// Returns ErrNotMember if the user is not currently a member.
func (c *Client) LeaveGroup(ctx context.Context, groupID string) error {
	if groupID == "" {
		return fmt.Errorf("%w: groupID must not be empty", ErrInvalidParams)
	}

	type variables struct {
		GroupID                  string `json:"groupID"`
		Source                   string `json:"source"`
		RevokeJoinRequestIfPending bool `json:"revokeJoinRequestIfPending"`
	}

	raw, err := c.graphql(ctx, "GroupLeaveMutation", variables{
		GroupID:                    groupID,
		Source:                     "GROUP_HEADER_DROPDOWN",
		RevokeJoinRequestIfPending: true,
	})
	if err != nil {
		if errors.Is(err, ErrNotMember) {
			return ErrNotMember
		}
		return err
	}

	var data leaveData
	return unmarshalData(raw, &data)
}

// CreateGroup creates a new Facebook Group and returns its metadata.
// params.Privacy defaults to PrivacyClosed if not specified.
// Returns ErrInvalidParams if params.Name is empty.
func (c *Client) CreateGroup(ctx context.Context, params CreateGroupParams) (*Group, error) {
	if params.Name == "" {
		return nil, fmt.Errorf("%w: Name must not be empty", ErrInvalidParams)
	}
	if params.Privacy == "" {
		params.Privacy = PrivacyClosed
	}

	type createInput struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Privacy     string `json:"privacy"`
	}
	type variables struct {
		Input createInput `json:"input"`
	}

	raw, err := c.graphql(ctx, "GroupCreateMutation", variables{
		Input: createInput{
			Name:        params.Name,
			Description: params.Description,
			Privacy:     string(params.Privacy),
		},
	})
	if err != nil {
		return nil, err
	}

	var data createGroupData
	if err := unmarshalData(raw, &data); err != nil {
		return nil, err
	}
	if data.GroupCreate == nil || data.GroupCreate.Group == nil {
		return nil, fmt.Errorf("%w: server returned no group after creation", ErrRequestFailed)
	}

	g := data.GroupCreate.Group.toGroup()
	return &g, nil
}
