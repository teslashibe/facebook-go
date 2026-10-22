package groups

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Shared sub-shapes
// ---------------------------------------------------------------------------

type fbPageInfo struct {
	HasNextPage bool   `json:"has_next_page"`
	EndCursor   string `json:"end_cursor"`
}

type fbText struct {
	Text string `json:"text"`
}

type fbImage struct {
	URI string `json:"uri"`
}

type fbActor struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type fbPrivacyInfo struct {
	PrivacyType string `json:"privacy_type"`
}

type fbReactionCount struct {
	Count int `json:"count"`
}

// fbGroup is the common group representation across multiple responses.
type fbGroup struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	URL         string        `json:"url"`
	Description *fbText       `json:"description"`
	MemberCount int           `json:"member_count"`
	PrivacyInfo fbPrivacyInfo `json:"privacy_info"`
	CoverPhoto  *fbCoverPhoto `json:"cover_photo"`
	// viewer_actor_membership_status: MEMBER | PENDING | NOT_MEMBER
	ViewerMembershipStatus string `json:"viewer_actor_membership_status"`
	// Creation time as unix seconds.
	CreationTime int64 `json:"creation_time"`
}

type fbCoverPhoto struct {
	Photo *struct {
		Image *fbImage `json:"image"`
	} `json:"photo"`
}

func (g *fbGroup) toGroup() Group {
	out := Group{
		ID:          g.ID,
		Name:        g.Name,
		URL:         g.URL,
		MemberCount: g.MemberCount,
		Privacy:     Privacy(strings.ToUpper(g.PrivacyInfo.PrivacyType)),
	}
	if g.Description != nil {
		out.Description = g.Description.Text
	}
	if g.CoverPhoto != nil && g.CoverPhoto.Photo != nil && g.CoverPhoto.Photo.Image != nil {
		out.CoverURL = g.CoverPhoto.Photo.Image.URI
	}
	if g.CreationTime > 0 {
		out.CreatedAt = time.Unix(g.CreationTime, 0).UTC()
	}
	switch g.ViewerMembershipStatus {
	case "MEMBER":
		out.Joined = true
	case "PENDING":
		out.PendingJoin = true
	}
	return out
}

// fbGroupSearchResult is the lightweight shape used in search / discover lists.
type fbGroupSearchResult struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	URL            string        `json:"url"`
	MemberCount    int           `json:"member_count"`
	PrivacyInfo    fbPrivacyInfo `json:"privacy_info"`
	ProfilePicture *struct {
		URI string `json:"uri"`
	} `json:"profile_picture"`
}

func (g *fbGroupSearchResult) toSearchResult() GroupSearchResult {
	out := GroupSearchResult{
		ID:          g.ID,
		Name:        g.Name,
		URL:         g.URL,
		MemberCount: g.MemberCount,
		Privacy:     Privacy(strings.ToUpper(g.PrivacyInfo.PrivacyType)),
	}
	if g.ProfilePicture != nil {
		out.CoverURL = g.ProfilePicture.URI
	}
	return out
}

// fbFeedback is the feedback node on a comment.
type fbFeedback struct {
	ID            string           `json:"id"`
	ReactionCount *fbReactionCount `json:"reaction_count"`
}

// fbComment represents a single comment node.
type fbComment struct {
	ID          string      `json:"id"`
	Body        *fbText     `json:"body"`
	Author      *fbActor    `json:"author"`
	CreatedTime int64       `json:"created_time"`
	Feedback    *fbFeedback `json:"feedback"`
}

func (fc *fbComment) toComment() Comment {
	c := Comment{ID: fc.ID}
	if fc.Body != nil {
		c.Message = fc.Body.Text
	}
	if fc.Author != nil {
		c.AuthorID = fc.Author.ID
		c.AuthorName = fc.Author.Name
	}
	if fc.CreatedTime > 0 {
		c.CreatedAt = time.Unix(fc.CreatedTime, 0).UTC()
	}
	if fc.Feedback != nil {
		c.FeedbackID = fc.Feedback.ID
		if fc.Feedback.ReactionCount != nil {
			c.ReactionCount = fc.Feedback.ReactionCount.Count
		}
	}
	return c
}

// ---------------------------------------------------------------------------
// Per-query data shapes
// ---------------------------------------------------------------------------

// --- Participation/membership questions ---

