package groups

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Credential login for Facebook (#266).
//
// Facebook gates login behind Bloks-encrypted, browser-only JavaScript that is
// impractical to reproduce in a pure-Go client. Rather than reimplement it, we
// delegate the interactive login to the headless-browser social-login sidecar
// (see smore's sidecars/social-login), which drives the real web login at
// facebook.com/login and returns the session cookies. Those cookies feed
// straight into New.

// LoginParams configures a credential login via the social-login sidecar.
type LoginParams struct {
	Username string
	Password string

	// SidecarURL is the base URL of the social-login sidecar (e.g.
	// "http://social-login:8090"). Required.
	SidecarURL string

	// ProxyURL, when set, is forwarded to the sidecar so the browser logs in
	// from a residential egress (Facebook challenges datacenter IPs).
	ProxyURL string

	// VerificationCode is the email/SMS code for Facebook's login checkpoint
	// (interposed from unfamiliar IPs).
	VerificationCode string

	// HTTPClient overrides the client used to talk to the sidecar. Optional.
	HTTPClient *http.Client
}

// LoginResult holds the session minted by a credential login.
type LoginResult struct {
	Cookies  Cookies
	FinalURL string
}

type sidecarLoginRequest struct {
	Platform         string `json:"platform"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	VerificationCode string `json:"verificationCode,omitempty"`
	ProxyURL         string `json:"proxyUrl,omitempty"`
}

type sidecarLoginResponse struct {
	OK        bool              `json:"ok"`
	Challenge bool              `json:"challenge"`
	FinalURL  string            `json:"finalUrl"`
	Cookies   map[string]string `json:"cookies"`
	Hints     []string          `json:"hints"`
	Error     string            `json:"error"`
}

// ErrChallengeRequired is returned when Facebook interposes a verification
// challenge (email/SMS code) that no supplied code could satisfy.
var ErrChallengeRequired = fmt.Errorf("facebook: verification challenge required")

// Login performs a credential login through the social-login sidecar and
// returns the resulting session cookies. The caller passes the cookies to New
// to build an authenticated Client.
func Login(ctx context.Context, p LoginParams) (*LoginResult, error) {
	if p.Username == "" || p.Password == "" {
		return nil, fmt.Errorf("%w: username and password required", ErrInvalidAuth)
	}
	if p.SidecarURL == "" {
		return nil, fmt.Errorf("%w: SidecarURL required for credential login", ErrInvalidAuth)
	}

	httpClient := p.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 180 * time.Second}
	}
	endpoint := strings.TrimRight(p.SidecarURL, "/") + "/login"

	payload, err := json.Marshal(sidecarLoginRequest{
		Platform:         "facebook",
		Username:         p.Username,
		Password:         p.Password,
		VerificationCode: p.VerificationCode,
		ProxyURL:         p.ProxyURL,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("facebook: social-login sidecar: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var out sidecarLoginResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("facebook: social-login sidecar: bad response (status %d): %s", resp.StatusCode, truncate(string(raw), 200))
	}

	if !out.OK {
		if out.Challenge || hasHint(out.Hints, "verification_code_required") {
			return nil, fmt.Errorf("%w: Facebook requires an email/SMS verification code for this login", ErrChallengeRequired)
		}
		detail := out.Error
		if detail == "" && len(out.Hints) > 0 {
			detail = strings.Join(out.Hints, "; ")
		}
		if detail == "" {
			detail = "login failed"
		}
		return nil, fmt.Errorf("%w: %s", ErrInvalidAuth, detail)
	}

	c := out.Cookies
	cookies := Cookies{
		SB:    c["sb"],
		DATR:  c["datr"],
		CUser: c["c_user"],
		XS:    c["xs"],
		FR:    c["fr"],
		PSL:   c["ps_l"],
		PSN:   c["ps_n"],
	}
	if cookies.CUser == "" || cookies.XS == "" {
		return nil, fmt.Errorf("%w: sidecar returned no session cookies (c_user/xs)", ErrInvalidAuth)
	}
	return &LoginResult{Cookies: cookies, FinalURL: out.FinalURL}, nil
}

func hasHint(hints []string, want string) bool {
	for _, h := range hints {
		if h == want {
			return true
		}
	}
	return false
}
