package lark

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ChatPin struct {
	ID           string    `json:"id"`
	MessageID    string    `json:"message_id"`
	Message      *Message  `json:"message,omitempty"`
	OperatorName string    `json:"operator_name,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type ChatPinsPage struct {
	Items      []ChatPin `json:"items"`
	NextCursor string    `json:"next_cursor,omitempty"`
	Total      int       `json:"total"`
}

// Pin errors contain no upstream message bodies or private message text.
type ChatPinError struct {
	Status        int
	Code, Message string
}

func (e *ChatPinError) Error() string { return e.Message }

var chatPinCursor = regexp.MustCompile(`^[0-9]{1,32}$`)

func (m rawMessage) pinCapability(key string) *bool {
	if m.Capabilities == nil {
		return nil
	}
	allowed, present := m.Capabilities[key]
	allowed = (!present || allowed) && m.LocalState != "pending" && m.LocalState != "failed"
	if key == "pin" {
		allowed = allowed && !m.IsDeleted && !m.IsRecalled && !m.IsSystem && m.MessageTypeName != "SYSTEM"
	}
	return &allowed
}

func pinError(status int, code, message string) error {
	return &ChatPinError{Status: status, Code: code, Message: message}
}

func pinReadError(err error) error {
	if err == nil || errors.Is(err, ErrNotLoggedIn) || errors.Is(err, context.Canceled) {
		return err
	}
	var up *doError
	if errors.As(err, &up) {
		code := strings.ToUpper(up.Code)
		if (up.Status == 404 || up.Status == 405) && (code == "" || code == "NOT_FOUND" || code == "METHOD_NOT_ALLOWED") {
			return pinError(426, "lark_lark-connector_upgrade_required", "升级 lark-connector 后可使用 Pin 消息。请在 Hub 连接的 lark-connector 中检查更新。")
		}
		if up.Status == 403 {
			return pinError(403, "lark_pin_forbidden", "没有权限查看或修改这条 Pin")
		}
		if up.Status == 404 {
			return pinError(404, "lark_pin_not_found", "原消息已不可用，请重新加载 Pin 列表")
		}
	}
	return pinError(502, "lark_pins_unavailable", "暂时无法加载 Pin 消息，请稍后重试")
}

// lark-connector clears the chat's unread-pin marker on this read. Call only when
// the user opens the list or explicitly changes a pin, never for prefetch.
func (c *Client) ChatPins(ctx context.Context, feed, cursor string, limit int) (ChatPinsPage, error) {
	out := ChatPinsPage{Items: []ChatPin{}}
	if cursor != "" && !chatPinCursor.MatchString(cursor) {
		return out, pinError(400, "lark_pin_cursor_invalid", "Pin 列表已变化，请重新加载")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var raw struct {
		Items []struct {
			ID           string      `json:"id"`
			MessageID    string      `json:"messageId"`
			Message      *rawMessage `json:"message"`
			OperatorName string      `json:"operatorName"`
			CreatedAt    time.Time   `json:"createdAt"`
		} `json:"items"`
		HasMore    bool   `json:"hasMore"`
		NextCursor string `json:"nextCursor"`
		Total      int    `json:"total"`
	}
	if err := c.do(ctx, "GET", "/conversations/"+url.PathEscape(feed)+"/pins", q, nil, &raw); err != nil {
		return out, pinReadError(err)
	}
	if raw.HasMore {
		if !chatPinCursor.MatchString(raw.NextCursor) || raw.NextCursor == cursor || raw.NextCursor == "0" {
			return out, pinError(502, "lark_pin_page_incomplete", "未能加载完整的 Pin 列表，请重试")
		}
		out.NextCursor = raw.NextCursor
	}
	seen := map[string]bool{}
	for _, p := range raw.Items {
		if p.ID == "" || p.MessageID == "" {
			return ChatPinsPage{}, pinError(502, "lark_pin_page_incomplete", "Pin 列表暂时不完整，请重试")
		}
		if seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		pin := ChatPin{ID: p.ID, MessageID: p.MessageID, CreatedAt: p.CreatedAt, OperatorName: p.OperatorName}
		if p.Message != nil && p.Message.ID == p.MessageID && p.Message.FeedID == feed {
			m := p.Message.normalize()
			pin.Message = &m
		}
		out.Items = append(out.Items, pin)
	}
	out.Total = max(raw.Total, len(out.Items))
	return out, nil
}

func (c *Client) ChatPinMessage(ctx context.Context, feed, id string) (Message, error) {
	return c.chatPinMessage(ctx, feed, id, false)
}

func (c *Client) chatPinMessage(ctx context.Context, feed, id string, allowDeleted bool) (Message, error) {
	var raw struct {
		Item rawMessage `json:"item"`
	}
	if err := c.do(ctx, "GET", "/messages/"+url.PathEscape(id), nil, nil, &raw); err != nil {
		return Message{}, pinReadError(err)
	}
	if raw.Item.ID != id || raw.Item.FeedID != feed || (raw.Item.IsDeleted && !allowDeleted) {
		return Message{}, pinError(404, "lark_pin_not_found", "原消息已不可用，请重新加载 Pin 列表")
	}
	return raw.Item.normalize(), nil
}

type ChatPinTarget struct {
	Message Message  `json:"message"`
	Root    *Message `json:"root,omitempty"`
	Page    Page     `json:"page"`
}

// Resolve first; the app does not discard its current window until the target
// is actually present. Replies use the existing thread page, not a second chat.
func (c *Client) ResolveChatPin(ctx context.Context, feed, id string, topic bool) (ChatPinTarget, error) {
	m, err := c.ChatPinMessage(ctx, feed, id)
	if err != nil {
		return ChatPinTarget{}, err
	}
	out := ChatPinTarget{Message: m}
	if m.ThreadKind == "reply" || (m.ThreadID != "" && m.ThreadID != m.ID) || topic {
		rootID := m.ThreadID
		if rootID == "" {
			rootID = m.ID
		}
		root, err := c.ChatPinMessage(ctx, feed, rootID)
		if err != nil {
			return out, err
		}
		out.Root = &root
		cursor := ""
		seen := map[string]bool{}
		for i := 0; i < 20; i++ {
			page, err := c.Thread(ctx, rootID, cursor, 100)
			if err != nil {
				return out, pinReadError(err)
			}
			page.Messages = append(page.Messages, out.Page.Messages...)
			out.Page = page
			if m.ID == rootID {
				return out, nil
			}
			for _, message := range page.Messages {
				if message.ID == id {
					return out, nil
				}
			}
			if !page.HasOlder || page.OlderCursor == "" || seen[page.OlderCursor] {
				break
			}
			seen[page.OlderCursor] = true
			cursor = page.OlderCursor
		}
	} else {
		if !chatPinCursor.MatchString(m.Position) {
			return out, pinError(409, "lark_pin_no_position", "暂时无法定位这条消息")
		}
		out.Page, err = c.Messages(ctx, feed, Window{Around: m.Position}, 40, false)
		if err != nil {
			return out, pinReadError(err)
		}
		for _, message := range out.Page.Messages {
			if message.ID == id {
				return out, nil
			}
		}
	}
	return out, pinError(409, "lark_pin_no_position", "暂时无法定位这条消息，当前聊天位置已保留")
}

// Unknown write results are never retried: the caller must reload the list.
func (c *Client) ChangeChatPin(ctx context.Context, account, feed, id, pinID string, remove bool) error {
	m, err := c.chatPinMessage(ctx, feed, id, remove)
	if err != nil {
		return err
	}
	allowed := m.CanPin
	if remove {
		allowed = m.CanUnpin
	}
	if allowed == nil || !*allowed {
		return pinError(403, "lark_pin_forbidden", "没有权限修改这条 Pin")
	}
	if remove {
		found, cursor := false, ""
		seen := map[string]bool{}
		for page := 0; page < 100; page++ {
			pins, err := c.ChatPins(ctx, feed, cursor, 100)
			if err != nil {
				return err
			}
			for _, p := range pins.Items {
				if p.ID == pinID && p.MessageID == id {
					found = true
				}
			}
			if found || pins.NextCursor == "" {
				break
			}
			if seen[pins.NextCursor] {
				break
			}
			seen[pins.NextCursor] = true
			cursor = pins.NextCursor
		}
		if !found {
			return pinError(409, "lark_pin_changed", "这条 Pin 已变化，请重新加载列表")
		}
	}
	selected, err := c.SelectedUserID(ctx)
	if err != nil {
		return err
	}
	if selected != account {
		return pinError(409, "lark_account_changed", "飞书账号已切换，请重新打开会话")
	}
	method := "POST"
	var body any
	if remove {
		method, body = "DELETE", map[string]string{"pinId": pinID}
	}
	var result struct {
		Pinned *bool `json:"pinned"`
	}
	err = c.do(ctx, method, "/messages/"+url.PathEscape(id)+"/pin", nil, body, &result)
	if err != nil {
		var up *doError
		if errors.As(err, &up) && up.Status >= 400 && up.Status < 500 {
			return pinReadError(err)
		}
		if errors.Is(err, ErrNotLoggedIn) {
			return err
		}
		return pinError(502, "lark_pin_result_unknown", "尚未确认操作结果，请重新加载 Pin 列表查看；不会自动重试")
	}
	if result.Pinned == nil || *result.Pinned == remove {
		return pinError(502, "lark_pin_result_unknown", "尚未确认操作结果，请重新加载 Pin 列表查看；不会自动重试")
	}
	return nil
}
