// Package apierr writes the JSON bodies of the hub's HTTP API (docs/protocol.md「错误」): a result, or an error with a
// stable code. It is shared by the hub and the services whose requests the hub relays (internal/assistant), so an
// error reads the same whichever of them answered.
package apierr

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/rpc"
)

// ErrOffline: the machine a call was for has no connection to the hub.
var ErrOffline = errors.New("machine is offline")

// WriteJSON writes v as the response.
func WriteJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// Write writes err with the code its status stands for.
func Write(w http.ResponseWriter, status int, err error) {
	WriteCode(w, status, "", err)
}

// WriteCode preserves error for old clients and adds a stable machine-readable code.
// An i18n.Message reads in the request's language (docs/i18n.md「协议」).
func WriteCode(w http.ResponseWriter, status int, code string, err error) {
	body := Body(status, code, err)
	if l := i18n.OfWriter(w); l != "" {
		body["error"] = i18n.Localize(err, l)
	}
	WriteBody(w, status, body)
}

func WriteBody(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func Body(status int, code string, err error) map[string]any {
	if code == "" {
		code = StatusCode(status)
		if status >= 500 && code != "timeout" {
			code = NodeCode(err)
		}
	}
	body := map[string]any{"error": err.Error(), "code": code}
	switch code {
	case "invalid_token", "forbidden", "bad_request", "not_found", "method_not_allowed", "gone":
		body["retryable"] = false
	case "input_draft", "dialog_open", "prompt_changed":
		body["retryable"], body["delivered"] = false, "none"
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		body["delivered"] = "none"
	}
	if errors.Is(err, ErrOffline) {
		body["retryable"], body["delivered"] = true, "none"
	}
	return body
}

func StatusCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "invalid_token"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return "timeout"
	case http.StatusConflict:
		return "conflict"
	case http.StatusGone:
		return "gone"
	case http.StatusRequestEntityTooLarge:
		return "too_large"
	case http.StatusTooManyRequests:
		return "rate_limited"
	default:
		return "internal"
	}
}

func NodeCode(err error) string {
	var ne net.Error
	var re *rpc.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.Is(err, ErrOffline), errors.Is(err, rpc.ErrClosed), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.As(err, &ne):
		return "node_unreachable"
	case errors.As(err, &re):
		// RPC loses the Go error type; recognize only the exact standard deadline message.
		if re.Message == context.DeadlineExceeded.Error() {
			return "timeout"
		}
	}
	return "internal"
}

// Call writes the error of a call that went to a machine's node (or further): offline is 503, a refusal the node
// coded is 409 with its details, anything else 502.
func Call(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, ErrOffline) {
		status = http.StatusServiceUnavailable
	}
	code := NodeCode(err)
	var re *rpc.Error
	if errors.As(err, &re) && re.Code != "" {
		code = re.Code
		switch code {
		case "internal", "node_unreachable", "timeout":
			status = http.StatusBadGateway
		default:
			status = http.StatusConflict
		}
	}
	body := Body(status, code, err)
	// delivered stays what the node said (sendsafe.go): the iOS app treats "unknown" as "maybe typed" and stops
	// retrying by itself, so a bare timeout must not claim it. Newer clients read code (timeout, node_unreachable).
	if re != nil {
		// Keep refusal details flat for existing clients, including draft and clearable.
		var data map[string]any
		if json.Unmarshal(re.Data, &data) == nil {
			for key, value := range data {
				if key == "retryable" {
					if _, ok := value.(bool); !ok {
						continue
					}
				}
				if key == "delivered" && value != "none" && value != "unknown" && value != "yes" {
					continue
				}
				body[key] = value
			}
		}
		if re.Code != "" || len(re.Data) > 0 {
			body["error"] = re.Message
		}
		body["code"] = code
	}
	WriteBody(w, status, body)
}
