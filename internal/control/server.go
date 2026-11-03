// Package control exposes service.Service as JSON over HTTP on the daemon's local socket.
package control

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/nhatnam1507/social-mcp/internal/logx"
	"github.com/nhatnam1507/social-mcp/internal/service"
)

// errorBody is the JSON error envelope.
type errorBody struct {
	Error string `json:"error"`
	Kind  string `json:"kind"` // not_found | validation | internal
}

type authRequest struct {
	Platform        string `json:"platform"`
	ReauthAccountID string `json:"reauth_account_id,omitempty"`
}

// NewHandler returns the control API. shutdown is called by POST /v1/shutdown.
func NewHandler(svc service.Service, shutdown func()) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		reply(w, svc.Status)
	})
	mux.HandleFunc("GET /v1/accounts", func(w http.ResponseWriter, r *http.Request) {
		reply(w, func() ([]service.AccountView, error) { return svc.ListAccounts(r.URL.Query().Get("platform")) })
	})
	mux.HandleFunc("DELETE /v1/accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		reply(w, func() (struct{}, error) { return struct{}{}, svc.RemoveAccount(r.PathValue("id")) })
	})
	mux.HandleFunc("POST /v1/auth", func(w http.ResponseWriter, r *http.Request) {
		var req authRequest
		if !decode(w, r, &req) {
			return
		}
		reply(w, func() (service.AuthStart, error) { return svc.BeginAuth(req.Platform, req.ReauthAccountID) })
	})
	mux.HandleFunc("GET /v1/auth/{id}", func(w http.ResponseWriter, r *http.Request) {
		reply(w, func() (service.AuthSessionView, error) { return svc.AuthSession(r.PathValue("id")) })
	})
	mux.HandleFunc("POST /v1/uploads", func(w http.ResponseWriter, r *http.Request) {
		var in service.UploadInput
		if !decode(w, r, &in) {
			return
		}
		reply(w, func() (service.UploadSubmission, error) { return svc.SubmitUpload(in) })
	})
	mux.HandleFunc("GET /v1/uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		reply(w, func() (service.JobView, error) { return svc.GetUpload(r.PathValue("id")) })
	})
	mux.HandleFunc("GET /v1/uploads", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		reply(w, func() ([]service.JobView, error) {
			return svc.ListUploads(service.UploadFilter{Status: q.Get("status"), AccountID: q.Get("account_id"), Limit: limit})
		})
	})
	mux.HandleFunc("POST /v1/uploads/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		reply(w, func() (service.JobView, error) { return svc.CancelUpload(r.PathValue("id")) })
	})
	mux.HandleFunc("POST /v1/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		if shutdown != nil {
			go shutdown()
		}
	})
	return mux
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid JSON body: " + err.Error(), Kind: "validation"})
		return false
	}
	return true
}

func reply[T any](w http.ResponseWriter, fn func() (T, error)) {
	v, err := fn()
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, v)
	case errors.Is(err, service.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Error: "not found", Kind: "not_found"})
	case service.IsValidation(err):
		writeJSON(w, http.StatusBadRequest, errorBody{Error: logx.Sanitize(err.Error()), Kind: "validation"})
	default:
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: logx.Error(err), Kind: "internal"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
