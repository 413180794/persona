// Package transcript turns Claude Code session JSONL files into normalized proto.Items.
package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// Limits keep huge tool outputs and images from flooding the phone.
const (
	maxTextLen  = 24 * 1024
	maxImageLen = 1536 * 1024
)

type entry struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	UUID             string          `json:"uuid"`
	Timestamp        time.Time       `json:"timestamp"`
	IsSidechain      bool            `json:"isSidechain"`
	IsMeta           bool            `json:"isMeta"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	Content          json.RawMessage `json:"content"`
	Message          *struct {
		Role    string          `json:"role"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	AITitle    string `json:"aiTitle"`
	Operation  string `json:"operation"`
	Attachment *struct {
		Type        string          `json:"type"`
		Prompt      json.RawMessage `json:"prompt"`
		CommandMode string          `json:"commandMode"`
		Origin      *struct {
			Kind string `json:"kind"`
		} `json:"origin"`
	} `json:"attachment"`
	Cwd           string          `json:"cwd"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Source    *struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source"`
}

// Meta is session-level information collected while parsing.
type Meta struct {
	// Queue holds input typed while Claude works and not yet taken (queue-operation lines:
	// enqueue / dequeue / remove / popAll — popAll is "up arrow pulls them back into the input").
	Queue []string
	// LastActivity is the time of the last real exchange (user input, assistant output, tool calls). Claude Code
	// also appends recaps, titles and mode lines to idle sessions, so the file mtime is not "last used".
	LastActivity time.Time
	FirstUser    string // first thing the user typed; titles untitled (headless) sessions
	Title        string
	Cwd          string
	Model        string
	LastText     string
	Updated      time.Time
}

// pastedRe matches the wrapper Claude Code puts around long pasted input (also what herdr's bracketed paste from
// the phone, e.g. a WeChat voice-input transcript, turns into): <pasted_content id="ae9b">…</pasted_content id="ae9b">.
var pastedRe = regexp.MustCompile(`<[\\/]*pasted_content(?:\s+id="[^"]*")?\s*>`) // also <\pasted…> / <\/pasted…> from nested pastes

var tagRe = regexp.MustCompile(`(?s)<(command-name|command-message|command-args|local-command-stdout|local-command-stderr|task-notification|system-reminder)>(.*?)</(command-name|command-message|command-args|local-command-stdout|local-command-stderr|task-notification|system-reminder)>`)

// ParseLine converts one JSONL line (1-based seq) into items, updating meta.
func ParseLine(seq int, line []byte, meta *Meta) []proto.Item {
	items := parseLine(seq, line, meta)
	for _, it := range items {
		if it.Role != "system" && !it.TS.IsZero() && it.TS.After(meta.LastActivity) {
			meta.LastActivity = it.TS
		}
	}
	return items
}

