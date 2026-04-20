package groups

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// GraphQL envelope
// ---------------------------------------------------------------------------

// gqlEnvelope is the top-level shape returned by every /api/graphql/ call.
// Facebook wraps all responses with "for (;;);" (stripped by client.go) and
// the body is always JSON matching this shape.
type gqlEnvelope struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors"`
}

type gqlError struct {
	Message string `json:"message"`
	// api_error_code 1357001 = not logged in, 368 = blocked, etc.
	Code     int    `json:"api_error_code"`
	Severity string `json:"severity"`
	// type is set to "OAuthException" for auth failures.
	Type string `json:"type"`
}

// err converts the error list into a Go error, mapping known codes to
// the package sentinels.
func (e *gqlEnvelope) err() error {
	if len(e.Errors) == 0 {
		return nil
	}
	first := e.Errors[0]
	msg := strings.ToLower(first.Message)

	switch {
	case first.Code == 1357001 || first.Type == "OAuthException" ||
		strings.Contains(msg, "not logged in") || strings.Contains(msg, "session"):
		return ErrSessionExpired

	case strings.Contains(msg, "already a member"), strings.Contains(msg, "already joined"):
		return ErrAlreadyMember

	case strings.Contains(msg, "not a member"), strings.Contains(msg, "not member"):
		return ErrNotMember

	case strings.Contains(msg, "does not exist"), strings.Contains(msg, "deleted"),
		strings.Contains(msg, "not found"):
		if strings.Contains(msg, "document") || strings.Contains(msg, "doc_id") {
			return ErrDocIDStale
		}
		return ErrNotFound

	case strings.Contains(msg, "permission"), strings.Contains(msg, "forbidden"),
		strings.Contains(msg, "not allowed"), strings.Contains(msg, "access denied"):
		return ErrForbidden
	}

	return fmt.Errorf("%w: %s (code %d)", ErrRequestFailed, first.Message, first.Code)
}

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

type fbCommentCount struct {
	TotalCount int `json:"total_count"`
}

