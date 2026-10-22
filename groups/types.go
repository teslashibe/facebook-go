package groups

import "time"

// RateLimitState captures rate-limit information from the most recently observed
// response headers. All fields are zero-valued until a response with rate-limit
// headers is received.
type RateLimitState struct {
	Limit      int           `json:"limit"`
	Remaining  int           `json:"remaining"`
	Reset      time.Time     `json:"reset"`
	RetryAfter time.Duration `json:"retry_after"`
}

// IsLimited reports whether the current state indicates requests are blocked.
func (r RateLimitState) IsLimited() bool {
	if !r.Reset.IsZero() && r.Remaining == 0 && time.Now().Before(r.Reset) {
		return true
	}
	return r.RetryAfter > 0
}

// ResetIn returns how long until the rate-limit window resets.
// Returns 0 if Reset is in the past or not set.
func (r RateLimitState) ResetIn() time.Duration {
	if r.Reset.IsZero() {
		return 0
	}
	if d := time.Until(r.Reset); d > 0 {
		return d
	}
	return 0
}

// Cookies holds the Facebook session cookies obtained from a browser export.
// CUser and XS are required (either as fields or in Jar); the others
// strengthen fingerprint matching.
type Cookies struct {
	SB    string // sb: browser session fingerprint
	DATR  string // datr: device auth token
	CUser string // c_user: the authenticated user ID (also used as __user in every request)
	XS    string // xs: session token — the primary auth credential
	FR    string // fr: friend-request + ad-tracking token
	PSL   string // ps_l: presence/status
	PSN   string // ps_n: presence/status (secondary)

	// Jar optionally carries every facebook.com cookie by name. When set, the
	// Cookie header carries the whole jar. Non-empty named fields above win
	// over the same cookie in Jar.
	Jar map[string]string
}

// Identity is the logged-in user read from the bootstrap page.
type Identity struct {
	UserID     string `json:"userId"`
	Name       string `json:"name"`
	ProfileURL string `json:"profileUrl"`
}

// ResponseInfo describes the most recent physical response. Code is a short
// safe code ("ok", "http_429", "gql_368", "checkpoint_redirect", ...), never a
// body.
type ResponseInfo struct {
	Status       int    `json:"status"`
	FriendlyName string `json:"friendlyName,omitempty"`
	Code         string `json:"code,omitempty"`
}

// Privacy is the visibility level of a Facebook Group.
type Privacy string

const (
	PrivacyPublic Privacy = "PUBLIC"
	PrivacyClosed Privacy = "CLOSED"
	PrivacySecret Privacy = "SECRET"
)

// Group is the canonical group metadata model.
type Group struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Description string    `json:"description,omitempty"`
	MemberCount int       `json:"memberCount,omitempty"`
	Privacy     Privacy   `json:"privacy"`
	CoverURL    string    `json:"coverUrl,omitempty"`
	AdminIDs    []string  `json:"adminIds,omitempty"`
	Joined      bool      `json:"joined"`
	PendingJoin bool      `json:"pendingJoin"`
	CreatedAt   time.Time `json:"createdAt,omitempty"`
}

// AuthorKind says who posted a story.
type AuthorKind string

const (
	AuthorPerson    AuthorKind = "person"
	AuthorPage      AuthorKind = "page"
	AuthorAnonymous AuthorKind = "anonymous"
	AuthorUnknown   AuthorKind = "unknown"
)

// Author is the poster of a story, decoded from the story's actor.
type Author struct {
	ID         string     `json:"id,omitempty"`
	Name       string     `json:"name,omitempty"`
	ProfileURL string     `json:"profileUrl,omitempty"`
	Kind       AuthorKind `json:"kind"`
}

// Post represents a group post / story node.
// FeedbackID is required for comment and reaction mutations — it differs from ID.
type Post struct {
	ID            string    `json:"id"`
	StoryID       string    `json:"storyId,omitempty"`
	FeedbackID    string    `json:"feedbackId"`
	GroupID       string    `json:"groupId,omitempty"`
	Author        Author    `json:"author"`
	AuthorID      string    `json:"authorId,omitempty"`   // mirrors Author.ID
	AuthorName    string    `json:"authorName,omitempty"` // mirrors Author.Name
	Message       string    `json:"message,omitempty"`
	Permalink     string    `json:"permalink,omitempty"`
	Attachments   []string  `json:"attachments,omitempty"`
	ReactionCount int       `json:"reactionCount"`
	CommentCount  int       `json:"commentCount"`
	ShareCount    int       `json:"shareCount"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt,omitempty"`
}

// Comment represents a comment on a post.
type Comment struct {
	ID            string    `json:"id"`
	FeedbackID    string    `json:"feedbackId,omitempty"`
	AuthorID      string    `json:"authorId,omitempty"`
	AuthorName    string    `json:"authorName,omitempty"`
	Message       string    `json:"message,omitempty"`
	ReactionCount int       `json:"reactionCount"`
	CreatedAt     time.Time `json:"createdAt"`
}

