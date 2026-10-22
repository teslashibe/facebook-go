package groups

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

const testGroup = "900000000000001"

var zeroTime time.Time

func at(hour int) time.Time { return time.Date(2026, 10, 22, hour, 0, 0, 0, time.UTC) }

// feedFake serves the three-page chronological feed fixture.
func feedFake(t *testing.T) *fakeFB {
	f := newFakeFB(t)
	f.onGraphQL("CometGroupDiscussionRootSuccessQuery", serveFixture(t, "group_feed_page1.json"))
	f.onGraphQL("GroupsCometFeedRegularStoriesPaginationQuery", func(w http.ResponseWriter, r recordedRequest, _ int) {
		switch varString(r, "cursor") {
		case "cursor-page-2":
			_, _ = w.Write(fixture(t, "group_feed_page2.json"))
		case "cursor-page-3":
			_, _ = w.Write(fixture(t, "group_feed_page3.json"))
		default:
			writeBody(w, http.StatusBadRequest, `{"errors":[{"message":"unexpected cursor"}]}`)
		}
	})
	return f
}

func TestListGroupPostsChronologicalFirstPage(t *testing.T) {
	f := feedFake(t)
	c := f.mustClient()
	page, err := c.ListGroupPosts(bg, testUser, testGroup, PostListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	req := f.last("CometGroupDiscussionRootSuccessQuery")
	if varString(req, "sortingSetting") != "CHRONOLOGICAL" || varString(req, "groupID") != testGroup {
		t.Fatalf("vars sortingSetting=%q groupID=%q", varString(req, "sortingSetting"), varString(req, "groupID"))
	}
	if varNumber(req, "regular_stories_count") != 10 {
		t.Fatalf("regular_stories_count = %v, want default 10", req.Vars["regular_stories_count"])
	}

	if !page.HasNext || page.NextCursor != "cursor-page-2" {
		t.Fatalf("paging = %v %q, want the deferred page_info", page.HasNext, page.NextCursor)
	}
	if len(page.Posts) != 3 {
		t.Fatalf("posts = %d, want 3 (header skipped, streamed edge merged)", len(page.Posts))
	}

	p1 := page.Posts[0]
	want1 := Post{
		ID:            "700000000000001",
		StoryID:       "UzpfSTEwMDAwMDAwMDAwMDAwMjo3MDAwMDAwMDAwMDAwMDE=",
		FeedbackID:    "ZmVlZGJhY2s6NzAwMDAwMDAwMDAwMDAx",
		GroupID:       testGroup,
		Author:        Author{ID: "100000000000002", Name: "Jordan Sample", ProfileURL: "https://www.facebook.com/jordan.sample.example", Kind: AuthorPerson},
		AuthorID:      "100000000000002",
		AuthorName:    "Jordan Sample",
		Message:       "Looking for a working capital loan for my bakery",
		Permalink:     "https://www.facebook.com/groups/900000000000001/posts/700000000000001/",
		ReactionCount: 4,
		CommentCount:  2,
		ShareCount:    1,
		CreatedAt:     at(16),
	}
	assertPost(t, p1, want1)

	// Nested comet_sections shape: time and permalink from metadata, feedback
	// from the UFI renderer, the attached story's older time ignored.
	p2 := page.Posts[1]
	want2 := Post{
		ID:            "700000000000002",
		StoryID:       "UzpfSTIwMDAwMDAwMDAwMDAwMTo3MDAwMDAwMDAwMDAwMDI=",
		FeedbackID:    "ZmVlZGJhY2s6NzAwMDAwMDAwMDAwMDAy",
		GroupID:       testGroup,
		Author:        Author{ID: "200000000000001", Name: "Example Bakery Page", ProfileURL: "https://www.facebook.com/examplebakerypage", Kind: AuthorPage},
		AuthorID:      "200000000000001",
		AuthorName:    "Example Bakery Page",
		Message:       "Our shop is hiring seasonal staff",
		Permalink:     "https://www.facebook.com/groups/900000000000001/posts/700000000000002/",
		ReactionCount: 9,
		CommentCount:  3,
		CreatedAt:     at(15),
	}
	assertPost(t, p2, want2)

	p3 := page.Posts[2]
	if p3.ID != "700000000000003" || p3.Author.Kind != AuthorAnonymous || !p3.CreatedAt.Equal(at(14)) {
		t.Fatalf("streamed anonymous post = %+v", p3)
	}
	if p3.Permalink != "https://www.facebook.com/groups/900000000000001/posts/700000000000003/" {
		t.Fatalf("built permalink = %q", p3.Permalink)
	}
}

func TestListGroupPostsCursorPaging(t *testing.T) {
	f := feedFake(t)
	c := f.mustClient()
	page, err := c.ListGroupPosts(bg, testUser, testGroup, PostListOptions{Cursor: "cursor-page-2", Count: 50})
	if err != nil {
		t.Fatal(err)
	}
	req := f.last("GroupsCometFeedRegularStoriesPaginationQuery")
	if varString(req, "cursor") != "cursor-page-2" || varString(req, "id") != testGroup || varString(req, "sortingSetting") != "CHRONOLOGICAL" {
		t.Fatalf("pagination vars = %v", req.Vars)
	}
	if varNumber(req, "count") != 20 {
		t.Fatalf("count = %v, want capped 20", req.Vars["count"])
	}
	if f.count("CometGroupDiscussionRootSuccessQuery") != 0 {
		t.Fatal("a cursor page used the first-page query")
	}
	if len(page.Posts) != 2 || page.NextCursor != "cursor-page-3" || !page.HasNext {
		t.Fatalf("page 2 = %d posts, %q, %v", len(page.Posts), page.NextCursor, page.HasNext)
	}
	if p := page.Posts[0]; p.ID != "700000000000004" || !p.CreatedAt.Equal(at(13)) || p.Author.Kind != AuthorUnknown || p.AuthorID != "400000000000001" {
		t.Fatalf("typename-less actor = %+v", p)
	}
	// No actors: owning_profile fallback, kind unknown without a typename.
	if p := page.Posts[1]; p.Author != (Author{ID: "100000000000005", Name: "Casey Demo", Kind: AuthorUnknown}) || p.AuthorID != "100000000000005" {
		t.Fatalf("owning_profile fallback = %+v", p.Author)
	}
}

func TestListGroupPostsSortOptions(t *testing.T) {
	f := feedFake(t)
	c := f.mustClient()
	if _, err := c.ListGroupPosts(bg, testUser, testGroup, PostListOptions{Sort: SortRecentActivity, Count: 5}); err != nil {
		t.Fatal(err)
	}
	req := f.last("CometGroupDiscussionRootSuccessQuery")
	if varString(req, "sortingSetting") != "RECENT_ACTIVITY" || varNumber(req, "regular_stories_count") != 5 {
		t.Fatalf("vars = %v", req.Vars)
	}
	if _, err := c.ListGroupPosts(bg, testUser, testGroup, PostListOptions{Sort: "NEWEST"}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("unknown sort err = %v", err)
	}
	if _, err := c.ListGroupPosts(bg, testUser, " ", PostListOptions{}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("empty group err = %v", err)
	}
	// The legacy single-group read keeps its "Most relevant" order.
	if _, err := c.GetGroupPosts(bg, testGroup); err != nil {
		t.Fatal(err)
	}
	if req := f.last("CometGroupDiscussionRootSuccessQuery"); varString(req, "sortingSetting") != "TOP_POSTS" || varNumber(req, "regular_stories_count") != 3 {
		t.Fatalf("GetGroupPosts vars = %v", req.Vars)
	}
}

func TestPostsSinceStopRules(t *testing.T) {
	ids := func(posts []Post) []string {
		out := make([]string, len(posts))
		for i, p := range posts {
			out[i] = p.ID[len(p.ID)-1:]
		}
		return out
	}
	cases := []struct {
		name      string
		sinceID   string
		sinceTime time.Time
		maxPages  int
		want      []string
		pages     int
	}{
		{"stops at sinceID on page 2", "700000000000004", zeroTime, 5, []string{"1", "2", "3"}, 2},
		{"sinceID may be the story id", "UzpfSTMwMDAwMDAwMDAwMDAwMTo3MDAwMDAwMDAwMDAwMDM=", zeroTime, 5, []string{"1", "2"}, 1},
		{"stops at the first older post", "", at(13).Add(30 * time.Minute), 5, []string{"1", "2", "3"}, 2},
		{"post at sinceTime is kept", "", at(12), 5, []string{"1", "2", "3", "4", "5"}, 3},
		{"maxPages bounds the walk", "", zeroTime, 1, []string{"1", "2", "3"}, 1},
		{"maxPages below one reads one page", "", zeroTime, 0, []string{"1", "2", "3"}, 1},
		{"no stop reads to the end", "", zeroTime, 10, []string{"1", "2", "3", "4", "5", "6"}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := feedFake(t)
			c := f.mustClient()
			posts, err := c.PostsSince(bg, testUser, testGroup, tc.sinceID, tc.sinceTime, tc.maxPages)
			if err != nil {
				t.Fatal(err)
			}
			got := ids(posts)
			if len(got) != len(tc.want) {
				t.Fatalf("posts = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("posts = %v, want %v", got, tc.want)
				}
			}
			pages := f.count("CometGroupDiscussionRootSuccessQuery") + f.count("GroupsCometFeedRegularStoriesPaginationQuery")
			if pages != tc.pages {
				t.Fatalf("pages read = %d, want %d", pages, tc.pages)
			}
		})
	}
}

