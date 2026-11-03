package meta

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/nhatnam1507/social-mcp/internal/logx"
	"github.com/nhatnam1507/social-mcp/internal/provider"
)

func testClient(srv *httptest.Server) *Client {
	c := NewClient(Config{AppID: "app", AppSecret: "SECRETAPP", LoginConfigID: "cfg1", GraphVersion: "v26.0", AppMode: "development"})
	c.GraphBase, c.VideoBase, c.RuploadBase = srv.URL+"/graph", srv.URL+"/video", srv.URL+"/rupload"
	return c
}

func TestClassify(t *testing.T) {
	cases := []struct {
		status int
		body   string
		check  func(error) bool
	}{
		{400, `{"error":{"code":190,"error_subcode":463,"message":"expired"}}`, func(e error) bool { return errors.Is(e, provider.ErrReauthRequired) }},
		{400, `{"error":{"code":190,"message":"invalid"}}`, func(e error) bool { return errors.Is(e, provider.ErrReauthRequired) }},
		{400, `{"error":{"code":4,"message":"app limit"}}`, func(e error) bool { var r *provider.RateLimitError; return errors.As(e, &r) }},
		{400, `{"error":{"code":32,"message":"page limit"}}`, func(e error) bool { var r *provider.RateLimitError; return errors.As(e, &r) }},
		{400, `{"error":{"code":80001,"message":"page token BUC limit"}}`, func(e error) bool { var r *provider.RateLimitError; return errors.As(e, &r) }},
		{400, `{"error":{"code":80002,"message":"instagram BUC limit"}}`, func(e error) bool { var r *provider.RateLimitError; return errors.As(e, &r) }},
		{500, `{"error":{"code":2,"message":"temporary","is_transient":true}}`, func(e error) bool { return !provider.IsPermanent(e) && !errors.Is(e, provider.ErrReauthRequired) }},
		{403, `{"error":{"code":200,"message":"no permission"}}`, provider.IsPermanent},
		{400, `{"error":{"code":10,"message":"denied"}}`, provider.IsPermanent},
		{400, `{"error":{"code":100,"message":"no such object"}}`, func(e error) bool { return provider.IsPermanent(e) && IsNotFound(e) }},
		{502, `bad gateway`, func(e error) bool { return !provider.IsPermanent(e) }},
	}
	for _, tc := range cases {
		err := classify(&http.Response{StatusCode: tc.status, Status: fmt.Sprint(tc.status), Header: http.Header{}}, []byte(tc.body))
		if !tc.check(err) {
			t.Errorf("%d %s -> %v", tc.status, tc.body, err)
		}
	}
}

func TestAuthURLUsesConfigID(t *testing.T) {
	c := NewClient(Config{AppID: "app", LoginConfigID: "cfg1", GraphVersion: "v26.0"})
	u, _ := url.Parse(c.AuthURL("st", "http://localhost:8765/oauth/callback"))
	q := u.Query()
	if u.Host != "www.facebook.com" || !strings.HasSuffix(u.Path, "/v26.0/dialog/oauth") || q.Get("config_id") != "cfg1" || q.Has("scope") || q.Get("response_type") != "code" {
		t.Fatalf("auth url %s", u)
	}
}

func TestExchangeSwapsForLongLivedToken(t *testing.T) {
	var calls []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/graph/oauth/access_token":
			q := r.URL.Query()
			calls = append(calls, q)
			if q.Get("grant_type") == "fb_exchange_token" {
				if q.Get("fb_exchange_token") != "short" {
					t.Errorf("long-lived swap got %q", q.Get("fb_exchange_token"))
				}
				fmt.Fprint(w, `{"access_token":"long","token_type":"bearer","expires_in":5183944}`)
				return
			}
			fmt.Fprint(w, `{"access_token":"short","expires_in":3600}`)
		case "/graph/me":
			if r.Header.Get("Authorization") != "OAuth long" {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":{"code":190,"message":"bad"}}`)
				return
			}
			fmt.Fprint(w, `{"id":"u123"}`)
		}
	}))
	defer srv.Close()
	c := testClient(srv)
	tok, err := c.Exchange(context.Background(), "code1", "http://localhost:1/oauth/callback")
	if err != nil || tok.AccessToken != "long" || tok.Expiry.IsZero() || len(calls) != 2 || calls[0].Get("code") != "code1" {
		t.Fatalf("exchange %+v %v %v", tok, err, calls)
	}
	if id, err := c.Me(context.Background(), tok); err != nil || id != "u123" {
		t.Fatalf("me %q %v", id, err)
	}
}

func TestTransportErrorDoesNotLeakSecret(t *testing.T) {
	c := NewClient(Config{AppID: "app", AppSecret: "SECRETAPP", GraphVersion: "v26.0"})
	c.GraphBase = "http://127.0.0.1:1/graph" // nothing listens here
	_, err := c.Exchange(context.Background(), "code1", "http://localhost:1/cb")
	if err == nil || strings.Contains(logx.Error(err), "SECRETAPP") {
		t.Fatalf("err %v", logx.Error(err))
	}
}

func TestGetAllStaysOnGraphHost(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after") == "" {
			fmt.Fprintf(w, `{"data":[{"id":"1"}],"paging":{"next":"%s/graph/me/accounts?after=x"}}`, srv.URL)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"2"}],"paging":{"next":"https://evil.example/steal"}}`)
	}))
	defer srv.Close()
	c := testClient(srv)
	items, err := GetAll[struct {
		ID string `json:"id"`
	}](context.Background(), c, "/me/accounts", url.Values{"fields": {"id"}}, "tok")
	if err != nil || len(items) != 2 {
		t.Fatalf("items %v %v", items, err)
	}
}
