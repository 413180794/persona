package hub

import (
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestWithInjected(t *testing.T) {
	h := &Hub{}
	h.takeTodoReport(&assistantwire.TodoReport{Links: []assistantwire.TodoLink{
		{MachineID: "m1", SessionID: "a", TodoID: "td_1", Title: "跑测试", Task: true},
		{MachineID: "m1", SessionID: "b", TodoID: "td_1", Title: "跑测试", Task: true}, // one task, two agents
		{MachineID: "m1", SessionID: "c", TodoID: "td_2", Title: "重名", Task: true},
		{MachineID: "m1", SessionID: "d", TodoID: "td_3", Title: "重名", Task: true},
	}})
	notice := func(title string) string {
		return prompts.RenderTaskNotice(prompts.TaskNotice{Title: title, Machine: "电脑", State: "done", Text: "好了"})
	}
	items := []proto.Item{
		{ID: "1", Role: "user", Kind: proto.KindText, Text: notice("跑测试")},
		{ID: "2", Role: "user", Kind: proto.KindText, Text: notice("重名")},
		{ID: "3", Role: "user", Kind: proto.KindText, Text: notice("跑测试"), ClientMsgID: "c1"}, // the app sent it: the user's
		{ID: "4", Role: "assistant", Kind: proto.KindText, Text: notice("跑测试")},
		{ID: "5", Role: "user", Kind: proto.KindText, Text: "帮我看看"},
	}
	want := items[0].Text
	got := h.withInjected(items)
	if in := got[0].Injected; in == nil || in.Kind != proto.InjectedTaskNotice || in.TodoID != "td_1" || in.Line != "任务「跑测试」做完了" {
		t.Fatalf("notice: %+v", in)
	}
	if got[0].Text != want {
		t.Fatal("the text the model read changed")
	}
	if in := got[1].Injected; in == nil || in.TodoID != "" {
		t.Fatalf("two tasks with the title: %+v", in)
	}
	for _, i := range []int{2, 3, 4} {
		if got[i].Injected != nil {
			t.Fatalf("item %s marked: %+v", got[i].ID, got[i].Injected)
		}
	}
}
