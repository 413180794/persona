package lark

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// removed (Feishu Moments, once 头条圈) for the inbox and the knowledge base: what concerns me (notifications), a post's
// comments, my own posts, and commenting. lark-connector's moments routes (its lark-connector skill, references/moments.md).

// MomentNotice is one Moments notification: someone commented on my post, replied to my comment, @ed me…
type MomentNotice struct {
	ID                string    `json:"id"`
	Kind              string    `json:"kind"` // comment | reply | atInPost | atInComment | follower | postReaction | commentReaction
	PostID            string    `json:"postId"`
	CommentID         string    `json:"commentId"`
	ReplyToCommentID  string    `json:"replyToCommentId"`
	ActorUserID       string    `json:"actorUserId"`
	ActorName         string    `json:"actorName"`
	Anonymous         bool      `json:"isAnonymousActor"`
	CommentText       string    `json:"commentText"`
	PostText          string    `json:"postText"`
	TargetCommentText string    `json:"targetCommentText"` // the comment of mine it replies to
	Unread            bool      `json:"isUnread"`
	At                time.Time `json:"-"`
}

// MomentNotices are the newest message notifications (comments, replies, @s), newest first.
func (c *Client) MomentNotices(ctx context.Context, count int) ([]MomentNotice, error) {
	var out struct {
		Notifications []struct {
			MomentNotice
			CreateTime int64 `json:"createTime"`
		} `json:"notifications"`
	}
	q := url.Values{"category": {"message"}, "count": {strconv.Itoa(count)}}
	if err := c.do(ctx, "GET", "/moments/notifications", q, nil, &out); err != nil {
		return nil, err
	}
	ns := make([]MomentNotice, 0, len(out.Notifications))
	for _, n := range out.Notifications {
		m := n.MomentNotice
		m.At = time.UnixMilli(n.CreateTime)
		ns = append(ns, m)
	}
	return ns, nil
}

// MomentComment is one comment of a post; replies point at the comment they answer.
type MomentComment struct {
	ID             string
	UserID         string
	UserName       string
	Anonymous      bool
	ReplyCommentID string
	ReplyUserName  string
	Text           string
	At             time.Time
}

type rawMomentComment struct {
	ID             string `json:"id"`
	UserID         string `json:"userId"`
	UserName       string `json:"userName"`
	IsAnonymous    bool   `json:"isAnonymous"`
	ReplyCommentID string `json:"replyCommentId"`
	ReplyUserName  string `json:"replyUserName"`
	Content        struct {
		Text string `json:"text"`
	} `json:"content"`
	CreateTime int64 `json:"createTime"`
}