func TestNullGroupIsErrNotMember(t *testing.T) {
	cases := map[string]struct{ query, cursor, body string }{
		"null group":      {"CometGroupDiscussionRootSuccessQuery", "", `{"data":{"group":null},"extensions":{"is_final":true}}`},
		"null group_feed": {"CometGroupDiscussionRootSuccessQuery", "", `{"data":{"group":{"id":"900000000000001","name":"Example Founders Circle","group_feed":null}}}`},
		"null node":       {"GroupsCometFeedRegularStoriesPaginationQuery", "cursor-page-2", `{"data":{"node":null}}`},
		"error wording":   {"CometGroupDiscussionRootSuccessQuery", "", `{"errors":[{"message":"You are not a member of this group","severity":"CRITICAL"}]}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeFB(t)
			f.onGraphQL(tc.query, serveBody(http.StatusOK, tc.body))
			c := f.mustClient(WithRetry(3, 0))
			page, err := c.ListGroupPosts(bg, testUser, testGroup, PostListOptions{Cursor: tc.cursor})
			if !errors.Is(err, ErrNotMember) {
				t.Fatalf("err = %v, want ErrNotMember", err)
			}
			if page.Posts != nil {
				t.Fatal("returned a page with ErrNotMember")
			}
			if f.count(tc.query) != 1 {
				t.Fatalf("ErrNotMember was retried: %d requests", f.count(tc.query))
			}
		})
	}

	f := newFakeFB(t)
	f.onGraphQL("CometGroupDiscussionRootSuccessQuery", serveBody(http.StatusOK, `{"data":{"viewer":{"id":"1"}}}`))
	c := f.mustClient()
	if _, err := c.ListGroupPosts(bg, testUser, testGroup, PostListOptions{}); !errors.Is(err, ErrRequestFailed) || errors.Is(err, ErrNotMember) {
		t.Fatalf("unknown shape err = %v, want ErrRequestFailed", err)
	}
}

func TestGroupReadForbidden(t *testing.T) {
	f := newFakeFB(t)
	f.onGraphQL("CometGroupDiscussionRootSuccessQuery", serveBody(http.StatusForbidden, ``))
	c := f.mustClient()
	if _, err := c.ListGroupPosts(bg, testUser, testGroup, PostListOptions{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestAuthorKinds(t *testing.T) {
	cases := []struct {
		name string
		node string
		want Author
	}{
		{"person", `{"id":"s1","post_id":"1","actors":[{"__typename":"User","id":"100000000000002","name":"Jordan Sample"}]}`,
			Author{ID: "100000000000002", Name: "Jordan Sample", ProfileURL: "https://www.facebook.com/profile.php?id=100000000000002", Kind: AuthorPerson}},
		{"page", `{"id":"s1","post_id":"1","actors":[{"__typename":"Page","id":"200000000000001","name":"Example Bakery Page"}]}`,
			Author{ID: "200000000000001", Name: "Example Bakery Page", ProfileURL: "https://www.facebook.com/200000000000001", Kind: AuthorPage}},
		{"anonymous typename", `{"id":"s1","post_id":"1","actors":[{"__typename":"GroupAnonAuthorProfile","id":"300000000000001","name":"Anonymous participant"}]}`,
			Author{ID: "300000000000001", Name: "Anonymous participant", Kind: AuthorAnonymous}},
		{"anonymous marker on a User", `{"id":"s1","post_id":"1","actors":[{"__typename":"User","id":"300000000000002","name":"Group member","is_anonymous":true}]}`,
			Author{ID: "300000000000002", Name: "Group member", Kind: AuthorAnonymous}},
		{"anonymous name", `{"id":"s1","post_id":"1","actors":[{"__typename":"User","id":"300000000000003","name":"Anonymous member"}]}`,
			Author{ID: "300000000000003", Name: "Anonymous member", Kind: AuthorAnonymous}},
		{"missing typename", `{"id":"s1","post_id":"1","actors":[{"id":"400000000000001","name":"Riley Placeholder"}]}`,
			Author{ID: "400000000000001", Name: "Riley Placeholder", Kind: AuthorUnknown}},
		{"other typename", `{"id":"s1","post_id":"1","actors":[{"__typename":"Group","id":"900000000000001","name":"Example Founders Circle"}]}`,
			Author{ID: "900000000000001", Name: "Example Founders Circle", Kind: AuthorUnknown}},
		{"user without id", `{"id":"s1","post_id":"1","actors":[{"__typename":"User","name":"Nameless"}]}`,
			Author{Name: "Nameless", Kind: AuthorUnknown}},
		{"owning_profile fallback with typename", `{"id":"s1","post_id":"1","feedback":{"id":"f1","owning_profile":{"__typename":"User","id":"100000000000005","name":"Casey Demo"}}}`,
			Author{ID: "100000000000005", Name: "Casey Demo", ProfileURL: "https://www.facebook.com/profile.php?id=100000000000005", Kind: AuthorPerson}},
		{"owning_profile fallback without typename", `{"id":"s1","post_id":"1","feedback":{"id":"f1","owning_profile":{"id":"100000000000005","name":"Casey Demo"}}}`,
			Author{ID: "100000000000005", Name: "Casey Demo", Kind: AuthorUnknown}},
		{"actors win over owning_profile", `{"id":"s1","post_id":"1","actors":[{"__typename":"User","id":"100000000000002","name":"Jordan Sample","url":"https://www.facebook.com/jordan.sample.example"}],"feedback":{"id":"f1","owning_profile":{"__typename":"Group","id":"900000000000001"}}}`,
			Author{ID: "100000000000002", Name: "Jordan Sample", ProfileURL: "https://www.facebook.com/jordan.sample.example", Kind: AuthorPerson}},
		{"no author at all", `{"id":"s1","post_id":"1"}`, Author{Kind: AuthorUnknown}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := parseStory([]byte(tc.node), testGroup, true)
			if !ok {
				t.Fatal("story not parsed")
			}
			if p.Author != tc.want {
				t.Fatalf("Author = %+v, want %+v", p.Author, tc.want)
			}
			if p.AuthorID != tc.want.ID || p.AuthorName != tc.want.Name {
				t.Fatalf("AuthorID/AuthorName %q/%q do not mirror Author", p.AuthorID, p.AuthorName)
			}
		})
	}
}

func TestListJoinedGroupsPaging(t *testing.T) {
	const q = "GroupsCometAllJoinedGroupsSectionPaginationQuery"
	f := newFakeFB(t)
	f.onGraphQL(q, func(w http.ResponseWriter, r recordedRequest, _ int) {
		switch r.Vars["cursor"] {
		case nil:
			_, _ = w.Write(fixture(t, "joined_groups_page1.json"))
		case "groups-cursor-2":
			_, _ = w.Write(fixture(t, "joined_groups_page2.json"))
		default:
			writeBody(w, http.StatusBadRequest, `{}`)
		}
	})
	c := f.mustClient()

	page, err := c.ListJoinedGroups(bg, testUser, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if varNumber(f.last(q), "count") != 20 {
		t.Fatalf("default count = %v", f.last(q).Vars["count"])
	}
	if !page.HasNext || page.NextCursor != "groups-cursor-2" || len(page.Groups) != 3 {
		t.Fatalf("page 1 = %+v", page)
	}
	type state struct{ joined, pending bool }
	want := map[string]state{
		"900000000000001": {true, false},  // MEMBER
		"900000000000002": {false, true},  // REQUESTED
		"900000000000003": {true, false},  // no state: listed as joined
		"900000000000004": {false, false}, // NOT_MEMBER
	}
	g1 := page.Groups[0]
	if g1.Name != "Example Founders Circle" || g1.URL != "https://www.facebook.com/groups/900000000000001/" || g1.CoverURL != "https://static.example.invalid/g1.jpg" {
		t.Fatalf("group 1 = %+v", g1)
	}

	page2, err := c.ListJoinedGroups(bg, testUser, ListOptions{Cursor: page.NextCursor, Count: 500})
	if err != nil {
		t.Fatal(err)
	}
	if varNumber(f.last(q), "count") != 50 {
		t.Fatalf("count = %v, want capped 50", f.last(q).Vars["count"])
	}
	if page2.HasNext || len(page2.Groups) != 1 {
		t.Fatalf("page 2 = %+v", page2)
	}
	for _, g := range append(page.Groups, page2.Groups...) {
		if s := (state{g.Joined, g.PendingJoin}); s != want[g.ID] {
			t.Errorf("group %s joined/pending = %v, want %v", g.ID, s, want[g.ID])
		}
	}

	f.onGraphQL(q, serveBody(http.StatusOK, `{"data":{"viewer":null}}`))
	if _, err := c.ListJoinedGroups(bg, testUser, ListOptions{}); !errors.Is(err, ErrRequestFailed) {
		t.Fatalf("missing list err = %v, want ErrRequestFailed", err)
	}
}

func assertPost(t *testing.T, got, want Post) {
	t.Helper()
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %s, want %s", got.CreatedAt, want.CreatedAt)
	}
	got.CreatedAt, want.CreatedAt = time.Time{}, time.Time{}
	if got.ID != want.ID || got.StoryID != want.StoryID || got.FeedbackID != want.FeedbackID ||
		got.GroupID != want.GroupID || got.Author != want.Author || got.AuthorID != want.AuthorID ||
		got.AuthorName != want.AuthorName || got.Message != want.Message || got.Permalink != want.Permalink ||
		got.ReactionCount != want.ReactionCount || got.CommentCount != want.CommentCount || got.ShareCount != want.ShareCount {
		t.Errorf("post =\n %+v\nwant\n %+v", got, want)
	}
}
