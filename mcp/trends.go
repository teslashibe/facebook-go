package mcp

import (
	"context"

	"github.com/teslashibe/facebook-go/groups"
	"github.com/teslashibe/mcptool"
)

// ScrapeGroupTrendsInput is the typed input for facebook_scrape_group_trends.
type ScrapeGroupTrendsInput struct {
	GroupID   string   `json:"group_id" jsonschema:"description=numeric Facebook group ID to analyse,required"`
	MaxPosts  int      `json:"max_posts,omitempty" jsonschema:"description=cap on posts paginated through for analysis,minimum=1,maximum=2000,default=200"`
	TopN      int      `json:"top_n,omitempty" jsonschema:"description=number of top keywords and hashtags to include in the report,minimum=1,maximum=100,default=20"`
	StopWords []string `json:"stop_words,omitempty" jsonschema:"description=domain-specific stop words appended to the bundled English list before keyword extraction"`
}

func scrapeGroupTrends(ctx context.Context, c *groups.Client, in ScrapeGroupTrendsInput) (any, error) {
	var opts []groups.TrendOption
	if in.MaxPosts > 0 {
		opts = append(opts, groups.WithTrendMaxPosts(in.MaxPosts))
	}
	if in.TopN > 0 {
		opts = append(opts, groups.WithTrendTopN(in.TopN))
	}
	if len(in.StopWords) > 0 {
		opts = append(opts, groups.WithTrendStopWords(in.StopWords))
	}
	return c.ScrapeGroupTrends(ctx, in.GroupID, opts...)
}

var trendTools = []mcptool.Tool{
	mcptool.Define[*groups.Client, ScrapeGroupTrendsInput](
		"facebook_scrape_group_trends",
		"Paginate a Facebook group's feed and report top keywords, hashtags, peak hours, sentiment, and active authors",
		"ScrapeGroupTrends",
		scrapeGroupTrends,
	),
}
