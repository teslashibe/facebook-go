package groups

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Facebook's Comet story nodes move fields around between surfaces and
// deploys: the message, creation_time, permalink, feedback and actors may sit
// on the node itself or deep inside comet_sections. parseStory reads a story
// as generic JSON and tries the known places in a fixed order, then a
// deterministic deep search that skips nested (shared) stories.

// skipKeys are never descended into by deep searches: they hold a different
// story (a share) or comments rather than this story's own fields.
var skipKeys = map[string]bool{
	"attached_story":                 true,
	"attached_story_layout":          true,
	"comments":                       true,
	"comment_list_renderer":          true,
	"top_level_comments":             true,
	"interesting_top_level_comments": true,
}

var (
	paths = struct {
		message, creationTime, permalink, feedback, actor, attachments [][]any
	}{
		message: [][]any{
			{"message", "text"},
			{"comet_sections", "content", "story", "message", "text"},
			{"comet_sections", "content", "story", "comet_sections", "message", "story", "message", "text"},
			{"comet_sections", "message", "story", "message", "text"},
		},
		creationTime: [][]any{
			{"creation_time"},
			{"comet_sections", "context_layout", "story", "comet_sections", "metadata", "*", "story", "creation_time"},
			{"comet_sections", "timestamp", "story", "creation_time"},
			{"comet_sections", "content", "story", "creation_time"},
		},
		permalink: [][]any{
			{"permalink_url"},
			{"url"},
			{"wwwURL"},
			{"comet_sections", "context_layout", "story", "comet_sections", "metadata", "*", "story", "url"},
			{"comet_sections", "timestamp", "story", "url"},
			{"comet_sections", "content", "story", "wwwURL"},
		},
		feedback: [][]any{
			{"feedback"},
			{"comet_sections", "feedback", "story", "feedback_context", "feedback_target_with_context", "ufi_renderer", "feedback"},
			{"comet_sections", "feedback", "story", "comet_feed_ufi_container_renderer", "feedback"},
			{"comet_sections", "feedback", "story", "story_ufi_container", "story", "feedback_context", "feedback_target_with_context"},
		},
		actor: [][]any{
			{"actors", 0},
			{"comet_sections", "context_layout", "story", "comet_sections", "actor_photo", "story", "actors", 0},
			{"comet_sections", "context_layout", "story", "comet_sections", "title", "story", "actors", 0},
			{"comet_sections", "content", "story", "actors", 0},
		},
		attachments: [][]any{
			{"attachments"},
			{"comet_sections", "content", "story", "attachments"},
		},
	}

	rePostIDInURL = regexp.MustCompile(`/(?:posts|permalink)/(\d+)|[?&]story_fbid=(\d+)`)
)

