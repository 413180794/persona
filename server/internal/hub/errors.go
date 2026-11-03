package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/nwverse/persona/server/internal/rpc"
)

func writeErr(w http.ResponseWriter, status int, err error) {
	writeErrCode(w, status, "", err)
}

// writeErrCode preserves error for old clients and adds a stable machine-readable code.
func writeErrCode(w http.ResponseWriter, status int, code string, err error) {
	writeErrorBody(w, status, errorBody(status, code, err))
}

func writeErrorBody(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func errorBody(status int, code string, err error) map[string]any {
	if code == "" {
		code = statusErrorCode(status)
		if status >= 500 && code != "timeout" {
			code = nodeErrorCode(err)
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
	if errors.Is(err, errOffline) {
		body["retryable"], body["delivered"] = true, "none"
	}
	return body
}

func statusErrorCode(status int) string {
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

func nodeErrorCode(err error) string {
	var ne net.Error
	var re *rpc.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.Is(err, errOffline), errors.Is(err, rpc.ErrClosed), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.As(err, &ne):
		return "node_unreachable"
	case errors.As(err, &re):
		// RPC loses the Go error type; recognize only the exact standard deadline message.
		if re.Message == context.DeadlineExceeded.Error() {
			return "timeout"
		}
	}
	return "internal"
}

// jsonRouteErrors only wraps ServeMux's own fallback, leaving matched handlers (including mail and streams) alone.
func jsonRouteErrors(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern == "" {
			mux.ServeHTTP(&routeErrorWriter{ResponseWriter: w}, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

type routeErrorWriter struct {
	http.ResponseWriter
	replaced bool
}

func (w *routeErrorWriter) WriteHeader(status int) {
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
		w.replaced = true
		w.Header().Del("Content-Length")
		writeErr(w.ResponseWriter, status, errors.New(http.StatusText(status)))
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *routeErrorWriter) Write(b []byte) (int, error) {
	if w.replaced {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}
