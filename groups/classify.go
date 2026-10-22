package groups

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Short safe codes recorded in LastResponse.
const (
	codeOK          = "ok"
	codeDecodeError = "decode_error"
	codeNoData      = "no_data"
)

// flexInt decodes a JSON number or numeric string and ignores anything else,
// so an odd error code never makes a whole response undecodable.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var s string
		if json.Unmarshal(b, &s) == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				*f = flexInt(n)
			}
		}
		return nil
	}
	var n json.Number
	if json.Unmarshal(b, &n) == nil {
		if v, err := n.Int64(); err == nil {
			*f = flexInt(v)
		}
	}
	return nil
}

// looseString decodes a JSON string and ignores any other type.
type looseString string

func (l *looseString) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*l = looseString(s)
	}
	return nil
}

// gqlEnvelope is one JSON line of an /api/graphql/ response. Facebook wraps
// responses with "for (;;);" (stripped first) and may stream several lines.
// Besides GraphQL's errors[] it also answers with a top-level "error" (an
// object, or a number with errorSummary / errorDescription beside it).
type gqlEnvelope struct {
	Data             json.RawMessage `json:"data"`
	Errors           []gqlError      `json:"errors"`
	Error            json.RawMessage `json:"error"`
	ErrorSummary     looseString     `json:"errorSummary"`
	ErrorDescription looseString     `json:"errorDescription"`
	Label            looseString     `json:"label"`
	Path             []any           `json:"path"`
}

type gqlError struct {
	Message     looseString `json:"message"`
	Summary     looseString `json:"summary"`
	Description looseString `json:"description"`
	// api_error_code 1357001 = not logged in, 368 = temporarily blocked.
	APIErrorCode flexInt `json:"api_error_code"`
	Code         flexInt `json:"code"`
	ErrorCode    flexInt `json:"error_code"`
	Severity     string  `json:"severity"`
	// type is set to "OAuthException" for auth failures.
	Type looseString `json:"type"`
}

func (e gqlError) code() int {
	return firstNonZero(int(e.APIErrorCode), int(e.Code), int(e.ErrorCode))
}

func (e gqlError) text() string {
	return joinText(string(e.Summary), string(e.Description), string(e.Message))
}

// topLevelError is the object form of a top-level {"error": {...}} body.
type topLevelError struct {
	Code         flexInt     `json:"code"`
	ErrorCode    flexInt     `json:"error_code"`
	APIErrorCode flexInt     `json:"api_error_code"`
	Message      looseString `json:"message"`
	Summary      looseString `json:"summary"`
	Description  looseString `json:"description"`
	ErrorMsg     looseString `json:"error_msg"`
	UserTitle    looseString `json:"error_user_title"`
	UserMsg      looseString `json:"error_user_msg"`
	Type         looseString `json:"type"`
}

// classify returns the error this line carries, if any, with its safe code.
//
// Facebook frequently returns "WARNING" severity errors alongside successful
// data (e.g. missing relay provider variables that don't actually break the
// query). Only CRITICAL and unspecified-severity errors are failures; when
// every error is a warning and data is present, the line is a success.
func (e *gqlEnvelope) classify() (string, error) {
	if code, typ, text, ok := e.topLevelError(); ok {
		return classifyFacebookError(code, typ, text)
	}
	if len(e.Errors) == 0 {
		return "", nil
	}
	var first gqlError
	hasNonWarning := false
	for _, ge := range e.Errors {
		if !strings.EqualFold(ge.Severity, "WARNING") {
			first = ge
			hasNonWarning = true
			break
		}
	}
	if !hasNonWarning {
		if hasData(e.Data) {
			return "", nil
		}
		first = e.Errors[0]
	}
	return classifyFacebookError(first.code(), string(first.Type), first.text())
}

// topLevelError decodes a top-level "error" member. It accepts an object
// ({"code"|"error_code", "description"|"summary"|"message"}), a number (the
// legacy ajax shape, with errorSummary and errorDescription beside it) or a
// string. null, false and 0 mean no error.
func (e *gqlEnvelope) topLevelError() (code int, typ, text string, ok bool) {
	raw := bytes.TrimSpace(e.Error)
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "false" {
		return 0, "", "", false
	}
	summary := joinText(string(e.ErrorSummary), string(e.ErrorDescription))
	switch raw[0] {
	case '{':
		var te topLevelError
		if json.Unmarshal(raw, &te) != nil {
			return 0, "", "", false
		}
		code = firstNonZero(int(te.Code), int(te.ErrorCode), int(te.APIErrorCode))
		text = joinText(string(te.Summary), string(te.Description), string(te.Message),
			string(te.ErrorMsg), string(te.UserTitle), string(te.UserMsg), summary)
		return code, string(te.Type), text, true
	case '"':
		var s string
		_ = json.Unmarshal(raw, &s)
		return 0, "", joinText(s, summary), true
	default:
		var n flexInt
		_ = n.UnmarshalJSON(raw)
		if n == 0 {
			return 0, "", "", false
		}
		return int(n), "", summary, true
	}
}

