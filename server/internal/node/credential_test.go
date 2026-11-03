package node

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// fakeGh puts a gh on the PATH that runs script.
func fakeGh(t *testing.T, script string) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestGhToken(t *testing.T) {
	ctx := context.Background()
	code := func(err error) string {
		var re *rpc.Error
		if !errors.As(err, &re) {
			t.Fatalf("not an rpc.Error: %v", err)
		}
		return re.Code
	}

	fakeGh(t, `[ "$1 $2 $3 $4" = "auth token --hostname github.com" ] && [ "$GH_PROMPT_DISABLED" = 1 ] && echo gho_example0000000000000000000000000000`)
	out, err := ghToken(ctx, "")
	if err != nil || out["token"] != "gho_example0000000000000000000000000000" {
		t.Fatalf("%v %v", out, err)
	}

	fakeGh(t, `echo "no oauth token found for example.com" >&2; exit 1`)
	if _, err := ghToken(ctx, "example.com"); code(err) != proto.CodeNotSignedIn {
		t.Fatalf("not signed in: %v", err)
	}
	fakeGh(t, `exit 0`)
	if _, err := ghToken(ctx, "github.com"); code(err) != proto.CodeNotSignedIn {
		t.Fatalf("empty token: %v", err)
	}

	t.Setenv("PATH", t.TempDir())
	if _, err := ghToken(ctx, "github.com"); code(err) != proto.CodeNoGh {
		t.Fatalf("no gh: %v", err)
	}
	if _, err := ghToken(ctx, "github.com; rm -rf /"); err == nil {
		t.Fatal("a bad host was run")
	}
}