// parseStory decodes one story node. groupID, when known, is the group the
// story was read from. strictType skips nodes whose __typename is present and
// not "Story" (feed section headers). ok is false when the node has no id.
func parseStory(raw json.RawMessage, groupID string, strictType bool) (Post, bool) {
	node, ok := decodeObject(raw)
	if !ok {
		return Post{}, false
	}
	if typ := str(node["__typename"]); strictType && typ != "" && typ != "Story" {
		return Post{}, false
	}

	storyID := str(node["id"])
	permalink := firstString(node, paths.permalink)
	postID := str(node["post_id"])
	if postID == "" {
		postID = postIDFromURL(permalink)
	}
	id := postID
	if id == "" {
		id = storyID
	}
	if id == "" {
		return Post{}, false
	}

	p := Post{ID: id, StoryID: storyID, GroupID: groupID, Permalink: permalink}

	if p.GroupID == "" {
		if to, ok := node["to"].(map[string]any); ok && str(to["__typename"]) == "Group" {
			p.GroupID = str(to["id"])
		}
	}

	feedback := firstObject(node, paths.feedback, func(m map[string]any) bool { return str(m["id"]) != "" })
	if feedback == nil {
		if cs, ok := node["comet_sections"].(map[string]any); ok {
			if fb, ok := deepFind(cs["feedback"], "feedback", isObjectWithID).(map[string]any); ok {
				feedback = fb
			}
		}
	}
	if feedback != nil {
		p.FeedbackID = str(feedback["id"])
		p.ReactionCount = countAt(feedback, []any{"reaction_count", "count"}, []any{"reactors", "count"}, []any{"i18n_reaction_count"})
		p.CommentCount = countAt(feedback, []any{"comment_count", "total_count"}, []any{"comment_rendering_instance", "comments", "total_count"}, []any{"total_comment_count"})
		p.ShareCount = countAt(feedback, []any{"share_count", "count"}, []any{"i18n_share_count"})
		if p.ReactionCount == 0 {
			p.ReactionCount = deepCount(feedback, "reaction_count", "count")
		}
		if p.CommentCount == 0 {
			p.CommentCount = deepCount(feedback, "comment_count", "total_count")
		}
		if p.ShareCount == 0 {
			p.ShareCount = deepCount(feedback, "share_count", "count")
		}
		if p.GroupID == "" {
			if ag, ok := feedback["associated_group"].(map[string]any); ok {
				p.GroupID = str(ag["id"])
			}
		}
	}

	if p.Permalink == "" && p.GroupID != "" && postID != "" {
		p.Permalink = "https://www.facebook.com/groups/" + p.GroupID + "/posts/" + postID + "/"
	}

	p.Message = firstString(node, paths.message)
	if p.Message == "" {
		if cs, ok := node["comet_sections"].(map[string]any); ok {
			if m, ok := deepFind(cs["content"], "message", isObjectWithText).(map[string]any); ok {
				p.Message = str(m["text"])
			}
		}
	}

	if ts := firstUnix(node, paths.creationTime); ts > 0 {
		p.CreatedAt = time.Unix(ts, 0).UTC()
	} else if ts, ok := toInt64(deepFind(node["comet_sections"], "creation_time", isPositiveNumber)); ok && ts > 0 {
		p.CreatedAt = time.Unix(ts, 0).UTC()
	}
	if ts, ok := toInt64(node["edited_time"]); ok && ts > 0 {
		p.UpdatedAt = time.Unix(ts, 0).UTC()
	}

	if arr, ok := firstValue(node, paths.attachments).([]any); ok {
		for _, a := range arr {
			am, ok := a.(map[string]any)
			if !ok {
				continue
			}
			if u := str(am["url"]); u != "" {
				p.Attachments = append(p.Attachments, u)
			} else if u := str(dig(am, "media", "image", "uri")); u != "" {
				p.Attachments = append(p.Attachments, u)
			}
		}
	}

	if actor := firstObject(node, paths.actor, nil); actor != nil {
		p.Author = authorFromActor(actor, node)
	} else if op := ownerProfile(feedback, node); op != nil {
		// Actors missing: fall back to the feedback's owning profile. Its kind
		// stays unknown unless its __typename says otherwise.
		p.Author = authorFromActor(op, node)
	} else {
		p.Author = Author{Kind: AuthorUnknown}
	}
	p.AuthorID = p.Author.ID
	p.AuthorName = p.Author.Name
	return p, true
}

// ownerProfile returns feedback.owning_profile from the story's feedback.
func ownerProfile(feedback, node map[string]any) map[string]any {
	if feedback != nil {
		if op, ok := feedback["owning_profile"].(map[string]any); ok {
			return op
		}
	}
	if op, ok := deepFind(node, "owning_profile", isObjectWithID).(map[string]any); ok {
		return op
	}
	return nil
}

// anonymousNames are the labels Facebook shows for anonymous group posts.
var anonymousNames = map[string]bool{
	"anonymous":             true,
	"anonymous member":      true,
	"anonymous participant": true,
}

var anonymousMarkers = []string{"is_anonymous", "is_anonymous_author", "is_anonymous_participant", "is_anonymous_post", "anonymous"}

// authorFromActor decodes an actor. Kind comes from __typename: User is a
// person, Page is a page, an anonymous-participant marker or an "Anon"
// typename is anonymous, and anything else (or a missing id) is unknown.
func authorFromActor(actor, story map[string]any) Author {
	a := Author{
		ID:         str(actor["id"]),
		Name:       str(actor["name"]),
		ProfileURL: firstNonEmpty(str(actor["url"]), str(actor["profile_url"])),
	}
	typ := str(actor["__typename"])
	switch {
	case isAnonymousActor(actor, story):
		a.Kind = AuthorAnonymous
	case typ == "User" && a.ID != "":
		a.Kind = AuthorPerson
		if a.ProfileURL == "" {
			a.ProfileURL = profileURLFor(a.ID)
		}
	case typ == "Page" && a.ID != "":
		a.Kind = AuthorPage
		if a.ProfileURL == "" {
			a.ProfileURL = "https://www.facebook.com/" + a.ID
		}
	default:
		a.Kind = AuthorUnknown
	}
	return a
}

