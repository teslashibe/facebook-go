package groups

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"sync"
)

const (
	bootstrapURL      = "https://www.facebook.com/groups/feed/"
	bootstrapName     = "bootstrap"
	maxRedirects      = 10
	maxBootstrapBody  = 16 << 20
	profileURLPattern = "https://www.facebook.com/profile.php?id="
)

// sessionState holds the per-process session tokens extracted from the initial
// page load. All fields are protected by mu; callers must hold at least a read
// lock before accessing any field.
type sessionState struct {
	mu      sync.RWMutex
	fbDTSG  string // fb_dtsg: CSRF token for every GraphQL POST
	lsd     string // lsd: lightweight session data (X-FB-LSD header + form body)
	rev     string // __rev / __spin_r: client revision number
	hs      string // __hs: haste_session
	hsi     string // __hsi: haste_session instance
	spinT   string // __spin_t: spin timestamp
	spinS   string // __s: spin session (optional; omitted when absent)
	jazoest string // jazoest: computed anti-bot field

	userID   string // CurrentUserInitialData.USER_ID from the last bootstrap
	userName string // CurrentUserInitialData.NAME from the last bootstrap
}

// Regexes for token extraction from the HTML page source.
var (
	reDTSG  = regexp.MustCompile(`"DTSGInitialData"[^{]*\{"token":"([^"]+)"`)
	reLSD   = regexp.MustCompile(`"LSD"[^{]*\{"token":"([^"]+)"`)
	reRev   = regexp.MustCompile(`"client_revision"\s*:\s*(\d+)`)
	reHs    = regexp.MustCompile(`"haste_session"\s*:\s*"([^"]+)"`)
	reHsi   = regexp.MustCompile(`"hsi"\s*:\s*"(\d+)"`)
	reSpinT = regexp.MustCompile(`"_?_?spin_t"\s*:\s*(\d+)`)
	reSpinS = regexp.MustCompile(`"_?_?spin_s"\s*:\s*"([^"]+)"`)
)

// bootstrap loads /groups/feed/ under ctx and extracts the session tokens and
// the current user. Redirects are followed by hand (at most maxRedirects), so
// the request gate and Set-Cookie handling see every hop. A redirect to
// /checkpoint/ returns ErrCheckpoint and one to /login returns
// ErrSessionExpired; neither target is fetched.
func (c *Client) bootstrap(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	target, err := url.Parse(bootstrapURL)
	if err != nil {
		return fmt.Errorf("%w: bootstrap URL: %v", ErrRequestFailed, err)
	}

	for hop := 0; ; hop++ {
		if hop > maxRedirects {
			return fmt.Errorf("%w: bootstrap: too many redirects", ErrRequestFailed)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return fmt.Errorf("%w: building bootstrap request: %v", ErrRequestFailed, err)
		}
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Sec-Fetch-Dest", "document")
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Site", "none")
		req.Header.Set("Sec-Fetch-User", "?1")
		req.Header.Set("Upgrade-Insecure-Requests", "1")
		c.hints.apply(req.Header)
		req.Header.Set("Cookie", c.cookieHeader())

		resp, preflight, err := c.roundTrip(ctx, req, bootstrapName)
		if err != nil {
			if preflight {
				return err
			}
			return fmt.Errorf("%w: bootstrap GET failed: %w", ErrRequestFailed, err)
		}
		status := resp.StatusCode

		if status >= 300 && status < 400 {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			loc := resp.Header.Get("Location")
			next, perr := target.Parse(loc)
			if loc == "" || perr != nil {
				c.observe(status, bootstrapName, "http_"+strconv.Itoa(status))
				return fmt.Errorf("%w: bootstrap redirect without a usable Location", ErrRequestFailed)
			}
			switch classifyLocation(next) {
			case locCheckpoint:
				c.clearIdentity()
				c.observe(status, bootstrapName, "checkpoint_redirect")
				return fmt.Errorf("%w: bootstrap redirected to a checkpoint", ErrCheckpoint)
			case locRestricted:
				c.clearIdentity()
				c.observe(status, bootstrapName, "restricted_redirect")
				return fmt.Errorf("%w: bootstrap redirected to a disabled-account page", ErrAccountRestricted)
			case locLogin:
				c.clearIdentity()
				c.observe(status, bootstrapName, "login_redirect")
				return fmt.Errorf("%w: bootstrap redirected to login", ErrSessionExpired)
			}
			if next.Scheme != "https" || !isFacebookHost(next.Hostname()) {
				c.observe(status, bootstrapName, "offsite_redirect")
				return fmt.Errorf("%w: bootstrap redirect left facebook.com", ErrRequestFailed)
			}
			c.observe(status, bootstrapName, "redirect")
			target = next
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBootstrapBody))
		resp.Body.Close()
		httpCode := "http_" + strconv.Itoa(status)
		switch {
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			c.observe(status, bootstrapName, httpCode)
			return ErrUnauthorized
		case status == http.StatusTooManyRequests:
			wait := c.noteRetryAfter(resp.Header)
			c.observe(status, bootstrapName, httpCode)
			return fmt.Errorf("%w: bootstrap retry after %s", ErrRateLimited, wait)
		case status != http.StatusOK:
			c.observe(status, bootstrapName, httpCode)
			return fmt.Errorf("%w: bootstrap returned HTTP %d", ErrRequestFailed, status)
		}
		if readErr != nil {
			c.observe(status, bootstrapName, "read_error")
			return fmt.Errorf("%w: reading bootstrap body: %v", ErrRequestFailed, readErr)
		}

		code, err := c.parseBootstrapPage(body)
		c.observe(status, bootstrapName, code)
		return err
	}
}