// classifyFacebookError maps a Facebook error to a package sentinel and a
// short safe code ("gql_<code>", or "gql_<class>" when there is no code).
//
// Order matters: blocks, checkpoints and restrictions are matched before the
// generic "session" wording, so a checkpoint or block that mentions the
// session is never reported as a plain expiry.
func classifyFacebookError(code int, typ, text string) (string, error) {
	msg := strings.ToLower(text)
	label := func(class string) string {
		if code != 0 {
			return "gql_" + strconv.Itoa(code)
		}
		return "gql_" + class
	}
	wrap := func(sentinel error) error {
		detail := truncate(strings.TrimSpace(text), 200)
		switch {
		case detail != "" && code != 0:
			return fmt.Errorf("%w: %s (code %d)", sentinel, detail, code)
		case code != 0:
			return fmt.Errorf("%w (code %d)", sentinel, code)
		case detail != "":
			return fmt.Errorf("%w: %s", sentinel, detail)
		}
		return sentinel
	}

	switch {
	case code == 368 || containsAny(msg, "temporarily blocked", "going too fast",
		"you're blocked", "you’re blocked", "you are blocked",
		"can't use this feature", "can’t use this feature", "cannot use this feature"):
		return label("feature_blocked"), wrap(ErrFeatureBlocked)
	case containsAny(msg, "checkpoint", "confirm your identity", "verify your identity"):
		return label("checkpoint"), wrap(ErrCheckpoint)
	case containsAny(msg, "restricted", "disabled"):
		return label("restricted"), wrap(ErrAccountRestricted)
	case code == 1357001 || containsAny(msg, "not logged in", "log in to continue", "login required", "session"):
		return label("session"), wrap(ErrSessionExpired)
	case containsAny(msg, "already a member", "already joined"):
		return label("already_member"), wrap(ErrAlreadyMember)
	case containsAny(msg, "not a member", "not member"):
		return label("not_member"), wrap(ErrNotMember)
	case isStaleDocIDMessage(msg):
		return label("doc_id_stale"), wrap(ErrDocIDStale)
	case containsAny(msg, "does not exist", "deleted", "not found"):
		return label("not_found"), wrap(ErrNotFound)
	case containsAny(msg, "permission", "forbidden", "not allowed", "access denied"):
		return label("forbidden"), wrap(ErrForbidden)
	case strings.EqualFold(typ, "OAuthException") || code == 190 || code == 102:
		return label("session"), wrap(ErrSessionExpired)
	}
	return label("error"), wrap(ErrRequestFailed)
}

// isStaleDocIDMessage matches Facebook's wording for a doc_id it no longer
// knows ("document ... not found", "persisted query not found", ...).
func isStaleDocIDMessage(msg string) bool {
	if containsAny(msg, "persisted query", "persistedquery", "persisted_query") {
		return true
	}
	if !containsAny(msg, "document", "doc_id", "doc id", "docid", "query id", "queryid") {
		return false
	}
	return containsAny(msg, "not found", "does not exist", "invalid", "unknown", "missing", "no longer", "stale")
}

// isSafetyStop reports errors that must surface even when they arrive on a
// later line of a streamed response that already carried data.
func isSafetyStop(err error) bool {
	return errors.Is(err, ErrFeatureBlocked) ||
		errors.Is(err, ErrCheckpoint) ||
		errors.Is(err, ErrAccountRestricted) ||
		errors.Is(err, ErrSessionExpired)
}

// parseGraphQLBody decodes a (possibly multi-line) GraphQL response body.
// The first data payload becomes gqlResult.Data and every later payload with
// data is kept in order as an incremental payload.
func parseGraphQLBody(body []byte) (*gqlResult, string, error) {
	body = stripFBPrefix(bytes.TrimSpace(body))
	res := &gqlResult{}
	decoded := false
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = stripFBPrefix(bytes.TrimSpace(line))
		if len(line) == 0 {
			continue
		}
		var env gqlEnvelope
		if json.Unmarshal(line, &env) != nil {
			continue
		}
		decoded = true
		if code, err := env.classify(); err != nil {
			if res.Data == nil || isSafetyStop(err) {
				return nil, code, err
			}
			continue // an error on a later incremental payload; keep the data we have
		}
		if !hasData(env.Data) {
			continue
		}
		if res.Data == nil {
			res.Data = env.Data
			continue
		}
		res.Incremental = append(res.Incremental, gqlPayload{Path: env.Path, Label: string(env.Label), Data: env.Data})
	}
	if !decoded {
		return nil, codeDecodeError, fmt.Errorf("%w: undecodable response body", ErrRequestFailed)
	}
	if res.Data == nil {
		return nil, codeNoData, fmt.Errorf("%w: no data in response", ErrRequestFailed)
	}
	return res, codeOK, nil
}

func hasData(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && string(t) != "null"
}

// Redirect target classes.
const (
	locOther = iota
	locCheckpoint
	locLogin
	locRestricted
)

// classifyLocation classifies a redirect target on facebook.com.
func classifyLocation(u *url.URL) int {
	if u == nil || !isFacebookHost(u.Hostname()) {
		return locOther
	}
	p := strings.ToLower(u.Path)
	switch {
	case strings.HasPrefix(p, "/checkpoint"), strings.HasPrefix(p, "/two_step_verification"):
		return locCheckpoint
	case p == "/login" || strings.HasPrefix(p, "/login/") || strings.HasPrefix(p, "/login.php"):
		return locLogin
	case strings.HasPrefix(p, "/disabled"):
		return locRestricted
	}
	return locOther
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func joinText(parts ...string) string {
	out := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return strings.Join(out, " — ")
}

func firstNonZero(vals ...int) int {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}