func isAnonymousActor(actor, story map[string]any) bool {
	if strings.Contains(strings.ToLower(str(actor["__typename"])), "anon") {
		return true
	}
	for _, k := range anonymousMarkers {
		if truthy(actor[k]) || truthy(story[k]) {
			return true
		}
	}
	return anonymousNames[strings.ToLower(strings.TrimSpace(str(actor["name"])))]
}

func postIDFromURL(u string) string {
	m := rePostIDInURL.FindStringSubmatch(u)
	if m == nil {
		return ""
	}
	return firstNonEmpty(m[1], m[2])
}

// ---------------------------------------------------------------------------
// Generic JSON helpers (values decoded with UseNumber)
// ---------------------------------------------------------------------------

func decodeObject(raw json.RawMessage) (map[string]any, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil || m == nil {
		return nil, false
	}
	return m, true
}

// dig follows path (string keys, int indexes, "*" for any array element) and
// returns the first value found that is not nil or "".
func dig(v any, path ...any) any {
	if len(path) == 0 {
		if v == nil {
			return nil
		}
		if s, ok := v.(string); ok && s == "" {
			return nil
		}
		return v
	}
	switch p := path[0].(type) {
	case string:
		if p == "*" {
			arr, ok := v.([]any)
			if !ok {
				return nil
			}
			for _, el := range arr {
				if r := dig(el, path[1:]...); r != nil {
					return r
				}
			}
			return nil
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		return dig(m[p], path[1:]...)
	case int:
		arr, ok := v.([]any)
		if !ok || p < 0 || p >= len(arr) {
			return nil
		}
		return dig(arr[p], path[1:]...)
	}
	return nil
}

func firstValue(v any, candidates [][]any) any {
	for _, path := range candidates {
		if r := dig(v, path...); r != nil {
			return r
		}
	}
	return nil
}

func firstString(v any, candidates [][]any) string {
	for _, path := range candidates {
		if s := str(dig(v, path...)); s != "" {
			return s
		}
	}
	return ""
}

func firstUnix(v any, candidates [][]any) int64 {
	for _, path := range candidates {
		if n, ok := toInt64(dig(v, path...)); ok && n > 0 {
			return n
		}
	}
	return 0
}

func firstObject(v any, candidates [][]any, accept func(map[string]any) bool) map[string]any {
	for _, path := range candidates {
		if m, ok := dig(v, path...).(map[string]any); ok && (accept == nil || accept(m)) {
			return m
		}
	}
	return nil
}

func countAt(v any, candidates ...[]any) int {
	for _, path := range candidates {
		if n, ok := toInt64(dig(v, path...)); ok && n > 0 {
			return int(n)
		}
	}
	return 0
}

// deepCount finds the first object under key holding a positive numeric field.
func deepCount(v any, key, field string) int {
	m, ok := deepFind(v, key, func(x any) bool {
		mm, ok := x.(map[string]any)
		if !ok {
			return false
		}
		n, ok := toInt64(mm[field])
		return ok && n > 0
	}).(map[string]any)
	if !ok {
		return 0
	}
	n, _ := toInt64(m[field])
	return int(n)
}

// deepFind walks v depth first with map keys in sorted order (so the result
// is deterministic), skipping skipKeys, and returns the first value stored
// under key that accept approves.
func deepFind(v any, key string, accept func(any) bool) any {
	return deepFindDepth(v, key, accept, 0)
}

func deepFindDepth(v any, key string, accept func(any) bool, depth int) any {
	if depth > 24 {
		return nil
	}
	switch t := v.(type) {
	case map[string]any:
		if val, ok := t[key]; ok && accept(val) {
			return val
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			if !skipKeys[k] {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			if r := deepFindDepth(t[k], key, accept, depth+1); r != nil {
				return r
			}
		}
	case []any:
		for _, el := range t {
			if r := deepFindDepth(el, key, accept, depth+1); r != nil {
				return r
			}
		}
	}
	return nil
}

func isObjectWithID(v any) bool {
	m, ok := v.(map[string]any)
	return ok && str(m["id"]) != ""
}

func isObjectWithText(v any) bool {
	m, ok := v.(map[string]any)
	return ok && str(m["text"]) != ""
}

func isPositiveNumber(v any) bool {
	n, ok := toInt64(v)
	return ok && n > 0
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	}
	return ""
}

func toInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return n, true
		}
		if f, err := t.Float64(); err == nil {
			return int64(f), true
		}
	case float64:
		return int64(t), true
	case int64:
		return t, true
	case int:
		return int64(t), true
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true")
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
