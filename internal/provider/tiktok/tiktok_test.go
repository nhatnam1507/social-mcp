package tiktok

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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/oauth"
	"github.com/nhatnam1507/social-mcp/internal/provider"
)

func TestAuthURL(t *testing.T) {
	p := New("ckey", "secret")
	u, _ := url.Parse(p.AuthURL("st", "verifier-xyz", "http://127.0.0.1:8765/oauth/callback"))
	q := u.Query()
	if u.Host != "www.tiktok.com" || q.Get("client_key") != "ckey" || q.Get("client_id") != "" {
		t.Fatalf("auth url %s", u)
	}
	if q.Get("scope") != "user.info.basic,video.upload" || q.Get("code_challenge") != oauth.ChallengeS256Hex("verifier-xyz") || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("auth query %v", q)
	}
}

func TestExchangeRefresh(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		form = r.PostForm
		if r.PostForm.Get("refresh_token") == "dead" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"invalid_grant","error_description":"refresh token expired","log_id":"x"}`)
			return
		}
		fmt.Fprint(w, `{"access_token":"at","expires_in":7200,"refresh_token":"rt-new","refresh_expires_in":15552000,"open_id":"oid","scope":"user.info.basic,video.upload","token_type":"Bearer"}`)
	}))
	defer srv.Close()
	p := New("ckey", "secret")
	p.APIBase = srv.URL
	tok, err := p.Exchange(context.Background(), "c1", "ver", "http://127.0.0.1:1/oauth/callback")
	if err != nil || tok.AccessToken != "at" || tok.RefreshToken != "rt-new" || tok.RefreshExpiry.Before(time.Now().Add(170*24*time.Hour)) {
		t.Fatalf("exchange %+v %v", tok, err)
	}
	for k, v := range map[string]string{"client_key": "ckey", "client_secret": "secret", "code": "c1", "code_verifier": "ver", "grant_type": "authorization_code"} {
		if form.Get(k) != v {
			t.Errorf("form %s=%q", k, form.Get(k))
		}
	}
	tok2, err := p.Refresh(context.Background(), provider.Token{RefreshToken: "rt-old"})
	if err != nil || tok2.RefreshToken != "rt-new" || form.Get("grant_type") != "refresh_token" {
		t.Fatalf("refresh %+v %v", tok2, err)
	}
	if _, err := p.Refresh(context.Background(), provider.Token{RefreshToken: "dead"}); !errors.Is(err, provider.ErrReauthRequired) {
		t.Fatalf("dead refresh: %v", err)
	}
}

func TestIdentities(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":{"code":"access_token_invalid","message":"expired"}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"user":{"open_id":"oid1","display_name":"Creator"}},"error":{"code":"ok"}}`)
	}))
	defer srv.Close()
	p := New("k", "s")
	p.APIBase = srv.URL
	sub, ids, err := p.Identities(context.Background(), provider.Token{AccessToken: "at"})
	if err != nil || sub != "oid1" || ids[0].DisplayName != "Creator" {
		t.Fatalf("%q %+v %v", sub, ids, err)
	}
	if _, _, err := p.Identities(context.Background(), provider.Token{AccessToken: "x"}); !errors.Is(err, provider.ErrUnauthorized) {
		t.Fatalf("unauthorized: %v", err)
	}
}

func TestChunkPlan(t *testing.T) {
	cases := []struct {
		size        int64
		chunk, n    int64
		wantErr     bool
		lastChunkLE int64
	}{
		{1, 1, 1, false, 1},
		{4_999_999, 4_999_999, 1, false, 4_999_999},
		{9_999_999, 9_999_999, 1, false, 9_999_999},
		{10_000_000, 10_000_000, 1, false, 10_000_000},
		{64_000_001, 10_000_000, 6, false, 20_000_000},
		{200_000_000, 10_000_000, 20, false, 10_000_000},
		{4_000_000_000, 10_000_000, 400, false, 10_000_000},
		{4_000_000_001, 0, 0, true, 0},
	}
	for _, tc := range cases {
		chunk, n, err := chunkPlan(tc.size)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("size %d accepted", tc.size)
			}
			continue
		}
		if err != nil || chunk != tc.chunk || n != tc.n {
			t.Fatalf("size %d: chunk=%d n=%d err=%v", tc.size, chunk, n, err)
		}
		last := tc.size - chunk*(n-1)
		if last > tc.lastChunkLE || last > 64_000_000 || n > 1000 {
			t.Fatalf("size %d: last chunk %d", tc.size, last)
		}
	}
}

