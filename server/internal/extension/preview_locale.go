package extension

import "github.com/nwverse/persona/server/internal/i18n"

// In returns a presentation copy of a staged preview. Consent digests, manifests, staging data and
// the captured old manifest stay unchanged, so switching languages never changes what gets installed.
func (pv *Preview) In(l i18n.Lang) *Preview {
	if pv == nil || l == "" {
		return pv
	}
	out := *pv
	if pv.Manifest != nil {
		out.Permissions = ExplainIn(l, pv.Manifest)
		if pv.previous != nil {
			out.AddedPermissions = AddedIn(l, pv.previous, pv.Manifest)
		}
	}
	if pv.Bundle != nil {
		out.Bundle = make([]*Preview, len(pv.Bundle))
		for i, child := range pv.Bundle {
			out.Bundle[i] = child.In(l)
		}
	}
	return &out
}
