package groups

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// clientHints are the low-entropy User-Agent client hints a Chromium browser
// sends on every request. All three are empty for non-Chromium user agents.
type clientHints struct {
	brands   string // Sec-Ch-Ua
	mobile   string // Sec-Ch-Ua-Mobile
	platform string // Sec-Ch-Ua-Platform
}

func (h clientHints) apply(hdr http.Header) {
	if h.brands == "" {
		return
	}
	hdr.Set("Sec-Ch-Ua", h.brands)
	hdr.Set("Sec-Ch-Ua-Mobile", h.mobile)
	hdr.Set("Sec-Ch-Ua-Platform", h.platform)
}

var (
	reChromeMajor = regexp.MustCompile(`Chrome/(\d+)`)
	reEdgeMajor   = regexp.MustCompile(`Edg(?:e|A)?/(\d+)`)
	reOperaMajor  = regexp.MustCompile(`OPR/(\d+)`)
)

// deriveClientHints builds Sec-Ch-Ua, Sec-Ch-Ua-Mobile and Sec-Ch-Ua-Platform
// from a User-Agent string, the way Chromium does for that version.
//
// Firefox and Safari send no client hints, and every iOS browser runs on
// WebKit, so those user agents get none.
func deriveClientHints(ua string) clientHints {
	if ua == "" || strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad") || strings.Contains(ua, "iPod") {
		return clientHints{}
	}
	m := reChromeMajor.FindStringSubmatch(ua)
	if m == nil {
		return clientHints{}
	}
	major, err := strconv.Atoi(m[1])
	if err != nil || major <= 0 {
		return clientHints{}
	}

	brand, brandVersion := "Google Chrome", major
	switch {
	case reEdgeMajor.MatchString(ua):
		brand, brandVersion = "Microsoft Edge", atoiOr(reEdgeMajor.FindStringSubmatch(ua)[1], major)
	case reOperaMajor.MatchString(ua):
		brand, brandVersion = "Opera", atoiOr(reOperaMajor.FindStringSubmatch(ua)[1], major)
	case strings.Contains(ua, "HeadlessChrome/"):
		brand = "HeadlessChrome"
	}

	platform := "Unknown"
	mobile := "?0"
	switch {
	case strings.Contains(ua, "Android"):
		platform = "Android"
		if strings.Contains(ua, "Mobile") {
			mobile = "?1"
		}
	case strings.Contains(ua, "CrOS"):
		platform = "Chrome OS"
	case strings.Contains(ua, "Windows"):
		platform = "Windows"
	case strings.Contains(ua, "Macintosh"), strings.Contains(ua, "Mac OS X"):
		platform = "macOS"
	case strings.Contains(ua, "Linux"), strings.Contains(ua, "X11"):
		platform = "Linux"
	}

	return clientHints{
		brands:   chromiumBrandList(major, brand, brandVersion),
		mobile:   mobile,
		platform: `"` + platform + `"`,
	}
}

// chromiumBrandList reproduces Chromium's GREASEd brand list
// (components/embedder_support/user_agent_utils.cc, GenerateBrandVersionList):
// the major version seeds both the order of the three entries and the GREASE
// brand. For example Chrome 131 sends
// "Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24".
func chromiumBrandList(seed int, brand string, brandVersion int) string {
	greaseChars := []string{" ", "(", ":", "-", ".", "/", ")", ";", "=", "?", "_"}
	greaseVersions := []string{"8", "99", "24"}
	orders := [6][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}

	order := orders[seed%len(orders)]
	grease := `"Not` + greaseChars[seed%len(greaseChars)] + `A` + greaseChars[(seed+1)%len(greaseChars)] +
		`Brand";v="` + greaseVersions[seed%len(greaseVersions)] + `"`

	list := make([]string, 3)
	list[order[0]] = grease
	list[order[1]] = `"Chromium";v="` + strconv.Itoa(seed) + `"`
	list[order[2]] = `"` + brand + `";v="` + strconv.Itoa(brandVersion) + `"`
	return strings.Join(list, ", ")
}

func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return fallback
}