// fakeTikTok emulates the inbox upload API.
type fakeTikTok struct {
	mu         sync.Mutex
	t          *testing.T
	inits      int
	total      int64
	chunks     int64
	received   bytes.Buffer
	statusSeq  []string
	statusCall int
	failReason string
	initErr    string
	expireURL  bool
}

func (f *fakeTikTok) server(t *testing.T) *httptest.Server {
	f.t = t
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/v2/post/publish/inbox/video/init/":
			if f.initErr != "" {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprintf(w, `{"error":{"code":%q,"message":"too many"}}`, f.initErr)
				return
			}
			var body struct {
				SourceInfo struct {
					Source          string `json:"source"`
					VideoSize       int64  `json:"video_size"`
					ChunkSize       int64  `json:"chunk_size"`
					TotalChunkCount int64  `json:"total_chunk_count"`
				} `json:"source_info"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.SourceInfo.Source != "FILE_UPLOAD" {
				t.Errorf("source %q", body.SourceInfo.Source)
			}
			f.inits++
			f.total, f.chunks = body.SourceInfo.VideoSize, body.SourceInfo.TotalChunkCount
			f.received.Reset()
			fmt.Fprintf(w, `{"data":{"publish_id":"v_inbox_file~%d","upload_url":"%s/upload/%d"},"error":{"code":"ok"}}`, f.inits, srv.URL, f.inits)
		case "/v2/post/publish/status/fetch/":
			st := "PROCESSING_UPLOAD"
			if f.statusCall < len(f.statusSeq) {
				st = f.statusSeq[f.statusCall]
			} else if len(f.statusSeq) > 0 {
				st = f.statusSeq[len(f.statusSeq)-1]
			}
			f.statusCall++
			fmt.Fprintf(w, `{"data":{"status":%q,"fail_reason":%q},"error":{"code":"ok"}}`, st, f.failReason)
		default:
			if !strings.HasPrefix(r.URL.Path, "/upload/") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if f.expireURL {
				f.expireURL = false
				w.WriteHeader(http.StatusForbidden)
				return
			}
			var start, end, total int64
			if _, err := fmt.Sscanf(r.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); err != nil || start != int64(f.received.Len()) || total != f.total {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			if r.Header.Get("Content-Type") != "video/mp4" {
				t.Errorf("content type %q", r.Header.Get("Content-Type"))
			}
			data, _ := io.ReadAll(r.Body)
			if int64(len(data)) != end-start+1 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.received.Write(data)
			if int64(f.received.Len()) == f.total {
				w.WriteHeader(http.StatusCreated)
				return
			}
			w.WriteHeader(http.StatusPartialContent)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type recorder struct {
	phase  string
	resume json.RawMessage
	phases []string
}

func (r *recorder) progress(u provider.ProgressUpdate) error {
	if u.Phase != "" {
		r.phase = u.Phase
		r.phases = append(r.phases, u.Phase)
	}
	if u.Resume != nil {
		r.resume = u.Resume
	}
	return nil
}

func testProvider(srv *httptest.Server) *Provider {
	p := New("k", "s")
	p.APIBase = srv.URL
	p.PollInterval = time.Millisecond
	p.PollTimeout = 200 * time.Millisecond
	return p
}

func req(data []byte, rec *recorder) provider.UploadRequest {
	return provider.UploadRequest{Token: provider.Token{AccessToken: "at"}, File: bytes.NewReader(data), FileSize: int64(len(data)),
		Container: provider.FormatMP4, Meta: provider.VideoMeta{Title: "ignored"}, Phase: rec.phase, Resume: rec.resume}
}

func TestUploadToInbox(t *testing.T) {
	f := &fakeTikTok{statusSeq: []string{"PROCESSING_UPLOAD", "SEND_TO_USER_INBOX"}}
	p := testProvider(f.server(t))
	data := bytes.Repeat([]byte{7}, 25_000_000) // two chunks, the last absorbs the remainder
	rec := &recorder{}
	res, err := p.Upload(context.Background(), req(data, rec), rec.progress)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != provider.OutcomeSentToDrafts || res.RemoteID != "v_inbox_file~1" || !strings.Contains(res.Note, "TikTok app") {
		t.Fatalf("result %+v", res)
	}
	if f.chunks != 2 || int64(f.received.Len()) != f.total || f.inits != 1 {
		t.Fatalf("chunks=%d received=%d inits=%d", f.chunks, f.received.Len(), f.inits)
	}
	if strings.Join(rec.phases, ",") != "transferring,committing,processing" {
		t.Fatalf("phases %v", rec.phases)
	}
}

func TestFailedStatusIsPermanent(t *testing.T) {
	f := &fakeTikTok{statusSeq: []string{"FAILED"}, failReason: "duration_check_failed"}
	p := testProvider(f.server(t))
	rec := &recorder{}
	_, err := p.Upload(context.Background(), req(bytes.Repeat([]byte{1}, 1000), rec), rec.progress)
	if !provider.IsPermanent(err) || !strings.Contains(err.Error(), "duration_check_failed") {
		t.Fatalf("err %v", err)
	}
}

func TestPendingShareLimitIsPermanent(t *testing.T) {
	f := &fakeTikTok{initErr: "spam_risk_too_many_pending_share"}
	p := testProvider(f.server(t))
	rec := &recorder{}
	_, err := p.Upload(context.Background(), req([]byte("x"), rec), rec.progress)
	if !provider.IsPermanent(err) || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("err %v", err)
	}
}

func TestPollTimeoutIsOutcomeUnknown(t *testing.T) {
	f := &fakeTikTok{statusSeq: []string{"PROCESSING_UPLOAD"}}
	p := testProvider(f.server(t))
	rec := &recorder{}
	_, err := p.Upload(context.Background(), req([]byte("abc"), rec), rec.progress)
	if !errors.Is(err, provider.ErrOutcomeUnknown) {
		t.Fatalf("err %v", err)
	}
}

func TestResumeAfterUploadOnlyPolls(t *testing.T) {
	f := &fakeTikTok{statusSeq: []string{"SEND_TO_USER_INBOX"}}
	p := testProvider(f.server(t))
	rec := &recorder{phase: provider.PhaseProcessing, resume: json.RawMessage(`{"publish_id":"v_inbox_file~42","upload_url":"x","next_chunk":3,"started_at":"2020-01-01T00:00:00Z"}`)}
	res, err := p.Upload(context.Background(), req([]byte("abc"), rec), rec.progress)
	if err != nil || res.RemoteID != "v_inbox_file~42" || f.inits != 0 {
		t.Fatalf("res %+v err %v inits %d", res, err, f.inits)
	}
}

func TestResumeTransferWindow(t *testing.T) {
	f := &fakeTikTok{statusSeq: []string{"SEND_TO_USER_INBOX"}}
	srv := f.server(t)
	p := testProvider(srv)
	data := bytes.Repeat([]byte{3}, 25_000_000)

	// Start an upload, stop after the first chunk by failing the second PUT once.
	rec := &recorder{}
	calls := 0
	stopAfterFirst := func(u provider.ProgressUpdate) error {
		rec.progress(u)
		if u.Phase == provider.PhaseCommitting {
			calls++
			return errors.New("simulated crash before the final chunk")
		}
		return nil
	}
	if _, err := p.Upload(context.Background(), req(data, rec), stopAfterFirst); err == nil {
		t.Fatal("expected simulated crash")
	}
	// Persist a transferring state (the runner would not have recorded committing).
	rec.phase = provider.PhaseTransferring
	res, err := p.Upload(context.Background(), req(data, rec), rec.progress)
	if err != nil || f.inits != 1 || res.Outcome != provider.OutcomeSentToDrafts {
		t.Fatalf("resume within window: %+v %v inits=%d", res, err, f.inits)
	}

	// Outside the 55 minute window a transferring job starts over.
	var st resumeState
	json.Unmarshal(rec.resume, &st)
	st.StartedAt = time.Now().Add(-2 * time.Hour)
	rec.resume, _ = json.Marshal(st)
	rec.phase = provider.PhaseTransferring
	if _, err := p.Upload(context.Background(), req(data, rec), rec.progress); err != nil || f.inits != 2 {
		t.Fatalf("restart after window: %v inits=%d", err, f.inits)
	}
}

func TestExpiredUploadURLRestartsOnce(t *testing.T) {
	f := &fakeTikTok{statusSeq: []string{"SEND_TO_USER_INBOX"}, expireURL: true}
	p := testProvider(f.server(t))
	rec := &recorder{}
	if _, err := p.Upload(context.Background(), req(bytes.Repeat([]byte{1}, 25_000_000), rec), rec.progress); err != nil || f.inits != 2 {
		t.Fatalf("err %v inits %d", err, f.inits)
	}
}

func TestRejectedFinalChunkNeverRestarts(t *testing.T) {
	f := &fakeTikTok{statusSeq: []string{"SEND_TO_USER_INBOX"}, expireURL: true}
	p := testProvider(f.server(t))
	rec := &recorder{}
	if _, err := p.Upload(context.Background(), req(bytes.Repeat([]byte{1}, 100), rec), rec.progress); !errors.Is(err, provider.ErrOutcomeUnknown) || f.inits != 1 {
		t.Fatalf("err %v inits %d", err, f.inits)
	}
}
