package hub

import (
	"github.com/nwverse/persona/server/internal/compat"
	"net/http"
)

// Presence of the current cookie wins, including an invalid or empty value;
// compatibility must not turn a failed current credential into a second login.
func compatibleAdminCookie(r *http.Request) (*http.Cookie, error) {
	names := compat.AdminCookieNames(r.TLS != nil)
	cookie, err := r.Cookie(names[0])
	if err == http.ErrNoCookie {
		return r.Cookie(names[1])
	}
	return cookie, err
}
