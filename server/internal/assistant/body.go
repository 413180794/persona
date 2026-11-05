package assistant

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/proto"
)

// decodeBody decodes the request's JSON body into v, reading at most limit bytes: a larger body stops the read with an
// *http.MaxBytesError (as the hub's decodeJSONBody).
func decodeBody(w http.ResponseWriter, r *http.Request, v any, limit int64) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(v)
}

// writeBodyErr answers a body decodeBody could not use: 413 (proto.CodeTooLarge) when err is over the limit, else 400
// with bad (the caller's own message, or err itself).
func writeBodyErr(w http.ResponseWriter, err, bad error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		apierr.WriteCode(w, http.StatusRequestEntityTooLarge, proto.CodeTooLarge, fmt.Errorf("request body is larger than %d bytes", tooLarge.Limit))
		return
	}
	apierr.Write(w, http.StatusBadRequest, bad)
}
