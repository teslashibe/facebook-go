package groups

import (
	"errors"
	"net/http"
	"testing"
)

func TestWhoAmIParsesCurrentUserInitialData(t *testing.T) {
	f := newFakeFB(t)
	c := f.mustClient()
	if got := c.VerifiedUserID(); got != testUser {
		t.Fatalf("VerifiedUserID = %q", got)
	}
	id, err := c.WhoAmI(bg)
	if err != nil {
		t.Fatal(err)
	}
	want := Identity{UserID: testUser, Name: "Avery Example", ProfileURL: "https://www.facebook.com/profile.php?id=" + testUser}
	if *id != want {
		t.Fatalf("WhoAmI = %+v, want %+v", *id, want)
	}
	if f.count("bootstrap") != 2 {
		t.Fatalf("WhoAmI should load a fresh bootstrap page; bootstrap count = %d", f.count("bootstrap"))
	}
}

func TestWhoAmIWithoutCurrentUser(t *testing.T) {
	f := newFakeFB(t)
	f.setBootstrap(func(w http.ResponseWriter, _ *http.Request, _ int) { _, _ = w.Write(bootstrapPage(t, "")) })
	c := f.mustClient()
	if c.VerifiedUserID() != "" {
		t.Fatalf("VerifiedUserID = %q, want empty", c.VerifiedUserID())
	}
	if _, err := c.WhoAmI(bg); !errors.Is(err, ErrRequestFailed) {
		t.Fatalf("WhoAmI err = %v, want ErrRequestFailed", err)
	}
	// Identity-bound reads fail closed and send nothing.
	if _, err := c.ListJoinedGroups(bg, testUser, ListOptions{}); !errors.Is(err, ErrSenderIdentityMismatch) {
		t.Fatalf("ListJoinedGroups err = %v, want ErrSenderIdentityMismatch", err)
	}
	if f.count("GroupsCometAllJoinedGroupsSectionPaginationQuery") != 0 {
		t.Fatal("read was sent without a verified identity")
	}
}

func TestLoggedOutBootstrapIsSessionExpired(t *testing.T) {
	f := newFakeFB(t)
	f.setBootstrap(func(w http.ResponseWriter, _ *http.Request, _ int) { _, _ = w.Write(bootstrapPage(t, "0")) })
	if _, err := f.newClient(); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
}

func TestBootstrapRedirects(t *testing.T) {
	cases := []struct {
		name, location string
		want           error
	}{
		{"checkpoint after a hop", "https://www.facebook.com/checkpoint/1501092823525282/?next=%2Fgroups%2Ffeed%2F", ErrCheckpoint},
		{"two-step verification", "https://www.facebook.com/two_step_verification/authentication/", ErrCheckpoint},
		{"login", "https://www.facebook.com/login/?next=https%3A%2F%2Fwww.facebook.com%2Fgroups%2Ffeed%2F", ErrSessionExpired},
		{"login.php relative", "/login.php?next=%2Fgroups%2Ffeed%2F", ErrSessionExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeFB(t)
			f.onPath("/groups/feed/", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/hop/", http.StatusFound)
			})
			f.onPath("/hop/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", tc.location)
				w.WriteHeader(http.StatusFound)
			})
			_, err := f.newClient()
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			// The target page itself is never fetched.
			if f.count("") != 2 {
				t.Fatalf("server saw %d requests, want 2 (feed + hop)", f.count(""))
			}
		})
	}
}

func TestBootstrapRedirectCodeInLastResponse(t *testing.T) {
	f := newFakeFB(t)
	c := f.mustClient()
	f.onPath("/groups/feed/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://www.facebook.com/checkpoint/828281030927956/", http.StatusFound)
	})
	if _, err := c.WhoAmI(bg); !errors.Is(err, ErrCheckpoint) {
		t.Fatalf("err = %v, want ErrCheckpoint", err)
	}
	if got := c.LastResponse(); got.Code != "checkpoint_redirect" || got.FriendlyName != "bootstrap" || got.Status != http.StatusFound {
		t.Fatalf("LastResponse = %+v", got)
	}
	if c.VerifiedUserID() != "" {
		t.Fatal("identity kept after a checkpoint")
	}
}

