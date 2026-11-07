package proto

import (
	"encoding/json"
)

// Both keys carry the same value on all hub/node/manager boundaries. Old peers
// ignore the canonical key. Presence, not boolean truth, gives it precedence.
type sessionJSON Session

func (s Session) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		sessionJSON
		LegacyStarted bool `json:"started_by_jarvis"`
	}{sessionJSON(s), s.StartedByPersona})
}
func (s *Session) UnmarshalJSON(data []byte) error {
	v := struct {
		sessionJSON
		LegacyStarted  json.RawMessage `json:"started_by_jarvis"`
		CurrentStarted json.RawMessage `json:"started_by_persona"`
	}{sessionJSON: sessionJSON(*s)}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	raw := v.LegacyStarted
	if len(v.CurrentStarted) > 0 {
		raw = v.CurrentStarted
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &v.sessionJSON.StartedByPersona); err != nil {
			return err
		}
	}
	*s = Session(v.sessionJSON)
	return nil
}

// Only the managed-launcher enum is projected; focus-source IDs and arbitrary
// extension source strings do not pass through this adapter.
const legacyLauncherSource = "jarvis"

// Managed launcher source is a wire enum, independent of the MCP namespace.
func CanonicalLauncherSource(value string) string {
	if value == legacyLauncherSource {
		return "persona"
	}
	return value
}
func LegacyLauncherSource(value string) string {
	if value == "persona" {
		return legacyLauncherSource
	}
	return value
}

// LauncherEnvelope is an explicit transport projection. Launcher itself has no
// JSON methods: embedding it must not hide health, agent-type or other siblings.
type LauncherEnvelope Launcher

func (v LauncherEnvelope) MarshalJSON() ([]byte, error) {
	copy := Launcher(v)
	copy.Source = LegacyLauncherSource(copy.Source)
	return json.Marshal(copy)
}
func (v *LauncherEnvelope) UnmarshalJSON(raw []byte) error {
	copy := Launcher(*v)
	if err := json.Unmarshal(raw, &copy); err != nil {
		return err
	}
	copy.Source = CanonicalLauncherSource(copy.Source)
	*v = LauncherEnvelope(copy)
	return nil
}
