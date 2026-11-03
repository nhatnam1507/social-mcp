package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/nhatnam1507/social-mcp/internal/oauth"
	"github.com/nhatnam1507/social-mcp/internal/provider"
)

func newTestProvider(srv *httptest.Server) *Provider {
	p := New("client-id", "client-secret")
	p.TokenURL = srv.URL + "/token"
	p.APIBase = srv.URL + "/youtube/v3"
	p.UploadBase = srv.URL + "/upload/youtube/v3"
	p.ChunkSize = 256 * 1024
	return p
}

func TestAuthURL(t *testing.T) {
	p := New("cid", "secret")
	u, err := url.Parse(p.AuthURL("state1", "verifier-123", "http://127.0.0.1:8765/oauth/callback"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	want := map[string]string{
		"client_id": "cid", "redirect_uri": "http://127.0.0.1:8765/oauth/callback", "response_type": "code",
		"access_type": "offline", "prompt": "consent", "state": "state1",
		"code_challenge": oauth.ChallengeS256Base64URL("verifier-123"), "code_challenge_method": "S256",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if !strings.Contains(q.Get("scope"), "youtube.upload") || !strings.Contains(q.Get("scope"), "youtube.readonly") {
		t.Errorf("scope = %q", q.Get("scope"))
	}
	if u.Host != "accounts.google.com" {
		t.Errorf("host = %s", u.Host)
	}
}

func TestExchangeAndRefresh(t *testing.T) {
	var lastForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		lastForm = r.PostForm
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			fmt.Fprint(w, `{"access_token":"at1","refresh_token":"rt1","expires_in":3599}`)
		case "refresh_token":
			if r.PostForm.Get("refresh_token") == "revoked" {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`)
				return
			}
			fmt.Fprint(w, `{"access_token":"at2","expires_in":3599}`)
		}
	}))
	defer srv.Close()
	p := newTestProvider(srv)

	tok, err := p.Exchange(context.Background(), "code1", "verifier", "http://127.0.0.1:1/oauth/callback")
	if err != nil || tok.AccessToken != "at1" || tok.RefreshToken != "rt1" || tok.Expiry.IsZero() {
		t.Fatalf("exchange: %+v %v", tok, err)
	}
	for k, v := range map[string]string{"code": "code1", "code_verifier": "verifier", "client_secret": "client-secret", "redirect_uri": "http://127.0.0.1:1/oauth/callback"} {
		if lastForm.Get(k) != v {
			t.Errorf("exchange form %s = %q", k, lastForm.Get(k))
		}
	}
	tok2, err := p.Refresh(context.Background(), tok)
	if err != nil || tok2.AccessToken != "at2" || tok2.RefreshToken != "rt1" {
		t.Fatalf("refresh: %+v %v", tok2, err)
	}
	if _, err := p.Refresh(context.Background(), provider.Token{RefreshToken: "revoked"}); !errors.Is(err, provider.ErrReauthRequired) {
		t.Fatalf("revoked refresh: %v", err)
	}
	if _, err := p.Refresh(context.Background(), provider.Token{AccessToken: "x"}); !errors.Is(err, provider.ErrReauthRequired) {
		t.Fatalf("refresh without refresh token: %v", err)
	}
}

func TestIdentities(t *testing.T) {
	body := `{"items":[{"id":"UC1","snippet":{"title":"Main"}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("mine") != "true" || r.URL.Query().Get("part") != "snippet" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	p := newTestProvider(srv)
	sub, ids, err := p.Identities(context.Background(), provider.Token{AccessToken: "at"})
	if err != nil || sub != "UC1" || len(ids) != 1 || ids[0].DisplayName != "Main" {
		t.Fatalf("one channel: %q %+v %v", sub, ids, err)
	}
	body = `{"items":[{"id":"UCb","snippet":{"title":"B"}},{"id":"UCa","snippet":{"title":"A"}}]}`
	sub, ids, _ = p.Identities(context.Background(), provider.Token{AccessToken: "at"})
	if sub != "UCa,UCb" || len(ids) != 2 {
		t.Fatalf("two channels: %q %d", sub, len(ids))
	}
	body = `{"items":[]}`
	if _, _, err := p.Identities(context.Background(), provider.Token{AccessToken: "at"}); err == nil || !strings.Contains(err.Error(), "no YouTube channel") {
		t.Fatalf("no channel: %v", err)
	}
	if _, _, err := p.Identities(context.Background(), provider.Token{AccessToken: "bad"}); !errors.Is(err, provider.ErrUnauthorized) {
		t.Fatalf("unauthorized: %v", err)
	}
}

func TestValidateMeta(t *testing.T) {
	p := New("a", "b")
	ok := provider.VideoMeta{Title: "Hello", Privacy: provider.PrivacyPublic}
	if err := p.ValidateMeta(ok); err != nil {
		t.Fatal(err)
	}
	bad := []provider.VideoMeta{
		{Title: ""},
		{Title: strings.Repeat("x", 101)},
		{Title: "a <b>"},
		{Title: "ok", Description: strings.Repeat("x", 5001)},
	}
	for _, m := range bad {
		if p.ValidateMeta(m) == nil {
			t.Fatalf("accepted %+v", m)
		}
	}
}

// fakeUpload emulates the resumable upload protocol.
type fakeUpload struct {
	mu          sync.Mutex
	t           *testing.T
	total       int64
	received    bytes.Buffer
	sessions    int
	created     int
	failOnce    map[int]int // chunk index -> HTTP status to return once
	loseFinal   bool        // store the final chunk but drop the connection
	gone        bool        // session URI returns 404
	quota       bool        // session start returns quotaExceeded
	chunkIndex  int
	lastMeta    map[string]any
	contentType string
}

func (f *fakeUpload) handler(srvURL func() string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/upload/youtube/v3/videos"):
			if r.URL.Query().Get("uploadType") != "resumable" || r.URL.Query().Get("part") != "snippet,status" {
				f.t.Errorf("bad session query %s", r.URL.RawQuery)
			}
			if f.quota {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `{"error":{"code":403,"message":"quota","errors":[{"reason":"quotaExceeded"}]}}`)
				return
			}
			f.total, _ = strconv.ParseInt(r.Header.Get("X-Upload-Content-Length"), 10, 64)
			f.contentType = r.Header.Get("X-Upload-Content-Type")
			json.NewDecoder(r.Body).Decode(&f.lastMeta)
			f.sessions++
			f.received.Reset()
			f.gone = false
			w.Header().Set("Location", srvURL()+"/session/"+strconv.Itoa(f.sessions))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/session/"):
			if f.gone {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			cr := r.Header.Get("Content-Range")
			if strings.HasPrefix(cr, "bytes */") {
				f.status(w)
				return
			}
			var start, end, total int64
			if _, err := fmt.Sscanf(cr, "bytes %d-%d/%d", &start, &end, &total); err != nil || start != int64(f.received.Len()) || total != f.total {
				f.t.Errorf("bad Content-Range %q (have %d)", cr, f.received.Len())
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			data, _ := io.ReadAll(r.Body)
			idx := f.chunkIndex
			f.chunkIndex++
			if code, ok := f.failOnce[idx]; ok {
				delete(f.failOnce, idx)
				w.WriteHeader(code)
				return
			}
			f.received.Write(data)
			if int64(f.received.Len()) == f.total && f.loseFinal {
				f.loseFinal = false
				f.created++
				hj, _ := w.(http.Hijacker)
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
			if int64(f.received.Len()) == f.total {
				f.created++
			}
			f.status(w)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func (f *fakeUpload) status(w http.ResponseWriter) {
	if f.total > 0 && int64(f.received.Len()) == f.total {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"vid123","status":{"privacyStatus":"public"}}`)
		return
	}
	if f.received.Len() > 0 {
		w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", f.received.Len()-1))
	}
	w.WriteHeader(308)
}

func newUploadServer(t *testing.T) (*fakeUpload, *httptest.Server) {
	f := &fakeUpload{t: t, failOnce: map[int]int{}}
	var srv *httptest.Server
	srv = httptest.NewServer(f.handler(func() string { return srv.URL }))
	t.Cleanup(srv.Close)
	return f, srv
}

type recorder struct {
	updates []provider.ProgressUpdate
	phase   string
	resume  json.RawMessage
}

func (r *recorder) progress(u provider.ProgressUpdate) error {
	r.updates = append(r.updates, u)
	if u.Phase != "" {
		r.phase = u.Phase
	}
	if u.Resume != nil {
		r.resume = u.Resume
	}
	return nil
}

func file(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func request(data []byte, rec *recorder) provider.UploadRequest {
	return provider.UploadRequest{
		Token: provider.Token{AccessToken: "at"}, File: bytes.NewReader(data), FileSize: int64(len(data)), Container: provider.FormatMP4,
		Meta:  provider.VideoMeta{Title: "T", Description: "D", Tags: []string{"a"}, Privacy: provider.PrivacyPublic, Options: map[string]any{"made_for_kids": true}},
		Phase: rec.phase, Resume: rec.resume,
	}
}

func TestUploadChunked(t *testing.T) {
	f, srv := newUploadServer(t)
	p := newTestProvider(srv)
	data := file(700 * 1024) // three 256 KiB chunks
	rec := &recorder{}
	res, err := p.Upload(context.Background(), request(data, rec), rec.progress)
	if err != nil {
		t.Fatal(err)
	}
	if res.RemoteID != "vid123" || res.URL != "https://www.youtube.com/watch?v=vid123" || res.Outcome != provider.OutcomePublished {
		t.Fatalf("result %+v", res)
	}
	if !bytes.Equal(f.received.Bytes(), data) || f.created != 1 || f.contentType != "video/mp4" {
		t.Fatalf("received %d bytes, created %d, type %s", f.received.Len(), f.created, f.contentType)
	}
	status := f.lastMeta["status"].(map[string]any)
	if status["privacyStatus"] != "public" || status["selfDeclaredMadeForKids"] != true {
		t.Fatalf("status sent %+v", status)
	}
	if f.lastMeta["snippet"].(map[string]any)["categoryId"] != "22" {
		t.Fatalf("snippet sent %+v", f.lastMeta["snippet"])
	}
	var sawCommitting bool
	for _, u := range rec.updates {
		if u.Phase == provider.PhaseCommitting {
			sawCommitting = true
			if u.BytesSent >= int64(len(data)) {
				t.Fatal("committing reported after the final chunk")
			}
		}
	}
	if !sawCommitting {
		t.Fatal("no committing phase reported before the final chunk")
	}
}

func TestUploadTransientErrorThenResume(t *testing.T) {
	f, srv := newUploadServer(t)
	f.failOnce[1] = http.StatusServiceUnavailable
	p := newTestProvider(srv)
	data := file(700 * 1024)
	rec := &recorder{}
	if _, err := p.Upload(context.Background(), request(data, rec), rec.progress); err == nil || provider.IsPermanent(err) {
		t.Fatalf("503 should be transient, got %v", err)
	}
	res, err := p.Upload(context.Background(), request(data, rec), rec.progress)
	if err != nil || res.RemoteID != "vid123" {
		t.Fatalf("resume: %+v %v", res, err)
	}
	if f.sessions != 1 || !bytes.Equal(f.received.Bytes(), data) {
		t.Fatalf("resume opened %d sessions, received %d bytes", f.sessions, f.received.Len())
	}
}

func TestLostFinalResponseReconciles(t *testing.T) {
	f, srv := newUploadServer(t)
	f.loseFinal = true
	p := newTestProvider(srv)
	data := file(300 * 1024)
	rec := &recorder{}
	res, err := p.Upload(context.Background(), request(data, rec), rec.progress)
	if err != nil || res.RemoteID != "vid123" {
		t.Fatalf("reconcile: %+v %v", res, err)
	}
	if f.created != 1 || f.sessions != 1 {
		t.Fatalf("created %d videos over %d sessions", f.created, f.sessions)
	}
}

func TestSessionGone(t *testing.T) {
	f, srv := newUploadServer(t)
	p := newTestProvider(srv)
	data := file(300 * 1024)

	// Gone while transferring: start a new session.
	rec := &recorder{phase: provider.PhaseTransferring, resume: json.RawMessage(`{"session_uri":"` + srv.URL + `/session/99"}`)}
	f.gone = true
	res, err := p.Upload(context.Background(), request(data, rec), rec.progress)
	if err != nil || res.RemoteID != "vid123" || f.sessions != 1 {
		t.Fatalf("restart after gone: %+v %v sessions=%d", res, err, f.sessions)
	}

	// Gone while committing: the outcome is unknown, never re-upload.
	rec = &recorder{phase: provider.PhaseCommitting, resume: json.RawMessage(`{"session_uri":"` + srv.URL + `/session/1"}`)}
	f.gone = true
	if _, err := p.Upload(context.Background(), request(data, rec), rec.progress); !errors.Is(err, provider.ErrOutcomeUnknown) {
		t.Fatalf("gone while committing: %v", err)
	}
	if f.sessions != 1 {
		t.Fatalf("a new session was opened while committing: %d", f.sessions)
	}
}

func TestQuotaIsPermanent(t *testing.T) {
	f, srv := newUploadServer(t)
	f.quota = true
	p := newTestProvider(srv)
	rec := &recorder{}
	_, err := p.Upload(context.Background(), request(file(1000), rec), rec.progress)
	if !provider.IsPermanent(err) || !strings.Contains(err.Error(), "quotaExceeded") {
		t.Fatalf("quota: %v", err)
	}
}

func TestRateLimitAndUnauthorized(t *testing.T) {
	code := http.StatusTooManyRequests
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(code)
	}))
	defer srv.Close()
	p := newTestProvider(srv)
	rec := &recorder{}
	var rl *provider.RateLimitError
	if _, err := p.Upload(context.Background(), request(file(1000), rec), rec.progress); !errors.As(err, &rl) || rl.RetryAfter.Seconds() != 7 {
		t.Fatalf("429: %v", err)
	}
	code = http.StatusUnauthorized
	if _, err := p.Upload(context.Background(), request(file(1000), rec), rec.progress); !errors.Is(err, provider.ErrUnauthorized) {
		t.Fatalf("401: %v", err)
	}
}
