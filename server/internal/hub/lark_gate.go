package hub

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

var larkFeedPath = regexp.MustCompile(`(?m)^(?:POST|PUT|PATCH|DELETE) /conversations/(\d+)/`)

// larkChatNames adds the chat's name under each write that goes to a chat ("会话：「项目群」"), so the phone card reads like
// lark_send's did; a name it cannot find is left out (the card still has the path).
func (h *Hub) larkChatNames(ctx context.Context, summary string) string {
	if h.lark == nil {
		return summary
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	names := map[string]string{}
	for _, m := range larkFeedPath.FindAllStringSubmatch(summary, -1) {
		if _, ok := names[m[1]]; ok {
			continue
		}
		names[m[1]] = ""
		raw, err := h.lark.Read(ctx, "GET", "/conversations/"+m[1], nil, nil)
		if err != nil {
			continue
		}
		var c struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &c) == nil {
			names[m[1]] = strings.TrimSpace(c.Name)
		}
	}
	return larkFeedPath.ReplaceAllStringFunc(summary, func(line string) string {
		id := larkFeedPath.FindStringSubmatch(line)[1]
		if n := names[id]; n != "" {
			return "会话：「" + n + "」\n" + line
		}
		return line
	})
}
