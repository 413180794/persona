package db

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// A bucket is written once and replaced after; the old ones go.
func TestExtensionMetricsRows(t *testing.T) {
	d, ctx := open(t), context.Background()
	at := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	for i, data := range []string{`{"starts": 1}`, `{"starts": 2}`} {
		if err := d.PutExtensionMetrics(ctx, "demo", at, json.RawMessage(data)); err != nil {
			t.Fatal(i, err)
		}
	}
	if err := d.PutExtensionMetrics(ctx, "demo", at.Add(-8*24*time.Hour), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := d.PutExtensionMetrics(ctx, "other", at, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	rows, err := d.ExtensionMetrics(ctx, "demo", at.Add(-30*24*time.Hour))
	if err != nil || len(rows) != 2 || !rows[1].Start.Equal(at) {
		t.Fatal(rows, err)
	}
	var got map[string]int
	if err := json.Unmarshal(rows[1].Data, &got); err != nil || got["starts"] != 2 {
		t.Fatalf("replaced: %s %v", rows[1].Data, err)
	}
	if err := d.DeleteExtensionMetricsBefore(ctx, at.Add(-7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if rows, err := d.ExtensionMetrics(ctx, "demo", at.Add(-30*24*time.Hour)); err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
}
