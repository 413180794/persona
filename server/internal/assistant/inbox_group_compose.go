package assistant

import (
	"strings"

	"github.com/nwverse/persona/server/internal/connector"
)

// Events of a connector that groups but keeps no meta (capability group without meta): the inbox writes them itself,
// from the batch, with no call to the connector and no meta.

// composeEvent is one event of the plan as the hub writes it. A new one gets an id of its own, stable for it (the
// connector, the conversation, its first message), and the text of its messages; an existing one keeps its id and
// title and takes its new messages' text after what it had. Thread, kind, label, sender, preview, last, rules and
// extra come from the batch; title, summary and tier are set by putEvent (the model's, or the rules').
func (s *Service) composeEvent(connectorID string, t connector.InboxThing, a connector.GroupAppend) connector.InboxThing {
	thing := t
	thing.Group, thing.Meta = nil, nil
	if a.Event == "" {
		thing.ID = connectorID + ":" + t.Thread + "#" + a.Messages[0]
		thing.Context = eventText(t.Context, t.Group.New, a.Messages, true)
	} else {
		b := &s.inbox
		b.mu.Lock()
		if cur := b.items[a.Event]; cur != nil {
			thing.ID, thing.Title = cur.ID, cur.Title
			thing.Context = strings.TrimSpace(cur.Context + "\n" + eventText(t.Context, t.Group.New, a.Messages, false))
		}
		b.mu.Unlock()
	}
	return thing
}

// eventText is the lines of a batch's text about some of its messages (mine). A line naming one of the batch's
// messages (all, lineOwner) starts that message's part and the lines after it go with it; lines before the first such line (a heading) go with every new event (head), not with what is added to
// one. When no line names one: the whole text.
func eventText(text string, all, mine []string, head bool) string {
	isMine := map[string]bool{}
	for _, m := range mine {
		isMine[m] = true
	}
	var heading, out []string
	owner, named := "", false
	for _, l := range strings.Split(text, "\n") {
		if id := lineOwner(l, all); id != "" {
			owner, named = id, true
		}
		switch {
		case owner == "":
			heading = append(heading, l)
		case isMine[owner]:
			out = append(out, l)
		}
	}
	if !named {
		return strings.TrimSpace(text)
	}
	if head {
		out = append(heading, out...)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// lineOwner is the message a line of a batch's text is about: of the batch's ids it names (each as a word of its own:
// m10 is not m1), the one written as id=<id>, else the last one (a line ends with its message's id; an id earlier in it
// is one it answers or quotes). "" for none.
func lineOwner(line string, ids []string) string {
	owner, at := "", -1
	for _, id := range ids {
		for from := 0; id != "" && from < len(line); {
			i := strings.Index(line[from:], id)
			if i < 0 {
				break
			}
			i += from
			end := i + len(id)
			if (i == 0 || !idByte(line[i-1])) && (end == len(line) || !idByte(line[end])) {
				if strings.HasSuffix(line[:i], "id=") {
					return id
				}
				if i > at {
					owner, at = id, i
				}
			}
			from = i + 1
		}
	}
	return owner
}

func idByte(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '_' || c == '-'
}
