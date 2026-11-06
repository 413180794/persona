package hub

import (
	"errors"
	"net/http"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/i18n"
)

// The bodies are apierr's, shared with the assistant (internal/assistant), whose routes the hub relays.

func writeErr(w http.ResponseWriter, status int, err error) { apierr.Write(w, status, err) }

// writeErrCode preserves error for old clients and adds a stable machine-readable code.
func writeErrCode(w http.ResponseWriter, status int, code string, err error) {
	apierr.WriteCode(w, status, code, err)
}

func writeErrorBody(w http.ResponseWriter, status int, body map[string]any) {
	apierr.WriteBody(w, status, body)
}

func errorBody(status int, code string, err error) map[string]any {
	return apierr.Body(status, code, err)
}

// jsonRouteErrors only wraps ServeMux's own fallback, leaving matched handlers (including mail and streams) alone.
func jsonRouteErrors(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern == "" {
			// ServeMux fallback runs outside the matched route's request logger, so retain the header locale here.
			localized := routeLanguageWriter{ResponseWriter: w, language: i18n.FromRequest(r)}
			mux.ServeHTTP(&routeErrorWriter{ResponseWriter: localized}, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

type routeLanguageWriter struct {
	http.ResponseWriter
	language i18n.Lang
}

func (w routeLanguageWriter) Lang() i18n.Lang { return w.language }

func (w routeLanguageWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

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
