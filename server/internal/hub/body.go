package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/nwverse/persona/server/internal/proto"
)

// decodeJSONBody decodes the request's JSON body into v, reading at most limit bytes: a larger body stops the read
// with an *http.MaxBytesError (and the server closes the connection instead of draining the rest).
func decodeJSONBody(w http.ResponseWriter, r *http.Request, v any, limit int64) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(v)
}

// bodyTooLarge reports whether err is decodeJSONBody's over-the-limit error.
func bodyTooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.As(err, &tooLarge)
}

// writeBodyErr answers a body decodeJSONBody could not use: 413 (proto.CodeTooLarge) when err is over the limit,
// else 400 with bad (the caller's own message, or err itself).
func writeBodyErr(w http.ResponseWriter, err, bad error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeErrCode(w, http.StatusRequestEntityTooLarge, proto.CodeTooLarge,
			fmt.Errorf("request body is larger than %d bytes", tooLarge.Limit))
		return
	}
	writeErr(w, http.StatusBadRequest, bad)
}
