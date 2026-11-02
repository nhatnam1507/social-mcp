package daemon

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/logx"
	"github.com/nhatnam1507/social-mcp/internal/oauth"
	"github.com/nhatnam1507/social-mcp/internal/service"
)

var callbackPage = template.Must(template.New("cb").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>social-mcp</title>
<style>body{font-family:system-ui,sans-serif;max-width:36rem;margin:4rem auto;padding:0 1rem;line-height:1.5}</style></head>
<body>{{if .OK}}<h1>Connected</h1><p>Connected {{len .Accounts}} account(s):</p><ul>{{range .Accounts}}<li>{{.}}</li>{{end}}</ul>
<p>You can close this tab and return to the terminal.</p>{{else}}<h1>Login failed</h1><p>{{.Error}}</p>
<p>Run <code>social-mcp account add</code> again.</p>{{end}}</body></html>`))

type callbackData struct {
	OK       bool
	Accounts []string
	Error    string
}

// callbackHandler completes OAuth logins redirected to /oauth/callback.
func callbackHandler(svc *service.Local) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		state, code := q.Get("state"), q.Get("code")
		oauthErr := q.Get("error")
		if d := q.Get("error_description"); d != "" {
			oauthErr += ": " + d
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if state == "" {
			w.WriteHeader(http.StatusBadRequest)
			callbackPage.Execute(w, callbackData{Error: "The login response is missing its state parameter."})
			return
		}
		// Finish the exchange even if the browser tab goes away.
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		view, err := svc.CompleteAuth(ctx, state, code, oauthErr)
		if err != nil {
			msg := logx.Sanitize(err.Error())
			if errors.Is(err, oauth.ErrUnknownState) {
				msg = "This login link has expired or was already used."
			}
			w.WriteHeader(http.StatusBadRequest)
			callbackPage.Execute(w, callbackData{Error: msg})
			return
		}
		data := callbackData{OK: true}
		for _, a := range view.Accounts {
			data.Accounts = append(data.Accounts, a.Platform+": "+a.DisplayName)
		}
		callbackPage.Execute(w, data)
	}
}
