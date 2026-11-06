package db

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func assertUltracodeJSON(t *testing.T, got, want json.RawMessage) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("process = %s, want %s", got, want)
	}
}

// state.put and state.load use NodeSession as their JSON boundary. Its opaque
// record must preserve false, missing values, timestamps and future node keys.
func TestNodeSessionUltracodeJSON(t *testing.T) {
	for _, process := range []string{
		`{"since":"2026-01-01T00:00:00Z"}`,
		`{"since":"2026-01-01T00:00:00Z","value":true,"at":"2026-01-01T00:00:01Z"}`,
		`{"since":"2026-01-01T00:00:00Z","value":false,"at":"2026-01-01T00:00:01Z","lost_at":"2026-01-01T00:00:01Z","future":1}`,
	} {
		var state NodeSession
		if err := json.Unmarshal([]byte(`{"started_at":"0001-01-01T00:00:00Z","ultracode_process":`+process+`}`), &state); err != nil {
			t.Fatal(err)
		}
		if !state.StartedAt.IsZero() {
			t.Fatal("external session acquired a start time")
		}
		assertUltracodeJSON(t, state.UltracodeProcess, json.RawMessage(process))
		wire, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]json.RawMessage
		if err := json.Unmarshal(wire, &out); err != nil {
			t.Fatal(err)
		}
		assertUltracodeJSON(t, out["ultracode_process"], json.RawMessage(process))
	}
	legacy, err := json.Marshal(NodeSession{})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(legacy, &out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out["ultracode_process"]; ok {
		t.Fatal("legacy state gained an ultracode process record")
	}
}

func TestNodeUltracodeProcessIsPersisted(t *testing.T) {
	d, ctx := open(t), context.Background()
	started := time.Now().UTC().Truncate(time.Microsecond)
	for _, managed := range []bool{true, false} {
		name := "external"
		state := NodeSession{}
		if managed {
			name = "managed"
			state.StartedAt, state.Pane, state.Launcher, state.Cwd = started, "w1:p1", "claude", "/tmp/example"
		}
		for _, record := range []string{
			`{"since":"2026-01-01T00:00:00Z","value":true,"at":"2026-01-01T00:00:01Z"}`,
			`{"since":"2026-01-01T00:00:02Z","value":false,"at":"2026-01-01T00:00:02Z","lost_at":"2026-01-01T00:00:02Z"}`,
			`{"since":"2026-01-01T00:00:02Z","value":false,"at":"2026-01-01T00:00:02Z"}`,
		} {
			state.UltracodeProcess = json.RawMessage(record)
			if err := d.PutNodeSession(ctx, "machine", name, state); err != nil {
				t.Fatal(err)
			}
			sessions, err := d.NodeSessions(ctx, "machine")
			if err != nil {
				t.Fatal(err)
			}
			got, ok := sessions[name]
			if !ok || !got.StartedAt.Equal(state.StartedAt) || got.Pane != state.Pane || got.Launcher != state.Launcher || got.Cwd != state.Cwd {
				t.Fatalf("%s session changed ownership: %+v", name, got)
			}
			assertUltracodeJSON(t, got.UltracodeProcess, state.UltracodeProcess)
			// Another node restart/write must keep external sessions external too.
			state = got
		}
	}
	// Records written by old nodes keep their existing fallback timestamp.
	if err := d.PutNodeSession(ctx, "machine", "legacy", NodeSession{Launcher: "claude"}); err != nil {
		t.Fatal(err)
	}
	sessions, err := d.NodeSessions(ctx, "machine")
	if err != nil || sessions["legacy"].StartedAt.IsZero() {
		t.Fatalf("legacy session = %+v, error = %v", sessions["legacy"], err)
	}
}
