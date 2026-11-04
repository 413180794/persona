package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The clipboard goes through pbcopy and pbpaste (fakes here: a file stands for the clipboard).
func TestMacClipboard(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "board")
	copyBin, pasteBin := filepath.Join(dir, "pbcopy"), filepath.Join(dir, "pbpaste")
	_ = os.WriteFile(copyBin, []byte("#!/bin/sh\ncat > "+board+"\n"), 0o755)
	_ = os.WriteFile(pasteBin, []byte("#!/bin/sh\ncat "+board+"\n"), 0o755)
	oldCopy, oldPaste, oldOS := pbcopyPath, pbpastePath, installOS
	pbcopyPath, pbpastePath, installOS = copyBin, pasteBin, "darwin"
	t.Cleanup(func() { pbcopyPath, pbpastePath, installOS = oldCopy, oldPaste, oldOS })

	if _, err := macClipboard(context.Background(), json.RawMessage(`{"set": "从手机来的 · ok"}`)); err != nil {
		t.Fatal(err)
	}
	got, err := macClipboard(context.Background(), json.RawMessage(`{}`))
	if err != nil || got.(map[string]any)["text"] != "从手机来的 · ok" {
		t.Fatalf("%v %v", got, err)
	}
	installOS = "linux"
	if _, err := macClipboard(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("only a Mac")
	}
}
