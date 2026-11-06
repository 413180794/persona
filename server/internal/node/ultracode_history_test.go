package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ultracodeHistoryLine(t *testing.T, at time.Time, on, sidechain bool) string {
	t.Helper()
	kind := "ultra_effort_exit"
	if on {
		kind = "ultra_effort_enter"
	}
	raw, err := json.Marshal(map[string]any{"type": "attachment", "timestamp": at, "isSidechain": sidechain,
		"attachment": map[string]any{"type": kind, "reminderType": "sparse"}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw) + "\n"
}

func TestReadUltracodeHistoryUsesOnlyPreviousProcess(t *testing.T) {
	since := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	boundary := since.Add(time.Hour)
	before := ultracodeHistoryLine(t, since.Add(-time.Minute), true, false)
	on := ultracodeHistoryLine(t, since.Add(time.Minute), true, false)
	off := ultracodeHistoryLine(t, since.Add(2*time.Minute), false, false)
	for _, tt := range []struct {
		name, body string
		want       *bool
	}{
		{"before previous process", before, nil},
		{"sparse before a large tail", on + strings.Repeat("{\"type\":\"noise\"}\n", 100000), ultraBool(true)},
		{"off is latest", on + off, ultraBool(false)},
		{"timestamp order", off + on, ultraBool(false)},
		{"sidechain ignored", on + ultracodeHistoryLine(t, since.Add(3*time.Minute), false, true), ultraBool(true)},
		{"new process ignored", on + ultracodeHistoryLine(t, boundary, false, false), ultraBool(true)},
		{"partial last line", on + strings.TrimSuffix(off, "\n"), ultraBool(true)},
		{"long line skipped", strings.Repeat("x", 400000) + on, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := readUltracodeHistory(strings.NewReader(tt.body), since, boundary)
			if (got == nil) != (tt.want == nil) || got != nil && *got != *tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// A resume before the cold background scan finishes still notices that the old process had ultracode on.
func TestUltracodeResumeRecoversLostFromLargeColdHistory(t *testing.T) {
	n, home := testNode(t)
	n.metas, n.bgScans = map[string]quickMeta{}, map[string]*bgScan{}
	since := time.Now().Add(-time.Hour).UTC()
	boundary := since.Add(30 * time.Minute)
	path := filepath.Join(home, sidA+".jsonl")
	data := ultracodeHistoryLine(t, since.Add(time.Minute), true, false) + strings.Repeat("{\"type\":\"noise\"}\n", int(bgSyncLimit)/17+100000)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	n.paths[sidA] = path
	n.state.put(sidA, sessionState{Ultracode: &ultracodeRecord{Since: since}})
	bgSlot <- struct{}{}
	n.beginUltracodeProcess(sidA, true, false, boundary)
	n.mu.Lock()
	u := n.ultracodeLocked(sidA)
	if u.Value == nil || *u.Value || u.LostAt != nil {
		n.mu.Unlock()
		<-bgSlot
		t.Fatal("resume did not reset the unknown switch before reading history")
	}
	n.mu.Unlock()
	<-bgSlot
	waitFor(t, "the recovered resume notice", func() bool {
		st, _ := n.state.get(sidA)
		return st.Ultracode != nil && st.Ultracode.LostAt != nil
	})
	st, _ := n.state.get(sidA)
	if st.Ultracode.Value == nil || *st.Ultracode.Value || !st.Ultracode.LostAt.Equal(boundary) {
		t.Fatalf("recovery changed the resumed switch or notice time: %+v", st.Ultracode)
	}
}

func TestDelayedUltracodeHistoryDoesNotReviveDismissedNotice(t *testing.T) {
	for _, action := range []string{"unchanged", "dismissed", "reopened", "next process", "newer screen off", "appended off"} {
		t.Run(action, func(t *testing.T) {
			n, home := testNode(t)
			since := time.Now().Add(-time.Hour).UTC()
			boundary := since.Add(30 * time.Minute)
			path := filepath.Join(home, sidA+".jsonl")
			if err := os.WriteFile(path, []byte(ultracodeHistoryLine(t, since.Add(time.Minute), true, false)), 0600); err != nil {
				t.Fatal(err)
			}
			n.paths[sidA] = path
			n.mu.Lock()
			u := n.ultracodeLocked(sidA)
			u.ultracodeRecord = ultracodeRecord{Since: boundary, At: boundary, Value: ultraBool(false)}
			n.mu.Unlock()
			previousAt := since
			if action == "newer screen off" {
				previousAt = since.Add(2 * time.Minute)
			}
			bgSlot <- struct{}{}
			done := n.recoverUltracodeLost(sidA, since, previousAt, boundary)
			n.mu.Lock()
			switch action {
			case "dismissed":
				u.LostDismissed = true
			case "reopened":
				u.observe(quickMeta{ultracode: ultraBool(true), ultracodeAt: boundary.Add(time.Minute)})
				// Closing it again must not make the earlier resume notice reappear.
				u.observe(quickMeta{ultracode: ultraBool(false), ultracodeAt: boundary.Add(2 * time.Minute)})
			case "next process":
				u.Since = boundary.Add(time.Minute)
			}
			n.mu.Unlock()
			if action == "appended off" {
				f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					<-bgSlot
					t.Fatal(err)
				}
				_, err = f.WriteString(ultracodeHistoryLine(t, boundary.Add(-time.Minute), false, false))
				_ = f.Close()
				if err != nil {
					<-bgSlot
					t.Fatal(err)
				}
			}
			<-bgSlot
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("history recovery did not finish")
			}
			n.mu.Lock()
			lost := u.LostAt != nil
			n.mu.Unlock()
			if lost != (action == "unchanged" || action == "appended off") {
				t.Fatalf("lost=%v after %s", lost, action)
			}
		})
	}
}
