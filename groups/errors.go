package groups

import "errors"

var (
	// ErrInvalidAuth is returned when required cookies (xs or c_user) are missing.
	ErrInvalidAuth = errors.New("facebook/groups: missing required cookie (xs or c_user)")

	// ErrUnauthorized is returned when the first bootstrap is rejected (HTTP 401/403)
	// or the bootstrap page carries no usable session tokens.
	ErrUnauthorized = errors.New("facebook/groups: authentication failed — session may be expired")

	// ErrForbidden is returned on HTTP 403 or when Facebook's error wording says
	// the authenticated user does not have access to the resource.
	ErrForbidden = errors.New("facebook/groups: access denied")

	// ErrNotFound is returned when the requested group, post, or comment does not exist.
	ErrNotFound = errors.New("facebook/groups: resource not found")

	// ErrRateLimited is returned when Facebook returns HTTP 429. The Retry-After
	// value is available through Client.RateLimit().
	ErrRateLimited = errors.New("facebook/groups: rate limited")

	// ErrAlreadyMember is returned when JoinGroup is called on a group the user has already joined.
	ErrAlreadyMember = errors.New("facebook/groups: already a member of this group")

	// ErrNotMember is returned when the user is not a member of the group: by
	// LeaveGroup, and by group feed reads whose group or group_feed comes back null.
	ErrNotMember = errors.New("facebook/groups: not a member of this group")

	// ErrInvalidParams is returned when required parameters or options are
	// missing or invalid, including an unparseable WithProxy URL.
	ErrInvalidParams = errors.New("facebook/groups: invalid or missing required parameters")

	// ErrPartialResult is returned by ScrapeGroupTrends when the context is cancelled mid-scrape.
	// The caller receives a partial TrendReport alongside this sentinel via errors.Is.
	ErrPartialResult = errors.New("facebook/groups: partial result — context cancelled mid-scrape")

	// ErrRequestFailed is returned when an HTTP request cannot be completed or
	// Facebook answers with an error this package does not classify.
	ErrRequestFailed = errors.New("facebook/groups: HTTP request failed")

	// ErrSessionExpired is returned when the Facebook session is lost: a 401, a
	// not-logged-in GraphQL error or a redirect to /login. Reads re-bootstrap
	// once and retry exactly once; when that cannot recover the session the
	// caller gets ErrSessionExpired. Writes never retry and return it directly.
	ErrSessionExpired = errors.New("facebook/groups: session expired")

	// ErrDocIDStale is returned when Facebook signals the doc_id is no longer valid.
	// Use WithDocIDs to supply updated identifiers.
	ErrDocIDStale = errors.New("facebook/groups: doc_id is stale — use WithDocIDs to override")

	// ErrCheckpoint is returned when Facebook sends the account to a checkpoint:
	// a redirect whose final URL is under /checkpoint/ (or two-step
	// verification), or an error that names a checkpoint. A person must clear it
	// in a browser before the session works again.
	ErrCheckpoint = errors.New("facebook/groups: account checkpoint")

	// ErrFeatureBlocked is returned when Facebook temporarily blocks a feature
	// for the account: api_error_code 368, "temporarily blocked", "going too
	// fast" or "you're blocked".
	ErrFeatureBlocked = errors.New("facebook/groups: feature temporarily blocked")

	// ErrAccountRestricted is returned when Facebook says the account is
	// restricted or disabled.
	ErrAccountRestricted = errors.New("facebook/groups: account restricted or disabled")

	// ErrSenderIdentityMismatch is returned by reads that take an expectedUser
	// when the bootstrap page's USER_ID, or the c_user cookie, is not that user.
	ErrSenderIdentityMismatch = errors.New("facebook/groups: session user does not match expected user")

	// ErrWriteUnknown is returned by mutations when the request may have
	// reached Facebook but no definite answer came back (transport error after
	// dispatch, HTTP 5xx, or an undecodable body). Writes are never retried;
	// treat the outcome as unknown and do not replay it.
	ErrWriteUnknown = errors.New("facebook/groups: write outcome unknown")
)