// MomentCommentList reads a post's comments, oldest first, up to max of them (pages of 50).
func (c *Client) MomentCommentList(ctx context.Context, postID string, max int) ([]MomentComment, error) {
	var all []MomentComment
	token := ""
	for len(all) < max {
		var out struct {
			Comments      []rawMomentComment `json:"comments"`
			NextPageToken string             `json:"nextPageToken"`
		}
		q := url.Values{"count": {"50"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		if err := c.do(ctx, "GET", "/moments/posts/"+url.PathEscape(postID)+"/comments", q, nil, &out); err != nil {
			return all, err
		}
		for _, r := range out.Comments {
			all = append(all, MomentComment{ID: r.ID, UserID: r.UserID, UserName: r.UserName, Anonymous: r.IsAnonymous,
				ReplyCommentID: r.ReplyCommentID, ReplyUserName: r.ReplyUserName, Text: r.Content.Text, At: time.UnixMilli(r.CreateTime)})
		}
		if out.NextPageToken == "" || out.NextPageToken == token || len(out.Comments) == 0 {
			break
		}
		token = out.NextPageToken
	}
	return all, nil
}

// MomentPost is a post as lists return it.
type MomentPost struct {
	ID           string
	UserID       string
	UserName     string
	Text         string
	CommentCount int
	At           time.Time
}

// MyMomentPosts are my newest posts.
func (c *Client) MyMomentPosts(ctx context.Context, count int) ([]MomentPost, error) {
	var out struct {
		Posts []struct {
			ID           string `json:"id"`
			UserID       string `json:"userId"`
			UserName     string `json:"userName"`
			CommentCount int    `json:"commentCount"`
			CreateTime   int64  `json:"createTime"`
			Content      struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"posts"`
	}
	if err := c.do(ctx, "GET", "/moments/users/me/posts", url.Values{"count": {strconv.Itoa(count)}}, nil, &out); err != nil {
		return nil, err
	}
	ps := make([]MomentPost, 0, len(out.Posts))
	for _, p := range out.Posts {
		ps = append(ps, MomentPost{ID: p.ID, UserID: p.UserID, UserName: p.UserName, Text: p.Content.Text, CommentCount: p.CommentCount,
			At: time.UnixMilli(p.CreateTime)})
	}
	return ps, nil
}

// CommentMoment posts a comment on a post as me, as a reply to replyTo when given. Everyone in the company sees it.
func (c *Client) CommentMoment(ctx context.Context, postID, replyTo, text string) error {
	body := map[string]any{"text": text}
	if replyTo != "" { // omitted, not "", for a top-level comment (lark-connector sends "" upstream as-is)
		body["replyCommentId"] = replyTo
	}
	return c.do(ctx, "POST", "/moments/posts/"+url.PathEscape(postID)+"/comments", nil, body, nil)
}

// MomentTab is one tab of the feed: 推荐 ("1") or a category.
type MomentTab struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type int    `json:"type"` // 1 推荐, 2 a category
}

func (c *Client) MomentTabs(ctx context.Context) ([]MomentTab, error) {
	var out struct {
		Tabs []MomentTab `json:"tabs"`
	}
	err := c.do(ctx, "GET", "/moments/tabs", nil, nil, &out)
	return out.Tabs, err
}

// MomentFeedPost is a post as a feed page lists it.
type MomentFeedPost struct {
	MomentPost
	Anonymous   bool
	Categories  []string // names when the feed gives them
	CategoryIDs []string // a category's id is its tab's id (MomentTabs)
	Images      int
	Reactions   int
}

// MomentFeed is one page of a tab (count ≤ 50; 推荐 ignores it), with the token for the next page ("" at the end).
func (c *Client) MomentFeed(ctx context.Context, tabID, pageToken string, count int) ([]MomentFeedPost, string, error) {
	var out struct {
		Posts []struct {
			ID            string   `json:"id"`
			UserID        string   `json:"userId"`
			UserName      string   `json:"userName"`
			IsAnonymous   bool     `json:"isAnonymous"`
			CommentCount  int      `json:"commentCount"`
			ReactionCount int      `json:"reactionCount"`
			CreateTime    int64    `json:"createTime"`
			CategoryNames []string `json:"categoryNames"`
			CategoryIDs   []string `json:"categoryIds"`
			Images        []any    `json:"images"`
			Content       struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"posts"`
		NextPageToken string `json:"nextPageToken"`
	}
	q := url.Values{"tabId": {tabID}, "count": {strconv.Itoa(count)}}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	if err := c.do(ctx, "GET", "/moments/feed", q, nil, &out); err != nil {
		return nil, "", err
	}
	ps := make([]MomentFeedPost, 0, len(out.Posts))
	for _, p := range out.Posts {
		ps = append(ps, MomentFeedPost{
			MomentPost: MomentPost{ID: p.ID, UserID: p.UserID, UserName: p.UserName, Text: p.Content.Text, CommentCount: p.CommentCount,
				At: time.UnixMilli(p.CreateTime)},
			Anonymous: p.IsAnonymous, Categories: p.CategoryNames, CategoryIDs: p.CategoryIDs, Images: len(p.Images), Reactions: p.ReactionCount,
		})
	}
	return ps, out.NextPageToken, nil
}
