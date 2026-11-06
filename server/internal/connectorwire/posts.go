package connectorwire

import (
	"time"
)

// Post is one public post of a feed (a company's internal social feed, say) as a connector hands it over.
type Post struct {
	ID string `json:"id"`
	// Feed is what the feed is called, for the archive's heading ("" is "Posts", in Chinese).
	Feed         string    `json:"feed,omitempty"`
	Author       string    `json:"author,omitempty"`
	AuthorID     string    `json:"author_id,omitempty"`
	Anonymous    bool      `json:"anonymous,omitempty"`
	Categories   []string  `json:"categories,omitempty"`
	Text         string    `json:"text"`
	Images       int       `json:"images,omitempty"`
	Reactions    int       `json:"reactions,omitempty"`
	CommentCount int       `json:"comment_count"`
	At           time.Time `json:"at"`
	Mine         bool      `json:"mine,omitempty"` // the user's own post
}

// PostComment is one comment on a post, as it is to be written (the connector names the user "me" and hides who is
// anonymous).
type PostComment struct {
	Author  string    `json:"author,omitempty"`
	ReplyTo string    `json:"reply_to,omitempty"`
	Text    string    `json:"text"`
	At      time.Time `json:"at"`
}

// PostPut is a post to keep, with its comments when they were read this time (nil: only the post was seen; its
// comments in the archive stay as they are; an empty list: it has none).
type PostPut struct {
	Post     Post          `json:"post"`
	Comments []PostComment `json:"comments"`
}

// What posts.put says of each post.
const (
	PostNew     = "new"     // first time in the archive
	PostChanged = "changed" // its comment count moved
	PostSame    = ""
	PostRefused = "refused" // another connector's post, or not a post (no id)
)

// PendingPost is a post whose comments are to be read again (its comment count moved since they were).
type PendingPost struct {
	ID           string    `json:"id"`
	CommentCount int       `json:"comment_count"`
	At           time.Time `json:"at"`
}

// ArchivedChat is a conversation the knowledge base has under one of the connector's names: the chat's id at the
// source, and how far it was archived.
type ArchivedChat struct {
	ID       string    `json:"id"`
	Name     string    `json:"name,omitempty"`
	Cursor   string    `json:"cursor,omitempty"`
	LastAt   time.Time `json:"last_at,omitempty"`
	Messages int       `json:"messages"`
}

// Limits of those calls.
const (
	PostsPerPut      = 200      // posts in one posts.put
	PostCommentsMost = 5000     // comments of one post
	PostTextMost     = 64 << 10 // bytes of a post's or a comment's text
	PendingPostsMost = 500
)
