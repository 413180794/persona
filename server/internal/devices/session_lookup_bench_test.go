package devices

import (
	"fmt"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// BenchmarkSessionLookupAll: one lookup per listed session (what the assistant's fleet does per change), 300 sessions.
func BenchmarkSessionLookupAll(b *testing.B) {
	m := &Machine{}
	for i := range 300 {
		m.sessions = append(m.sessions, proto.Session{SessionID: fmt.Sprintf("s%03d", i), Cwd: "/code/app", Title: "a session"})
	}
	b.ReportAllocs()
	for b.Loop() {
		for i := range m.sessions {
			if _, ok := m.session(m.sessions[i].SessionID); !ok {
				b.Fatal("missing")
			}
		}
	}
}
