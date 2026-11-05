package hub

import (
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// withInjected marks, on items going to the apps, the user lines Persona wrote for the model (a task's notice, a
// scheduled run, a wake-up…): the apps show one line instead of a bubble with the model's instructions in it
// (docs/protocol.md「Persona 注入的消息」). Read off the text, so old lines and older nodes' get it too; a line an app
// sent (client_msg_id) is the user's whatever it says. A task notice is tied to its task when the assistant's report
// names one open task by that title.
func (h *Hub) withInjected(items []proto.Item) []proto.Item {
	for i := range items {
		it := &items[i]
		if it.Role != "user" || it.Kind != proto.KindText || it.ClientMsgID != "" || it.From != "" || it.Injected != nil {
			continue
		}
		in := prompts.ParseInjected(it.Text)
		if in == nil {
			continue
		}
		if in.Kind == proto.InjectedTaskNotice {
			in.TodoID = h.taskByTitle(in.Title)
		}
		it.Injected = in
	}
	return items
}

// taskByTitle is the open task (a todo an assistant conversation opened) titled title; "" when none or several.
func (h *Hub) taskByTitle(title string) string {
	tl := &h.todoLinks
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	id := ""
	for _, l := range tl.links {
		if !l.Task || l.Title != title {
			continue
		}
		if id != "" && id != l.TodoID {
			return ""
		}
		id = l.TodoID
	}
	return id
}
