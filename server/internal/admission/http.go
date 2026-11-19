package admission

import "net/http"

// HTTP refuses before the wrapped handler runs, including on HTTP keep-alive.
func (g *Gate) HTTP(kind string, w http.ResponseWriter) (func(), bool) {
	done, err := g.Admit(kind)
	if err == nil {
		return done, true
	}
	w.Header().Set("Retry-After", "5")
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "service paused for maintenance", http.StatusServiceUnavailable)
	return nil, false
}
