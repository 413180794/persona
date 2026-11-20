package assistantwire

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	MReceiveNotice        = "receive_notice"
	MNoticeStatus         = "notice_status"
	MConfirmNotice        = "confirm_notice"
	ChannelsConfirmNotice = "channels.confirm_notice"
)

type Notice struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	SourceURL string `json:"source_url"`
	Text      string `json:"text"`
	Forward   string `json:"forward"`
}

type ConfirmChannelNotice struct {
	Extension string `json:"extension"`
	Channel   string `json:"channel"`
	ID        string `json:"id"`
}

type NoticeParams struct {
	Contribution string `json:"contribution"`
	Notice       Notice `json:"notice"`
}

type NoticeStatusParams struct {
	Contribution string `json:"contribution"`
}

type NoticeStatusResult struct {
	Ready bool `json:"ready"`
}

type NoticeResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

var noticeID = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`) })

func (n Notice) Validate() error {
	switch n.Kind {
	case "grounded_reply", "task_signal", "agent_suggestion":
	default:
		return errors.New("invalid notice kind")
	}
	if !noticeID().MatchString(n.ID) || !noticeText(n.Text, 2400) || !noticeText(n.Forward, 3600) || !noticeLink(n.SourceURL) {
		return errors.New("invalid notice fields")
	}
	return nil
}

func noticeText(s string, max int) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= max &&
		strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) < 0
}

func noticeLink(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 1024 || u.Scheme != "https" || u.Host != "applink.feishu.cn" ||
		u.User != nil || u.EscapedPath() != "/client/chat/open" || strings.Contains(raw, "#") {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 2 {
		return false
	}
	for _, key := range []string{"chatId", "messageId"} {
		v := q[key]
		if len(v) != 1 || !noticeText(v[0], 200) || strings.TrimSpace(v[0]) != v[0] {
			return false
		}
	}
	return true
}

// ChannelNotice carries hub-authenticated attribution on the private assistant bus.
type ChannelNotice struct {
	Extension string `json:"extension"`
	Channel   string `json:"channel"`
	Notice
}
