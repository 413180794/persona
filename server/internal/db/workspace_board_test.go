package db

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/nwverse/persona/server/internal/workspaceboard"
	"os"
	"reflect"
	"sync"
	"testing"
)

func openWorkspaceBoard(t *testing.T) *DB {
	t.Helper()
	d := open(t)
	if _, err := d.pool.Exec(context.Background(), `TRUNCATE workspace_board, workspace_snapshots`); err != nil {
		t.Fatal(err)
	}
	return d
}
func TestWorkspaceBoardPersistenceConflictAndRestart(t *testing.T) {
	d := openWorkspaceBoard(t)
	ctx := context.Background()
	initial, err := d.WorkspaceBoard(ctx)
	if err != nil || !reflect.DeepEqual(initial, workspaceboard.Default()) {
		t.Fatalf("default: %+v %v", initial, err)
	}
	title := "Project"
	next, err := workspaceboard.Apply(initial, workspaceboard.Mutation{Action: "add_card", ID: "card-a", Title: &title, ColumnID: "todo", MachineID: "machine-a", WorkspaceID: "workspace-a", Cwd: "/projects/a", Source: "directory"})
	if err != nil {
		t.Fatal(err)
	}
	next.Revision = 999
	saved, err := d.SaveWorkspaceBoard(ctx, 0, next)
	if err != nil || saved.Revision != 1 || len(saved.Cards) != 1 {
		t.Fatalf("save: %+v %v", saved, err)
	}
	next.Cards[0].Title = "caller mutation"
	if saved.Cards[0].Title != "Project" {
		t.Fatal("save aliases input")
	}
	if _, err := d.SaveWorkspaceBoard(ctx, 0, initial); !errors.Is(err, ErrWorkspaceBoardConflict) {
		t.Fatalf("stale: %v", err)
	}
	invalid := saved
	invalid.Columns = nil
	if _, err := d.SaveWorkspaceBoard(ctx, 1, invalid); !errors.Is(err, workspaceboard.ErrInvalid) {
		t.Fatalf("invalid: %v", err)
	}
	reopened, err := Open(ctx, os.Getenv("JARVIS_TEST_DATABASE_URL"), AllSets)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reloaded, err := reopened.WorkspaceBoard(ctx)
	if err != nil || !reflect.DeepEqual(saved, reloaded) {
		t.Fatalf("restart: %+v %v", reloaded, err)
	}
	if err := reopened.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	again, err := reopened.WorkspaceBoard(ctx)
	if err != nil || !reflect.DeepEqual(saved, again) {
		t.Fatalf("reseeded: %+v %v", again, err)
	}
}
func TestWorkspaceBoardConcurrentSeedAndCAS(t *testing.T) {
	d := openWorkspaceBoard(t)
	ctx := context.Background()
	const writers = 16
	var wg sync.WaitGroup
	failures := make(chan error, writers)
	for range writers {
		wg.Go(func() {
			s, err := d.WorkspaceBoard(ctx)
			if err != nil {
				failures <- err
			} else if !reflect.DeepEqual(s, workspaceboard.Default()) {
				failures <- errors.New("inconsistent initial state")
			}
		})
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	results := make(chan error, writers)
	start := make(chan struct{})
	for range writers {
		wg.Go(func() { <-start; _, err := d.SaveWorkspaceBoard(ctx, 0, workspaceboard.Default()); results <- err })
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrWorkspaceBoardConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != writers-1 {
		t.Fatalf("lost update: %d success %d conflict", success, conflicts)
	}
	s, err := d.WorkspaceBoard(ctx)
	if err != nil || s.Revision != 1 {
		t.Fatalf("revision: %d %v", s.Revision, err)
	}
}
func TestWorkspaceSnapshotsMachineScopedAndIndependent(t *testing.T) {
	d := openWorkspaceBoard(t)
	ctx := context.Background()
	for machine, data := range map[string]json.RawMessage{"machine-b": json.RawMessage(`{"workspaces":[]}`), "machine-a": json.RawMessage(`{"workspaces":[{"id":"old"}]}`)} {
		if err := d.SaveWorkspaceSnapshot(ctx, machine, data); err != nil {
			t.Fatal(err)
		}
	}
	replacement := json.RawMessage(`{"workspaces":[{"id":"new"}]}`)
	if err := d.SaveWorkspaceSnapshot(ctx, "machine-a", replacement); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []json.RawMessage{nil, json.RawMessage(`{broken`)} {
		if err := d.SaveWorkspaceSnapshot(ctx, "machine-a", bad); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
	if err := d.SaveWorkspaceSnapshot(ctx, "", replacement); err == nil {
		t.Fatal("empty machine accepted")
	}
	rows, err := d.ListWorkspaceSnapshots(ctx)
	if err != nil || len(rows) != 2 || rows[0].MachineID != "machine-a" || rows[1].MachineID != "machine-b" || rows[0].UpdatedAt.IsZero() {
		t.Fatalf("snapshots: %+v %v", rows, err)
	}
	var got, want any
	if err := json.Unmarshal(rows[0].Data, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(replacement, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("replacement: %s", rows[0].Data)
	}
	state, err := d.WorkspaceBoard(ctx)
	if err != nil || state.Revision != 0 || len(state.Cards) != 0 {
		t.Fatalf("discovery mutated board: %+v %v", state, err)
	}
}

func TestDecodeWorkspaceBoardUsesSQLRevisionAndRejectsCorruption(t *testing.T) {
	raw, err := json.Marshal(workspaceboard.Default())
	if err != nil {
		t.Fatal(err)
	}
	s, err := decodeWorkspaceBoard(8, raw)
	if err != nil || s.Revision != 8 || s.Cards == nil {
		t.Fatalf("decode: %+v %v", s, err)
	}
	for _, raw := range [][]byte{[]byte(`{broken`), []byte(`{"columns":[],"cards":[]}`), []byte(`null`)} {
		if _, err := decodeWorkspaceBoard(1, raw); err == nil {
			t.Fatalf("accepted corrupt persisted state: %s", raw)
		}
	}
}

func TestWorkspaceBoardInUseIgnoresTheSeededBoard(t *testing.T) {
	d := openWorkspaceBoard(t)
	ctx := context.Background()
	if used, err := d.WorkspaceBoardInUse(ctx); err != nil || used {
		t.Fatalf("no board: %v %v", used, err)
	}
	initial, err := d.WorkspaceBoard(ctx) // opening the page seeds revision 0
	if err != nil {
		t.Fatal(err)
	}
	if used, err := d.WorkspaceBoardInUse(ctx); err != nil || used {
		t.Fatalf("seeded board: %v %v", used, err)
	}
	if _, err := d.SaveWorkspaceBoard(ctx, initial.Revision, initial); err != nil {
		t.Fatal(err)
	}
	if used, err := d.WorkspaceBoardInUse(ctx); err != nil || !used {
		t.Fatalf("changed board: %v %v", used, err)
	}
}
