# Moments (公司圈 / removed)

Paths are relative to `/agent/v1`.

**Everything written here is visible company-wide or notifies the author.**

- Confirm the exact text before posting, commenting or sharing.
- ⚠ marks a write that others see or that cannot be undone.
- _(unverified)_ marks a write that was never sent to the real gateway. Read it back afterwards.

## Browse (all read-only)

| Call | Notes |
| --- | --- |
| `GET /moments/tabs` | `tabs[{id,name,type}]`. `id:"1"` is Recommended; for type-2 tabs, the id is a **category id** |
| `GET /moments/feed?tabId=1\|following\|<categoryId>&count=1..50&pageToken=` | Returns `posts[]` and `nextPageToken`, which may be `""` at the end. The Recommended tab ignores `count` |
| `GET /moments/posts/POST_ID` | The post. A deleted post that was kept locally carries `retained:true`. **Each open re-crawls all of the post's comments in the background**, so avoid reopening the same post in loops |
| `GET /moments/posts/POST_ID/comments?count=≤50&pageToken=` | Oldest first. Replies are inline, linked by `replyCommentId`. Each comment has `id`, `userName`, `content{text}`, `createTime` (ms), `reactionCount`, `canEdit` |
| `GET /moments/categories`, `…/categories/ID`, `GET /moments/postable-categories` | Categories. `postable-categories` shows `canAnonymous` |
| `GET /moments/hashtags/ID`, `…/hashtags/ID/posts?order=new\|hot`, `GET /moments/hashtag-suggestions?q=` | Hashtags |
| `GET /moments/translations/ENTITY_ID?lang=zh&type=post\|comment` | Translation. Lark remembers the choice, so the feed later shows `translatedTo` |
| `GET /moments/videos/TOKEN -o v.mp4` | `TOKEN` comes from `post.video.token`. Supports Range; files are large |
| `GET /moments/reactions/ENTITY_ID/summary?entityType=1\|2`, `…/users?type=&count=&pageToken=` | Reaction counts and who reacted. `entityType` 1 is a post, 2 a comment |
| `GET /moments/home/user/profile`, `…/posts`, `…/activities`, `…/follow`, `…/followers`, `…/followings` | `me` works here. `posts` marks your own posts with `canEdit` and `editBlock` |
| `GET /moments/notifications?category=message\|reaction&count=&pageToken=` | `kind` is one of: follower, postReaction, commentReaction, comment, reply, atInPost, atInComment |
| `GET /moments/badge` | `{messageCount,reactionCount}` |
| `GET /moments/broadcasts`, `GET /moments/anonymous-quota`, `GET /moments/nickname-profile`, `…/nickname-profile/ID` | Banner posts, anonymous quota, nickname cards. The id form takes a numeric Moments id only, not `me` |
| `GET /moments/recommend-at`, `GET /moments/posts/ID/dislike-reasons`, `GET /moments/retained?limit=&offset=` | People suggested for @, reasons for "not interested", locally kept deleted posts |

## Write

| Job | Call | Notes |
| --- | --- | --- |
| ⚠ Text post | `POST /moments/posts {text, categoryIds:[id], circleId, isAnonymous?}` | **Always send `circleId` and `categoryIds`.** Without `circleId`, the server substitutes a category id, which looks like a bug. Copy `circleId` from any post in `GET /moments/feed` (posts in one tenant share it). Pick the category from `postable-categories`. For anonymous posts, check `canAnonymous` and `anonymous-quota` first |
| ⚠ Comment or reply | `POST /moments/posts/POST_ID/comments {text, replyCommentId?, isAnonymous?}` | When not replying, **omit** `replyCommentId`; an empty string is sent upstream as-is. Notifies the author or the person you reply to |
| ⚠ React | `POST /moments/reactions/ENTITY_ID {type? (default "THUMBSUP"), entityType? (1 post, default; 2 comment)}` | Notifies the author |
| Remove a reaction | `DELETE /moments/reactions/ENTITY_ID?entityType=&type=` |  |
| ⚠ Post with images or @ | `POST /moments/rich-posts {text≤10000, images:[{key,width?,height?}]≤9, mentions:[{userId,name}]≤50, categoryIds≤5, circleId, isAnonymous?}` | _(unverified)_. Image keys come from `POST /image-uploads`; whether Moments accepts them is also unverified |
| ⚠ Comment with @ | `POST /moments/posts/POST_ID/rich-comments {text, mentions, replyCommentId?}` | _(unverified)_ |
| ⚠ Edit own post | `PATCH /moments/posts/POST_ID {text}` | Text posts only, within 1440 minutes; check `editBlock`. Existing @ and hashtags become plain text |
| ⚠ Delete own post or comment | `DELETE /moments/posts/POST_ID`, `DELETE /moments/comments/COMMENT_ID` | Cannot be undone |
| Edit a comment | `PATCH /moments/comments/ID {text}` | A tenant can turn comment editing off; then it always returns 409 `COMMENT_NOT_EDITABLE` |
| ⚠ Share to chats | `POST /moments/posts/POST_ID/share {chatIds:[feedId…≤20], replyMessage?≤2000}` | Sends a card to those chats |
| ⚠ Report | `POST /moments/posts/POST_ID/report {reason, commentId?}` | Goes to the admins |
| Not interested | `POST /moments/posts/POST_ID/dislike {reasonIds}` | Affects your feed only |
| Mark notifications read | `POST /moments/notifications/read {category, notificationId?}` |  |
| Follow | `POST`/`DELETE /moments/home/user/follow` | A tenant can turn following off; then it returns 409 `FOLLOWING_DISABLED`. `GET …/follow` shows `disabled` |

## Workflows

1. **Browse and engage**
   1. `GET /moments/tabs`, then `GET /moments/feed?tabId=…&count=20`.
   2. `GET /moments/posts/ID/comments?count=50` until no `nextPageToken` is returned.
   3. Like a post: `POST /moments/reactions/POST_ID`. Like a comment: `POST /moments/reactions/COMMENT_ID '{"entityType":2}'`.
   4. Read back with `GET /moments/reactions/ID/summary?entityType=…` and check `selfInvolved`.
2. **Post**
   1. `GET /moments/postable-categories`.
   2. Show the user the text and the category.
   3. `POST /moments/posts` with `circleId` and `categoryIds`.
   4. Read back with `GET /moments/users/me/posts?count=1`.
3. **Handle notifications**
   1. `GET /moments/badge`.
   2. `GET /moments/notifications?category=message` (then `reaction`).
   3. Reply through workflow 1, using the notification's `postId` and `commentId`.

Times: `createTime` and `time` are ms. Broadcast end times are seconds.

The website arranges sections, posts, the viewer profile, and featured broadcasts in responsive columns. Opening the default composer only reads its category, quota, and nickname data; publishing still requires an explicit action. This UI layout does not change the agent API, read/write boundaries, or retained-post restrictions.

The featured sidebar uses explicit loaded-result state for its responsive visibility; no API behavior changes.