type fbShareCount struct {
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
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	URL         string        `json:"url"`
	MemberCount int           `json:"member_count"`
	PrivacyInfo fbPrivacyInfo `json:"privacy_info"`
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

// fbStory represents a single news feed story (post).
type fbStory struct {
	ID           string      `json:"id"`
	Feedback     *fbFeedback `json:"feedback"`
	Actors       []fbActor   `json:"actors"`
	Message      *fbText     `json:"message"`
	CreationTime int64       `json:"creation_time"`
	EditedTime   int64       `json:"edited_time"`
	// Attachments contain URLs for linked content.
	Attachments []fbAttachment `json:"attachments"`
}

type fbFeedback struct {
	ID            string           `json:"id"`
	ReactionCount *fbReactionCount `json:"reaction_count"`
	CommentCount  *fbCommentCount  `json:"comment_count"`
	ShareCount    *fbShareCount    `json:"share_count"`
}

type fbAttachment struct {
	Media *struct {
		PlayableURL string   `json:"playable_url"`
		Image       *fbImage `json:"image"`
	} `json:"media"`
	URL string `json:"url"`
}

func (s *fbStory) toPost(groupID string) Post {
	p := Post{
		ID:      s.ID,
		GroupID: groupID,
	}
	if s.Feedback != nil {
		p.FeedbackID = s.Feedback.ID
		if s.Feedback.ReactionCount != nil {
			p.ReactionCount = s.Feedback.ReactionCount.Count
		}
		if s.Feedback.CommentCount != nil {
			p.CommentCount = s.Feedback.CommentCount.TotalCount
		}
		if s.Feedback.ShareCount != nil {
			p.ShareCount = s.Feedback.ShareCount.Count
		}
	}
	if len(s.Actors) > 0 {
		p.AuthorID = s.Actors[0].ID
		p.AuthorName = s.Actors[0].Name
	}
	if s.Message != nil {
		p.Message = s.Message.Text
	}
	if s.CreationTime > 0 {
		p.CreatedAt = time.Unix(s.CreationTime, 0).UTC()
	}
	if s.EditedTime > 0 {
		p.UpdatedAt = time.Unix(s.EditedTime, 0).UTC()
	}
	for _, a := range s.Attachments {
		if a.URL != "" {
			p.Attachments = append(p.Attachments, a.URL)
		} else if a.Media != nil && a.Media.Image != nil {
			p.Attachments = append(p.Attachments, a.Media.Image.URI)
		}
	}
	return p
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

// --- Search / Discover ---

type searchData struct {
	SerpResponse *struct {
		Results *struct {
			Edges    []searchEdge `json:"edges"`
			PageInfo *fbPageInfo  `json:"page_info"`
		} `json:"results"`
	} `json:"serpResponse"`
}

type searchEdge struct {
	Node *struct {
		StyleRenderer *struct {
			PrimaryRenderer *struct {
				Group *fbGroupSearchResult `json:"group"`
			} `json:"primaryRenderer"`
		} `json:"style_renderer"`
	} `json:"node"`
}

func (d *searchData) groups() []GroupSearchResult {
	if d.SerpResponse == nil || d.SerpResponse.Results == nil {
		return nil
	}
	out := make([]GroupSearchResult, 0, len(d.SerpResponse.Results.Edges))
	for _, e := range d.SerpResponse.Results.Edges {
		if e.Node == nil || e.Node.StyleRenderer == nil ||
			e.Node.StyleRenderer.PrimaryRenderer == nil ||
			e.Node.StyleRenderer.PrimaryRenderer.Group == nil {
			continue
		}
		out = append(out, e.Node.StyleRenderer.PrimaryRenderer.Group.toSearchResult())
	}
	return out
}

// --- Discover suggestions ---

type discoverData struct {
	Viewer *struct {
		GroupSuggestions *struct {
			Edges []struct {
				Node *struct {
					Group *fbGroupSearchResult `json:"group"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"group_suggestions"`
	} `json:"viewer"`
}

func (d *discoverData) groups() []GroupSearchResult {
	if d.Viewer == nil || d.Viewer.GroupSuggestions == nil {
		return nil
	}
	out := make([]GroupSearchResult, 0, len(d.Viewer.GroupSuggestions.Edges))
	for _, e := range d.Viewer.GroupSuggestions.Edges {
		if e.Node == nil || e.Node.Group == nil {
			continue
		}
		out = append(out, e.Node.Group.toSearchResult())
	}
	return out
}

// --- My Groups ---

type myGroupsData struct {
	Viewer *struct {
		AllJoinedGroups *struct {
			TabGroupsList *struct {
				Edges []struct {
					Node *fbMyGroupNode `json:"node"`
				} `json:"edges"`
			} `json:"tab_groups_list"`
		} `json:"all_joined_groups"`
	} `json:"viewer"`
}

type fbMyGroupNode struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	URL            string `json:"url"`
	ProfilePicture *struct {
		URI string `json:"uri"`
	} `json:"profile_picture"`
}

func (d *myGroupsData) groups() []Group {
	if d.Viewer == nil || d.Viewer.AllJoinedGroups == nil || d.Viewer.AllJoinedGroups.TabGroupsList == nil {
		return nil
	}
	edges := d.Viewer.AllJoinedGroups.TabGroupsList.Edges
	out := make([]Group, 0, len(edges))
	for _, e := range edges {
		if e.Node == nil {
			continue
		}
		g := Group{
			ID:     e.Node.ID,
			Name:   e.Node.Name,
			URL:    e.Node.URL,
			Joined: true,
		}
		if e.Node.ProfilePicture != nil {
			g.CoverURL = e.Node.ProfilePicture.URI
		}
		out = append(out, g)
	}
	return out
}

// --- Group info ---

type groupData struct {
	Group *fbGroup `json:"group"`
}

// --- Group feed (first page) ---

type feedData struct {
	Group *struct {
		ID        string `json:"id"`
		GroupFeed *struct {
			Edges    []feedEdge  `json:"edges"`
			PageInfo *fbPageInfo `json:"page_info"`
		} `json:"group_feed"`
	} `json:"group"`
}

type feedEdge struct {
	Node *struct {
		Story *fbStory `json:"story"`
	} `json:"node"`
}

func (d *feedData) toFeedPage() FeedPage {
	if d.Group == nil || d.Group.GroupFeed == nil {
		return FeedPage{}
	}
	gid := d.Group.ID
	page := FeedPage{}
	for _, e := range d.Group.GroupFeed.Edges {
		if e.Node == nil || e.Node.Story == nil {
			continue
		}
		page.Posts = append(page.Posts, e.Node.Story.toPost(gid))
	}
	if pi := d.Group.GroupFeed.PageInfo; pi != nil {
		page.HasNext = pi.HasNextPage
		page.NextCursor = pi.EndCursor
	}
	return page
}

// --- Group feed pagination ---

type feedPaginationData struct {
	Group *struct {
		ID        string `json:"id"`
		GroupFeed *struct {
			Edges    []feedEdge  `json:"edges"`
			PageInfo *fbPageInfo `json:"page_info"`
		} `json:"group_feed"`
	} `json:"group"`
}

func (d *feedPaginationData) toFeedPage() FeedPage {
	fd := (*feedData)(d)
	return fd.toFeedPage()
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
		Story *fbStory `json:"story"`
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

// --- Members ---

type membersData struct {
	Group *struct {
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
	if d.Group == nil || d.Group.NewMembers == nil {
		return MemberPage{}
	}
	page := MemberPage{}
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
	return page
}

// unmarshalData is a helper that decodes a json.RawMessage into v.
func unmarshalData(raw json.RawMessage, v interface{}) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%w: decoding response data: %v (snippet: %s)",
			ErrRequestFailed, err, truncate(string(raw), 300))
	}
	return nil
}
