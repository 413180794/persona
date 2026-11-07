package assistant

import (
	"encoding/json"
	"github.com/nwverse/persona/server/internal/compat"
)

// Persist the pre-cutover enum so an older manager can resume the same bot
// threads. The typed reader canonicalizes only this exact enum, not user data.
type botTargetJSON botTarget

func (t botTarget) MarshalJSON() ([]byte, error) {
	copy := botTargetJSON(t)
	copy.Kind = compat.LegacySource(copy.Kind)
	return json.Marshal(copy)
}
func (t *botTarget) UnmarshalJSON(raw []byte) error {
	var copy botTargetJSON
	if err := json.Unmarshal(raw, &copy); err != nil {
		return err
	}
	copy.Kind = compat.Source(copy.Kind)
	*t = botTarget(copy)
	return nil
}

type launcherViewJSON LauncherView

func (v LauncherView) MarshalJSON() ([]byte, error) {
	copy := launcherViewJSON(v)
	copy.Source = compat.LegacySource(copy.Source)
	return json.Marshal(copy)
}
func (v *LauncherView) UnmarshalJSON(raw []byte) error {
	copy := launcherViewJSON(*v)
	if err := json.Unmarshal(raw, &copy); err != nil {
		return err
	}
	copy.Source = compat.Source(copy.Source)
	*v = LauncherView(copy)
	return nil
}

type launcherWithHealthJSON launcherWithHealth

func (v launcherWithHealth) MarshalJSON() ([]byte, error) {
	copy := launcherWithHealthJSON(v)
	copy.Source = compat.LegacySource(copy.Source)
	return json.Marshal(copy)
}