type participationQuestionsData struct {
	Group *struct {
		ID                              string       `json:"id"`
		Name                            string       `json:"name"`
		CommunityParticipationQuestions []fbQuestion `json:"community_participation_questions"`
		MembershipQuestions             []fbQuestion `json:"membership_questions"`
		ViewerForumJoinState            string       `json:"viewer_forum_join_state"`
		ProfilePicture                  *struct {
			URI string `json:"uri"`
		} `json:"profile_picture"`
		GroupSnippets *struct {
			Edges []struct {
				Node *struct {
					Title *fbText `json:"title"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"group_snippets"`
	} `json:"group"`
}

type fbQuestion struct {
	ID       string  `json:"id"`
	Question *fbText `json:"question_title"`
	// Some responses use "title" or "text" directly
	TitleText string `json:"title"`
	BodyText  string `json:"text"`
	Type      string `json:"question_type"` // OPEN_ENDED | MULTIPLE_CHOICE
	Required  bool   `json:"is_required"`
	Choices   []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Text  string `json:"text"`
	} `json:"answer_choices"`
}

func (d *participationQuestionsData) toQuestions() []MembershipQuestion {
	if d.Group == nil {
		return nil
	}
	all := append([]fbQuestion{}, d.Group.CommunityParticipationQuestions...)
	all = append(all, d.Group.MembershipQuestions...)

	out := make([]MembershipQuestion, 0, len(all))
	for _, q := range all {
		mq := MembershipQuestion{
			ID:       q.ID,
			Type:     q.Type,
			Required: q.Required,
		}
		switch {
		case q.Question != nil && q.Question.Text != "":
			mq.Text = q.Question.Text
		case q.TitleText != "":
			mq.Text = q.TitleText
		case q.BodyText != "":
			mq.Text = q.BodyText
		}
		for _, c := range q.Choices {
			label := c.Label
			if label == "" {
				label = c.Text
			}
			if label != "" {
				mq.Options = append(mq.Options, label)
			}
		}
		out = append(out, mq)
	}
	return out
}

// --- Discover suggestions ---
// Response: viewer.categories.discover_tab.units.edges[].node
// Node types: GroupsTabGroupSuggestionRowUnit (has group), GroupsSuggestionUnit (section header)

type discoverData struct {
	Viewer *struct {
		Categories *struct {
			DiscoverTab *struct {
				Units *struct {
					Edges []discoverEdge `json:"edges"`
				} `json:"units"`
			} `json:"discover_tab"`
		} `json:"categories"`
	} `json:"viewer"`
}

type discoverEdge struct {
	Node json.RawMessage `json:"node"`
}

type discoverGroupRow struct {
	Typename string `json:"__typename"`
	Group    *struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		URL            string `json:"url"`
		MemberCount    int    `json:"member_count"`
		ProfilePicture *struct {
			URI string `json:"uri"`
		} `json:"profile_picture"`
	} `json:"group"`
}

func (d *discoverData) groups() []GroupSearchResult {
	if d.Viewer == nil || d.Viewer.Categories == nil ||
		d.Viewer.Categories.DiscoverTab == nil ||
		d.Viewer.Categories.DiscoverTab.Units == nil {
		return nil
	}
	var out []GroupSearchResult
	for _, e := range d.Viewer.Categories.DiscoverTab.Units.Edges {
		var row discoverGroupRow
		if err := json.Unmarshal(e.Node, &row); err != nil {
			continue
		}
		if row.Group == nil {
			continue
		}
		r := GroupSearchResult{
			ID:          row.Group.ID,
			Name:        row.Group.Name,
			URL:         row.Group.URL,
			MemberCount: row.Group.MemberCount,
		}
		if row.Group.ProfilePicture != nil {
			r.CoverURL = row.Group.ProfilePicture.URI
		}
		out = append(out, r)
	}
	return out
}

// --- Joined groups ---
// GroupsCometJoinsRootQuery and GroupsCometAllJoinedGroupsSectionPaginationQuery
// both answer with a tab_groups_list connection, normally at
// viewer.all_joined_groups.tab_groups_list.

type fbGroupConnection struct {
	Edges []struct {
		Node *fbJoinedGroupNode `json:"node"`
	} `json:"edges"`
	PageInfo *fbPageInfo `json:"page_info"`
}

type fbJoinedGroupNode struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	URL            string `json:"url"`
	ProfilePicture *struct {
		URI string `json:"uri"`
	} `json:"profile_picture"`
	PrivacyInfo fbPrivacyInfo `json:"privacy_info"`
	MemberCount int           `json:"member_count"`
	// Membership state; whichever the response carries.
	ViewerJoinState        string `json:"viewer_join_state"`
	ViewerForumJoinState   string `json:"viewer_forum_join_state"`
	ViewerMembershipStatus string `json:"viewer_actor_membership_status"`
}

