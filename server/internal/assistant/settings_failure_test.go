package assistant

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// settingFailing is the fleet's store whose reads of some settings fail while their key is in fail.
type settingFailing struct {
	*fleetMem
	mu   sync.Mutex
	fail map[string]bool
}

func (m *settingFailing) Setting(ctx context.Context, key string, out any) (bool, error) {
	m.mu.Lock()
	failing := m.fail[key]
	m.mu.Unlock()
	if failing {
		return false, errors.New("database hiccup")
	}
	return m.fleetMem.Setting(ctx, key, out)
}

func (m *settingFailing) setFail(keys ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = map[string]bool{}
	for _, k := range keys {
		m.fail[k] = true
	}
}

// newSettingsHiccup is a manager on machine "mac" whose saved settings name m1 the main conversation, read once;
// fail makes the next read of the settings fail (their cache is past settingsFresh).
func newSettingsHiccup(t *testing.T) (ft *fleetTest, st *settingFailing, fail func()) {
	t.Helper()
	st = &settingFailing{fleetMem: newFleetMem()}
	if err := st.PutSetting(context.Background(), settingsKey, Settings{MachineID: "mac", Main: "m1"}); err != nil {
		t.Fatal(err)
	}
	f := newFakeHub()
	f.hello.Assistant.MachineID, f.hello.Assistant.Machines = "mac", []string{"mac"}
	d := newFleetDevs()
	d.add("mac", "Mac")
	s := New(Options{Hub: f, Store: func() Store { return st }, Logf: testLogf(t), Devices: d})
	ft = &fleetTest{t: t, s: s, d: d, f: f, st: st.fleetMem}
	if got := s.settings(context.Background()).Main; got != "m1" {
		t.Fatalf("main = %q, want m1", got)
	}
	return ft, st, func() {
		st.setFail(settingsKey)
		s.prefs.mu.Lock()
		s.prefs.at = time.Time{} // past settingsFresh: the next use reads them again
		s.prefs.mu.Unlock()
	}
}

// One settings read failing (the database busy for a moment, after the 5s the settings are kept) is no reason to
// show the main conversation under its first message: the snapshot keeps titling it "Persona" (#1338).
func TestMainTitleSurvivesSettingsReadFailure(t *testing.T) {
	ft, _, fail := newSettingsHiccup(t)
	ft.s.publish(context.Background())
	ft.sessions("mac", proto.Session{SessionID: "m1", Cwd: "/home/u/.persona/assistant", Title: "first message"})
	title := func() string {
		for _, s := range ft.lastList("mac") {
			if s.SessionID == "m1" {
				return s.Title
			}
		}
		return "(not listed)"
	}
	if got := title(); got != mainTitle {
		t.Fatalf("before: title %q, want %q", got, mainTitle)
	}
	fail()
	ft.s.publish(context.Background())
	if got := title(); got != mainTitle {
		t.Fatalf("settings unreadable: title %q, want %q", got, mainTitle)
	}
	if !ft.f.lastSnapshot().Partial {
		t.Fatal("a snapshot made without the settings is partial (the hub asks again)")
	}
}

// The main conversation can't be archived, a settings read failing or not.
func TestArchiveMainRefusedOnSettingsReadFailure(t *testing.T) {
	ft, _, fail := newSettingsHiccup(t)
	fail()
	if _, err := ft.s.setArchived(context.Background(), "mac", "m1", true, titleByUser); !errors.Is(err, errArchiveMain) {
		t.Fatalf("archive the main conversation: %v, want errArchiveMain", err)
	}
	if ft.s.isArchived("m1") {
		t.Fatal("the main conversation was archived")
	}
}

// Settings never read yet are no older settings to serve: what failed to read is still the defaults.
func TestSettingsUnreadAtStartAreDefaults(t *testing.T) {
	st := &settingFailing{fleetMem: newFleetMem()}
	_ = st.PutSetting(context.Background(), settingsKey, Settings{MachineID: "mac", Main: "m1"})
	st.setFail(settingsKey)
	s := New(Options{Hub: newFakeHub(), Store: func() Store { return st }, Logf: testLogf(t)})
	if got := s.settings(context.Background()).Main; got != "" {
		t.Fatalf("main = %q before any read went through", got)
	}
}
