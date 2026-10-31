package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// assistantSnapshot is the live context a starting assistant process gets in its system prompt
// (--append-system-prompt, node/assistant.go): the time, which machines are up, agents waiting for the user,
// unread Feishu chats and the rest of today's meetings. Each source has a short deadline; a slow one is left out.
func (h *Hub) assistantSnapshot(ctx context.Context) string {
	h.mu.Lock()
	if time.Since(h.snapAt) < 45*time.Second && h.snap != "" {
		s := h.snap
		h.mu.Unlock()
		return s
	}
	h.mu.Unlock()

	loc := h.userLocation()
	now := time.Now().In(loc)
	var (
		wg              sync.WaitGroup
		chats, calendar string
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		chats = h.snapshotChats(ctx)
	}()
	go func() {
		defer wg.Done()
		calendar = h.snapshotCalendar(ctx, now)
	}()

	var b strings.Builder
	weekdays := []string{"日", "一", "二", "三", "四", "五", "六"}
	fmt.Fprintf(&b, "# 实时快照（%s 生成；之后的变化用工具查）\n\n", now.Format("15:04"))
	fmt.Fprintf(&b, "- 现在：%s 周%s %s（%s）\n", now.Format("2006-01-02"), weekdays[now.Weekday()], now.Format("15:04"), loc.String())
	var up, down []string
	for _, m := range h.machines {
		if m.snapshot().Online {
			up = append(up, m.cfg.Name+"（"+m.cfg.ID+"）")
		} else {
			down = append(down, m.cfg.Name+"（"+m.cfg.ID+"）")
		}
	}
	fmt.Fprintf(&b, "- 在线的电脑：%s", strings.Join(up, "、"))
	if len(down) > 0 {
		fmt.Fprintf(&b, "；离线：%s", strings.Join(down, "、"))
	}
	b.WriteString("\n")
	b.WriteString(h.snapshotAgents())
	b.WriteString(h.snapshotPhone())
	if e := h.kb.eng; e != nil {
		if kbs := e.Prompt(6, 8); kbs != "" {
			b.WriteString("- 知识库（kb_get 看详情）：\n" + indent(kbs, "  "))
		}
	}

	wg.Wait()
	b.WriteString(chats)
	b.WriteString(calendar)
	b.WriteString(h.snapshotSchedule(ctx, loc))

	out := b.String()
	h.mu.Lock()
	h.snap, h.snapAt = out, time.Now()
	h.mu.Unlock()
	return out
}

func (h *Hub) snapshotAgents() string {
	var needs, unread, working []string
	for _, m := range h.machines {
		for _, s := range h.decorated(m) {
			if s.Assistant {
				continue
			}
			label := fmt.Sprintf("%s（%s，session_id=%s）", agentLabel(s), m.cfg.ID, short(s.SessionID))
			switch s.State {
			case proto.StateNeedsYou:
				needs = append(needs, label)
			case proto.StateUnread:
				if time.Since(s.UpdatedAt) < 24*time.Hour {
					unread = append(unread, label)
				}
			case proto.StateWorking:
				working = append(working, agentLabel(s))
			}
		}
	}
	list := func(xs []string, n int) string {
		if len(xs) > n {
			return strings.Join(xs[:n], "；") + fmt.Sprintf(" 等 %d 个", len(xs))
		}
		return strings.Join(xs, "；")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- Agents：需要你 %d 个，完成未看 %d 个，工作中 %d 个（详情用 agents_list）\n", len(needs), len(unread), len(working))
	if len(needs) > 0 {
		fmt.Fprintf(&b, "  - 需要你：%s\n", list(needs, 6))
	}
	if len(unread) > 0 {
		fmt.Fprintf(&b, "  - 完成未看：%s\n", list(unread, 6))
	}
	if len(working) > 0 {
		fmt.Fprintf(&b, "  - 工作中：%s\n", list(working, 8))
	}
	return b.String()
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (h *Hub) snapshotPhone() string {
	if h.phoneClient() != nil {
		return "- iPhone：App 在线，phone_* 工具可用\n"
	}
	return "- iPhone：App 不在前台，phone_* 工具会先尝试在后台唤醒 App，失败就请他打开 App\n"
}

func (h *Hub) snapshotChats(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	convs, _, err := h.lark.Conversations(ctx, "inbox", "", 100)
	if err != nil {
		return ""
	}
	var at, other []string
	total := 0
	for _, c := range convs {
		if c.Unread == 0 && !c.MarkedUnread && !c.AtMe { // an @me can outlive the unread count (read elsewhere, not answered)
			continue
		}
		if c.Muted && !c.AtMe {
			continue
		}
		total++
		label := fmt.Sprintf("%s（%d 条）", c.Name, c.Unread)
		if c.AtMe {
			at = append(at, label+"："+clip(oneLine(c.Preview), 40))
		} else {
			other = append(other, label)
		}
	}
	if total == 0 {
		return "- 飞书：没有未读（免打扰的不算）\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- 飞书未读会话 %d 个（免打扰的不算；用 lark_chats / lark_messages 看）\n", total)
	if len(at) > 0 {
		fmt.Fprintf(&b, "  - @我：%s\n", strings.Join(at[:min(len(at), 6)], "；"))
	}
	if len(other) > 0 {
		fmt.Fprintf(&b, "  - 其他：%s\n", strings.Join(other[:min(len(other), 8)], "、"))
	}
	return b.String()
}

func (h *Hub) snapshotCalendar(ctx context.Context, now time.Time) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	q := url.Values{"start": {day.UTC().Format("2006-01-02T15:04:05Z")}, "end": {day.AddDate(0, 0, 1).UTC().Format("2006-01-02T15:04:05Z")}}
	raw, err := h.lark.Read(ctx, "GET", "/calendar/week", q, nil)
	if err != nil {
		return ""
	}
	var res struct {
		Events []struct {
			Title              string    `json:"title"`
			StartAt            time.Time `json:"startAt"`
			EndAt              time.Time `json:"endAt"`
			AllDay             bool      `json:"allDay"`
			SelfAttendeeStatus int       `json:"selfAttendeeStatus"`
		} `json:"events"`
	}
	if json.Unmarshal(raw, &res) != nil {
		return ""
	}
	sort.Slice(res.Events, func(i, j int) bool { return res.Events[i].StartAt.Before(res.Events[j].StartAt) })
	var rest []string
	for _, e := range res.Events {
		if e.AllDay || e.SelfAttendeeStatus == 4 || !e.EndAt.After(now) { // declined or over
			continue
		}
		rest = append(rest, fmt.Sprintf("%s–%s %s", e.StartAt.In(now.Location()).Format("15:04"), e.EndAt.In(now.Location()).Format("15:04"), e.Title))
	}
	if len(rest) == 0 {
		return "- 飞书日历：今天接下来没有会\n"
	}
	return "- 飞书日历今天接下来：" + strings.Join(rest, "；") + "\n"
}

func (h *Hub) snapshotSchedule(ctx context.Context, loc *time.Location) string {
	s := h.loadSchedule(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	var parts []string
	for _, t := range s.tasks {
		if !t.Enabled || t.Repeat == repeatDaily {
			continue
		}
		label := t.label()
		if t.Repeat == repeatInterval {
			label += "（每 " + everyText(t.EveryMinutes) + "）"
		}
		parts = append(parts, t.At.In(loc).Format("01-02 15:04")+" "+label)
	}
	if len(parts) == 0 {
		return ""
	}
	return "- 已安排的跟进：" + strings.Join(parts, "；") + "\n"
}

func indent(s, by string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = by + l
	}
	return strings.Join(lines, "\n") + "\n"
}
