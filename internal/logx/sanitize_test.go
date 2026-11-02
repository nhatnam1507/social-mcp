package logx

import (
	"bytes"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"testing"
)

const secret = "S3CRETVALUE"

func TestSanitizeMasksSecrets(t *testing.T) {
	inputs := []string{
		"GET https://graph.facebook.com/oauth/access_token?client_id=1&client_secret=" + secret + "&code=" + secret,
		"fb_exchange_token=" + secret + "&grant_type=fb_exchange_token",
		`body: {"access_token":"` + secret + `","refresh_token":"` + secret + `"}`,
		"Authorization: Bearer " + secret,
		"authorization: OAuth " + secret,
		"code_verifier=" + secret,
	}
	for _, in := range inputs {
		if out := Sanitize(in); strings.Contains(out, secret) {
			t.Errorf("secret leaked: %q -> %q", in, out)
		}
	}
	if Sanitize("plain message") != "plain message" {
		t.Fatal("plain text altered")
	}
}

func TestErrorStripsURLError(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "https://graph.facebook.com/v26.0/oauth/access_token?client_secret=" + secret, Err: errors.New("dial tcp: timeout")}
	out := Error(err)
	if strings.Contains(out, secret) || !strings.Contains(out, "graph.facebook.com") || !strings.Contains(out, "timeout") {
		t.Fatalf("Error() = %q", out)
	}
}

func TestHandlerSanitizes(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewHandler(slog.NewTextHandler(&buf, nil)))
	log.Info("request failed: access_token="+secret, "url", "https://x/?client_secret="+secret, "err", errors.New("Bearer "+secret))
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("log leaked secret: %s", buf.String())
	}
}