// membershipState maps Facebook's join-state values. known is false when no
// state field was present.
func membershipState(states ...string) (joined, pending, known bool) {
	for _, s := range states {
		switch strings.ToUpper(strings.TrimSpace(s)) {
		case "":
			continue
		case "MEMBER", "JOINED", "IS_MEMBER", "ADMIN", "MODERATOR":
			return true, false, true
		case "PENDING", "REQUESTED", "PENDING_REQUEST", "PENDING_APPROVAL", "CAN_CANCEL_REQUEST":
			return false, true, true
		default: // CAN_JOIN, NOT_MEMBER, CAN_REQUEST, ...
			return false, false, true
		}
	}
	return false, false, false
}

// toGroup converts a joined-groups row. A row with no membership state is
// reported as Joined: Facebook lists it in the joined-groups section.
func (n *fbJoinedGroupNode) toGroup() Group {
	g := Group{
		ID:          n.ID,
		Name:        n.Name,
		URL:         n.URL,
		MemberCount: n.MemberCount,
		Privacy:     Privacy(strings.ToUpper(n.PrivacyInfo.PrivacyType)),
	}
	if n.ProfilePicture != nil {
		g.CoverURL = n.ProfilePicture.URI
	}
	joined, pending, known := membershipState(n.ViewerMembershipStatus, n.ViewerJoinState, n.ViewerForumJoinState)
	if !known {
		joined = true
	}
	g.Joined, g.PendingJoin = joined, pending
	return g
}

// findGroupConnection locates the tab_groups_list connection in a joined-groups
// response: at viewer.all_joined_groups.tab_groups_list, or anywhere else a
// tab_groups_list object appears. found is false when there is none.
func findGroupConnection(raw json.RawMessage) (fbGroupConnection, bool) {
	var conn fbGroupConnection
	root, ok := decodeObject(raw)
	if !ok {
		return conn, false
	}
	list := dig(root, "viewer", "all_joined_groups", "tab_groups_list")
	if _, isObj := list.(map[string]any); !isObj {
		list = deepFind(root, "tab_groups_list", func(v any) bool {
			_, ok := v.(map[string]any)
			return ok
		})
	}
	if list == nil {
		return conn, false
	}
	b, err := json.Marshal(list)
	if err != nil || json.Unmarshal(b, &conn) != nil {
		return conn, false
	}
	return conn, true
}

func (conn fbGroupConnection) toGroupPage() GroupPage {
	page := GroupPage{Groups: make([]Group, 0, len(conn.Edges))}
	for _, e := range conn.Edges {
		if e.Node == nil || e.Node.ID == "" {
			continue
		}
		page.Groups = append(page.Groups, e.Node.toGroup())
	}
	if pi := conn.PageInfo; pi != nil {
		page.NextCursor = pi.EndCursor
		page.HasNext = pi.HasNextPage && pi.EndCursor != ""
	}
	return page
}

// --- Group info ---

type groupData struct {
	Group *fbGroup `json:"group"`
}

// --- Cross-group feed ---
// Response: viewer.groups_tab.cross_group_feed.edges[]

type feedData struct {
	Viewer *struct {
		GroupsTab *struct {
			CrossGroupFeed *struct {
				Edges []struct {
					Node json.RawMessage `json:"node"`
				} `json:"edges"`
				PageInfo *fbPageInfo `json:"page_info"`
			} `json:"cross_group_feed"`
		} `json:"groups_tab"`
	} `json:"viewer"`
}

func (d *feedData) toFeedPage() FeedPage {
	if d.Viewer == nil || d.Viewer.GroupsTab == nil || d.Viewer.GroupsTab.CrossGroupFeed == nil {
		return FeedPage{}
	}
	cgf := d.Viewer.GroupsTab.CrossGroupFeed
	page := FeedPage{}
	for _, e := range cgf.Edges {
		if !hasData(e.Node) {
			continue
		}
		if p, ok := parseStory(e.Node, "", false); ok {
			page.Posts = append(page.Posts, p)
		}
	}
	if pi := cgf.PageInfo; pi != nil {
		page.HasNext = pi.HasNextPage
		page.NextCursor = pi.EndCursor
	}
	return page
}

// --- Join / Leave mutations ---

type joinData struct {
	GroupJoin *struct {
		Group *struct {
			ViewerMembershipStatus string `json:"viewer_actor_membership_status"`
		} `json:"group"`
	} `json:"group_join"`
}

type leaveData struct {
	GroupLeave *struct {
		Group *struct {
			ID string `json:"id"`
		} `json:"group"`
	} `json:"group_leave"`
}

// --- Create group ---

type createGroupData struct {
	GroupCreate *struct {
		Group *fbGroup `json:"group"`
	} `json:"group_create"`
}

// --- Create post ---

type createPostData struct {
	StoryCreate *struct {
		Story json.RawMessage `json:"story"`
	} `json:"story_create"`
}

