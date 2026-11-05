package devices

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/nwverse/persona/server/internal/apierr"
)

var errUnknownMachine = errors.New("unknown machine")

func writeJSON(w http.ResponseWriter, v any) { apierr.WriteJSON(w, v) }

func writeErr(w http.ResponseWriter, status int, err error) { apierr.Write(w, status, err) }

func writeErrCode(w http.ResponseWriter, status int, code string, err error) {
	apierr.WriteCode(w, status, code, err)
}

// readAdminJSON reads an admin page's JSON body into v (an empty body leaves v as it is), as the hub's admin pages do.
func readAdminJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	b, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return false
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return true
	}
	if err := json.Unmarshal(b, v); err != nil {
		writeErrCode(w, http.StatusBadRequest, "bad_request", errors.New("bad JSON: "+err.Error()))
		return false
	}
	return true
}

func isOffline(err error) bool { return errors.Is(err, errOffline) }