// Member is a group member.
type Member struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url,omitempty"`
	IsAdmin bool   `json:"isAdmin"`
}

// GroupSearchResult is a lightweight result from SearchGroups or DiscoverGroups.
type GroupSearchResult struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	URL         string  `json:"url,omitempty"`
	MemberCount int     `json:"memberCount,omitempty"`
	Privacy     Privacy `json:"privacy,omitempty"`
	CoverURL    string  `json:"coverUrl,omitempty"`
}

// FeedPage is one page of group feed posts with a cursor for the next page.
type FeedPage struct {
	Posts      []Post `json:"posts"`
	NextCursor string `json:"nextCursor,omitempty"`
	HasNext    bool   `json:"hasNext"`
}

// ListOptions pages ListJoinedGroups. Count 0 means the default (20); it is
// capped at 50.
type ListOptions struct {
	Cursor string
	Count  int
}

// GroupPage is one page of joined groups with a cursor for the next page.
type GroupPage struct {
	Groups     []Group `json:"groups"`
	NextCursor string  `json:"nextCursor,omitempty"`
	HasNext    bool    `json:"hasNext"`
}

// PostSort is a group feed order (the GraphQL sortingSetting).
type PostSort string

const (
	// SortChronological is "New posts": newest post first.
	SortChronological PostSort = "CHRONOLOGICAL"
	// SortRecentActivity is "Newest activity".
	SortRecentActivity PostSort = "RECENT_ACTIVITY"
	// SortTopPosts is "Most relevant".
	SortTopPosts PostSort = "TOP_POSTS"
)

// PostListOptions pages ListGroupPosts. The default Sort is
// SortChronological and the default Count is 10, capped at 20.
type PostListOptions struct {
	Sort   PostSort
	Cursor string
	Count  int
}

// CommentPage is one page of post comments with a cursor for the next page.
type CommentPage struct {
	Comments   []Comment `json:"comments"`
	NextCursor string    `json:"nextCursor,omitempty"`
	HasNext    bool      `json:"hasNext"`
}

// MemberPage is one page of group members with a cursor for the next page.
type MemberPage struct {
	Members    []Member `json:"members"`
	NextCursor string   `json:"nextCursor,omitempty"`
	HasNext    bool     `json:"hasNext"`
}

// ReactionType enumerates the seven Facebook post reactions.
type ReactionType string

const (
	ReactionLike  ReactionType = "LIKE"
	ReactionLove  ReactionType = "LOVE"
	ReactionCare  ReactionType = "CARE"
	ReactionHaha  ReactionType = "HAHA"
	ReactionWow   ReactionType = "WOW"
	ReactionSad   ReactionType = "SAD"
	ReactionAngry ReactionType = "ANGRY"
)

// CreateGroupParams holds the inputs for CreateGroup.
type CreateGroupParams struct {
	Name        string
	Privacy     Privacy // defaults to PrivacyClosed if zero
	Description string
}

// MembershipQuestion represents a single question that a closed group asks
// prospective members. Some groups require all questions answered before the
// join request is accepted.
type MembershipQuestion struct {
	ID       string   `json:"id"`
	Text     string   `json:"text"`
	Type     string   `json:"type"` // OPEN_ENDED, MULTIPLE_CHOICE
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"` // populated for MULTIPLE_CHOICE
}

// MembershipAnswer is one answer to a [MembershipQuestion]. For open-ended
// questions, set Text. For multiple-choice questions, set Choice to one of
// the question's Options.
type MembershipAnswer struct {
	QuestionID string `json:"questionId"`
	Text       string `json:"text,omitempty"`
	Choice     string `json:"choice,omitempty"`
}

// TrendReport is the output of ScrapeGroupTrends.
type TrendReport struct {
	GroupID        string           `json:"groupId"`
	PostsAnalyzed  int              `json:"postsAnalyzed"`
	TopKeywords    []KeywordFreq    `json:"topKeywords"`
	TopHashtags    []KeywordFreq    `json:"topHashtags"`
	AvgEngagement  float64          `json:"avgEngagement"`
	PeakHours      []int            `json:"peakHours"`
	SentimentScore float64          `json:"sentimentScore"`
	ActiveAuthors  []AuthorActivity `json:"activeAuthors"`
}

// KeywordFreq pairs a term with its occurrence count.
type KeywordFreq struct {
	Term  string `json:"term"`
	Count int    `json:"count"`
}

// AuthorActivity summarises one author's posting activity within the analysis window.
type AuthorActivity struct {
	AuthorID   string `json:"authorId"`
	AuthorName string `json:"authorName"`
	PostCount  int    `json:"postCount"`
}
