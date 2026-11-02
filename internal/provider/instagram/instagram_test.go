package instagram

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
	"time"

	"github.com/nhatnam1507/social-mcp/internal/enginetest"
	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/provider/meta"
	"github.com/nhatnam1507/social-mcp/internal/service"
)

type fakeIG struct {
	mu           sync.Mutex
	t            *testing.T
	total        int64
	received     bytes.Buffer
	containers   int
	publishes    int
	status       string // container status after upload
	quotaUsed    int
	losePublish  bool
	failUploadAt int64 // stop accepting after this many bytes (simulate interruption)
	lastCaption  string
}

func (f *fakeIG) server(t *testing.T) *httptest.Server {
	f.t = t
	if f.status == "" {
		f.status = "FINISHED"
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "OAuth user-token" {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"code":190,"message":"bad token"}}`)
			return
		}
		switch {
		case r.URL.Path == "/graph/me":
			fmt.Fprint(w, `{"id":"user1"}`)
		case r.URL.Path == "/graph/me/accounts":
			fmt.Fprint(w, `{"data":[{"id":"p1","name":"Page","instagram_business_account":{"id":"ig1","username":"brand"}},{"id":"p2","name":"No IG"}]}`)
		case r.URL.Path == "/graph/ig1/content_publishing_limit":
			fmt.Fprintf(w, `{"data":[{"quota_usage":%d,"config":{"quota_total":50,"quota_duration":86400}}]}`, f.quotaUsed)
		case r.URL.Path == "/graph/ig1/media":
			r.ParseForm()
			if r.PostForm.Get("media_type") != "REELS" || r.PostForm.Get("upload_type") != "resumable" {
				t.Errorf("container form %v", r.PostForm)
			}
			f.lastCaption = r.PostForm.Get("caption")
			f.containers++
			f.received.Reset()
			fmt.Fprintf(w, `{"id":"c%d","uri":"https://rupload.facebook.com/ig-api-upload/v26.0/c%d"}`, f.containers, f.containers)
		case strings.HasPrefix(r.URL.Path, "/rupload/"):
			off, _ := strconv.ParseInt(r.Header.Get("offset"), 10, 64)
			f.total, _ = strconv.ParseInt(r.Header.Get("file_size"), 10, 64)
			if off != int64(f.received.Len()) {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"debug_info":{"retriable":false,"type":"BadOffset","message":"offset mismatch"}}`)
				return
			}
			data, _ := io.ReadAll(r.Body)
			if f.failUploadAt > 0 && int64(f.received.Len()+len(data)) > f.failUploadAt {
				f.received.Write(data[:f.failUploadAt-int64(f.received.Len())])
				f.failUploadAt = 0
				w.WriteHeader(500)
				fmt.Fprint(w, `{"debug_info":{"retriable":true,"type":"Interrupted","message":"try again"}}`)
				return
			}
			f.received.Write(data)
			fmt.Fprint(w, `{"success":true,"message":"Upload successful."}`)
		case strings.HasPrefix(r.URL.Path, "/graph/c"):
			st := "IN_PROGRESS"
			if f.total > 0 && int64(f.received.Len()) == f.total {
				st = f.status
			}
			fmt.Fprintf(w, `{"status_code":%q,"video_status":{"uploading_phase":{"status":"in_progress","bytes_transferred":%d}}}`, st, f.received.Len())
		case r.URL.Path == "/graph/ig1/media_publish":
			f.publishes++
			f.status = "PUBLISHED"
			if f.losePublish {
				hj, _ := w.(http.Hijacker)
				c, _, _ := hj.Hijack()
				c.Close()
				return
			}
			fmt.Fprint(w, `{"id":"m1"}`)
		case r.URL.Path == "/graph/m1":
			fmt.Fprint(w, `{"permalink":"https://www.instagram.com/reel/abc/"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testProvider(srv *httptest.Server) *Provider {
	c := meta.NewClient(meta.Config{AppID: "a", AppSecret: "s", LoginConfigID: "c", GraphVersion: "v26.0", AppMode: "live"})
	c.GraphBase, c.RuploadBase = srv.URL+"/graph", srv.URL+"/rupload"
	p := New(c)
	p.PollInterval, p.PollTimeout = time.Millisecond, 200*time.Millisecond
	return p
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

func req(data []byte, rec *recorder) provider.UploadRequest {
	return provider.UploadRequest{AccountExternalID: "ig1", Token: provider.Token{AccessToken: "user-token"}, File: bytes.NewReader(data),
		FileSize: int64(len(data)), Container: provider.FormatMP4,
		Meta:  provider.VideoMeta{Title: "Launch", Description: "New product", Tags: []string{"go", "#mcp"}, Privacy: provider.PrivacyPublic},
		Phase: rec.phase, Resume: rec.resume}
}

func TestIdentities(t *testing.T) {
	f := &fakeIG{}
	p := testProvider(f.server(t))
	sub, ids, err := p.Identities(context.Background(), provider.Token{AccessToken: "user-token"})
	if err != nil || sub != "user1" || len(ids) != 1 || ids[0].ExternalID != "ig1" || ids[0].DisplayName != "@brand" || ids[0].AccountToken != nil || ids[0].Meta["page_id"] != "p1" {
		t.Fatalf("%q %+v %v", sub, ids, err)
	}
}

func TestCaptionLimits(t *testing.T) {
	p := New(meta.NewClient(meta.Config{GraphVersion: "v26.0"}))
	if c := caption(provider.VideoMeta{Title: "T", Description: "D", Tags: []string{"a b", "#c"}}); c != "T\n\nD\n\n#ab #c" {
		t.Fatalf("caption %q", c)
	}
	many := make([]string, 31)
	for i := range many {
		many[i] = fmt.Sprintf("t%d", i)
	}
	if p.ValidateMeta(provider.VideoMeta{Title: "x", Tags: many}) == nil {
		t.Fatal("31 hashtags accepted")
	}
	if p.ValidateMeta(provider.VideoMeta{Title: strings.Repeat("x", 2201)}) == nil {
		t.Fatal("long caption accepted")
	}
}

func TestUploadAndPublish(t *testing.T) {
	f := &fakeIG{}
	p := testProvider(f.server(t))
	data := bytes.Repeat([]byte("reel"), 1000)
	rec := &recorder{}
	res, err := p.Upload(context.Background(), req(data, rec), rec.progress)
	if err != nil || res.RemoteID != "m1" || res.URL != "https://www.instagram.com/reel/abc/" || res.Outcome != provider.OutcomePublished {
		t.Fatalf("%+v %v", res, err)
	}
	if !bytes.Equal(f.received.Bytes(), data) || f.publishes != 1 || f.containers != 1 || f.lastCaption != "Launch\n\nNew product\n\n#go #mcp" {
		t.Fatalf("received=%d publishes=%d containers=%d caption=%q", f.received.Len(), f.publishes, f.containers, f.lastCaption)
	}
}

func TestResumeUsesBytesTransferred(t *testing.T) {
	f := &fakeIG{failUploadAt: 1500}
	p := testProvider(f.server(t))
	data := bytes.Repeat([]byte("x"), 4000)
	rec := &recorder{}
	if _, err := p.Upload(context.Background(), req(data, rec), rec.progress); err == nil || provider.IsPermanent(err) {
		t.Fatalf("interrupted upload should be transient: %v", err)
	}
	res, err := p.Upload(context.Background(), req(data, rec), rec.progress)
	if err != nil || res.RemoteID != "m1" || f.containers != 1 || !bytes.Equal(f.received.Bytes(), data) {
		t.Fatalf("resume: %+v %v containers=%d received=%d", res, err, f.containers, f.received.Len())
	}
}

func TestQuotaExhaustedIsPermanent(t *testing.T) {
	f := &fakeIG{quotaUsed: 50}
	p := testProvider(f.server(t))
	rec := &recorder{}
	if _, err := p.Upload(context.Background(), req([]byte("x"), rec), rec.progress); !provider.IsPermanent(err) || f.containers != 0 {
		t.Fatalf("err %v containers %d", err, f.containers)
	}
}

func TestProcessingErrorIsPermanent(t *testing.T) {
	f := &fakeIG{status: "ERROR"}
	p := testProvider(f.server(t))
	rec := &recorder{}
	if _, err := p.Upload(context.Background(), req([]byte("x"), rec), rec.progress); !provider.IsPermanent(err) || f.publishes != 0 {
		t.Fatalf("err %v publishes %d", err, f.publishes)
	}
}

func TestLostPublishResponseDoesNotRepublish(t *testing.T) {
	f := &fakeIG{losePublish: true}
	p := testProvider(f.server(t))
	rec := &recorder{}
	res, err := p.Upload(context.Background(), req([]byte("reel"), rec), rec.progress)
	if err != nil || res.Outcome != provider.OutcomePublished || f.publishes != 1 {
		t.Fatalf("%+v %v publishes=%d", res, err, f.publishes)
	}
	// Restart in committing phase: container is PUBLISHED, so no second publish.
	res, err = p.Upload(context.Background(), req([]byte("reel"), rec), rec.progress)
	if err != nil || f.publishes != 1 {
		t.Fatalf("restart: %+v %v publishes=%d", res, err, f.publishes)
	}
}

func TestCommittingWithUnknownStateIsOutcomeUnknown(t *testing.T) {
	f := &fakeIG{}
	p := testProvider(f.server(t))
	rec := &recorder{phase: provider.PhaseCommitting, resume: json.RawMessage(`{"container_id":"c9"}`)}
	if _, err := p.Upload(context.Background(), req([]byte("reel"), rec), rec.progress); !errors.Is(err, provider.ErrOutcomeUnknown) || f.publishes != 0 {
		t.Fatalf("err %v publishes %d", err, f.publishes)
	}
}

func TestServiceRejectsPrivateInstagram(t *testing.T) {
	env := enginetest.New(t)
	env.Registry["instagram"] = New(meta.NewClient(meta.Config{GraphVersion: "v26.0", AppMode: "development"}))
	_, acc := env.AddAccount(enginetest.AccountSpec{Platform: "instagram", ExternalID: "ig1"})
	svc := env.Local(nil, nil)
	for _, privacy := range []string{"", provider.PrivacyPrivate, provider.PrivacyUnlisted} {
		_, err := svc.SubmitUpload(service.UploadInput{FilePath: env.VideoFile("v.mp4", 100), AccountIDs: []string{acc.ID}, Title: "T", Privacy: privacy})
		if !service.IsValidation(err) {
			t.Fatalf("privacy %q accepted for Instagram: %v", privacy, err)
		}
	}
	accs, _ := svc.ListAccounts("instagram")
	r := accs[0].Capabilities.Restrictions
	if len(r) != 2 || r[0] != provider.RestrictionNoPrivateOption || r[1] != provider.RestrictionVisibleToAppRolesOnly {
		t.Fatalf("restrictions %v", r)
	}
}

func TestPublishedContainerBeforeCommitIsUnknown(t *testing.T) {
	f := &fakeIG{status: "PUBLISHED"}
	p := testProvider(f.server(t))
	data := []byte("reel")
	f.total, f.containers = int64(len(data)), 1
	f.received.Write(data)
	rec := &recorder{phase: provider.PhaseTransferring, resume: json.RawMessage(`{"container_id":"c1"}`)}
	if _, err := p.Upload(context.Background(), req(data, rec), rec.progress); !errors.Is(err, provider.ErrOutcomeUnknown) || f.publishes != 0 {
		t.Fatalf("err %v publishes %d", err, f.publishes)
	}
}
