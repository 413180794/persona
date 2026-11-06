package transcript

import (
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// UltracodeMarkers identify the lines that may change ultracode. Sparse reminders are not guaranteed on every
// turn: readers must preserve earlier evidence, including when a cold transcript's tail contains no marker.
var UltracodeMarkers = [][]byte{[]byte("local-command-stdout"), []byte("ultra_effort_enter"), []byte("ultra_effort_exit")}

const (
	UltracodeOnNotice  = "已开启 ultracode（只对本会话）"
	UltracodeOffNotice = "已关闭 ultracode"
)

func isUltracodeCommand(args string) bool {
	parts := strings.Fields(args)
	return len(parts) == 2 && parts[0] == "ultracode" && (parts[1] == "on" || parts[1] == "off")
}

// observeUltracode keeps the latest dated fact, never treating undated or pre-resume history as current state.
func (m *Meta) observeUltracode(on bool, at time.Time) bool {
	if at.IsZero() || at.Before(m.UltracodeSince) || at.Before(m.UltracodeAt) {
		return false
	}
	if m.ultracodeCommand != nil && *m.ultracodeCommand != on {
		m.ultracodeCommand = nil
	}
	m.Ultracode, m.UltracodeAt = &on, at
	return true
}

func ultracodeNoticeItem(on bool, it proto.Item) []proto.Item {
	it.Text = UltracodeOffNotice
	if on {
		it.Text = UltracodeOnNotice
	}
	return []proto.Item{it}
}

func (m *Meta) claudeUltracodeAttachment(kind, reminder string, at time.Time, notice func() proto.Item) []proto.Item {
	on := kind == "ultra_effort_enter"
	if on && reminder != "full" && reminder != "sparse" {
		return nil // Unknown reminder variants carry no confirmed meaning.
	}
	if !m.observeUltracode(on, at) {
		return nil // Old evidence must not consume a newer command's pending attachment.
	}
	paired := m.ultracodeCommand != nil && *m.ultracodeCommand == on
	m.ultracodeCommand = nil
	if paired || reminder == "sparse" {
		return nil
	}
	return ultracodeNoticeItem(on, notice())
}

// claudeUltracodeOutput handles confirmed /effort output only. Rejections remain ordinary notices and cannot
// change the last known state. A status without the optional suffix explicitly reports off.
func (m *Meta) claudeUltracodeOutput(text string, at time.Time, it proto.Item) ([]proto.Item, bool) {
	switch {
	case strings.HasPrefix(text, "Ultracode on (this session only):"):
		if m.observeUltracode(true, at) {
			on := true
			m.ultracodeCommand = &on
		}
		return ultracodeNoticeItem(true, it), true
	case strings.HasPrefix(text, "Ultracode off."):
		if m.observeUltracode(false, at) {
			on := false
			m.ultracodeCommand = &on
		}
		return ultracodeNoticeItem(false, it), true
	case strings.HasPrefix(text, "Effort level: "), strings.HasPrefix(text, "Current effort level: "):
		m.observeUltracode(strings.HasSuffix(strings.TrimSpace(text), "· Ultracode on"), at)
	}
	return nil, false
}
