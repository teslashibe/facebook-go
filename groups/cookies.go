package groups

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// cookieOrder is the order the well-known session cookies are sent in. Any
// other jar cookies follow, sorted by name, so the header is deterministic.
var cookieOrder = []string{"sb", "datr", "c_user", "xs", "fr", "ps_l", "ps_n"}

// initialJar merges Cookies.Jar with the named struct fields. Non-empty struct
// fields win over the same name in the jar.
func initialJar(c Cookies) map[string]string {
	jar := make(map[string]string, len(c.Jar)+len(cookieOrder))
	for name, val := range c.Jar {
		name = strings.TrimSpace(name)
		if name == "" || val == "" {
			continue
		}
		jar[name] = val
	}
	set := func(name, val string) {
		if val != "" {
			jar[name] = val
		}
	}
	set("sb", c.SB)
	set("datr", c.DATR)
	set("c_user", c.CUser)
	set("xs", c.XS)
	set("fr", c.FR)
	set("ps_l", c.PSL)
	set("ps_n", c.PSN)
	return jar
}

// cookieHeader builds the Cookie header value from the current jar.
func (c *Client) cookieHeader() string {
	c.jarMu.Lock()
	defer c.jarMu.Unlock()

	parts := make([]string, 0, len(c.jar))
	known := make(map[string]bool, len(cookieOrder))
	for _, name := range cookieOrder {
		known[name] = true
		if v := c.jar[name]; v != "" {
			parts = append(parts, name+"="+v)
		}
	}
	rest := make([]string, 0, len(c.jar))
	for name := range c.jar {
		if !known[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		if v := c.jar[name]; v != "" {
			parts = append(parts, name+"="+v)
		}
	}
	return strings.Join(parts, "; ")
}

// jarCookie returns one cookie from the current jar.
func (c *Client) jarCookie(name string) string {
	c.jarMu.Lock()
	defer c.jarMu.Unlock()
	return c.jar[name]
}

// AuthSnapshot returns the cookie jar (name to value, facebook.com cookies
// only) as it stands after Set-Cookie rotation on every response so far.
// Deleted and expired cookies are absent. The map is a copy.
func (c *Client) AuthSnapshot() map[string]string {
	c.jarMu.Lock()
	defer c.jarMu.Unlock()
	out := make(map[string]string, len(c.jar))
	for k, v := range c.jar {
		out[k] = v
	}
	return out
}

// absorbSetCookies applies the response's Set-Cookie headers to the jar.
// Only cookies for facebook.com (or a subdomain) are kept; a cookie that is
// deleted or already expired is removed.
func (c *Client) absorbSetCookies(reqURL *url.URL, resp *http.Response) {
	if reqURL == nil || resp == nil {
		return
	}
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return
	}
	host := reqURL.Hostname()
	now := time.Now()

	c.jarMu.Lock()
	defer c.jarMu.Unlock()
	for _, ck := range cookies {
		if ck == nil || ck.Name == "" || !cookieDomainOK(host, ck.Domain) {
			continue
		}
		expired := ck.MaxAge < 0 || (!ck.Expires.IsZero() && !ck.Expires.After(now))
		if expired || ck.Value == "" || ck.Value == "deleted" {
			delete(c.jar, ck.Name)
			continue
		}
		c.jar[ck.Name] = ck.Value
	}
}

// isFacebookHost reports whether host is facebook.com or one of its subdomains.
func isFacebookHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	return h == "facebook.com" || strings.HasSuffix(h, ".facebook.com")
}

// cookieDomainOK reports whether a Set-Cookie received from reqHost with the
// given Domain attribute belongs to facebook.com.
func cookieDomainOK(reqHost, domain string) bool {
	if !isFacebookHost(reqHost) {
		return false
	}
	if domain == "" {
		return true // host-only cookie for a facebook.com host
	}
	return isFacebookHost(strings.TrimPrefix(domain, "."))
}
