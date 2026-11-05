package assistant

import (
	"context"
	"testing"
)

// The assistant planned a task with cwd "~" and the Mac answered "directory ~ does not exist": only "~/" was made
// the machine's home, a bare "~" went through as a relative path (2026-11-04).
func TestPlaceTaskExpandsHome(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t, claudeMDHub(Device{ID: "mac", Name: "Mac", Kind: "local", Home: "/Users/example"}))
	for in, want := range map[string]string{
		"~":         "/Users/example",
		"~/src/app": "/Users/example/src/app",
		"/srv/app":  "/srv/app",
		"":          "",
	} {
		task := PlanTask{Machine: "Mac", Cwd: in}
		if !s.placeTask(ctx, &task) {
			t.Fatalf("placeTask(%q): machine not found", in)
		}
		if task.Machine != "mac" || task.Cwd != want {
			t.Errorf("placeTask(%q) = %s %q, want mac %q", in, task.Machine, task.Cwd, want)
		}
	}
	if task := (PlanTask{Machine: "nowhere", Cwd: "~"}); s.placeTask(ctx, &task) {
		t.Error("placeTask placed a task on an unknown machine")
	}
}
