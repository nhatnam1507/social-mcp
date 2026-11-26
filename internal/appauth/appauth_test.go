package appauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// withEmbedded sets the build-time variables for one test.
func withEmbedded(t *testing.T, ytID, ytSecret string) {
	t.Helper()
	oldID, oldSecret := youtubeClientID, youtubeClientSecret
	youtubeClientID, youtubeClientSecret = ytID, ytSecret
	t.Cleanup(func() { youtubeClientID, youtubeClientSecret = oldID, oldSecret })
}

func TestResolveSourceBuildHasNoApps(t *testing.T) {
	withEmbedded(t, "", "")
	apps, err := Resolve(env(nil))
	if err != nil || apps.YouTube != nil || apps.TikTok != nil || apps.Meta != nil {
		t.Fatalf("apps %+v err %v", apps, err)
	}
}

func TestResolveEmbedded(t *testing.T) {
	withEmbedded(t, "yt-id", "yt-secret")
	apps, err := Resolve(env(nil))
	if err != nil || apps.YouTube == nil || apps.YouTube.ClientID != "yt-id" || apps.YouTube.Source != SourceEmbedded || apps.YouTube.secret != "yt-secret" {
		t.Fatalf("apps %+v err %v", apps.YouTube, err)
	}
	if apps.TikTok != nil || apps.Meta != nil {
		t.Fatalf("platforms without an embedded app must be unavailable: %+v", apps)
	}
}

func TestResolveEnvOverridesEmbedded(t *testing.T) {
	withEmbedded(t, "yt-id", "yt-secret")
	apps, err := Resolve(env(map[string]string{
		"SOCIAL_MCP_YOUTUBE_CLIENT_ID": "mine", "SOCIAL_MCP_YOUTUBE_CLIENT_SECRET": "my-secret",
	}))
	if err != nil || apps.YouTube.ClientID != "mine" || apps.YouTube.Source != SourceEnv {
		t.Fatalf("apps %+v err %v", apps.YouTube, err)
	}
}

func TestResolvePartialOverrideFails(t *testing.T) {
	withEmbedded(t, "yt-id", "yt-secret")
	_, err := Resolve(env(map[string]string{"SOCIAL_MCP_YOUTUBE_CLIENT_ID": "mine"}))
	if err == nil || !strings.Contains(err.Error(), "SOCIAL_MCP_YOUTUBE_CLIENT_SECRET missing") {
		t.Fatalf("err %v", err)
	}
}

func TestResolveMeta(t *testing.T) {
	base := map[string]string{"SOCIAL_MCP_META_APP_ID": "a", "SOCIAL_MCP_META_APP_SECRET": "s", "SOCIAL_MCP_META_LOGIN_CONFIG_ID": "c"}
	apps, err := Resolve(env(base))
	if err != nil || apps.Meta == nil || apps.Meta.AppMode != AppModeDevelopment || apps.Meta.LoginConfigID != "c" {
		t.Fatalf("meta %+v err %v", apps.Meta, err)
	}
	base["SOCIAL_MCP_META_APP_MODE"] = "prod"
	if _, err := Resolve(env(base)); err == nil {
		t.Fatal("invalid app mode accepted")
	}
	// The optional app mode alone is not an override.
	if apps, err := Resolve(env(map[string]string{"SOCIAL_MCP_META_APP_MODE": "live"})); err != nil || apps.Meta != nil {
		t.Fatalf("meta %+v err %v", apps.Meta, err)
	}
}

func TestSecretNeverPrinted(t *testing.T) {
	a := NewApp(YouTube, "cid", "TOPSECRET", SourceEnv)
	for _, s := range []string{a.String(), fmt.Sprint(a), fmt.Sprintf("%v", a), a.LogValue().String()} {
		if strings.Contains(s, "TOPSECRET") {
			t.Fatalf("secret printed: %s", s)
		}
	}
}

func TestTokenEndpointAddsCredentials(t *testing.T) {
	for _, tc := range []struct{ platform, idParam string }{{YouTube, "client_id"}, {TikTok, "client_key"}, {Meta, "client_id"}} {
		var got url.Values
		var method, rawQuery string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method, rawQuery = r.Method, r.URL.RawQuery
			r.ParseForm()
			got = r.PostForm
			fmt.Fprint(w, `{}`)
		}))
		resp, err := NewApp(tc.platform, "id1", "sec1", SourceEnv).TokenEndpoint(srv.URL+"/token").Do(context.Background(), url.Values{"code": {"c1"}})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if method != http.MethodPost || rawQuery != "" {
			t.Errorf("%s: %s with query %q; credentials must travel in the body", tc.platform, method, rawQuery)
		}
		if got.Get(tc.idParam) != "id1" || got.Get("client_secret") != "sec1" || got.Get("code") != "c1" {
			t.Errorf("%s: form %v", tc.platform, got)
		}
	}
}

func TestAppFor(t *testing.T) {
	for in, want := range map[string]string{"youtube": YouTube, "tiktok": TikTok, "facebook": Meta, "instagram": Meta} {
		if AppFor(in) != want {
			t.Errorf("AppFor(%s) = %s", in, AppFor(in))
		}
	}
}
