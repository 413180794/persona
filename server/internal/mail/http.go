package mail

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// Handler returns the HTTP handler for /api/v1/mail/... . Authentication is
// done by the hub before requests reach this handler.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/mail/accounts", s.handleListAccounts)
	mux.HandleFunc("POST /api/v1/mail/accounts", s.handleAddAccount)
	mux.HandleFunc("DELETE /api/v1/mail/accounts/{aid}", s.handleDeleteAccount)
	mux.HandleFunc("POST /api/v1/mail/accounts/{aid}/test", s.handleTestAccount)
	mux.HandleFunc("GET /api/v1/mail/accounts/{aid}/mailboxes", s.handleMailboxes)
	mux.HandleFunc("GET /api/v1/mail/messages", s.handleListMessages)
	mux.HandleFunc("GET /api/v1/mail/messages/{aid}/{uid}", s.handleGetMessage)
	mux.HandleFunc("POST /api/v1/mail/messages/{aid}/{uid}/flags", s.handleSetFlags)
	mux.HandleFunc("POST /api/v1/mail/send", s.handleSend)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// statusFor maps service errors onto HTTP status codes.
func statusFor(err error) int {
	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, "unknown account"),
		strings.Contains(msg, "not found"),
		strings.Contains(msg, "unknown mailbox"):
		return http.StatusNotFound
	case strings.Contains(msg, "already exists"):
		return http.StatusConflict
	case strings.Contains(msg, "is required"),
		strings.Contains(msg, "invalid"),
		strings.Contains(msg, "unknown mail provider"),
		strings.Contains(msg, "no recipients"):
		return http.StatusBadRequest
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}

func (s *Service) fail(w http.ResponseWriter, err error) {
	writeError(w, statusFor(err), err)
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body: "+err.Error()))
		return false
	}
	return true
}

func (s *Service) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.ListAccounts()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": accounts})
}

func (s *Service) handleAddAccount(w http.ResponseWriter, r *http.Request) {
	var req AddAccountRequest
	if !decodeBody(w, r, &req) {
		return
	}
	account, err := s.AddAccount(req)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": account})
}

func (s *Service) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if err := s.DeleteAccount(r.PathValue("aid")); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handleTestAccount(w http.ResponseWriter, r *http.Request) {
	if err := s.TestAccount(r.PathValue("aid")); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handleMailboxes(w http.ResponseWriter, r *http.Request) {
	boxes, err := s.Mailboxes(r.Context(), r.PathValue("aid"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if boxes == nil {
		boxes = []Mailbox{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"mailboxes": boxes})
}

func (s *Service) handleListMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	before, _ := strconv.ParseUint(q.Get("before_uid"), 10, 32)
	res, err := s.ListMessages(r.Context(), ListOptions{
		AccountID: q.Get("account_id"),
		Mailbox:   q.Get("mailbox"),
		Limit:     limit,
		BeforeUID: uint32(before),
		Query:     q.Get("q"),
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	if res.Messages == nil {
		res.Messages = []MailSummary{}
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Service) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseUint(r.PathValue("uid"), 10, 32)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid uid"))
		return
	}
	msg, err := s.GetMessage(r.Context(), r.PathValue("aid"), uint32(uid), r.URL.Query().Get("mailbox"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": msg})
}

func (s *Service) handleSetFlags(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseUint(r.PathValue("uid"), 10, 32)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid uid"))
		return
	}
	var req SetFlagsRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if err := s.SetFlags(r.Context(), r.PathValue("aid"), uint32(uid), req); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handleSend(w http.ResponseWriter, r *http.Request) {
	var req SendRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if err := s.Send(r.Context(), req); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
