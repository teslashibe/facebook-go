// Package facebook provides a Go client for Facebook's internal GraphQL API.
//
// The package is organised into surfaces, each as a sub-package:
//
//   - [github.com/teslashibe/facebook-go/groups] — search, discover, join, post,
//     comment, and scrape trends across Facebook Groups.
//
// Authentication is cookie-based. Pass the six session cookies obtained from an
// authenticated browser session (xs, c_user, sb, datr, fr, ps_l) to [groups.New].
// The client performs a one-time session bootstrap to extract the CSRF tokens
// (fb_dtsg, lsd) required by every GraphQL request.
//
// Zero production dependencies — stdlib only.
package facebook
