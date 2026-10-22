package groups

import (
	"errors"
	"net/http"
	"testing"
)

func TestReadErrorClasses(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		header   map[string]string
		body     string
		want     error
		code     string
		notAlsoA error
	}{
		{name: "api_error_code 368", status: 200,
			body: `{"errors":[{"message":"You can't use this feature right now","severity":"CRITICAL","api_error_code":368}],"data":null}`,
			want: ErrFeatureBlocked, code: "gql_368"},
		{name: "temporarily blocked wording", status: 200,
			body: `{"errors":[{"message":"You're Temporarily Blocked. It looks like you were misusing this feature by going too fast.","severity":"CRITICAL"}]}`,
			want: ErrFeatureBlocked, code: "gql_feature_blocked"},
		{name: "top-level error object", status: 200,
			body: `{"error":{"code":368,"summary":"Blocked","description":"You’re temporarily blocked from posting in groups"}}`,
			want: ErrFeatureBlocked, code: "gql_368"},
		{name: "top-level error_code restricted", status: 200,
			body: `{"error":{"error_code":1404078,"summary":"Your account is restricted right now"}}`,
			want: ErrAccountRestricted, code: "gql_1404078"},
		{name: "top-level legacy number", status: 200,
			body: `for (;;);{"__ar":1,"error":1357001,"errorSummary":"Not logged in","errorDescription":"Please log in to continue.","payload":null}`,
			want: ErrSessionExpired, code: "gql_1357001", notAlsoA: ErrRequestFailed},
		{name: "checkpoint wins over session wording", status: 200,
			body: `{"errors":[{"message":"Your session is paused until you complete a security checkpoint","severity":"CRITICAL"}]}`,
			want: ErrCheckpoint, code: "gql_checkpoint", notAlsoA: ErrSessionExpired},
		{name: "blocked wins over session wording", status: 200,
			body: `{"errors":[{"message":"You're blocked from this feature for this session","severity":"CRITICAL"}]}`,
			want: ErrFeatureBlocked, code: "gql_feature_blocked", notAlsoA: ErrSessionExpired},
		{name: "disabled account", status: 200,
			body: `{"errors":[{"message":"Your account has been disabled","severity":"CRITICAL","code":1}]}`,
			want: ErrAccountRestricted, code: "gql_1"},
		{name: "stale doc_id", status: 200,
			body: `{"errors":[{"message":"PersistedQueryNotFound: document 26867841739523736 not found","severity":"CRITICAL"}]}`,
			want: ErrDocIDStale, code: "gql_doc_id_stale", notAlsoA: ErrNotFound},
		{name: "explicit block on a 500", status: 500,
			body: `{"error":{"code":368,"message":"Temporarily blocked"}}`,
			want: ErrFeatureBlocked, code: "gql_368"},
		{name: "429 with Retry-After", status: 429, header: map[string]string{"Retry-After": "90"},
			body: `{}`, want: ErrRateLimited, code: "http_429"},
		{name: "redirect to checkpoint", status: 302,
			header: map[string]string{"Location": "https://www.facebook.com/checkpoint/1501092823525282/"},
			want:   ErrCheckpoint, code: "checkpoint_redirect"},
		{name: "redirect to login", status: 302,
			header: map[string]string{"Location": "/login/?next=%2Fapi%2Fgraphql%2F"},
			want:   ErrSessionExpired, code: "login_redirect"},
		{name: "403", status: 403, want: ErrForbidden, code: "http_403"},
		{name: "500", status: 500, body: `oops`, want: ErrRequestFailed, code: "http_500"},
		{name: "undecodable 200", status: 200, body: `<html>Something went wrong</html>`, want: ErrRequestFailed, code: "decode_error"},
		{name: "unknown error code", status: 200,
			body: `{"errors":[{"message":"A server error field_exception occured.","severity":"CRITICAL","code":1675004}]}`,
			want: ErrRequestFailed, code: "gql_1675004"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeFB(t)
			f.onGraphQL("CometGroupDiscussionRootSuccessQuery", func(w http.ResponseWriter, _ recordedRequest, _ int) {
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				writeBody(w, tc.status, tc.body)
			})
			c := f.mustClient()
			_, err := c.ListGroupPosts(bg, testUser, testGroup, PostListOptions{})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.notAlsoA != nil && errors.Is(err, tc.notAlsoA) {
				t.Fatalf("err = %v also matches %v", err, tc.notAlsoA)
			}
			if got := c.LastResponse(); got.Code != tc.code {
				t.Fatalf("LastResponse = %+v, want code %s", got, tc.code)
			}
		})
	}
}

func TestClassifyFacebookErrorOrder(t *testing.T) {
	cases := []struct {
		code int
		typ  string
		text string
		want error
	}{
		{368, "OAuthException", "", ErrFeatureBlocked},
		{0, "", "It looks like you were going too fast", ErrFeatureBlocked},
		{0, "", "Your session needs a checkpoint", ErrCheckpoint},
		{0, "", "This account is restricted from messaging", ErrAccountRestricted},
		{1357001, "", "", ErrSessionExpired},
		{0, "", "Your session has expired", ErrSessionExpired},
		{0, "OAuthException", "Invalid OAuth access token", ErrSessionExpired},
		{0, "OAuthException", "Permissions error", ErrForbidden},
		{0, "", "persisted query not found", ErrDocIDStale},
		{0, "", "The doc_id is invalid", ErrDocIDStale},
		{0, "", "This content was deleted", ErrNotFound},
		{0, "", "You are already a member", ErrAlreadyMember},
		{0, "", "Something odd happened", ErrRequestFailed},
	}
	for _, tc := range cases {
		if _, err := classifyFacebookError(tc.code, tc.typ, tc.text); !errors.Is(err, tc.want) {
			t.Errorf("classify(%d, %q, %q) = %v, want %v", tc.code, tc.typ, tc.text, err, tc.want)
		}
	}
}