// --- Create comment ---

type createCommentData struct {
	CommentCreate *struct {
		Comment *fbComment `json:"comment"`
	} `json:"comment_create"`
}

// --- React ---

type reactData struct {
	FeedbackReact *struct {
		Feedback *struct {
			ID string `json:"id"`
		} `json:"feedback"`
	} `json:"feedback_react"`
}

// --- Comments list ---

type commentsData struct {
	Feedback *struct {
		Comments *struct {
			Edges    []commentEdge `json:"edges"`
			PageInfo *fbPageInfo   `json:"page_info"`
		} `json:"comments"`
	} `json:"feedback"`
}

type commentEdge struct {
	Node *fbComment `json:"node"`
}

func (d *commentsData) toCommentPage() CommentPage {
	if d.Feedback == nil || d.Feedback.Comments == nil {
		return CommentPage{}
	}
	page := CommentPage{}
	for _, e := range d.Feedback.Comments.Edges {
		if e.Node == nil {
			continue
		}
		page.Comments = append(page.Comments, e.Node.toComment())
	}
	if pi := d.Feedback.Comments.PageInfo; pi != nil {
		page.HasNext = pi.HasNextPage
		page.NextCursor = pi.EndCursor
	}
	return page
}

// --- Search ---

type searchData struct {
	SerpResponse *struct {
		Results *struct {
			Edges []searchEdge `json:"edges"`
		} `json:"results"`
	} `json:"serpResponse"`
}

type searchEdge struct {
	// rendering_strategy is at the EDGE level (sibling of node), not nested
	// inside node. Verified against live Facebook SERP response.
	RenderingStrategy *struct {
		ViewModel *struct {
			Profile *struct {
				Typename       string `json:"__typename"`
				ID             string `json:"id"`
				Name           string `json:"name"`
				URL            string `json:"url"`
				ProfilePicture *struct {
					URI string `json:"uri"`
				} `json:"profile_picture"`
			} `json:"profile"`
		} `json:"view_model"`
	} `json:"rendering_strategy"`
}

func (d *searchData) groups() []GroupSearchResult {
	if d.SerpResponse == nil || d.SerpResponse.Results == nil {
		return nil
	}
	var out []GroupSearchResult
	for _, e := range d.SerpResponse.Results.Edges {
		if e.RenderingStrategy == nil ||
			e.RenderingStrategy.ViewModel == nil ||
			e.RenderingStrategy.ViewModel.Profile == nil {
			continue
		}
		p := e.RenderingStrategy.ViewModel.Profile
		if p.Typename != "" && p.Typename != "Group" {
			continue
		}
		r := GroupSearchResult{
			ID:   p.ID,
			Name: p.Name,
			URL:  p.URL,
		}
		if p.ProfilePicture != nil {
			r.CoverURL = p.ProfilePicture.URI
		}
		out = append(out, r)
	}
	return out
}

// --- Members ---

type membersData struct {
	Group *struct {
		ID                  string `json:"id"`
		GroupMemberProfiles *struct {
			Count int `json:"count"`
		} `json:"group_member_profiles"`
		AllActiveMembers *struct {
			Count int `json:"count"`
		} `json:"all_active_members"`
		NewMembers *struct {
			Edges    []memberEdge `json:"edges"`
			PageInfo *fbPageInfo  `json:"page_info"`
		} `json:"new_members"`
	} `json:"group"`
}

type memberEdge struct {
	Node *fbActor `json:"node"`
}

func (d *membersData) toMemberPage() MemberPage {
	if d.Group == nil {
		return MemberPage{}
	}
	page := MemberPage{}
	if d.Group.NewMembers != nil {
		for _, e := range d.Group.NewMembers.Edges {
			if e.Node == nil {
				continue
			}
			page.Members = append(page.Members, Member{
				ID:   e.Node.ID,
				Name: e.Node.Name,
			})
		}
		if pi := d.Group.NewMembers.PageInfo; pi != nil {
			page.HasNext = pi.HasNextPage
			page.NextCursor = pi.EndCursor
		}
	}
	return page
}

func (d *membersData) totalCount() int {
	if d.Group == nil {
		return 0
	}
	if d.Group.AllActiveMembers != nil && d.Group.AllActiveMembers.Count > 0 {
		return d.Group.AllActiveMembers.Count
	}
	if d.Group.GroupMemberProfiles != nil {
		return d.Group.GroupMemberProfiles.Count
	}
	return 0
}

// unmarshalData decodes a read's data payload into v. The error never quotes
// the payload, which can carry names and post text.
func unmarshalData(raw json.RawMessage, v interface{}) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%w: decoding response data: %v", ErrRequestFailed, err)
	}
	return nil
}
