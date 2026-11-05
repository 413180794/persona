package devices

import (
	"errors"
	"net/http"

	"github.com/nwverse/persona/server/internal/apierr"
)

var errUnknownMachine = errors.New("unknown machine")

func writeJSON(w http.ResponseWriter, v any) { apierr.WriteJSON(w, v) }

func writeErr(w http.ResponseWriter, status int, err error) { apierr.Write(w, status, err) }

func isOffline(err error) bool { return errors.Is(err, errOffline) }