// writeCalls covers every mutation in the package.
var writeCalls = []struct {
	name     string
	friendly string
	call     func(c *Client) error
}{
	{"CreatePost", "ComposerStoryCreateMutation", func(c *Client) error {
		_, err := c.CreatePost(bg, testGroup, "Hello from a test")
		return err
	}},
	{"CreateComment", "useCometUFICreateCommentMutation", func(c *Client) error {
		_, err := c.CreateComment(bg, "ZmVlZGJhY2s6NzAwMDAwMDAwMDAwMDAx", "Thanks for sharing")
		return err
	}},
	{"ReactToPost", "CometUFIFeedbackReactMutation", func(c *Client) error {
		return c.ReactToPost(bg, "ZmVlZGJhY2s6NzAwMDAwMDAwMDAwMDAx", ReactionLike)
	}},
	{"JoinGroup", "useGroupRequestToJoinMutation", func(c *Client) error { return c.JoinGroup(bg, testGroup) }},
	{"JoinGroupWithAnswers", "useGroupRequestToJoinMutation", func(c *Client) error {
		return c.JoinGroupWithAnswers(bg, testGroup, []MembershipAnswer{{QuestionID: "q1", Text: "A test answer"}})
	}},
	{"LeaveGroup", "useGroupLeaveMutation", func(c *Client) error { return c.LeaveGroup(bg, testGroup) }},
	{"CreateGroup", "useGroupsCometCreateMutation", func(c *Client) error {
		_, err := c.CreateGroup(bg, CreateGroupParams{Name: "Example Test Group"})
		return err
	}},
}

func TestMutationsAreNeverRetried(t *testing.T) {
	for _, wc := range writeCalls {
		t.Run(wc.name, func(t *testing.T) {
			f := newFakeFB(t)
			f.onGraphQL(wc.friendly, serveBody(http.StatusInternalServerError, `upstream error`))
			c := f.mustClient(WithRetry(5, 0))
			err := wc.call(c)
			if !errors.Is(err, ErrWriteUnknown) {
				t.Fatalf("err = %v, want ErrWriteUnknown", err)
			}
			if n := f.count(wc.friendly); n != 1 {
				t.Fatalf("mutation sent %d times, want 1", n)
			}
		})
	}
}

func TestMutationOutcomes(t *testing.T) {
	const q = "useCometUFICreateCommentMutation"
	comment := func(c *Client) error {
		_, err := c.CreateComment(bg, "ZmVlZGJhY2s6NzAwMDAwMDAwMDAwMDAx", "Thanks for sharing")
		return err
	}
	cases := []struct {
		name     string
		handler  gqlHandler
		want     error
		notAlsoA error
	}{
		{name: "connection dropped after dispatch", want: ErrWriteUnknown, handler: func(w http.ResponseWriter, _ recordedRequest, _ int) {
			hj, ok := w.(http.Hijacker)
			if !ok {
				panic("no hijacker")
			}
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		}},
		{name: "undecodable 200", want: ErrWriteUnknown, handler: serveBody(http.StatusOK, `<html>Something went wrong</html>`)},
		{name: "200 without data", want: ErrWriteUnknown, handler: serveBody(http.StatusOK, `{"data":null}`)},
		{name: "data without the comment", want: ErrWriteUnknown, handler: serveBody(http.StatusOK, `{"data":{"comment_create":null}}`)},
		{name: "unexpected redirect", want: ErrWriteUnknown, handler: func(w http.ResponseWriter, _ recordedRequest, _ int) {
			w.Header().Set("Location", "https://www.facebook.com/somewhere/")
			w.WriteHeader(http.StatusFound)
		}},
		{name: "feature blocked is definite", want: ErrFeatureBlocked, notAlsoA: ErrWriteUnknown,
			handler: serveBody(http.StatusOK, `{"errors":[{"message":"You can't use this feature right now","api_error_code":368}]}`)},
		{name: "401 is definite and not re-bootstrapped", want: ErrSessionExpired, notAlsoA: ErrWriteUnknown,
			handler: serveBody(http.StatusUnauthorized, `{}`)},
		{name: "429 is definite", want: ErrRateLimited, notAlsoA: ErrWriteUnknown,
			handler: serveBody(http.StatusTooManyRequests, `{}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeFB(t)
			f.onGraphQL(q, tc.handler)
			c := f.mustClient(WithRetry(5, 0))
			err := comment(c)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.notAlsoA != nil && errors.Is(err, tc.notAlsoA) {
				t.Fatalf("err = %v also matches %v", err, tc.notAlsoA)
			}
			if f.count(q) != 1 || f.count("bootstrap") != 1 {
				t.Fatalf("mutation=%d bootstrap=%d, want 1 and 1", f.count(q), f.count("bootstrap"))
			}
		})
	}

	t.Run("success", func(t *testing.T) {
		f := newFakeFB(t)
		f.onGraphQL(q, serveBody(http.StatusOK, `{"data":{"comment_create":{"comment":{"id":"Y29tbWVudDox","body":{"text":"Thanks for sharing"},"author":{"id":"100000000000001","name":"Avery Example"},"created_time":1792684800}}}}`))
		c := f.mustClient()
		got, err := c.CreateComment(bg, "ZmVlZGJhY2s6NzAwMDAwMDAwMDAwMDAx", "Thanks for sharing")
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != "Y29tbWVudDox" || got.AuthorID != testUser || !got.CreatedAt.Equal(at(16)) {
			t.Fatalf("comment = %+v", got)
		}
		if f.last(q).Form.Get("av") != testUser {
			t.Fatal("mutation not sent as the session user")
		}
	})
}
