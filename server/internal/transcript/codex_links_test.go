package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexCreatedThreadBecomesPersonaLink(t *testing.T) {
	LocalMachine.Store("")
	in := "建好了\n::created-thread{threadId=\"01a19b67-dce8-7a73-a357-8edeeda396bc\"}"
	if got := codexThreadLinks(in); got != in {
		t.Fatalf("without a machine id the directive stays: %q", got)
	}
	LocalMachine.Store("m1")
	t.Cleanup(func() { LocalMachine.Store("") })
	want := "建好了\n[打开新建的对话](persona://session/m1/01a19b67-dce8-7a73-a357-8edeeda396bc)"
	if got := codexThreadLinks(in); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestRelocatedFileKeepsReadingWhereItWas(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.jsonl"), filepath.Join(dir, "b.jsonl")
	user := func(text string) string {
		return `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"` + text + `","content":[{"type":"text","text":"` + text + `"}]}}}` + "\n"
	}
	os.WriteFile(a, []byte(`{"type":"session_meta","payload":{}}`+"\n"+user("one")), 0o600)
	f := NewFile(a)
	if items, _ := f.Refresh(); len(items) != 1 {
		t.Fatalf("first read: %d items", len(items))
	}
	os.Rename(a, b) // archived or restored: the same file elsewhere
	fh, _ := os.OpenFile(b, os.O_APPEND|os.O_WRONLY, 0)
	fh.WriteString(user("two"))
	fh.Close()
	if _, err := f.Refresh(); err == nil {
		t.Fatal("the old path is gone")
	}
	f.Relocate(b)
	items, err := f.Refresh()
	if err != nil || len(items) != 1 || items[0].Text != "two" {
		t.Fatalf("after relocate: %v %+v", err, items)
	}
}
