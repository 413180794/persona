package hub

import (
	"encoding/json"
	"maps"
	"net/http"

	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/i18n"
)

var contractHeaders = []string{"X-Persona-Client", "X-Persona-Op-Id", "X-Persona-CSRF",
	hdrPeerTTL, hdrPeerSource, hdrPeerNode, hdrPeerFrom}

// compatibilityRequests normalizes names before authentication and receipt
// admission. Conflicting aliases cannot be used to present two different facts
// to an older and a newer hop. It does not buffer bodies or streaming responses.
func compatibilityRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range contractHeaders {
			if compat.HeaderConflict(r.Header, name) {
				writeErrCode(w, http.StatusBadRequest, "bad_request", i18n.Err("hub-compatibility.header_conflict"))
				return
			}
			if value := compat.Header(r.Header, name); value != "" {
				compat.SetHeader(r.Header, name, value)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Push capabilities are persisted at device registration; a socket hello alone
// cannot identify an offline device or its push-to-start token.
func compatibleStartPayload(d Device, payload map[string]any) map[string]any {
	if hasFeature(d, compat.Feature) {
		return payload
	}
	out := maps.Clone(payload)
	if aps, ok := payload["aps"].(map[string]any); ok {
		copy := maps.Clone(aps)
		if copy["event"] == "start" {
			copy["attributes-type"] = compat.ActivityAttributes
		}
		out["aps"] = copy
	}
	return out
}

// These typed adapters preserve old look IDs in persisted choices and HTTP
// DTOs, without rewriting custom IDs, names or arbitrary extension JSON.
type lookChoiceJSON lookChoice

func (v lookChoice) MarshalJSON() ([]byte, error) {
	copy := lookChoiceJSON(v)
	copy.ID = compat.LegacyLook(copy.ID)
	return json.Marshal(copy)
}
func (v *lookChoice) UnmarshalJSON(raw []byte) error {
	var copy lookChoiceJSON
	if err := json.Unmarshal(raw, &copy); err != nil {
		return err
	}
	copy.ID = compat.Looks(copy.ID)
	*v = lookChoice(copy)
	return nil
}

type lookViewJSON lookView

func (v lookView) MarshalJSON() ([]byte, error) {
	copy := lookViewJSON(v)
	if copy.Source == "builtin" {
		copy.ID = compat.LegacyLook(copy.ID)
	}
	copy.AppIcon = compat.LegacyLook(copy.AppIcon)
	return json.Marshal(copy)
}
func (v *lookView) UnmarshalJSON(raw []byte) error {
	var copy lookViewJSON
	if err := json.Unmarshal(raw, &copy); err != nil {
		return err
	}
	if copy.Source == "builtin" {
		copy.ID = compat.Looks(copy.ID)
	}
	copy.AppIcon = compat.Looks(copy.AppIcon)
	*v = lookView(copy)
	return nil
}

type lookCurrentJSON lookCurrent

func (v lookCurrent) MarshalJSON() ([]byte, error) {
	copy := lookCurrentJSON(v)
	if copy.ID != nil {
		id := compat.LegacyLook(*copy.ID)
		copy.ID = &id
	}
	copy.Effective = compat.LegacyLook(copy.Effective)
	return json.Marshal(copy)
}
func (v *lookCurrent) UnmarshalJSON(raw []byte) error {
	var copy lookCurrentJSON
	if err := json.Unmarshal(raw, &copy); err != nil {
		return err
	}
	if copy.ID != nil {
		id := compat.Looks(*copy.ID)
		copy.ID = &id
	}
	copy.Effective = compat.Looks(copy.Effective)
	*v = lookCurrent(copy)
	return nil
}