// parseBootstrapPage reads the current user and the session tokens from a
// bootstrap page and stores them. It returns the safe code for LastResponse.
func (c *Client) parseBootstrapPage(body []byte) (string, error) {
	ident, found := parseCurrentUser(body)
	if found && (ident.UserID == "" || ident.UserID == "0") {
		c.clearIdentity()
		return "logged_out", fmt.Errorf("%w: bootstrap page is logged out", ErrSessionExpired)
	}
	if err := c.parseSessionTokens(body); err != nil {
		if bytes.Contains(body, []byte("/checkpoint/")) {
			c.clearIdentity()
			return "checkpoint_page", fmt.Errorf("%w: bootstrap page is a checkpoint", ErrCheckpoint)
		}
		return "bootstrap_tokens_missing", err
	}
	c.session.mu.Lock()
	c.session.userID = ident.UserID
	c.session.userName = ident.Name
	c.session.mu.Unlock()
	return codeOK, nil
}

// parseCurrentUser reads the CurrentUserInitialData config entry, which a
// logged-in page carries as ["CurrentUserInitialData",[],{"USER_ID":"…",
// "NAME":"…",…},…]. found is false when the entry is absent or unreadable.
func parseCurrentUser(body []byte) (Identity, bool) {
	marker := []byte(`"CurrentUserInitialData"`)
	idx := bytes.Index(body, marker)
	if idx < 0 {
		return Identity{}, false
	}
	rest := body[idx+len(marker):]
	brace := bytes.IndexByte(rest, '{')
	if brace < 0 || brace > 64 {
		return Identity{}, false
	}
	dec := json.NewDecoder(bytes.NewReader(rest[brace:]))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return Identity{}, false
	}
	uid := str(m["USER_ID"])
	return Identity{UserID: uid, Name: str(m["NAME"]), ProfileURL: profileURLFor(uid)}, true
}

func profileURLFor(userID string) string {
	if userID == "" || userID == "0" {
		return ""
	}
	return profileURLPattern + url.QueryEscape(userID)
}

func (c *Client) clearIdentity() {
	c.session.mu.Lock()
	c.session.userID = ""
	c.session.userName = ""
	c.session.mu.Unlock()
}