func parseLine(seq int, line []byte, meta *Meta) []proto.Item {
	var e entry
	if err := json.Unmarshal(line, &e); err != nil {
		return nil
	}
	if e.Cwd != "" {
		meta.Cwd = e.Cwd
	}
	if !e.Timestamp.IsZero() {
		meta.Updated = e.Timestamp
	}
	if e.Type == "queue-operation" {
		meta.applyQueue(e.Operation, rawString(e.Content))
		return nil
	}
	if e.Type == "ai-title" && e.AITitle != "" {
		meta.Title = e.AITitle
		return nil
	}
	if e.IsSidechain {
		return nil
	}
	mk := func(idx int, role, kind string) proto.Item {
		id := e.UUID
		if id == "" {
			id = fmt.Sprintf("line%d", seq)
		}
		return proto.Item{ID: fmt.Sprintf("%s#%d", id, idx), Seq: seq, TS: e.Timestamp, Role: role, Kind: kind}
	}
	switch e.Type {
	case "attachment":
		// Input typed while Claude was working is queued, then absorbed into the running turn; it is stored as a
		// queued_command attachment instead of a user message, so without this it never shows up in the chat.
		a := e.Attachment
		if a == nil || a.Type != "queued_command" || (a.CommandMode != "" && a.CommandMode != "prompt") ||
			(a.Origin != nil && a.Origin.Kind != "human") {
			return nil
		}
		var text string
		if json.Unmarshal(a.Prompt, &text) != nil {
			var blocks []block
			if json.Unmarshal(a.Prompt, &blocks) == nil {
				var parts []string
				for _, b := range blocks {
					if b.Type == "text" {
						parts = append(parts, b.Text)
					}
				}
				text = strings.Join(parts, "\n")
			}
		}
		items := userText(mk, text)
		if meta.FirstUser == "" && len(items) > 0 && items[0].Kind == proto.KindText {
			meta.FirstUser = items[0].Text
		}
		return items
	case "system":
		var text string
		switch e.Subtype {
		case "compact_boundary":
			text = "上下文已压缩"
		case "api_error":
			text = "API 错误：" + rawString(e.Content)
		case "away_summary":
			recap := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rawString(e.Content)), "(disable recaps in /config)"))
			if recap == "" {
				return nil
			}
			it := mk(0, "system", proto.KindRecap)
			it.Text = clip(recap)
			return []proto.Item{it}
		case "informational":
			text = rawString(e.Content)
		default:
			return nil
		}
		if strings.TrimSpace(text) == "" {
			return nil
		}
		it := mk(0, "system", proto.KindNotice)
		it.Text = clip(text)
		return []proto.Item{it}
	case "user":
		if e.Message == nil {
			return nil
		}
		if e.IsCompactSummary {
			it := mk(0, "system", proto.KindNotice)
			it.Text = "已从压缩的上下文继续"
			return []proto.Item{it}
		}
		var s string
		if json.Unmarshal(e.Message.Content, &s) == nil {
			if e.IsMeta {
				return nil
			}
			items := userText(mk, s)
			if meta.FirstUser == "" && len(items) > 0 && items[0].Kind == proto.KindText {
				meta.FirstUser = items[0].Text
			}
			return items
		}
		var blocks []block
		if json.Unmarshal(e.Message.Content, &blocks) != nil {
			return nil
		}
		var out []proto.Item
		for i, b := range blocks {
			switch b.Type {
			case "text":
				if e.IsMeta {
					continue
				}
				items := userText(func(_ int, role, kind string) proto.Item { return mk(i, role, kind) }, b.Text)
				if meta.FirstUser == "" && len(items) > 0 && items[0].Kind == proto.KindText {
					meta.FirstUser = items[0].Text
				}
				out = append(out, items...)
			case "image":
				if e.IsMeta {
					continue
				}
				if img := imageOf(b); img != nil {
					it := mk(i, "user", proto.KindImage)
					it.Image = img
					out = append(out, it)
				}
			case "tool_result":
				text, imgs := toolResultContent(b.Content)
				it := mk(i, "user", proto.KindToolResult)
				it.ToolUseID = b.ToolUseID
				it.IsError = b.IsError
				it.Text = clip(text)
				out = append(out, it)
				for j, img := range imgs {
					im := mk(i*100+j+1, "user", proto.KindImage)
					im.ToolUseID = b.ToolUseID
					im.Image = img
					out = append(out, im)
				}
			}
		}
		return out
	case "assistant":
		if e.Message == nil {
			return nil
		}
		if e.Message.Model != "" && e.Message.Model != "<synthetic>" {
			meta.Model = e.Message.Model
		}
		var blocks []block
		if json.Unmarshal(e.Message.Content, &blocks) != nil {
			return nil
		}
		var out []proto.Item
		for i, b := range blocks {
			switch b.Type {
			case "text":
				if strings.TrimSpace(b.Text) == "" {
					continue
				}
				it := mk(i, "assistant", proto.KindText)
				it.Text = clip(b.Text)
				meta.LastText = b.Text
				out = append(out, it)
			case "thinking":
				if strings.TrimSpace(b.Thinking) == "" {
					continue
				}
				it := mk(i, "assistant", proto.KindThinking)
				it.Text = clip(b.Thinking)
				out = append(out, it)
			case "tool_use", "server_tool_use":
				it := mk(i, "assistant", proto.KindToolUse)
				it.ToolUseID = b.ID
				it.ToolName = b.Name
				it.ToolInput = clipJSON(b.Input)
				out = append(out, it)
			}
		}
		return out
	}
	return nil
}

func (m *Meta) applyQueue(op, content string) {
	removeOne := func() bool {
		for i, q := range m.Queue {
			if q == content {
				m.Queue = append(m.Queue[:i:i], m.Queue[i+1:]...)
				return true
			}
		}
		return false
	}
	switch op {
	case "enqueue":
		if content != "" {
			m.Queue = append(m.Queue, content)
		}
	case "dequeue":
		if len(m.Queue) > 0 {
			m.Queue = m.Queue[1:]
		}
	case "remove", "popAll":
		if content == "" || !removeOne() {
			if op == "popAll" {
				m.Queue = nil
			} else if len(m.Queue) > 0 {
				m.Queue = m.Queue[1:]
			}
		}
	}
}

