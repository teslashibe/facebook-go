// Package mcp exposes the facebook-go [groups.Client] surface as a set of
// MCP (Model Context Protocol) tools that any host application can mount on
// its own MCP server.
//
// All tools wrap exported methods on *groups.Client (the Facebook Groups
// surface). Each tool is defined via [mcptool.Define] so the JSON input
// schema is reflected from the typed input struct — no hand-maintained
// schemas, no drift.
//
// Usage from a host application:
//
//	import (
//	    "github.com/teslashibe/mcptool"
//	    "github.com/teslashibe/facebook-go/groups"
//	    fbmcp "github.com/teslashibe/facebook-go/mcp"
//	)
//
//	client, _ := groups.New(groups.Cookies{...})
//	for _, tool := range fbmcp.Provider{}.Tools() {
//	    // register tool with your MCP server, passing client as the client arg
//	    // when invoking
//	}
//
// The [Excluded] map documents methods on *groups.Client that are
// intentionally not exposed via MCP, with a one-line reason. The coverage
// test in mcp_test.go fails if a new exported method is added without
// either being wrapped by a tool or appearing in [Excluded].
package mcp

import "github.com/teslashibe/mcptool"

// Provider implements [mcptool.Provider] for facebook-go. The zero value is
// ready to use.
type Provider struct{}

// Platform returns "facebook".
func (Provider) Platform() string { return "facebook" }

// Tools returns every facebook-go MCP tool, in registration order.
func (Provider) Tools() []mcptool.Tool {
	out := make([]mcptool.Tool, 0,
		len(groupTools)+len(membershipTools)+len(feedTools)+len(postTools)+
			len(commentTools)+len(memberTools)+len(trendTools))
	out = append(out, groupTools...)
	out = append(out, membershipTools...)
	out = append(out, feedTools...)
	out = append(out, postTools...)
	out = append(out, commentTools...)
	out = append(out, memberTools...)
	out = append(out, trendTools...)
	return out
}