// parseSessionTokens extracts all session tokens from an HTML page body and
// atomically updates sessionState.
func (c *Client) parseSessionTokens(body []byte) error {
	extract := func(re *regexp.Regexp, required bool, name string) (string, error) {
		m := re.FindSubmatch(body)
		if m == nil {
			if required {
				return "", fmt.Errorf("%w: could not extract %s from page — session may be invalid", ErrUnauthorized, name)
			}
			return "", nil
		}
		return string(m[1]), nil
	}

	dtsg, err := extract(reDTSG, true, "fb_dtsg")
	if err != nil {
		return err
	}
	lsd, err := extract(reLSD, true, "lsd")
	if err != nil {
		return err
	}
	rev, err := extract(reRev, true, "__rev")
	if err != nil {
		return err
	}
	hs, err := extract(reHs, true, "__hs")
	if err != nil {
		return err
	}
	hsi, err := extract(reHsi, true, "__hsi")
	if err != nil {
		return err
	}
	spinT, err := extract(reSpinT, true, "__spin_t")
	if err != nil {
		return err
	}
	spinS, _ := extract(reSpinS, false, "__spin_s")

	c.session.mu.Lock()
	c.session.fbDTSG = dtsg
	c.session.lsd = lsd
	c.session.rev = rev
	c.session.hs = hs
	c.session.hsi = hsi
	c.session.spinT = spinT
	c.session.spinS = spinS
	c.session.jazoest = computeJazoest(dtsg)
	c.session.mu.Unlock()

	return nil
}

// computeJazoest derives the jazoest anti-bot field from fb_dtsg by summing
// the unicode code points of each rune and prepending "2".
func computeJazoest(token string) string {
	sum := 0
	for _, r := range token {
		sum += int(r)
	}
	return "2" + strconv.Itoa(sum)
}

// snap returns a consistent copy of all session fields under the read lock.
func (s *sessionState) snap() (dtsg, lsd, rev, hs, hsi, spinT, spinS, jazoest string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fbDTSG, s.lsd, s.rev, s.hs, s.hsi, s.spinT, s.spinS, s.jazoest
}

// WhoAmI loads a fresh bootstrap page and returns the logged-in user from its
// CurrentUserInitialData (USER_ID and NAME). ProfileURL is
// https://www.facebook.com/profile.php?id=<USER_ID>. The page also refreshes
// the session tokens and VerifiedUserID.
func (c *Client) WhoAmI(ctx context.Context) (*Identity, error) {
	if err := c.bootstrap(ctx); err != nil {
		return nil, err
	}
	c.session.mu.RLock()
	id := Identity{UserID: c.session.userID, Name: c.session.userName}
	c.session.mu.RUnlock()
	if id.UserID == "" {
		return nil, fmt.Errorf("%w: bootstrap page carried no CurrentUserInitialData USER_ID", ErrRequestFailed)
	}
	id.ProfileURL = profileURLFor(id.UserID)
	return &id, nil
}

// VerifiedUserID returns CurrentUserInitialData.USER_ID from the last
// bootstrap, or "" before one (or when the last page carried none).
func (c *Client) VerifiedUserID() string {
	c.session.mu.RLock()
	defer c.session.mu.RUnlock()
	return c.session.userID
}

// requireUser fails unless the session belongs to expectedUser: the
// bootstrap USER_ID, the c_user cookie in the jar and the acting user id must
// all equal it.
func (c *Client) requireUser(expectedUser string) error {
	if expectedUser == "" {
		return fmt.Errorf("%w: expectedUser must not be empty", ErrInvalidParams)
	}
	verified := c.VerifiedUserID()
	if verified == "" {
		return fmt.Errorf("%w: the bootstrap page carried no USER_ID", ErrSenderIdentityMismatch)
	}
	if verified != expectedUser {
		return fmt.Errorf("%w: bootstrap USER_ID is a different user", ErrSenderIdentityMismatch)
	}
	cUser := c.jarCookie("c_user")
	if cUser == "" {
		return fmt.Errorf("%w: c_user cookie was removed", ErrSessionExpired)
	}
	if cUser != expectedUser || c.cookies.CUser != expectedUser {
		return fmt.Errorf("%w: c_user cookie is a different user", ErrSenderIdentityMismatch)
	}
	return nil
}