func TestSessionExpiryOneAttemptReturnsErrSessionExpired(t *testing.T) {
	const q = "GroupsCometAllJoinedGroupsSectionPaginationQuery"

	t.Run("401 every time", func(t *testing.T) {
		f := newFakeFB(t)
		f.onGraphQL(q, serveBody(http.StatusUnauthorized, `{}`))
		c := f.mustClient(WithRetry(1, 0))
		page, err := c.ListJoinedGroups(bg, testUser, ListOptions{})
		if !errors.Is(err, ErrSessionExpired) || errors.Is(err, ErrRequestFailed) {
			t.Fatalf("err = %v, want ErrSessionExpired only", err)
		}
		if page.Groups != nil {
			t.Fatal("got a page with the error")
		}
		if f.count(q) != 2 || f.count("bootstrap") != 2 {
			t.Fatalf("graphql=%d bootstrap=%d, want 2 and 2", f.count(q), f.count("bootstrap"))
		}
	})

	t.Run("not logged in, many attempts allowed", func(t *testing.T) {
		f := newFakeFB(t)
		f.onGraphQL(q, serveBody(http.StatusOK, `{"errors":[{"message":"Not logged in.","severity":"CRITICAL","api_error_code":1357001}],"data":null}`))
		c := f.mustClient(WithRetry(4, 0))
		if _, err := c.ListJoinedGroups(bg, testUser, ListOptions{}); !errors.Is(err, ErrSessionExpired) {
			t.Fatalf("err = %v, want ErrSessionExpired", err)
		}
		if f.count(q) != 2 || f.count("bootstrap") != 2 {
			t.Fatalf("graphql=%d bootstrap=%d, want exactly one retry after one re-bootstrap", f.count(q), f.count("bootstrap"))
		}
	})

	t.Run("recovers after re-bootstrap with one attempt", func(t *testing.T) {
		f := newFakeFB(t)
		f.onGraphQL(q, func(w http.ResponseWriter, _ recordedRequest, call int) {
			if call == 1 {
				writeBody(w, http.StatusUnauthorized, `{}`)
				return
			}
			_, _ = w.Write(fixture(t, "joined_groups_page1.json"))
		})
		c := f.mustClient(WithRetry(1, 0))
		page, err := c.ListJoinedGroups(bg, testUser, ListOptions{})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(page.Groups) != 3 {
			t.Fatalf("groups = %d, want 3", len(page.Groups))
		}
	})

	t.Run("re-bootstrap lands on login", func(t *testing.T) {
		f := newFakeFB(t)
		f.onGraphQL(q, serveBody(http.StatusUnauthorized, `{}`))
		c := f.mustClient()
		f.onPath("/groups/feed/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://www.facebook.com/login/", http.StatusFound)
		})
		if _, err := c.ListJoinedGroups(bg, testUser, ListOptions{}); !errors.Is(err, ErrSessionExpired) {
			t.Fatalf("err = %v, want ErrSessionExpired", err)
		}
		if f.count(q) != 1 {
			t.Fatalf("graphql=%d, want 1", f.count(q))
		}
	})

	t.Run("re-bootstrap lands on a checkpoint", func(t *testing.T) {
		f := newFakeFB(t)
		f.onGraphQL(q, serveBody(http.StatusUnauthorized, `{}`))
		c := f.mustClient()
		f.onPath("/groups/feed/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://www.facebook.com/checkpoint/1501092823525282/", http.StatusFound)
		})
		if _, err := c.ListJoinedGroups(bg, testUser, ListOptions{}); !errors.Is(err, ErrCheckpoint) {
			t.Fatalf("err = %v, want ErrCheckpoint", err)
		}
	})

	t.Run("re-bootstrap returns another user", func(t *testing.T) {
		f := newFakeFB(t)
		f.onGraphQL(q, serveBody(http.StatusUnauthorized, `{}`))
		c := f.mustClient()
		f.setBootstrap(func(w http.ResponseWriter, _ *http.Request, _ int) {
			_, _ = w.Write(bootstrapPage(t, "100000000000099"))
		})
		if _, err := c.ListJoinedGroups(bg, testUser, ListOptions{}); !errors.Is(err, ErrSenderIdentityMismatch) {
			t.Fatalf("err = %v, want ErrSenderIdentityMismatch", err)
		}
		if f.count(q) != 1 {
			t.Fatalf("graphql=%d, want no retry as another user", f.count(q))
		}
	})
}

func TestExpectedUserMismatch(t *testing.T) {
	f := newFakeFB(t)
	f.onGraphQL("CometGroupDiscussionRootSuccessQuery", serveFixture(t, "group_feed_page1.json"))
	c := f.mustClient()

	if _, err := c.ListGroupPosts(bg, "100000000000099", "900000000000001", PostListOptions{}); !errors.Is(err, ErrSenderIdentityMismatch) {
		t.Fatalf("ListGroupPosts err = %v", err)
	}
	if _, err := c.PostsSince(bg, "100000000000099", "900000000000001", "", zeroTime, 3); !errors.Is(err, ErrSenderIdentityMismatch) {
		t.Fatalf("PostsSince err = %v", err)
	}
	if _, err := c.ListJoinedGroups(bg, "100000000000099", ListOptions{}); !errors.Is(err, ErrSenderIdentityMismatch) {
		t.Fatalf("ListJoinedGroups err = %v", err)
	}
	if _, err := c.ListGroupPosts(bg, "", "900000000000001", PostListOptions{}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("empty expectedUser err = %v", err)
	}
	if n := f.count("") - f.count("bootstrap"); n != 0 {
		t.Fatalf("%d GraphQL requests sent for a mismatched user", n)
	}
}

func TestBootstrapUserDiffersFromCUser(t *testing.T) {
	f := newFakeFB(t)
	f.setBootstrap(func(w http.ResponseWriter, _ *http.Request, _ int) {
		_, _ = w.Write(bootstrapPage(t, "100000000000099"))
	})
	c := f.mustClient()
	for _, expected := range []string{testUser, "100000000000099"} {
		if _, err := c.ListJoinedGroups(bg, expected, ListOptions{}); !errors.Is(err, ErrSenderIdentityMismatch) {
			t.Fatalf("expected %s: err = %v, want ErrSenderIdentityMismatch", expected, err)
		}
	}
}

func TestRotatedCUserCookieIsMismatch(t *testing.T) {
	f := newFakeFB(t)
	f.onGraphQL("CometGroupDiscussionRootSuccessQuery", func(w http.ResponseWriter, _ recordedRequest, _ int) {
		w.Header().Add("Set-Cookie", "c_user=100000000000099; Domain=.facebook.com; Path=/")
		_, _ = w.Write(fixture(t, "group_feed_page1.json"))
	})
	c := f.mustClient()
	if _, err := c.ListGroupPosts(bg, testUser, "900000000000001", PostListOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListGroupPosts(bg, testUser, "900000000000001", PostListOptions{}); !errors.Is(err, ErrSenderIdentityMismatch) {
		t.Fatalf("err = %v, want ErrSenderIdentityMismatch after c_user changed", err)
	}
}
