package facebook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/provider/meta"
)

type fakeGraph struct {
	mu         sync.Mutex
	t          *testing.T
	total      int64
	received   bytes.Buffer
	finishes   int
	published  string
	loseFinish bool
	failFinish int // respond 500 to this many finish calls
	videoKnown bool
	chunk      int64
}

func (f *fakeGraph) server(t *testing.T) *httptest.Server {
	f.t = t
	if f.chunk == 0 {
		f.chunk = 1000
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.URL.Path == "/graph/me":
			fmt.Fprint(w, `{"id":"user1"}`)
		case r.URL.Path == "/graph/me/accounts":
			if r.URL.Query().Get("after") == "" {
				fmt.Fprintf(w, `{"data":[{"id":"p1","name":"Page One","access_token":"pt1","tasks":["ANALYZE","CREATE_CONTENT"]},
					{"id":"p2","name":"Read Only","access_token":"pt2","tasks":["ANALYZE"]}],"paging":{"next":"http://%s/graph/me/accounts?after=c1"}}`, r.Host)
				return
			}
			fmt.Fprint(w, `{"data":[{"id":"p3","name":"Page Three","access_token":"pt3","tasks":["CREATE_CONTENT"]}]}`)
		case r.URL.Path == "/video/p1/videos":
			if r.Header.Get("Authorization") != "OAuth pt1" {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":{"code":190,"message":"bad token"}}`)
				return
			}
			ct := r.Header.Get("Content-Type")
			if strings.HasPrefix(ct, "multipart/") {
				r.ParseMultipartForm(1 << 20)
				off, _ := strconv.ParseInt(r.FormValue("start_offset"), 10, 64)
				file, _, err := r.FormFile("video_file_chunk")
				if err != nil || off != int64(f.received.Len()) || r.FormValue("upload_session_id") != "sess1" {
					t.Errorf("bad transfer: off=%d have=%d err=%v", off, f.received.Len(), err)
					w.WriteHeader(400)
					return
				}
				data, _ := io.ReadAll(file)
				f.received.Write(data)
				f.offsets(w)
				return
			}
			r.ParseForm()
			switch r.PostForm.Get("upload_phase") {
			case "start":
				f.total, _ = strconv.ParseInt(r.PostForm.Get("file_size"), 10, 64)
				f.received.Reset()
				f.videoKnown = true
				f.offsets(w)
			case "finish":
				if f.failFinish > 0 {
					f.failFinish--
					w.WriteHeader(500)
					fmt.Fprint(w, `{"error":{"code":2,"message":"temporary","is_transient":true}}`)
					return
				}
				f.finishes++
				f.published = r.PostForm.Get("published")
				if f.loseFinish {
					hj, _ := w.(http.Hijacker)
					c, _, _ := hj.Hijack()
					c.Close()
					return
				}
				fmt.Fprint(w, `{"success":true}`)
			}
		case r.URL.Path == "/graph/vid1":
			if !f.videoKnown {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":{"code":100,"message":"Unsupported get request"}}`)
				return
			}
			upload, publish, published := "in_progress", "not_started", "false"
			if f.total > 0 && int64(f.received.Len()) == f.total {
				upload = "complete"
			}
			if f.finishes > 0 {
				publish, published = "complete", f.published
			}
			fmt.Fprintf(w, `{"id":"vid1","published":%s,"status":{"video_status":"processing","uploading_phase":{"status":%q},"publishing_phase":{"status":%q}}}`, published, upload, publish)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeGraph) offsets(w http.ResponseWriter) {
	start := int64(f.received.Len())
	end := min(start+f.chunk, f.total)
	fmt.Fprintf(w, `{"upload_session_id":"sess1","video_id":"vid1","start_offset":"%d","end_offset":"%d"}`, start, end)
}

func testProvider(srv *httptest.Server) *Provider {
	c := meta.NewClient(meta.Config{AppID: "a", AppSecret: "s", LoginConfigID: "c", GraphVersion: "v26.0", AppMode: "development"})
	c.GraphBase, c.VideoBase = srv.URL+"/graph", srv.URL+"/video"
	return New(c)
}

type recorder struct {
	phase  string
	resume json.RawMessage
}

func (r *recorder) progress(u provider.ProgressUpdate) error {
	if u.Phase != "" {
		r.phase = u.Phase
	}
	if u.Resume != nil {
		r.resume = u.Resume
	}
	return nil
}

func req(data []byte, privacy string, rec *recorder) provider.UploadRequest {
	return provider.UploadRequest{AccountExternalID: "p1", Token: provider.Token{AccessToken: "pt1"}, File: bytes.NewReader(data),
		FileSize: int64(len(data)), Container: provider.FormatMP4, Meta: provider.VideoMeta{Title: "T", Description: "D", Privacy: privacy},
		Phase: rec.phase, Resume: rec.resume}
}

func TestIdentitiesFilterAndPaging(t *testing.T) {
	f := &fakeGraph{}
	p := testProvider(f.server(t))
	sub, ids, err := p.Identities(context.Background(), provider.Token{AccessToken: "user"})
	if err != nil || sub != "user1" || len(ids) != 2 || ids[0].ExternalID != "p1" || ids[1].ExternalID != "p3" {
		t.Fatalf("%q %+v %v", sub, ids, err)
	}
	if ids[0].AccountToken == nil || ids[0].AccountToken.AccessToken != "pt1" || !ids[0].AccountToken.Expiry.IsZero() {
		t.Fatalf("page token %+v", ids[0].AccountToken)
	}
	caps := p.Capabilities(nil)
	if len(caps.Restrictions) != 1 || caps.Restrictions[0] != provider.RestrictionVisibleToAppRolesOnly {
		t.Fatalf("restrictions %v", caps.Restrictions)
	}
}

func TestUploadPublic(t *testing.T) {
	f := &fakeGraph{}
	p := testProvider(f.server(t))
	data := bytes.Repeat([]byte("abcdefgh"), 400) // 3200 bytes, four transfers
	rec := &recorder{}
	res, err := p.Upload(context.Background(), req(data, provider.PrivacyPublic, rec), rec.progress)
	if err != nil || res.RemoteID != "vid1" || res.Outcome != provider.OutcomePublished || res.URL != "https://www.facebook.com/p1/videos/vid1" {
		t.Fatalf("%+v %v", res, err)
	}
	if !bytes.Equal(f.received.Bytes(), data) || f.finishes != 1 || f.published != "true" {
		t.Fatalf("received=%d finishes=%d published=%s", f.received.Len(), f.finishes, f.published)
	}
}

func TestUploadPrivateIsUnpublished(t *testing.T) {
	f := &fakeGraph{}
	p := testProvider(f.server(t))
	rec := &recorder{}
	res, err := p.Upload(context.Background(), req([]byte("small"), provider.PrivacyPrivate, rec), rec.progress)
	if err != nil || f.published != "false" || res.Outcome != provider.OutcomeUnpublished {
		t.Fatalf("%+v %v published=%s", res, err, f.published)
	}
}

func TestLostFinishReconciles(t *testing.T) {
	f := &fakeGraph{loseFinish: true}
	p := testProvider(f.server(t))
	rec := &recorder{}
	res, err := p.Upload(context.Background(), req([]byte("video bytes"), provider.PrivacyPublic, rec), rec.progress)
	if err != nil || res.RemoteID != "vid1" || f.finishes != 1 {
		t.Fatalf("%+v %v finishes=%d", res, err, f.finishes)
	}
	// A restart in the committing phase reconciles without calling finish again.
	res, err = p.Upload(context.Background(), req([]byte("video bytes"), provider.PrivacyPublic, rec), rec.progress)
	if err != nil || res.RemoteID != "vid1" || f.finishes != 1 {
		t.Fatalf("restart: %+v %v finishes=%d", res, err, f.finishes)
	}
}

func TestUnknownVideoIsOutcomeUnknown(t *testing.T) {
	f := &fakeGraph{}
	p := testProvider(f.server(t))
	rec := &recorder{phase: provider.PhaseCommitting, resume: json.RawMessage(`{"upload_session_id":"sess1","video_id":"vid1"}`)}
	if _, err := p.Upload(context.Background(), req([]byte("x"), provider.PrivacyPublic, rec), rec.progress); !errors.Is(err, provider.ErrOutcomeUnknown) {
		t.Fatalf("err %v", err)
	}
	if f.finishes != 0 {
		t.Fatal("finish called while reconciling")
	}
}

func TestPageTokenRejectedNeedsReauth(t *testing.T) {
	f := &fakeGraph{}
	p := testProvider(f.server(t))
	rec := &recorder{}
	r := req([]byte("x"), provider.PrivacyPublic, rec)
	r.Token.AccessToken = "revoked"
	if _, err := p.Upload(context.Background(), r, rec.progress); !errors.Is(err, provider.ErrReauthRequired) {
		t.Fatalf("err %v", err)
	}
}

func TestCrashBeforeFinishFinishesOnResume(t *testing.T) {
	f := &fakeGraph{}
	p := testProvider(f.server(t))
	data := []byte("video bytes")
	rec := &recorder{}
	crash := func(u provider.ProgressUpdate) error {
		rec.progress(u)
		if u.Phase == provider.PhaseCommitting {
			return errors.New("daemon stopped before finish")
		}
		return nil
	}
	if _, err := p.Upload(context.Background(), req(data, provider.PrivacyPublic, rec), crash); err == nil || f.finishes != 0 {
		t.Fatalf("expected crash before finish: %v finishes=%d", err, f.finishes)
	}
	rec.phase = provider.PhaseCommitting // the runner persisted committing
	res, err := p.Upload(context.Background(), req(data, provider.PrivacyPublic, rec), rec.progress)
	if err != nil || res.Outcome != provider.OutcomePublished || f.finishes != 1 {
		t.Fatalf("resume: %+v %v finishes=%d", res, err, f.finishes)
	}
}

func TestUploadCompleteAloneIsNotSuccess(t *testing.T) {
	f := &fakeGraph{failFinish: 1}
	p := testProvider(f.server(t))
	rec := &recorder{}
	_, err := p.Upload(context.Background(), req([]byte("video bytes"), provider.PrivacyPublic, rec), rec.progress)
	if !errors.Is(err, provider.ErrOutcomeUnknown) || f.finishes != 0 {
		t.Fatalf("transient finish failure must not report success: %v finishes=%d", err, f.finishes)
	}
}