func userText(mk func(int, string, string) proto.Item, s string) []proto.Item {
	trimmed := strings.TrimSpace(pastedRe.ReplaceAllString(s, ""))
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "<command-name>") || strings.HasPrefix(trimmed, "<command-message>") {
		name := firstTag(trimmed, "command-name")
		args := firstTag(trimmed, "command-args")
		it := mk(0, "user", proto.KindText)
		it.Text = strings.TrimSpace(name + " " + args)
		return []proto.Item{it}
	}
	if strings.HasPrefix(trimmed, "<local-command-stdout>") || strings.HasPrefix(trimmed, "<local-command-stderr>") {
		it := mk(0, "system", proto.KindNotice)
		it.Text = clip(strings.TrimSpace(tagRe.ReplaceAllString(trimmed, "$2")))
		if it.Text == "" {
			return nil
		}
		return []proto.Item{it}
	}
	if strings.HasPrefix(trimmed, "<task-notification>") {
		it := mk(0, "system", proto.KindNotice)
		summary := firstTag(trimmed, "summary")
		if summary == "" {
			summary = "后台任务有更新"
		}
		it.Text = clip(summary)
		return []proto.Item{it}
	}
	if trimmed == "[Request interrupted by user]" || trimmed == "[Request interrupted by user for tool use]" {
		it := mk(0, "system", proto.KindNotice)
		it.Text = "已中断"
		return []proto.Item{it}
	}
	text := strings.TrimSpace(tagRe.ReplaceAllStringFunc(trimmed, func(m string) string {
		if strings.HasPrefix(m, "<system-reminder>") {
			return ""
		}
		return m
	}))
	if text == "" {
		return nil
	}
	it := mk(0, "user", proto.KindText)
	it.Text = clip(text)
	return []proto.Item{it}
}

func firstTag(s, tag string) string {
	open, close := "<"+tag+">", "</"+tag+">"
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	rest := s[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

func toolResultContent(raw json.RawMessage) (string, []*proto.Image) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return "", nil
	}
	var parts []string
	var imgs []*proto.Image
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "image":
			if img := imageOf(b); img != nil {
				imgs = append(imgs, img)
			}
		case "tool_reference":
			parts = append(parts, "已加载工具 "+b.Name)
		}
	}
	return strings.Join(parts, "\n"), imgs
}

func imageOf(b block) *proto.Image {
	if b.Source == nil || b.Source.Type != "base64" || len(b.Source.Data) > maxImageLen {
		return nil
	}
	return &proto.Image{Mime: b.Source.MediaType, Data: b.Source.Data}
}

func rawString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func clip(s string) string { return clipTo(s, maxTextLen) }

// clipTo cuts s to at most n bytes on a UTF-8 boundary and notes the original size.
func clipTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + fmt.Sprintf("\n…（已截断，共 %d 字节）", len(s))
}

// clipJSON shortens very long string fields (e.g. Write content) inside tool inputs.
func clipJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) <= maxTextLen {
		return raw
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	for k, v := range m {
		if s, ok := v.(string); ok && len(s) > maxTextLen/2 {
			m[k] = clipTo(s, maxTextLen/2)
		}
	}
	b, _ := json.Marshal(m)
	return b
}

// File incrementally parses a transcript file. Safe for concurrent use.
type File struct {
	Path string

	mu      sync.Mutex
	offset  int64
	lines   int
	partial []byte
	items   []proto.Item
	meta    Meta
}

func NewFile(path string) *File { return &File{Path: path} }

// Refresh reads newly appended complete lines and returns the new items.
func (f *File) Refresh() ([]proto.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fh, err := os.Open(f.Path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() < f.offset { // truncated or replaced: start over
		f.offset, f.lines, f.partial, f.items, f.meta = 0, 0, nil, nil, Meta{}
	}
	if st.Size() == f.offset {
		return nil, nil
	}
	if _, err := fh.Seek(f.offset, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(fh)
	if err != nil {
		return nil, err
	}
	f.offset += int64(len(data))
	data = append(f.partial, data...)
	var fresh []proto.Item
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		line := data[:i]
		data = data[i+1:]
		f.lines++
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		fresh = append(fresh, ParseLine(f.lines, line, &f.meta)...)
	}
	f.partial = append([]byte(nil), data...)
	f.items = append(f.items, fresh...)
	return fresh, nil
}

// Page returns items with seq < beforeSeq (0 = from the end), newest `limit`, in ascending order.
func (f *File) Page(beforeSeq, limit int) ([]proto.Item, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	end := len(f.items)
	if beforeSeq > 0 {
		end = 0
		for end < len(f.items) && f.items[end].Seq < beforeSeq {
			end++
		}
	}
	start := max(end-limit, 0)
	// Never split a transcript line across pages.
	for start > 0 && f.items[start].Seq == f.items[start-1].Seq {
		start--
	}
	out := append([]proto.Item(nil), f.items[start:end]...)
	return out, start > 0
}

// After returns items with seq > afterSeq (at most limit, oldest first).
func (f *File) After(afterSeq, limit int) []proto.Item {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []proto.Item
	for _, it := range f.items {
		if it.Seq > afterSeq {
			out = append(out, it)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func (f *File) Meta() Meta {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.meta
}

func (f *File) LastSeq() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lines
}
