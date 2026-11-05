package assistant

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// listCardMaxBytes is the size the bot's own answer cards stay under (feishubot); the list cards stay under it too.
const listCardMaxBytes = 24000

func longListName(i int) string {
	return fmt.Sprintf("长名%02d-", i) + strings.Repeat("*_[`", 70) // 285 runes, all Markdown metacharacters
}

// /list and /agents clip long conversation / agent / device names so Feishu cannot refuse the card, and post the
// same list as the card's text fallback (with Markdown metacharacters stripped), so a refused card still delivers
// it (#4362).
func TestBotCommandListCardsClipAndFallback(t *testing.T) {
	f, posts := feishuHub("ou_a", time.Unix(100, 0))
	s := newTestService(t, f)
	setSettings(t, s, func(v *Settings) { v.MachineID = "server"; v.Main = "main0" })
	now := time.Now().UTC()
	var cs []proto.Session
	for i := 0; i < 8; i++ {
		cs = append(cs, proto.Session{SessionID: fmt.Sprintf("c%d", i), MachineID: "server", Assistant: true,
			Title: longListName(i), UpdatedAt: now.Add(-time.Duration(i) * time.Minute)})
	}
	f.setSessions("server", cs...)
	ctx := context.Background()
	if h, _ := s.botCommand(ctx, FeishuFeatures{Main: true}, "om", "/对话", "", ""); !h {
		t.Fatal("/list not handled")
	}
	post := (*posts)[len(*posts)-1]
	card, _ := post["card"].(string)
	fallback, _ := post["fallback"].(string)
	if len(card) >= listCardMaxBytes {
		t.Fatalf("/list card %d bytes, want under %d", len(card), listCardMaxBytes)
	}
	// Button values carry every name verbatim and JSON keeps "[", so its count proves the names were clipped: a
	// clipped name keeps <=14 groups (shown twice at most), an unclipped one 70.
	if n := strings.Count(card, "["); n > 8*25 {
		t.Fatalf("/list card carries %d unclipped name groups", n)
	}
	lines := strings.Split(fallback, "\n") // a title line plus eight "- …" rows, Markdown metacharacters stripped
	if len(lines) != 9 || !strings.HasPrefix(lines[1], "- ") || strings.ContainsAny(fallback, "*_[`") {
		t.Fatalf("/list fallback lines=%d: %q", len(lines), fallback)
	}

	f.devices = []Device{{ID: "box", Name: strings.Repeat("机", 280), Online: true, Reachable: true}}
	states := []string{proto.StateNeedsYou, proto.StateWorking, proto.StateUnread, proto.StateIdle}
	var as []proto.Session
	for i := 0; i < 12; i++ {
		as = append(as, proto.Session{SessionID: fmt.Sprintf("a%02d", i), MachineID: "box", Agent: "claude",
			Title: longListName(i), State: states[i%4], UpdatedAt: now})
	}
	f.setSessions("box", as...)
	if h, _ := s.botCommand(ctx, FeishuFeatures{Agents: true}, "om", "/agents", "", ""); !h {
		t.Fatal("/agents not handled")
	}
	post = (*posts)[len(*posts)-1]
	card, _ = post["card"].(string)
	fallback, _ = post["fallback"].(string)
	if len(card) >= listCardMaxBytes {
		t.Fatalf("/agents card %d bytes, want under %d", len(card), listCardMaxBytes)
	}
	if n := strings.Count(card, "["); n > 10*25 {
		t.Fatalf("/agents card carries %d unclipped name groups", n)
	}
	if n := strings.Count(fallback, "\n- "); n != 10 || !strings.Contains(fallback, "🔴") || strings.ContainsAny(fallback, "*_[`") {
		t.Fatalf("/agents fallback rows=%d, icon or escaping wrong: %q", n, fallback)
	}
}
